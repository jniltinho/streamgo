package mediaservers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

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

// restart is one exit of a server seen by the supervisor and the start that followed it.
type restart struct {
	delay string        // the delay the supervisor chose, as it logged it
	gap   time.Duration // from the supervisor seeing the exit to the next process started: the delay and a fork
}

// restarts reads the first n restarts of component from the supervisor's log, waiting until there are n. The gap
// is measured inside the supervisor, so the start-up of the fake (slow under the race detector) is not in it.
func restarts(t *testing.T, s *stack, component string, n int) []restart {
	t.Helper()
	var out []restart
	waitFor(t, 30*time.Second, "the restarts", func() bool {
		out = out[:0]
		var exited *time.Time
		var delay string
		for line := range strings.Lines(s.logs.String()) {
			var e struct {
				Time           time.Time
				Msg, Component string
				Delay          string
			}
			if json.Unmarshal([]byte(line), &e) != nil || e.Component != component {
				continue
			}
			switch {
			case e.Msg == "media server exited: starting it again":
				exited, delay = &e.Time, e.Delay
			case e.Msg == "media server started" && exited != nil:
				out = append(out, restart{delay: delay, gap: e.Time.Sub(*exited)})
				exited = nil
			}
		}
		return len(out) >= n
	})
	return out[:n]
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
	for i, r := range restarts(t, s, mediatest.LiveKit, 7) {
		want := []time.Duration{10, 20, 40, 80, 160, 300, 300}[i] * time.Millisecond
		// the log's clock has milliseconds; the gap adds a fork and exec to the delay
		if r.delay != want.String() || r.gap < want-2*time.Millisecond || r.gap > want+100*time.Millisecond {
			t.Errorf("restart %d: delay %s after %v, want %v", i+1, r.delay, r.gap, want)
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
	var got []string
	for _, r := range restarts(t, s, mediatest.LiveKit, 5) {
		got = append(got, r.delay)
	}
	// the fifth run lasted 600 ms: the next delay is 20 ms again, not 320 ms
	if strings.Join(got, " ") != "20ms 40ms 80ms 160ms 20ms" {
		t.Errorf("delays %v, want 20ms 40ms 80ms 160ms 20ms", got)
	}
}

// TestRestartsStopWhenShutdownBegins is the first half of "Stop": once the context of Start ends (SIGTERM), a server
// that exits is not started again, though the children are stopped only later, by Stop.
func TestRestartsStopWhenShutdownBegins(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	ctx, cancel := context.WithCancel(context.Background())
	g, err := mediaservers.Start(ctx, s.options(), s.children(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Stop)
	cancel()
	time.Sleep(50 * time.Millisecond) // the cancellation reaches the supervisor
	if err := syscall.Kill(s.pids(mediatest.LiveKit)[0], syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second) // twenty times the delay of the options
	if n := len(s.pids(mediatest.LiveKit)); n != 1 || strings.Contains(s.logs.String(), "starting it again") {
		t.Fatalf("%d starts of livekit after shutdown began", n)
	}
	if mtx := s.pids(mediatest.MediaMTX)[0]; gone(mtx) {
		t.Fatal("mediamtx was stopped before Stop")
	}
}

// blockingHandler is a log destination that blocks on the lines of a flood until released, as a full terminal does.
type blockingHandler struct {
	slog.Handler
	release chan struct{}
}

func (h blockingHandler) Handle(ctx context.Context, r slog.Record) error {
	if strings.HasPrefix(r.Message, "flood line") {
		<-h.release
	}
	return h.Handler.Handle(ctx, r)
}

// TestASlowLogDoesNotBlockAServer is "A server cannot block by writing": while the log blocks, a server that writes
// far more than a pipe holds runs on, and the lines that did not fit are counted.
func TestASlowLogDoesNotBlockAServer(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	mediatest.SetModes(t, s.home, mediatest.LiveKit, "flood")
	opts := s.options()
	release := make(chan struct{})
	opts.Logger = slog.New(blockingHandler{Handler: slog.NewJSONHandler(s.logs, nil), release: release})
	s.start(t, opts, s.children(t))
	t.Cleanup(func() { close(release) }) // before the Stop of start: cleanups run last first
	flooded := filepath.Join(s.home, mediatest.LiveKit+".flooded")
	waitFor(t, 10*time.Second, "the flood written", func() bool { _, err := os.Stat(flooded); return err == nil })
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

// TestStop is "Stop": every process group is gone, the one of a server that started a process of its own too.
func TestStop(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	mediatest.SetModes(t, s.home, mediatest.LiveKit, "fork")
	g := s.start(t, s.options(), s.children(t))
	pids := append(s.pids(mediatest.LiveKit), s.pids(mediatest.MediaMTX)...)
	if n := len(groupMembers(pids[0])); n != 2 {
		t.Fatalf("livekit's process group has %d processes, want it and its sleep", n)
	}
	g.Stop()
	for _, pid := range pids {
		if !gone(pid) {
			t.Errorf("%d is still there", pid)
		}
		// independently of its leader: no live member of the group is left
		waitFor(t, 2*time.Second, "the process group gone", func() bool { return len(groupMembers(pid)) == 0 })
	}
	g.Stop() // twice is fine
}

// TestLogs is "Logs": a line of 1 MiB that never ends is logged cut, as soon as the reader holds more than a line,
// and the server keeps running.
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
		if json.Unmarshal([]byte(line), &e) == nil && strings.HasPrefix(e.Msg, "xxx") && e.Msg != strings.Repeat("x", mediaservers.MaxLine)+" [line cut]" {
			t.Errorf("the long line logged as %.100q... (%d bytes)", e.Msg, len(e.Msg))
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
	if keys := liveKitKeys(t, lk.Env); len(keys) != 1 || keys[testCreds.LiveKitKey] != testCreds.LiveKitSecret ||
		!has(mtx.Env, "MTX_AUTHINTERNALUSERS_0_PASS="+testCreds.MediaMTXPass) {
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
	// what the fakes themselves listen on, not what the host has: 9999 and 9998 may be another program's
	if !listens(s.home, mediatest.MediaMTX, mtx.Pid, "tcp", s.setup.Ports.RTSP) ||
		listens(s.home, mediatest.MediaMTX, mtx.Pid, "tcp", 9999) || listens(s.home, mediatest.LiveKit, lk.Pid, "tcp", 9998) {
		t.Errorf("RTSP is not on the port up chose, or a fake listens on 9999 or 9998: %v %v",
			mediatest.Listeners(s.home, mediatest.MediaMTX, mtx.Pid), mediatest.Listeners(s.home, mediatest.LiveKit, lk.Pid))
	}
}

// liveKitKeys reads LIVEKIT_KEYS of env as LiveKit does: YAML, the string values kept.
func liveKitKeys(t *testing.T, env []string) map[string]string {
	t.Helper()
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "LIVEKIT_KEYS="); ok {
			var raw map[string]any
			if err := yaml.Unmarshal([]byte(v), &raw); err != nil {
				t.Fatalf("LIVEKIT_KEYS is not YAML: %v", err)
			}
			keys := map[string]string{}
			for k, v := range raw {
				if s, ok := v.(string); ok {
					keys[k] = s
				}
			}
			return keys
		}
	}
	t.Fatal("no LIVEKIT_KEYS")
	return nil
}

// TestAnyKeyAndSecretReachLiveKit is the encoding side of "The environment wins": a key and a secret that YAML would
// read as a number, a comment, a boolean or a map reach LiveKit as the same strings.
func TestAnyKeyAndSecretReachLiveKit(t *testing.T) {
	t.Parallel()
	for _, c := range []mediaservers.Credentials{
		{LiveKitKey: "streamgo", LiveKitSecret: "01234567890123456789012345678901"},
		{LiveKitKey: "yes", LiveKitSecret: "a # not a comment"},
		{LiveKitKey: "k: v", LiveKitSecret: `{"x": [1]} '"\`},
		{LiveKitKey: "07", LiveKitSecret: "true"},
	} {
		s := newStack(t)
		s.setup.Credentials = c
		children, err := mediaservers.Children(s.setup, s.programs[0], s.programs[1])
		if err != nil {
			t.Fatal(err)
		}
		if keys := liveKitKeys(t, children[0].Env); len(keys) != 1 || keys[c.LiveKitKey] != c.LiveKitSecret {
			t.Errorf("key %q: LiveKit reads %d keys, not the secret given", c.LiveKitKey, len(keys))
		}
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
	if !strings.Contains(err.Error(), `keys: {"streamgo":"[redacted]"} pass: `) {
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
	for _, b := range [][]byte{lk, mtx} {
		if !strings.HasPrefix(string(b), "# Generated by streamgo up") {
			t.Errorf("generated file without its first line:\n%s", b)
		}
	}
	var lkConf struct {
		Port          int      `yaml:"port"`
		BindAddresses []string `yaml:"bind_addresses"`
		RTC           struct {
			TCPPort       int  `yaml:"tcp_port"`
			UDPPort       int  `yaml:"udp_port"`
			UseExternalIP bool `yaml:"use_external_ip"`
		} `yaml:"rtc"`
		Keys map[string]string `yaml:"keys"`
	}
	if err := yaml.Unmarshal(lk, &lkConf); err != nil {
		t.Fatalf("livekit.yaml: %v", err)
	}
	p := s.setup.Ports
	if lkConf.Port != p.LiveKit || fmt.Sprint(lkConf.BindAddresses) != "[127.0.0.1]" || lkConf.RTC.TCPPort != 17881 ||
		lkConf.RTC.UDPPort != 17882 || lkConf.RTC.UseExternalIP || len(lkConf.Keys) != 0 {
		t.Errorf("livekit.yaml reads as %+v", lkConf)
	}
	var mtxConf map[string]any
	if err := yaml.Unmarshal(mtx, &mtxConf); err != nil {
		t.Fatalf("mediamtx.yml: %v", err)
	}
	want := map[string]any{
		"apiAddress": fmt.Sprintf("127.0.0.1:%d", p.API), "rtspAddress": fmt.Sprintf("127.0.0.1:%d", p.RTSP),
		"webrtcAddress": fmt.Sprintf("127.0.0.1:%d", p.WHIP), "webrtcLocalUDPAddress": ":18189", "webrtcLocalTCPAddress": ":18189",
		"api": true, "rtsp": true, "webrtc": true, "rtmp": false, "hls": false, "srt": false, "moq": false, "metrics": false,
		"pprof": false, "playback": false, "rtspEncryption": "no", "webrtcIPsFromInterfaces": false, "authMethod": "internal",
	}
	for k, v := range want {
		if mtxConf[k] != v {
			t.Errorf("mediamtx.yml: %s is %#v, want %#v", k, mtxConf[k], v)
		}
	}
	if fmt.Sprint(mtxConf["rtspTransports"]) != "[tcp]" {
		t.Errorf("mediamtx.yml: rtspTransports is %v", mtxConf["rtspTransports"])
	}
	// no credential in the file: the user and password come from the environment
	users, _ := mtxConf["authInternalUsers"].([]any)
	if u, _ := users[0].(map[string]any); len(users) != 1 || u["pass"] != "" {
		t.Errorf("mediamtx.yml: authInternalUsers %v", users)
	}
}

func TestOtherMediaPorts(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	s.start(t, s.options(), s.children(t))
	p := s.setup.Ports
	lk, mtx := s.pids(mediatest.LiveKit)[0], s.pids(mediatest.MediaMTX)[0]
	for _, l := range []struct {
		component, network string
		pid, port          int
	}{{mediatest.LiveKit, "tcp", lk, p.LiveKitTCP}, {mediatest.LiveKit, "udp", lk, p.LiveKitUDP},
		{mediatest.MediaMTX, "tcp", mtx, p.WebRTC}, {mediatest.MediaMTX, "udp", mtx, p.WebRTC}} {
		if !listens(s.home, l.component, l.pid, l.network, l.port) {
			t.Errorf("%s does not listen on %d/%s", l.component, l.port, l.network)
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
	_, err = mediaservers.Finder{Executable: exe, DataDir: filepath.Join(dir, "data dir"), Pinned: pinnedOf(program, program)}.Find(context.Background())
	if err == nil {
		t.Fatal("the data directory does not exist yet: up creates it first")
	}
	if err := os.Mkdir(filepath.Join(dir, "data dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	programs, err := mediaservers.Finder{Executable: exe, DataDir: filepath.Join(dir, "data dir"), Pinned: pinnedOf(program, program)}.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := newStack(t)
	s.programs = programs
	s.setup.Dir = filepath.Dir(programs[0].Path)
	s.start(t, s.options(), s.children(t))
}
