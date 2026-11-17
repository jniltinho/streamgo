package ffmpeg_test

// The tests are sequential: each writes a script and runs it, and a fork from another
// goroutine while a script is open for writing fails with "text file busy".

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/core/output/outputtest"
	"github.com/jniltinho/streamgo/internal/ffmpeg"
)

var fast = ffmpeg.Timing{StallAfter: 300 * time.Millisecond, FirstFlowWithin: 400 * time.Millisecond, StopGrace: 300 * time.Millisecond}

// script writes an executable shell script. The processes get an empty environment, so
// commands are called by absolute path.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	return path
}

// progress prints -progress blocks with out_time_us from the shell expression's values.
const progress = `block() { echo "frame=1"; echo "out_time_us=$1"; echo "progress=continue"; }`

type events struct {
	flowing chan struct{}
	ended   chan string
}

func watch() (events, func(bool, string)) {
	e := events{flowing: make(chan struct{}, 4), ended: make(chan string, 4)}
	return e, func(flowing bool, reason string) {
		if flowing {
			e.flowing <- struct{}{}
		} else {
			e.ended <- reason
		}
	}
}

func (e events) waitEnded(t *testing.T, within time.Duration) string {
	t.Helper()
	select {
	case r := <-e.ended:
		return r
	case <-time.After(within):
		t.Fatal("the process did not end")
		return ""
	}
}

// target is a destination's relay target with a key, through the destination service.
func target(t *testing.T) destination.Target {
	t.Helper()
	e := outputtest.New(t)
	e.AddDestination(t, "k3y-SECRET", "u1", "YouTube", "")
	tg, err := e.DestService.Target(context.Background(), "k3y-SECRET")
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func TestArgsAreTheSpikes(t *testing.T) {
	in, out := "rtsp://streamgo:pw@127.0.0.1:18554/program/ID", "rtsp://streamgo:pw@127.0.0.1:18554/air/ID"
	got := strings.Join(ffmpeg.TranscoderArgs(in, out, 1280, 720, 4500, 160), " ")
	want := "-hide_banner -nostats -loglevel warning -progress pipe:1 " +
		"-rtsp_transport tcp -timeout 5000000 -i " + in + " " +
		"-map 0:v:0 -map 0:a:0 " +
		"-vf scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2,format=yuv420p -fps_mode cfr -r 30 " +
		"-c:v libx264 -preset veryfast -tune zerolatency -profile:v high -g 60 -keyint_min 60 -sc_threshold 0 " +
		"-b:v 4500k -maxrate 4500k -bufsize 9000k " +
		"-c:a aac -ar 48000 -ac 2 -b:a 160k " +
		"-f rtsp -rtsp_transport tcp " + out
	if got != want {
		t.Fatalf("transcoder\n got %s\nwant %s", got, want)
	}
	got = strings.Join(ffmpeg.RelayArgs(out, "rtmps://a.example.com/app/key"), " ")
	want = "-hide_banner -nostats -loglevel warning -progress pipe:1 " +
		"-rtsp_transport tcp -timeout 5000000 -i " + out + " " +
		"-map 0 -c copy -f flv -rw_timeout 5000000 rtmps://a.example.com/app/key"
	if got != want {
		t.Fatalf("relay\n got %s\nwant %s", got, want)
	}
}

// TestTranscoderTakesTheProfile: the transcoder encodes at the size and bitrate of the profile it is started with.
func TestTranscoderTakesTheProfile(t *testing.T) {
	path := script(t, `echo "$*" >&2; exit 3`)
	s := ffmpeg.New(ffmpeg.Options{Path: path, RTSPURL: "rtsp://mediamtx:8554", AudioKbps: 128, Timing: fast})
	defer s.StopAll()
	ev, notify := watch()
	s.StartTranscoder("B1", broadcast.Profile{Resolution: "1440p", Width: 2560, Height: 1440, VideoKbps: 12000}, notify)
	reason := ev.waitEnded(t, 2*time.Second)
	for _, want := range []string{"scale=2560:1440:", "pad=2560:1440:", "-b:v 12000k -maxrate 12000k -bufsize 24000k", "-b:a 128k"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the transcoder's command line %q lacks %q", reason, want)
		}
	}
}

// TestFlowingThenEnded: media time advancing is flowing; the end carries the last line of
// stderr with the destination's address after the host and its key redacted, and the
// internal credentials too. Nothing of it reaches the log.
func TestFlowingThenEnded(t *testing.T) {
	path := script(t, progress+`
block 0; block 0
for t in 1000 2000 3000; do block $t; /bin/sleep 0.05; done
for a in "$@"; do case "$a" in rtsp://*) echo "$a: reading" >&2;; esac; done
for a in "$@"; do last="$a"; done
echo "[tcp @ 0x55] Connection to tcp://dest.test:1935 failed" >&2
echo "$last: Connection refused" >&2
exit 1`)
	var log bytes.Buffer
	s := ffmpeg.New(ffmpeg.Options{Path: path, RTSPURL: "rtsp://mediamtx:8554", User: "streamgo", Secret: "int3rnal-pw",
		Timing: fast, Logger: slog.New(slog.NewTextHandler(&log, nil))})
	defer s.StopAll()
	ev, notify := watch()
	tg := target(t)
	s.StartRelay("B1", tg, notify)
	select {
	case <-ev.flowing:
	case <-time.After(2 * time.Second):
		t.Fatal("not flowing")
	}
	reason := ev.waitEnded(t, 2*time.Second)
	if reason != "rtmp://dest.test***: Connection refused" { // the path, with its slash, is redacted
		t.Fatalf("reason %q", reason)
	}
	if len(ev.flowing) != 0 {
		t.Fatal("flowing was reported twice")
	}
	if strings.Contains(log.String(), "k3y-SECRET") || strings.Contains(log.String(), "int3rnal-pw") {
		t.Fatalf("the log holds a secret: %s", log.String())
	}

	// the transcoder's input names the internal credentials: redacted
	path = script(t, `for a in "$@"; do case "$a" in rtsp://*program*) echo "$a: 404 Not Found" >&2;; esac; done; exit 8`)
	s2 := ffmpeg.New(ffmpeg.Options{Path: path, RTSPURL: "rtsp://mediamtx:8554", User: "streamgo", Secret: "int3rnal-pw", Timing: fast})
	defer s2.StopAll()
	ev, notify = watch()
	s2.StartTranscoder("B1", broadcast.Profile{Width: 1280, Height: 720, VideoKbps: 4000}, notify)
	if reason := ev.waitEnded(t, 2*time.Second); reason != "rtsp://***@mediamtx:8554/program/B1: 404 Not Found" {
		t.Fatalf("reason %q", reason)
	}
}

func TestStalled(t *testing.T) {
	// flowing, then the media time stands still while blocks keep coming
	path := script(t, progress+`
block 1000; block 2000
while :; do block 2000; /bin/sleep 0.05; done`)
	s := ffmpeg.New(ffmpeg.Options{Path: path, Timing: fast})
	defer s.StopAll()
	ev, notify := watch()
	start := time.Now()
	s.StartRelay("B1", target(t), notify)
	<-ev.flowing
	if r := ev.waitEnded(t, 2*time.Second); !strings.Contains(r, "stopped advancing") || time.Since(start) < fast.StallAfter {
		t.Fatalf("reason %q after %v", r, time.Since(start))
	}

	// blocked: no progress at all
	path = script(t, `exec /bin/sleep 30`)
	s2 := ffmpeg.New(ffmpeg.Options{Path: path, Timing: fast})
	defer s2.StopAll()
	ev, notify = watch()
	s2.StartRelay("B1", target(t), notify)
	if r := ev.waitEnded(t, 2*time.Second); !strings.Contains(r, "no media flowed") {
		t.Fatalf("reason %q", r)
	}
}

// TestStop: a process that ignores SIGINT is killed after the grace; StopAll stops every
// process under one deadline.
func TestStop(t *testing.T) {
	path := script(t, progress+`
trap '' INT
t=0
while :; do t=$((t+1000)); block $t; /bin/sleep 0.05; done`)
	s := ffmpeg.New(ffmpeg.Options{Path: path, Timing: fast})
	ev, notify := watch()
	p := s.StartRelay("B1", target(t), notify)
	<-ev.flowing
	start := time.Now()
	p.Stop()
	ev.waitEnded(t, 2*time.Second)
	if d := time.Since(start); d < fast.StopGrace {
		t.Fatalf("killed after %v, before the grace", d)
	}

	all := make([]events, 3)
	for i := range all {
		var n func(bool, string)
		all[i], n = watch()
		s.StartRelay("B1", target(t), n)
		<-all[i].flowing
	}
	start = time.Now()
	s.StopAll()
	if d := time.Since(start); d > fast.StopGrace+time.Second {
		t.Fatalf("StopAll took %v: not at once", d)
	}
	for _, e := range all {
		e.waitEnded(t, time.Second)
	}
	// started after the stop: ends at once
	ev, notify = watch()
	s.StartRelay("B1", target(t), notify)
	ev.waitEnded(t, time.Second)
}

func TestCleanEnvironment(t *testing.T) {
	t.Setenv("STREAMGO_SECRET_KEY", "must-not-leak")
	out := filepath.Join(t.TempDir(), "env")
	path := script(t, `/usr/bin/env > `+out+`; exit 0`)
	s := ffmpeg.New(ffmpeg.Options{Path: path, Timing: fast})
	defer s.StopAll()
	ev, notify := watch()
	s.StartRelay("B1", target(t), notify)
	ev.waitEnded(t, 2*time.Second)
	env, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "STREAMGO_") {
		t.Fatalf("the relay's environment holds a STREAMGO_ variable:\n%s", env)
	}
}

// TestDiesWithTheServer: the server is killed with SIGKILL; its ffmpeg dies with it. The
// "server" is this test binary run again as TestHelperServer.
func TestDiesWithTheServer(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	path := script(t, `echo $$ > `+pidFile+`.tmp; /bin/mv `+pidFile+`.tmp `+pidFile+`; exec /bin/sleep 60`)
	helper := exec.Command(os.Args[0], "-test.run=^TestHelperServer$") //nolint:gosec // this test binary
	helper.Env = append(os.Environ(), "STREAMGO_FFMPEG_HELPER="+path)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	for range 200 {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		_ = helper.Process.Kill()
		t.Fatal("the helper did not start ffmpeg")
	}
	time.Sleep(200 * time.Millisecond) // let the runtime move goroutines between threads
	_ = helper.Process.Signal(syscall.SIGKILL)
	_ = helper.Wait()
	for range 200 {
		if !alive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatal("ffmpeg survived the server's SIGKILL")
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

func TestHelperServer(t *testing.T) {
	path := os.Getenv("STREAMGO_FFMPEG_HELPER")
	if path == "" {
		t.Skip("run by TestDiesWithTheServer")
	}
	s := ffmpeg.New(ffmpeg.Options{Path: path, Timing: ffmpeg.Timing{StallAfter: time.Hour, FirstFlowWithin: time.Hour, StopGrace: time.Hour}})
	done := make(chan struct{})
	go func() { s.StartTranscoder("B1", broadcast.Profile{}, func(bool, string) {}); close(done) }() // a goroutine that ends at once
	<-done
	for range 100 { // churn threads
		go func() { _ = errors.New("x") }()
	}
	time.Sleep(time.Minute)
}
