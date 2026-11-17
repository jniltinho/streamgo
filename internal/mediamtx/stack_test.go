package mediamtx_test

// The integration test's stack: the real store (SQLite), destination and broadcast services,
// stage writer, live output, ffmpeg supervisor (the tools image's ffmpeg), MediaMTX client and
// HTTP routes, against the MediaMTX and RTMP sink of deployments/compose/compose.test.yml.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/core/output"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/ffmpeg"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/httpserver/handlers"
	"github.com/jniltinho/streamgo/internal/mediamtx"
	"github.com/jniltinho/streamgo/internal/sealed"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

// The ingest MediaMTX's internal credentials in deployments/compose/compose.test.yml.
const (
	mtxUser   = "streamgo"
	mtxSecret = "streamgo-test-mediamtx" //nolint:gosec // fixture of the throwaway test container
	origin    = "https://studio.example.test"
	grace     = 10 * time.Second
)

type stack struct {
	t              *testing.T
	mtxHost, sink  string
	settings       store.Settings
	db             *store.DB
	locks          *broadcast.Locks
	broadcasts     *store.Broadcasts
	destRepo       *store.Destinations
	destSvc        *destination.Service
	broadcastSvc   *broadcast.Service
	mtx            *mediamtx.Client
	log            *syncBuffer
	out            *output.Service
	http           *httptest.Server
	stopRun        context.CancelFunc
	runDone        chan struct{}
	publishersMu   sync.Mutex
	publishersList []*exec.Cmd
}

// hosts returns the media servers' host names, or skips the test.
func hosts(t *testing.T) (mtxHost, sink string) {
	t.Helper()
	mtxHost, sink = os.Getenv("STREAMGO_TEST_MEDIAMTX"), os.Getenv("STREAMGO_TEST_SINK")
	if mtxHost == "" || sink == "" {
		t.Skip("STREAMGO_TEST_MEDIAMTX and STREAMGO_TEST_SINK not set: the live output's integration test runs under make test-db")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg on PATH")
	}
	return mtxHost, sink
}

func newStack(t *testing.T, settings store.Settings) *stack {
	t.Helper()
	mtxHost, sink := hosts(t)
	db := storetest.OpenEmpty(t, settings)
	return assemble(t, mtxHost, sink, settings, db)
}

func assemble(t *testing.T, mtxHost, sink string, settings store.Settings, db *store.DB) *stack {
	t.Helper()
	box, err := sealed.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	s := &stack{t: t, mtxHost: mtxHost, sink: sink, settings: settings, db: db, locks: &broadcast.Locks{},
		broadcasts: store.NewBroadcasts(db), destRepo: store.NewDestinations(db), log: &syncBuffer{}}
	s.broadcastSvc = &broadcast.Service{Repo: s.broadcasts, Locks: s.locks, Sealer: box, PublicURL: origin}
	s.destSvc = &destination.Service{Repo: s.destRepo, Broadcasts: s.broadcasts, Locks: s.locks, Sealer: box,
		AllowPrivate: true, MaxPerBroadcast: 5}
	s.mtx, err = mediamtx.New("http://"+mtxHost+":8889", "http://"+mtxHost+":9997", mtxUser, mtxSecret)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for s.mtx.Ready(ctx) != nil {
		if ctx.Err() != nil {
			t.Fatalf("MediaMTX at %s is not ready", mtxHost)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Cleanup(func() {
		s.stopOutput()
		s.publishersMu.Lock()
		defer s.publishersMu.Unlock()
		for _, p := range s.publishersList {
			_ = p.Process.Kill()
			_ = p.Wait()
		}
	})
	s.startOutput()
	return s
}

// startOutput starts a live output (a "server process") with its routes.
func (s *stack) startOutput() {
	logger := slog.New(slog.NewTextHandler(s.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sup := ffmpeg.New(ffmpeg.Options{Path: "ffmpeg", RTSPURL: "rtsp://" + s.mtxHost + ":8554", User: mtxUser, Secret: mtxSecret,
		Width: 1280, Height: 720, VideoKbps: 2500, AudioKbps: 128, Logger: logger})
	s.out = &output.Service{
		Broadcasts: s.broadcasts, Locks: s.locks, Destinations: s.destRepo, Targets: s.destSvc,
		Stage:  &stage.Writer{Broadcasts: s.broadcasts, Locks: s.locks, Rooms: nopPusher{}, MaxOnStage: 6, Logger: logger},
		Ingest: s.mtx, Processes: sup, MaxConcurrent: 4, StartTimeout: 30 * time.Second, Grace: grace, Logger: logger,
	}
	s.destSvc.Statuses, s.destSvc.Notifier = s.out, s.out
	srv := httpserver.New(httpserver.Options{PublicOrigin: origin, Logger: logger,
		Authenticate: func(c *echo.Context) (string, error) {
			if u := c.Request().Header.Get("X-Test-User"); u != "" {
				return u, nil
			}
			return "", httpserver.ErrUnauthenticated
		}})
	handlers.RegisterBroadcasts(srv.UserAPI(), handlers.BroadcastsDeps{Broadcasts: s.broadcastSvc, Destinations: s.destSvc})
	handlers.RegisterLive(srv.UserAPI(), handlers.LiveDeps{Output: s.out})
	handlers.RegisterWHIP(srv.UserAPI(), handlers.LiveDeps{Output: s.out})
	s.http = httptest.NewServer(srv.Handler())
	ctx, cancel := context.WithCancel(context.Background())
	s.stopRun, s.runDone = cancel, make(chan struct{})
	go func() { s.out.Run(ctx); close(s.runDone) }()
}

// stopOutput stops the live output as SIGTERM would: Run returns once its processes are gone.
func (s *stack) stopOutput() {
	if s.stopRun == nil {
		return
	}
	s.http.Close()
	s.stopRun()
	<-s.runDone
	s.stopRun = nil
}

func (s *stack) user(name string) string {
	s.t.Helper()
	id := strings.ToUpper(name) + strings.Repeat("0", 26-len(name))
	if err := s.db.Gorm.Exec("INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)",
		id, name+"@example.com", name, "x").Error; err != nil {
		s.t.Fatal(err)
	}
	return id
}

func (s *stack) newBroadcast(owner string) string {
	s.t.Helper()
	b, err := s.broadcastSvc.Create(context.Background(), owner, "Show")
	if err != nil {
		s.t.Fatal(err)
	}
	return b.ID
}

func (s *stack) destination(owner, name, address, key string) string {
	s.t.Helper()
	d, err := s.destSvc.Create(context.Background(), owner, destination.Input{Name: name, Address: address, Key: key})
	if err != nil {
		s.t.Fatal(err)
	}
	return d.ID
}

func (s *stack) do(method, path, user, contentType, body string) (*http.Response, string) {
	s.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, s.http.URL+path, strings.NewReader(body))
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("X-Test-User", user)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func (s *stack) expect(status int, method, path, user, contentType, body string) string {
	s.t.Helper()
	res, got := s.do(method, path, user, contentType, body)
	if res.StatusCode != status {
		s.t.Fatalf("%s %s: %d %s, want %d", method, path, res.StatusCode, got, status)
	}
	return got
}

func (s *stack) choose(owner, b string, ids ...string) {
	s.t.Helper()
	body, _ := json.Marshal(map[string][]string{"ids": ids})
	s.expect(http.StatusOK, http.MethodPut, "/api/v1/broadcasts/"+b+"/destinations", owner, "application/json", string(body))
}

func (s *stack) output(b string) broadcast.Output {
	s.t.Helper()
	row, err := s.broadcasts.Get(context.Background(), b)
	if err != nil {
		s.t.Fatal(err)
	}
	return row.Output
}

func (s *stack) onAir(b string) bool {
	s.t.Helper()
	row, err := s.broadcasts.Get(context.Background(), b)
	if err != nil {
		s.t.Fatal(err)
	}
	st, err := stage.Parse(row.Stage)
	if err != nil {
		s.t.Fatal(err)
	}
	return st.OnAir
}

func (s *stack) status(b, d string) destination.Status {
	st, ok := s.out.StatusOf(b)[d]
	if !ok {
		return destination.Status{State: destination.Off}
	}
	return st
}

// waitFor polls cond every 100 ms for up to within; it fails the test with what otherwise.
func (s *stack) waitFor(within time.Duration, what string, cond func() bool) time.Duration {
	s.t.Helper()
	start := time.Now()
	for time.Since(start) < within {
		if cond() {
			return time.Since(start)
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.t.Fatalf("not within %v: %s (output %+v)", within, what, s.lastOutput())
	return 0
}

func (s *stack) lastOutput() string { return strings.TrimSpace(s.log.tail(5)) }

func (s *stack) waitState(b string, state broadcast.OutputState, within time.Duration) {
	s.t.Helper()
	s.waitFor(within, fmt.Sprintf("broadcast %s %s", b, state), func() bool { return s.output(b).State == state })
}

func (s *stack) waitStatus(b, d, state string, within time.Duration) time.Duration {
	s.t.Helper()
	return s.waitFor(within, fmt.Sprintf("destination %s %s", d, state), func() bool { return s.status(b, d).State == state })
}

// publish starts a synthetic program on program/<id> with the internal credentials (VP8 or
// H.264 video, Opus audio), as the studio would through WHIP.
func (s *stack) publish(b, videoCodec string) *exec.Cmd {
	s.t.Helper()
	video := []string{"-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8", "-b:v", "1M", "-g", "30"}
	if videoCodec == "h264" {
		video = []string{"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-profile:v", "baseline", "-b:v", "1M", "-g", "30"}
	}
	args := append([]string{"-hide_banner", "-loglevel", "error", "-re",
		"-f", "lavfi", "-i", "testsrc2=size=960x540:rate=30", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-map", "0:v", "-map", "1:a"}, video...)
	args = append(args, "-c:a", "libopus", "-ac", "2", "-b:a", "64k",
		"-f", "rtsp", "-rtsp_transport", "tcp", "rtsp://"+mtxUser+":"+mtxSecret+"@"+s.mtxHost+":8554/program/"+b)
	cmd := exec.Command("ffmpeg", args...) //nolint:gosec // test publisher
	if err := cmd.Start(); err != nil {
		s.t.Fatal(err)
	}
	s.publishersMu.Lock()
	s.publishersList = append(s.publishersList, cmd)
	s.publishersMu.Unlock()
	return cmd
}

func stopPublisher(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

// ffmpegPIDs lists the running ffmpeg processes whose command line contains every part.
func ffmpegPIDs(parts ...string) []int {
	entries, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !alive(pid) {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		args := strings.Split(string(raw), "\x00")
		if !strings.HasSuffix(args[0], "ffmpeg") || !slices.Contains(args, "-progress") {
			continue
		}
		line := strings.Join(args, " ")
		match := true
		for _, p := range parts {
			match = match && strings.Contains(line, p)
		}
		if match {
			out = append(out, pid)
		}
	}
	return out
}

// alive reports whether pid runs (a zombie waiting for its reaper is dead).
func alive(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z"
}

// probe runs ffprobe on a URL and returns its JSON output.
func probe(t *testing.T, args ...string) []byte {
	t.Helper()
	var err error
	for range 10 { // the sink publishes the path a moment after the relay connects
		var out []byte
		cmd := exec.Command("ffprobe", append([]string{"-v", "error", "-rtsp_transport", "tcp"}, args...)...) //nolint:gosec // test
		if out, err = cmd.Output(); err == nil {
			return out
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("ffprobe %v: %v", args, err)
	return nil
}

type nopPusher struct{}

func (nopPusher) UpdateMetadata(context.Context, string, string) error { return nil }

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func (b *syncBuffer) tail(n int) string {
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// stallProxy forwards TCP to target until it is stalled: then it stops reading from clients,
// as a destination that accepts the connection and stops reading.
type stallProxy struct {
	ln      net.Listener
	target  string
	stalled atomic.Bool
}

func newStallProxy(t *testing.T, target string) *stallProxy {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	p := &stallProxy{ln: ln, target: target}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.handle(c)
		}
	}()
	return p
}

func (p *stallProxy) handle(c net.Conn) {
	defer func() { _ = c.Close() }()
	up, err := net.Dial("tcp", p.target)
	if err != nil {
		return
	}
	defer func() { _ = up.Close() }()
	go func() { _, _ = io.Copy(c, up) }()
	buf := make([]byte, 32<<10)
	for {
		for p.stalled.Load() {
			time.Sleep(50 * time.Millisecond)
		}
		n, err := c.Read(buf)
		if n > 0 {
			if _, err := up.Write(buf[:n]); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// address is the proxy's address as other containers reach it.
func (p *stallProxy) address(t *testing.T) string {
	t.Helper()
	port := p.ln.Addr().(*net.TCPAddr).Port
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			return net.JoinHostPort(n.IP.String(), strconv.Itoa(port))
		}
	}
	t.Fatal("no address of this container")
	return ""
}

// readLine reads one line from r within d.
func readLine(r io.Reader, d time.Duration) (string, error) {
	ch := make(chan string, 1)
	errc := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(r).ReadString('\n')
		if err != nil {
			errc <- err
			return
		}
		ch <- strings.TrimSpace(line)
	}()
	select {
	case l := <-ch:
		return l, nil
	case err := <-errc:
		return "", err
	case <-time.After(d):
		return "", fmt.Errorf("no line within %v", d)
	}
}

func basicAuth(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}
