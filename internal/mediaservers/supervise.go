package mediaservers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	LogQueue  = 256  // lines of a server waiting for the logger; more are dropped and counted, never waited for
	redacted  = "[redacted]"
)

// Options configure a Group.
type Options struct {
	Logger       *slog.Logger
	Secrets      []string      // values replaced in every line before it is logged or kept, as JSON strings too
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
	quiet    chan struct{} // closed when restarts are disabled: shutdown began
	monitors sync.WaitGroup
	loggers  sync.WaitGroup   // the goroutines that log: the output of each run and the notes
	notes    chan slog.Record // the supervisor's own messages: sent without waiting, logged by one goroutine

	mu        sync.Mutex
	noRestart bool // no process is started any more; quiet is closed
	stopped   bool // Stop was called
	children  []*child
}

type launch struct {
	c    *child
	cmd  *exec.Cmd
	done chan error
}

// errShuttingDown is the start of a process refused because shutdown began.
var errShuttingDown = errors.New("shutting down")

type child struct {
	Child
	mu   sync.Mutex
	cmd  *exec.Cmd // the running process, nil between runs
	tail []string
}

// Start starts every child and waits until each answers its readiness check. When one cannot be started or does
// not become ready within the timeout, the children already started are stopped and the error carries the last
// lines of the one that failed. When ctx ends, restarts are disabled at once (shutdown began); the children that
// run keep running until Stop.
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
	opts.Secrets = withEncoded(opts.Secrets)
	g := &Group{opts: opts, launches: make(chan launch), launcher: make(chan struct{}), quiet: make(chan struct{}),
		notes: make(chan slog.Record, LogQueue)}
	go g.launch()
	g.loggers.Add(1)
	logCtx := context.WithoutCancel(ctx) // the notes of the shutdown come after ctx ended
	go func() {
		defer g.loggers.Done()
		h := g.opts.Logger.Handler()
		for r := range g.notes {
			if h.Enabled(logCtx, r.Level) {
				_ = h.Handle(logCtx, r)
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			g.disableRestarts()
		case <-g.quiet:
		}
	}()
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
// waited for. A process is started and registered under g.mu, so it is either refused (restarts disabled) or
// registered before Stop can signal the children: a launch never wins against a stop.
func (g *Group) launch() {
	defer close(g.launcher)
	runtime.LockOSThread()
	for l := range g.launches {
		g.mu.Lock()
		err := errShuttingDown
		if !g.noRestart {
			if err = l.cmd.Start(); err == nil {
				l.c.mu.Lock()
				l.c.cmd = l.cmd
				l.c.mu.Unlock()
			}
		}
		g.mu.Unlock()
		l.done <- err
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
	case g.launches <- launch{c: c, cmd: cmd, done: done}:
		err = <-done
	case <-g.quiet:
		err = errShuttingDown
	}
	_ = w.Close()
	if err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("start %s (%s): %w", c.Name, c.Path, err)
	}
	// the process is registered (launch); its reader and its waiter exist before anything is logged about it
	lines := make(chan logLine, LogQueue)
	g.loggers.Add(1)
	go g.deliver(c, lines)
	output := make(chan struct{})
	go func() {
		defer close(output)
		defer close(lines)
		g.read(c, r, lines)
	}()
	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		c.mu.Lock()
		c.cmd = nil
		c.mu.Unlock()
		select { // its last lines, unless something it started still holds the pipe
		case <-output:
		case <-time.After(2 * time.Second):
			_ = r.Close() // ends the reader
			<-output
		}
		_ = r.Close()
		exited <- err
	}()
	g.note(slog.LevelInfo, "media server started", "component", c.Name, "pid", cmd.Process.Pid)
	return exited, nil
}

// supervise starts c again each time it exits, after the backoff delay, until restarts are disabled.
func (g *Group) supervise(c *child, exited <-chan error) {
	defer g.monitors.Done()
	var delay time.Duration
	started := time.Now()
	for {
		err := <-exited
		if g.restartsDisabled() {
			return
		}
		delay = g.opts.Backoff.Next(delay, time.Since(started))
		g.note(slog.LevelWarn, "media server exited: starting it again", "component", c.Name, "error", err, "delay", delay.String())
		select {
		case <-g.quiet:
			return
		case <-time.After(delay):
		}
		started = time.Now()
		if exited, err = g.run(c); err != nil {
			if g.restartsDisabled() {
				return
			}
			g.note(slog.LevelError, "media server did not start", "component", c.Name, "error", err)
			failed := make(chan error, 1)
			failed <- err
			exited = failed
		}
	}
}

func (g *Group) restartsDisabled() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.noRestart
}

// disableRestarts makes the launcher refuse every further start: shutdown began.
func (g *Group) disableRestarts() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.noRestart {
		g.noRestart = true
		close(g.quiet)
	}
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
// there after the stop timeout, and every child waited for. The launcher's thread ends last. Nothing of it waits on
// the logger: the lines still on their way are given at most the stop timeout more, then left behind.
func (g *Group) Stop() {
	g.mu.Lock()
	first := !g.stopped
	g.stopped = true
	g.mu.Unlock()
	if !first {
		<-g.launcher
		return
	}
	g.disableRestarts() // from here on every process is registered, or never started
	g.signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		g.monitors.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(g.opts.StopTimeout):
		g.signal(syscall.SIGKILL)
		g.note(slog.LevelWarn, "media servers did not stop in time: killed them", "timeout", g.opts.StopTimeout.String())
		<-done
	}
	close(g.launches)
	<-g.launcher
	g.note(slog.LevelInfo, "media servers stopped")
	close(g.notes) // nothing notes any more: the monitors and the launcher returned
	logged := make(chan struct{})
	go func() {
		g.loggers.Wait()
		close(logged)
	}()
	select {
	case <-logged:
	case <-time.After(g.opts.StopTimeout):
	}
}

// note is a message of the supervisor, stamped now and handed to the logger without waiting: dropped when the
// queue is full.
func (g *Group) note(level slog.Level, msg string, args ...any) {
	r := slog.NewRecord(time.Now(), level, msg, 0)
	r.Add(args...)
	select {
	case g.notes <- r:
	default:
	}
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

// logLine is a line on its way to the logger, with the count of lines dropped before it.
type logLine struct {
	text    string
	dropped int
}

// read drains r line by line: each line, with the secrets replaced and then cut at MaxLine, goes to the tail and to
// lines without ever waiting (a full queue drops the line and counts it). A secret is replaced before the line is
// cut: the reader holds MaxLine bytes plus the longest secret, so a secret that starts before the cut is seen whole.
func (g *Group) read(c *child, r io.Reader, lines chan<- logLine) {
	longest := 0
	for _, s := range g.opts.Secrets {
		longest = max(longest, len(s))
	}
	br := bufio.NewReaderSize(r, MaxLine+longest)
	dropped := 0
	emit := func(raw []byte, cut bool) {
		select {
		case lines <- logLine{text: g.line(c, raw, cut), dropped: dropped}:
			dropped = 0
		default:
			dropped++
		}
	}
	for {
		line, err := br.ReadSlice('\n')
		switch {
		case err == nil:
			emit(line[:len(line)-1], false)
		case errors.Is(err, bufio.ErrBufferFull):
			emit(line, true)
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = br.ReadSlice('\n')
			}
			if err != nil {
				return
			}
		default:
			if len(line) > 0 {
				emit(line, false)
			}
			return
		}
	}
}

// line redacts raw, cuts it at MaxLine and keeps it in the tail.
func (g *Group) line(c *child, raw []byte, cut bool) string {
	raw = bytes.TrimSuffix(raw, []byte("\r"))
	text := redactPrefix(string(raw), g.opts.Secrets, MaxLine)
	if cut || len(raw) > MaxLine {
		text += " [line cut]"
	}
	text = strings.ToValidUTF8(text, "?")
	c.mu.Lock()
	c.tail = append(c.tail, text)
	if len(c.tail) > TailLines {
		c.tail = c.tail[len(c.tail)-TailLines:]
	}
	c.mu.Unlock()
	return text
}

// deliver logs the lines of one run; it alone waits on the logger.
func (g *Group) deliver(c *child, lines <-chan logLine) {
	defer g.loggers.Done()
	for l := range lines {
		if l.dropped > 0 {
			g.opts.Logger.Warn("lines of output dropped: the log does not keep up", "component", c.Name, "dropped", l.dropped)
		}
		g.opts.Logger.Info(l.text, "component", c.Name)
	}
}

// withEncoded is secrets with the form each takes inside a JSON string, as LIVEKIT_KEYS hands it to LiveKit (the
// encoding escapes &, <, >, " and \), where that form differs.
func withEncoded(secrets []string) []string {
	out := append([]string(nil), secrets...)
	for _, s := range secrets {
		b, err := json.Marshal(s)
		if err == nil && string(b[1:len(b)-1]) != s {
			out = append(out, string(b[1:len(b)-1]))
		}
	}
	return out
}

func (g *Group) redact(s string) string { return redactPrefix(s, g.opts.Secrets, len(s)) }

// redactPrefix returns the first limit bytes of s with every byte that belongs to an occurrence of a secret
// replaced: each run of such bytes, overlapping occurrences included, becomes one marker. An occurrence that starts
// before limit is replaced whole, even when it ends after it.
func redactPrefix(s string, secrets []string, limit int) string {
	var covered []bool
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for i := 0; i < len(s); {
			j := strings.Index(s[i:], secret)
			if j < 0 {
				break
			}
			if covered == nil {
				covered = make([]bool, len(s))
			}
			for k := i + j; k < i+j+len(secret); k++ {
				covered[k] = true
			}
			i += j + 1
		}
	}
	limit = min(limit, len(s))
	if covered == nil {
		return s[:limit]
	}
	var b strings.Builder
	for i := 0; i < limit; i++ {
		if !covered[i] {
			b.WriteByte(s[i])
			continue
		}
		b.WriteString(redacted)
		for i+1 < len(s) && covered[i+1] {
			i++
		}
	}
	return b.String()
}

func (c *child) lastLines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.tail...)
}
