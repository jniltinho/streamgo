package mediaservers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/jniltinho/streamgo/internal/livekit"
	"github.com/jniltinho/streamgo/internal/mediamtx"
	"github.com/jniltinho/streamgo/internal/mediaservers"
	"github.com/jniltinho/streamgo/internal/mediaservers/mediatest"
)

func TestMain(m *testing.M) {
	mediatest.Main()
	os.Exit(m.Run())
}

// Made-up credentials of the fakes: never real ones.
var testCreds = mediaservers.Credentials{
	LiveKitKey: "streamgo", LiveKitSecret: "lk-secret-0123456789abcdef0123456789",
	MediaMTXUser: "streamgo", MediaMTXPass: "mtx-pass-0123456789abcdef",
}

// syncBuffer is a log destination safe for the supervisor's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// stack is two fakes ready to start: a home that steers them, their programs and setup.
type stack struct {
	home     string
	bin      string
	setup    mediaservers.Setup
	programs []mediaservers.Program
	logs     *syncBuffer
}

func newStack(t *testing.T, extraEnv ...string) *stack {
	t.Helper()
	s := &stack{home: t.TempDir(), bin: t.TempDir(), logs: &syncBuffer{}}
	mediatest.Install(t, s.bin)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the supervisor's log:\n%.20000s", s.logs.String())
		}
	})
	// all seven from the fixture's own ports: outside the ephemeral range the readiness checks' connections use
	ports := mediatest.FreePorts(t, 7)
	internal, media := ports[:4], ports[4:]
	s.setup = mediaservers.Setup{
		Dir:    t.TempDir(),
		NodeIP: "127.0.0.1",
		Ports: mediaservers.Ports{LiveKit: internal[0], WHIP: internal[1], RTSP: internal[2], API: internal[3],
			LiveKitTCP: media[0], LiveKitUDP: media[1], WebRTC: media[2]},
		Credentials: testCreds,
		Environ:     append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + s.home}, extraEnv...),
	}
	s.programs = []mediaservers.Program{
		{Name: mediaservers.LiveKit, Path: filepath.Join(s.bin, "livekit-server")},
		{Name: mediaservers.MediaMTX, Path: filepath.Join(s.bin, "mediamtx")},
	}
	return s
}

// children are the two fakes with the real readiness checks of the server.
func (s *stack) children(t *testing.T) []mediaservers.Child {
	t.Helper()
	children, err := mediaservers.Children(s.setup, s.programs[0], s.programs[1])
	if err != nil {
		t.Fatal(err)
	}
	p := s.setup.Ports
	children[0].Ready = livekit.New(fmt.Sprintf("http://127.0.0.1:%d", p.LiveKit), testCreds.LiveKitKey, testCreds.LiveKitSecret).Ready
	mtx, err := mediamtx.New(fmt.Sprintf("http://127.0.0.1:%d", p.WHIP), fmt.Sprintf("http://127.0.0.1:%d", p.API),
		testCreds.MediaMTXUser, testCreds.MediaMTXPass)
	if err != nil {
		t.Fatal(err)
	}
	children[1].Ready = mtx.Ready
	return children
}

func (s *stack) options() mediaservers.Options {
	return mediaservers.Options{
		Logger:       slog.New(slog.NewJSONHandler(s.logs, nil)),
		Secrets:      []string{testCreds.LiveKitSecret, testCreds.MediaMTXPass},
		ReadyTimeout: 10 * time.Second,
		StopTimeout:  2 * time.Second,
		Backoff:      mediaservers.Backoff{Min: 50 * time.Millisecond, Max: time.Second, ResetAfter: time.Minute},
	}
}

func (s *stack) start(t *testing.T, opts mediaservers.Options, children []mediaservers.Child) *mediaservers.Group {
	t.Helper()
	g, err := mediaservers.Start(context.Background(), opts, children)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, s.logs.String())
	}
	t.Cleanup(g.Stop)
	return g
}

// pids are the processes component started so far.
func (s *stack) pids(component string) []int {
	var out []int
	for _, r := range mediatest.Records(s.home, component) {
		out = append(out, r.Pid)
	}
	return out
}

// waitFor polls cond for up to d.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// gone reports whether the process has exited (a zombie has).
func gone(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	_, after, _ := strings.Cut(string(b), ") ")
	return strings.HasPrefix(after, "Z")
}

// groupMembers are the live (not zombie) processes of the process group pgid, read from /proc: independent of
// whether its leader is still there.
func groupMembers(pgid int) []int {
	entries, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		_, after, _ := strings.Cut(string(b), ") ") // state ppid pgrp ...
		f := strings.Fields(after)
		if len(f) > 2 && f[0] != "Z" && f[2] == strconv.Itoa(pgid) {
			out = append(out, pid)
		}
	}
	return out
}

// listens reports whether the fake start pid of component listens on network :port (from its own record).
func listens(home, component string, pid int, network string, port int) bool {
	for _, l := range mediatest.Listeners(home, component, pid) {
		n, addr, _ := strings.Cut(l, " ")
		if _, p, err := net.SplitHostPort(addr); err == nil && n == network && p == strconv.Itoa(port) {
			return true
		}
	}
	return false
}

// pinnedOf is the digests a release pins for these two programs.
func pinnedOf(liveKit, mediaMTX []byte) map[string]string {
	return map[string]string{mediaservers.LiveKit: digest(liveKit), mediaservers.MediaMTX: digest(mediaMTX)}
}

// payload builds the single file at path: base, then each program compressed, the trailer and the footer, in the
// format packaging/single-payload.sh writes.
func payload(t *testing.T, path string, base []byte, liveKit, mediaMTX []byte) {
	t.Helper()
	var out bytes.Buffer
	out.Write(base)
	var trailer strings.Builder
	for _, p := range []struct {
		name string
		data []byte
	}{{mediaservers.LiveKit, liveKit}, {mediaservers.MediaMTX, mediaMTX}} {
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
		if err != nil {
			t.Fatal(err)
		}
		z := enc.EncodeAll(p.data, nil)
		sum := sha256.Sum256(p.data)
		fmt.Fprintf(&trailer, "%s v1.0.0-%s %s %d %d\n", p.name, p.name, hex.EncodeToString(sum[:]), out.Len(), len(z))
		out.Write(z)
	}
	out.WriteString(trailer.String())
	fmt.Fprintf(&out, "%016d%s", trailer.Len(), mediaservers.Mark)
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
