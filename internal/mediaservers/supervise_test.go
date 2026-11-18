package mediaservers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/mediaservers"
	"github.com/jniltinho/streamgo/internal/mediaservers/mediatest"
)

// The scenarios of "Supervision", "Ports" and "The children's environment", with real processes: the test binary
// runs as a fake LiveKit and MediaMTX.

func TestBackoff(t *testing.T) {
	t.Parallel()
	b := mediaservers.DefaultBackoff()
	var got []time.Duration
	var d time.Duration
	for range 7 {
		d = b.Next(d, 0)
		got = append(got, d)
	}
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30}
	for i := range want {
		if got[i] != want[i]*time.Second {
			t.Fatalf("delays %v, want 1 2 4 8 16 30 30 s", got)
		}
	}
	if d := b.Next(8*time.Second, time.Minute); d != time.Second {
		t.Fatalf("after a minute of running: %v, want 1s", d)
	}
}

func TestAMediaServerThatExitsIsStartedAgain(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	children := s.children(t)
	s.start(t, s.options(), children)
	for i, component := range []string{mediatest.LiveKit, mediatest.MediaMTX} {
		pid := s.pids(component)[0]
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 5*time.Second, component+" failing", func() bool { return children[i].Ready(context.Background()) != nil })
		waitFor(t, 10*time.Second, component+" started again", func() bool {
			return len(s.pids(component)) == 2 && children[i].Ready(context.Background()) == nil
		})
		if !gone(pid) {
			t.Errorf("the killed %s is still there", component)
		}
	}
}

// startTimes are the start times of component, waited for until there are n.
func startTimes(t *testing.T, s *stack, component string, n int) []time.Time {
	t.Helper()
	waitFor(t, 20*time.Second, "the starts", func() bool { return len(mediatest.Records(s.home, component)) >= n })
	var out []time.Time
	for _, r := range mediatest.Records(s.home, component)[:n] {
		out = append(out, time.Unix(0, r.At))
	}
	return out
}

// TestRestartDelay is "Restart delay" at a hundredth of the time: 10, 20, 40, 80, 160, 300 and 300 ms.
func TestRestartDelay(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	mediatest.SetModes(t, s.home, mediatest.LiveKit, "exit")
	opts := s.options()
	opts.Backoff = mediaservers.Backoff{Min: 10 * time.Millisecond, Max: 300 * time.Millisecond, ResetAfter: time.Minute}
	children := s.children(t)
	children[0].Ready = nil
	s.start(t, opts, children)
	starts := startTimes(t, s, mediatest.LiveKit, 8)
	for i, want := range []time.Duration{10, 20, 40, 80, 160, 300, 300} {
		want *= time.Millisecond
		gap := starts[i+1].Sub(starts[i])
		// the gap holds the delay and a process start, which the race detector slows
		if gap < want*8/10 || gap > want*12/10+250*time.Millisecond {
			t.Errorf("delay %d: %v, want %v within 20 %%", i+1, gap, want)
		}
	}
}

// TestTheDelayReturns is "The delay returns", scaled: after a run longer than ResetAfter the delay is Min again.
func TestTheDelayReturns(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	mediatest.SetModes(t, s.home, mediatest.LiveKit, "exit", "exit", "exit", "exit", "run=600ms", "exit")
	opts := s.options()
	opts.Backoff = mediaservers.Backoff{Min: 20 * time.Millisecond, Max: 5 * time.Second, ResetAfter: 500 * time.Millisecond}
	children := s.children(t)
	children[0].Ready = nil
	s.start(t, opts, children)
	starts := startTimes(t, s, mediatest.LiveKit, 6)
	if gap := starts[4].Sub(starts[3]); gap < 160*time.Millisecond { // the fourth delay: 160 ms
		t.Errorf("fourth delay %v, want 160ms", gap)
	}
	// the fifth run lasted 600 ms: the next delay is 20 ms again, not 320 ms
	if gap := starts[5].Sub(starts[4]); gap < 600*time.Millisecond || gap > 600*time.Millisecond+250*time.Millisecond {
		t.Errorf("run and delay %v, want 600ms + 20ms", gap)
	}
}

func TestAStopDuringADelay(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	mediatest.SetModes(t, s.home, mediatest.LiveKit, "exit")
	opts := s.options()
	opts.Backoff = mediaservers.Backoff{Min: 30 * time.Second, Max: 30 * time.Second, ResetAfter: time.Minute}
	children := s.children(t)
	children[0].Ready = nil
	g, err := mediaservers.Start(context.Background(), opts, children)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "the exit", func() bool { return strings.Contains(s.logs.String(), "starting it again") })
	began := time.Now()
	g.Stop()
	if d := time.Since(began); d > 5*time.Second {
		t.Fatalf("stopped in %v", d)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(s.pids(mediatest.LiveKit)); n != 1 {
		t.Fatalf("%d starts, want 1", n)
	}
}

func TestAMediaServerThatNeverComesUp(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	mediatest.SetModes(t, s.home, mediatest.LiveKit, "hang")
	opts := s.options()
	opts.ReadyTimeout = time.Second
	opts.StopTimeout = 300 * time.Millisecond // it ignores SIGTERM: SIGKILL after this
	_, err := mediaservers.Start(context.Background(), opts, s.children(t))
	if err == nil {
		t.Fatal("it started")
	}
	msg := err.Error()
	if !strings.Contains(msg, "livekit did not answer") || !strings.Contains(msg, "hang line 6\n") ||
		!strings.Contains(msg, "hang line 25") || strings.Contains(msg, "hang line 5\n") {
		t.Fatalf("not the last 20 lines:\n%s", msg)
	}
	for _, c := range []string{mediatest.LiveKit, mediatest.MediaMTX} {
		for _, pid := range s.pids(c) {
			if !gone(pid) {
				t.Errorf("%s %d is still there", c, pid)
			}
		}
	}
}

// TestASecondServerThatFails is the media side of "A failure after a media server started": the second cannot
// start, the first is stopped.
func TestASecondServerThatFails(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	children := s.children(t)
	children[1].Path = filepath.Join(t.TempDir(), "absent")
	if _, err := mediaservers.Start(context.Background(), s.options(), children); err == nil || !strings.Contains(err.Error(), "start mediamtx") {
		t.Fatalf("error %v", err)
	}
	var pid int // from the log: the fake may be stopped before it records itself
	for line := range strings.Lines(s.logs.String()) {
		var e struct {
			Msg, Component string
			Pid            int
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.Msg == "media server started" && e.Component == "livekit" {
			pid = e.Pid
		}
	}
	if pid == 0 {
		t.Fatal("livekit did not start")
	}
	waitFor(t, 15*time.Second, "livekit gone", func() bool { return gone(pid) })
}

func TestStop(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	g := s.start(t, s.options(), s.children(t))
	pids := append(s.pids(mediatest.LiveKit), s.pids(mediatest.MediaMTX)...)
	g.Stop()
	for _, pid := range pids {
		if !gone(pid) {
			t.Errorf("%d is still there", pid)
		}
		if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) && !gone(pid) {
			t.Errorf("the process group of %d: %v", pid, err)
		}
	}
	g.Stop() // twice is fine
}

func TestLogs(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	mediatest.SetModes(t, s.home, mediatest.LiveKit, "big")
	children := s.children(t)
	s.start(t, s.options(), children)
	waitFor(t, 5*time.Second, "the long line", func() bool { return strings.Contains(s.logs.String(), "[line cut]") })
	var sawFirst bool
	for line := range strings.Lines(s.logs.String()) {
		if len(line) > mediaservers.MaxLine+512 {
			t.Errorf("a log line of %d bytes", len(line))
		}
		var e struct{ Msg, Component string }
		if json.Unmarshal([]byte(line), &e) == nil && e.Msg == "before the long line" && e.Component == "livekit" {
			sawFirst = true
		}
	}
	if !sawFirst {
		t.Errorf("the line is not in the log with component livekit:\n%.2000s", s.logs.String())
	}
	if err := children[0].Ready(context.Background()); err != nil || len(s.pids(mediatest.LiveKit)) != 1 {
		t.Fatalf("it did not keep running: %v", err)
	}
}

// TestWhatAChildReceives is "What a child receives" and "A variable of the shell does not reach a child".
func TestWhatAChildReceives(t *testing.T) {
	t.Parallel()
	s := newStack(t, "STREAMGO_SECRET_KEY=made-up-key", "STREAMGO_DATABASE_DSN=made-up-dsn", "MTX_RTSPADDRESS=:9999",
		"RTSP_PROTOCOLS=udp", "LIVEKIT_PORT=9998", "LC_ALL=C.UTF-8", "TZ=UTC", "OTHER=x")
	s.start(t, s.options(), s.children(t))
	lk := mediatest.Records(s.home, mediatest.LiveKit)[0]
	mtx := mediatest.Records(s.home, mediatest.MediaMTX)[0]
	has := func(env []string, prefix string) bool {
		for _, kv := range env {
			if strings.HasPrefix(kv, prefix) {
				return true
			}
		}
		return false
	}
	if !has(lk.Env, "LIVEKIT_KEYS=streamgo: "+testCreds.LiveKitSecret) || !has(mtx.Env, "MTX_AUTHINTERNALUSERS_0_PASS=") {
		t.Fatal("the credentials were not handed over")
	}
	for _, env := range [][]string{lk.Env, mtx.Env} {
		for _, bad := range []string{"STREAMGO_", "MTX_RTSPADDRESS", "RTSP_", "LIVEKIT_PORT", "OTHER="} {
			if has(env, bad) {
				t.Errorf("a child has %s", bad)
			}
		}
		for _, want := range []string{"PATH=", "HOME=", "LC_ALL=", "TZ="} {
			if !has(env, want) {
				t.Errorf("a child lacks %s", want)
			}
		}
	}
	if !listening("tcp", s.setup.Ports.RTSP) || listening("tcp", 9999) || listening("tcp", 9998) {
		t.Error("RTSP is not on the port up chose, or something is on 9999 or 9998")
	}
}

// TestCredentialsStayOutOfSight is "Credentials stay out of sight": a server prints its credentials and fails.
func TestCredentialsStayOutOfSight(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	mediatest.SetModes(t, s.home, mediatest.LiveKit, "secret")
	mediatest.SetModes(t, s.home, mediatest.MediaMTX, "secret")
	opts := s.options()
	opts.ReadyTimeout = time.Second
	_, err := mediaservers.Start(context.Background(), opts, s.children(t))
	if err == nil {
		t.Fatal("it started")
	}
	everything := err.Error() + s.logs.String()
	for _, r := range append(mediatest.Records(s.home, mediatest.LiveKit), mediatest.Records(s.home, mediatest.MediaMTX)...) {
		everything += strings.Join(r.Args, " ")
	}
	entries, _ := os.ReadDir(s.setup.Dir)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(s.setup.Dir, e.Name())) //nolint:gosec // a test file
		everything += string(b)
	}
	for _, secret := range []string{testCreds.LiveKitSecret, testCreds.MediaMTXPass} {
		if strings.Contains(everything, secret) {
			t.Error("a secret is visible")
		}
	}
	if !strings.Contains(err.Error(), "keys: streamgo: [redacted]") {
		t.Errorf("the tail is not in the message:\n%s", err)
	}
}

// TestGeneratedConfiguration is the configuration side of "Nothing else listens" and "Other media ports": the
// generated files open only the listeners of the setup, the internal ones on the loopback address.
func TestGeneratedConfiguration(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	s.setup.Ports.LiveKitTCP, s.setup.Ports.LiveKitUDP, s.setup.Ports.WebRTC = 17881, 17882, 18189
	if _, err := mediaservers.Children(s.setup, s.programs[0], s.programs[1]); err != nil {
		t.Fatal(err)
	}
	lk, _ := os.ReadFile(filepath.Join(s.setup.Dir, "livekit.yaml"))  //nolint:gosec // a test file
	mtx, _ := os.ReadFile(filepath.Join(s.setup.Dir, "mediamtx.yml")) //nolint:gosec // a test file
	for _, want := range []string{"bind_addresses: [127.0.0.1]", "tcp_port: 17881", "udp_port: 17882", "use_external_ip: false"} {
		if !strings.Contains(string(lk), want) {
			t.Errorf("livekit.yaml lacks %q", want)
		}
	}
	for _, want := range []string{"webrtcLocalUDPAddress: :18189", "webrtcLocalTCPAddress: :18189", "rtmp: false", "hls: false",
		"srt: false", "moq: false", "metrics: false", "pprof: false", "playback: false", "rtspTransports: [tcp]",
		"apiAddress: 127.0.0.1:", "rtspAddress: 127.0.0.1:", "webrtcAddress: 127.0.0.1:"} {
		if !strings.Contains(string(mtx), want) {
			t.Errorf("mediamtx.yml lacks %q", want)
		}
	}
	for _, b := range [][]byte{lk, mtx} {
		if !strings.HasPrefix(string(b), "# Generated by streamgo up") || strings.Contains(string(b), " 7881\n") || strings.Contains(string(b), ":8189\n") {
			t.Errorf("generated file:\n%s", b)
		}
	}
}

func TestOtherMediaPorts(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	s.start(t, s.options(), s.children(t))
	p := s.setup.Ports
	for _, l := range []struct {
		network string
		port    int
	}{{"tcp", p.LiveKitTCP}, {"udp", p.LiveKitUDP}, {"tcp", p.WebRTC}, {"udp", p.WebRTC}} {
		if !listening(l.network, l.port) {
			t.Errorf("nothing on %d/%s", l.port, l.network)
		}
	}
}

func TestAMediaPortInUse(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	u, err := net.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = u.Close() }()
	tcpPort, udpPort := l.Addr().(*net.TCPAddr).Port, u.LocalAddr().(*net.UDPAddr).Port
	err = mediaservers.CheckMediaPorts(context.Background(), []mediaservers.MediaPort{
		{Network: "tcp", Port: tcpPort, Flag: "--livekit-tcp-port"},
		{Network: "udp", Port: udpPort, Flag: "--webrtc-port"},
	})
	if err == nil || !strings.Contains(err.Error(), "--livekit-tcp-port") || !strings.Contains(err.Error(), "--webrtc-port") {
		t.Fatalf("error %v", err)
	}
}

func TestFreePorts(t *testing.T) {
	t.Parallel()
	ports, err := mediaservers.FreePorts(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, p := range ports {
		if seen[p] || p == 0 {
			t.Fatalf("ports %v", ports)
		}
		seen[p] = true
	}
}

// fakeConn is a UDP "connection" with a chosen local address.
type fakeConn struct {
	net.Conn
	local net.Addr
}

func (c fakeConn) LocalAddr() net.Addr { return c.local }
func (c fakeConn) Close() error        { return nil }

// TestNodeAddress is "Node address" and "No network".
func TestNodeAddress(t *testing.T) {
	t.Parallel()
	route := func(string, string) (net.Conn, error) {
		return fakeConn{local: &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000}}, nil
	}
	if got := mediaservers.NodeIP(route); got != "192.0.2.10" {
		t.Errorf("with a default route: %s", got)
	}
	noRoute := func(string, string) (net.Conn, error) { return nil, errors.New("network is unreachable") }
	if got := mediaservers.NodeIP(noRoute); got != "127.0.0.1" {
		t.Errorf("without a default route: %s", got)
	}
}

// TestAPathWithSpaces is "A path with spaces", on the payload: a single file in a directory whose name has a space
// unpacks its servers into a data directory with a space, and they become ready.
func TestAPathWithSpaces(t *testing.T) { // not parallel: it runs files it wrote (text file busy)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, _ := os.ReadFile(self) //nolint:gosec // this test binary, run as the fakes
	dir := filepath.Join(t.TempDir(), "my studio")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "streamgo")
	payload(t, exe, []byte("an executable"), program, program)
	_, err = mediaservers.Finder{Executable: exe, DataDir: filepath.Join(dir, "data dir")}.Find(context.Background())
	if err == nil {
		t.Fatal("the data directory does not exist yet: up creates it first")
	}
	if err := os.Mkdir(filepath.Join(dir, "data dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	programs, err := mediaservers.Finder{Executable: exe, DataDir: filepath.Join(dir, "data dir")}.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := newStack(t)
	s.programs = programs
	s.setup.Dir = filepath.Dir(programs[0].Path)
	s.start(t, s.options(), s.children(t))
}
