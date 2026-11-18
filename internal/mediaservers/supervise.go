package mediaservers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Child is a media server to supervise.
type Child struct {
	Name  string // the component of its log lines: livekit or mediamtx
	Path  string
	Args  []string
	Env   []string                    // its whole environment
	Ready func(context.Context) error // the readiness check /readyz uses
}

// Backoff is the delay before a server that exited is started again: Min after its first exit or after it ran for
// ResetAfter, else twice the previous delay, at most Max.
type Backoff struct {
	Min, Max, ResetAfter time.Duration
}

// DefaultBackoff is 1 s doubling to 30 s, back to 1 s after a minute of running.
func DefaultBackoff() Backoff {
	return Backoff{Min: time.Second, Max: 30 * time.Second, ResetAfter: time.Minute}
}

// Next is the delay after a run of ran that followed a delay of prev (0: none yet).
func (b Backoff) Next(prev, ran time.Duration) time.Duration {
	if prev == 0 || ran >= b.ResetAfter {
		return b.Min
	}
	return min(prev*2, b.Max)
}

// Limits of the output kept of a server.
const (
	MaxLine   = 4096 // bytes of a line logged; the rest of a longer line is dropped
	TailLines = 20   // lines kept for a failure message
	redacted  = "[redacted]"
)

// Options configure a Group.
type Options struct {
	Logger       *slog.Logger
	Secrets      []string      // values replaced in every line before it is logged or kept
	ReadyTimeout time.Duration // zero: 30 s
	StopTimeout  time.Duration // SIGTERM to SIGKILL; zero: 10 s
	Backoff      Backoff       // zero: DefaultBackoff()
}

// Group supervises the media servers. Every start and restart happens on one launcher goroutine locked to its OS
// thread: the parent-death signal of a child belongs to the thread that created it, so that thread stays alive
// until every child has been stopped and waited for.
type Group struct {
	opts     Options
	launches chan launch
	launcher chan struct{} // closed when the launcher returned
	stop     chan struct{} // closed when shutdown begins
	monitors sync.WaitGroup

	mu       sync.Mutex
	stopping bool
	children []*child
}

type launch struct {
	cmd  *exec.Cmd
	done chan error
}

type child struct {
	Child
	mu   sync.Mutex
	cmd  *exec.Cmd // the running process, nil between runs
	tail []string
}

// Start starts every child and waits until each answers its readiness check. When one cannot be started or does
// not become ready within the timeout, the children already started are stopped and the error carries the last
// lines of the one that failed.
func Start(ctx context.Context, opts Options, children []Child) (*Group, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.ReadyTimeout == 0 {
		opts.ReadyTimeout = 30 * time.Second
	}
	if opts.StopTimeout == 0 {
		opts.StopTimeout = 10 * time.Second
	}
	if opts.Backoff == (Backoff{}) {
		opts.Backoff = DefaultBackoff()
	}
	g := &Group{opts: opts, launches: make(chan launch), launcher: make(chan struct{}), stop: make(chan struct{})}
	go g.launch()
	for _, c := range children {
		ch := &child{Child: c}
		g.mu.Lock()
		g.children = append(g.children, ch)
		g.mu.Unlock()
		exited, err := g.run(ch)
		if err != nil {
			g.Stop()
			return nil, err
		}
		g.monitors.Add(1)
		go g.supervise(ch, exited)
	}
	if err := g.ready(ctx); err != nil {
		g.Stop()
		return nil, err
	}
	return g, nil
}

// launch is the launcher goroutine. It never unlocks its thread: the thread ends with it, after every child was
// waited for.
func (g *Group) launch() {
	defer close(g.launcher)
	runtime.LockOSThread()
	for l := range g.launches {
		l.done <- l.cmd.Start()
	}
}

// run starts one process of c on the launcher, its output read line by line. The channel receives the result of
// waiting for it.
func (g *Group) run(c *child) (<-chan error, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", c.Name, err)
	}
	cmd := exec.Command(c.Path, c.Args...) //nolint:gosec,noctx // the chosen media server; stopped by its process group
	cmd.Env = c.Env
	cmd.Stdout, cmd.Stderr = w, w
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	done := make(chan error, 1)
	select {
	case g.launches <- launch{cmd: cmd, done: done}:
		err = <-done
	case <-g.stop:
		err = errors.New("shutting down")
	}
	_ = w.Close()
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("start %s (%s): %w", c.Name, c.Path, err)
	}
	g.opts.Logger.Info("media server started", "component", c.Name, "pid", cmd.Process.Pid)
	output := make(chan struct{})
	go func() {
		defer close(output)
		defer func() { _ = r.Close() }()
		g.read(c, r)
	}()
	c.mu.Lock()
	c.cmd = cmd
	c.mu.Unlock()
	if g.isStopping() { // Stop signalled the children before this process was there
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		c.mu.Lock()
		c.cmd = nil
		c.mu.Unlock()
		select { // its last lines, unless something it started still holds the pipe
		case <-output:
		case <-time.After(2 * time.Second):
		}
		exited <- err
	}()
	return exited, nil
}

// supervise starts c again each time it exits, after the backoff delay, until shutdown begins.
func (g *Group) supervise(c *child, exited <-chan error) {
	defer g.monitors.Done()
	var delay time.Duration
	started := time.Now()
	for {
		err := <-exited
		if g.isStopping() {
			return
		}
		delay = g.opts.Backoff.Next(delay, time.Since(started))
		g.opts.Logger.Warn("media server exited: starting it again", "component", c.Name, "error", err, "delay", delay.String())
		select {
		case <-g.stop:
			return
		case <-time.After(delay):
		}
		started = time.Now()
		if exited, err = g.run(c); err != nil {
			if g.isStopping() {
				return
			}
			g.opts.Logger.Error("media server did not start", "component", c.Name, "error", err)
			failed := make(chan error, 1)
			failed <- err
			exited = failed
		}
	}
}

func (g *Group) isStopping() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopping
}

// ready waits until every child answers its readiness check.
func (g *Group) ready(ctx context.Context) error {
	deadline := time.Now().Add(g.opts.ReadyTimeout)
	for _, c := range g.children {
		if c.Ready == nil {
			continue
		}
		for {
			cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := c.Ready(cctx)
			cancel()
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s did not answer within %s (%s); its last lines:\n%s",
					c.Name, g.opts.ReadyTimeout, g.redact(err.Error()), strings.Join(c.lastLines(), "\n"))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return nil
}

// Stop ends supervision: no more restarts, SIGTERM to the process group of each child, SIGKILL to those still
// there after the stop timeout, and every child waited for. The launcher's thread ends last.
func (g *Group) Stop() {
	g.mu.Lock()
	first := !g.stopping
	if first {
		g.stopping = true
		close(g.stop)
	}
	g.mu.Unlock()
	if !first {
		<-g.launcher
		return
	}
	g.signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		g.monitors.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(g.opts.StopTimeout):
		g.opts.Logger.Warn("media servers did not stop in time: killing them", "timeout", g.opts.StopTimeout.String())
		g.signal(syscall.SIGKILL)
		<-done
	}
	close(g.launches)
	<-g.launcher
	g.opts.Logger.Info("media servers stopped")
}

// signal sends sig to the process group of every running child.
func (g *Group) signal(sig syscall.Signal) {
	g.mu.Lock()
	children := g.children
	g.mu.Unlock()
	for _, c := range children {
		c.mu.Lock()
		if c.cmd != nil {
			_ = syscall.Kill(-c.cmd.Process.Pid, sig)
		}
		c.mu.Unlock()
	}
}

// read logs each line of r, cut at MaxLine and with the secrets replaced, and keeps the last TailLines.
func (g *Group) read(c *child, r io.Reader) {
	br := bufio.NewReaderSize(r, MaxLine)
	for {
		line, err := br.ReadSlice('\n')
		switch {
		case err == nil:
			g.line(c, string(line[:len(line)-1]))
		case errors.Is(err, bufio.ErrBufferFull):
			g.line(c, string(line)+" [line cut]")
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = br.ReadSlice('\n')
			}
			if err != nil {
				return
			}
		default:
			if len(line) > 0 {
				g.line(c, string(line))
			}
			return
		}
	}
}

func (g *Group) line(c *child, text string) {
	text = g.redact(strings.ToValidUTF8(strings.TrimSuffix(text, "\r"), "?"))
	c.mu.Lock()
	c.tail = append(c.tail, text)
	if len(c.tail) > TailLines {
		c.tail = c.tail[len(c.tail)-TailLines:]
	}
	c.mu.Unlock()
	g.opts.Logger.Info(text, "component", c.Name)
}

func (g *Group) redact(s string) string {
	for _, secret := range g.opts.Secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, redacted)
		}
	}
	return s
}

func (c *child) lastLines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.tail...)
}
