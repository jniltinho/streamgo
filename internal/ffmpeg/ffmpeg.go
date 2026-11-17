// Package ffmpeg supervises the live output's ffmpeg processes (design D4): the transcoder
// that turns the program into H.264 and AAC, and one relay per destination that copies it to
// RTMP. It implements output.Processes.
//
// Each process starts without a shell, with an empty environment, in its own process group and
// with SIGKILL as its parent-death signal, from a goroutine locked to its thread for the
// child's whole life (Linux delivers that signal when the thread that started the child ends).
// It is flowing when the media time of its -progress reports advances, stalled when it does not
// advance for StallAfter, and stopped with SIGINT and then SIGKILL after StopGrace.
package ffmpeg

import (
	"bufio"
	"io"
	"log/slog"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/core/output"
)

// Timing of the supervision (docs/spike-media-path.md, 1.5).
const (
	// StallAfter is how long the output's media time may stand still once it flowed.
	StallAfter = 5 * time.Second
	// FirstFlowWithin is how long a process may take to start flowing (connecting, probing).
	FirstFlowWithin = 15 * time.Second
	// StopGrace is how long a process gets after SIGINT before SIGKILL.
	StopGrace = 5 * time.Second
	// tailLines is how many lines of stderr are kept for the reason a process ended.
	tailLines = 5
)

// Options configure a Supervisor.
type Options struct {
	Path         string // output.ffmpeg_path
	RTSPURL      string // mediamtx.rtsp_url, e.g. rtsp://mediamtx:8554
	User, Secret string // the media server's internal credentials
	Width        int
	Height       int
	VideoKbps    int
	AudioKbps    int
	Logger       *slog.Logger
}

// Supervisor runs and watches ffmpeg processes. The zero value is not usable; call New.
type Supervisor struct {
	o      Options
	mu     sync.Mutex
	procs  map[*proc]struct{}
	closed bool
	wg     sync.WaitGroup
}

var _ output.Processes = (*Supervisor)(nil)

// New returns a supervisor.
func New(o Options) *Supervisor {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return &Supervisor{o: o, procs: map[*proc]struct{}{}}
}

// Size is the program's frame size for output.resolution.
func Size(resolution string) (width, height int) {
	if resolution == "1080p" {
		return 1920, 1080
	}
	return 1280, 720
}

// TranscoderArgs is the transcoder's command line from the spike (docs/spike-media-path.md),
// without the program name.
func TranscoderArgs(in, out string, width, height, videoKbps, audioKbps int) []string {
	w, h := strconv.Itoa(width), strconv.Itoa(height)
	br := strconv.Itoa(videoKbps) + "k"
	return []string{
		"-hide_banner", "-nostats", "-loglevel", "warning", "-progress", "pipe:1",
		"-rtsp_transport", "tcp", "-timeout", "5000000", "-i", in,
		"-map", "0:v:0", "-map", "0:a:0",
		"-vf", "scale=" + w + ":" + h + ":force_original_aspect_ratio=decrease,pad=" + w + ":" + h + ":(ow-iw)/2:(oh-ih)/2,format=yuv420p",
		"-fps_mode", "cfr", "-r", "30",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-profile:v", "high",
		"-g", "60", "-keyint_min", "60", "-sc_threshold", "0",
		"-b:v", br, "-maxrate", br, "-bufsize", strconv.Itoa(2*videoKbps) + "k",
		"-c:a", "aac", "-ar", "48000", "-ac", "2", "-b:a", strconv.Itoa(audioKbps) + "k",
		"-f", "rtsp", "-rtsp_transport", "tcp", out,
	}
}

// RelayArgs is a relay's command line from the spike, without the program name.
func RelayArgs(in, target string) []string {
	return []string{
		"-hide_banner", "-nostats", "-loglevel", "warning", "-progress", "pipe:1",
		"-rtsp_transport", "tcp", "-timeout", "5000000", "-i", in,
		"-map", "0", "-c", "copy", "-f", "flv", "-rw_timeout", "5000000", target,
	}
}

// StartTranscoder starts the transcoder of the broadcast: program/<id> to air/<id>.
func (s *Supervisor) StartTranscoder(broadcastID string, notify output.Notify) output.Stopper {
	args := TranscoderArgs(s.path("program", broadcastID), s.path("air", broadcastID),
		s.o.Width, s.o.Height, s.o.VideoKbps, s.o.AudioKbps)
	return s.start(args, s.redact, notify)
}

// StartRelay starts a relay of the broadcast's air/<id> to the target.
func (s *Supervisor) StartRelay(broadcastID string, t destination.Target, notify output.Notify) output.Stopper {
	redact := func(text string) string { return s.redact(t.Redact(text)) }
	return s.start(RelayArgs(s.path("air", broadcastID), t.URL), redact, notify)
}

// StopAll stops every process at once (SIGINT, SIGKILL after StopGrace) and waits for them;
// processes started afterwards end at once.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	s.closed = true
	for p := range s.procs {
		p.Stop()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// path is the media server's RTSP URL of a path, with the internal credentials.
func (s *Supervisor) path(prefix, id string) string {
	u, err := url.Parse(strings.TrimSuffix(s.o.RTSPURL, "/") + "/" + prefix + "/" + id)
	if err != nil {
		return ""
	}
	if s.o.User != "" {
		u.User = url.UserPassword(s.o.User, s.o.Secret)
	}
	return u.String()
}

// redact removes the internal credentials from a line of ffmpeg's output.
func (s *Supervisor) redact(text string) string {
	if s.o.Secret == "" {
		return text
	}
	text = strings.ReplaceAll(text, url.UserPassword(s.o.User, s.o.Secret).String(), "***")
	return strings.ReplaceAll(text, s.o.Secret, "***")
}

type proc struct {
	once sync.Once
	stop chan struct{}
}

// Stop asks the process to stop; it does not wait.
func (p *proc) Stop() { p.once.Do(func() { close(p.stop) }) }

func (s *Supervisor) start(args []string, redact func(string) string, notify output.Notify) output.Stopper {
	p := &proc{stop: make(chan struct{})}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		go notify(false, "the server is stopping")
		return p
	}
	s.procs[p] = struct{}{}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.procs, p)
			s.mu.Unlock()
		}()
		notify(false, s.run(p, args, redact, notify))
	}()
	return p
}

// run starts the process, watches it until it ends and returns why it ended. It keeps its
// goroutine on one thread for the child's life: the parent-death signal is tied to that thread.
func (s *Supervisor) run(p *proc, args []string, redact func(string) string, notify output.Notify) string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cmd := exec.Command(s.o.Path, args...) //nolint:gosec,noctx // the operator's ffmpeg, no shell; stopped by signals, not a context
	cmd.Env = []string{}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "ffmpeg could not start"
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "ffmpeg could not start"
	}
	if err := cmd.Start(); err != nil {
		s.o.Logger.Error("start ffmpeg", "error", redact(err.Error()))
		return "ffmpeg could not start"
	}
	pgid := cmd.Process.Pid
	signal := func(sig syscall.Signal) { _ = syscall.Kill(-pgid, sig) }

	progress := make(chan int64, 16)
	tail := &tail{}
	exited := make(chan error, 1)
	go func() {
		var wg sync.WaitGroup
		wg.Go(func() { readProgress(stdout, progress) })
		wg.Go(func() { tail.read(stderr, redact) })
		wg.Wait()
		exited <- cmd.Wait()
	}()

	var last int64
	flowing, stalled := false, false
	watchdog := time.NewTimer(FirstFlowWithin)
	defer watchdog.Stop()
	var kill <-chan time.Time
	stop := p.stop
	for {
		select {
		case t := <-progress:
			if t <= last {
				continue
			}
			last = t
			watchdog.Reset(StallAfter)
			if !flowing {
				flowing = true
				notify(true, "")
			}
		case <-watchdog.C:
			stalled = true
			signal(syscall.SIGKILL)
		case <-stop:
			stop = nil
			signal(syscall.SIGINT)
			kill = time.After(StopGrace)
		case <-kill:
			signal(syscall.SIGKILL)
		case err := <-exited:
			signal(syscall.SIGKILL) // anything left in the group
			switch {
			case stalled && flowing:
				return "the stream stopped advancing for " + StallAfter.String()
			case stalled:
				return "no media flowed within " + FirstFlowWithin.String()
			case tail.last() != "":
				return tail.last()
			case err != nil:
				return "ffmpeg ended: " + err.Error()
			default:
				return "ffmpeg ended"
			}
		}
	}
}

// readProgress sends the output's media time (microseconds) of each -progress block.
func readProgress(r io.Reader, out chan<- int64) {
	sc := bufio.NewScanner(r)
	var t int64 = -1
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), "=")
		switch k {
		case "out_time_us":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				t = n
			}
		case "progress":
			if t >= 0 {
				out <- t
			}
		}
	}
	_, _ = io.Copy(io.Discard, r)
}

// tail keeps the last lines of stderr, redacted.
type tail struct {
	mu    sync.Mutex
	lines []string
}

func (t *tail) read(r io.Reader, redact func(string) string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		t.mu.Lock()
		t.lines = append(t.lines, redact(line))
		if len(t.lines) > tailLines {
			t.lines = t.lines[1:]
		}
		t.mu.Unlock()
	}
	_, _ = io.Copy(io.Discard, r)
}

func (t *tail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.lines) == 0 {
		return ""
	}
	return t.lines[len(t.lines)-1]
}
