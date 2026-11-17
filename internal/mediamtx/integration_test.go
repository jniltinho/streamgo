package mediamtx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

func migrated(t *testing.T) store.Settings {
	t.Helper()
	hosts(t)
	settings := storetest.NewSettings(t, store.SQLite)
	db := storetest.OpenEmpty(t, settings)
	m, err := store.NewMigrator(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return settings
}

// TestLiveOutput runs one broadcast through the live output's scenarios on the real stack.
func TestLiveOutput(t *testing.T) {
	s := newStack(t, migrated(t))
	ana := s.user("ana")
	b := s.newBroadcast(ana)
	sinkRTMP := "rtmp://" + s.sink + ":1935/live"
	proxy := newStallProxy(t, s.sink+":1935")
	dA := s.destination(ana, "YouTube", sinkRTMP, "a-KEYA")
	dB := s.destination(ana, "Twitch", sinkRTMP, "b-KEYB")
	dR := s.destination(ana, "Refuses", "rtmp://"+s.sink+":1999/live", "r-KEYR")
	dS := s.destination(ana, "Stalls", "rtmp://"+proxy.address(t)+"/live", "s-KEYS")
	dC := s.destination(ana, "Facebook", sinkRTMP, "c-KEYC")
	live := "/api/v1/broadcasts/" + b + "/live"

	s.expect(http.StatusConflict, http.MethodPost, live, ana, "", "") // no destination
	s.choose(ana, b, dA, dB, dR, dS)
	if got := s.expect(http.StatusOK, http.MethodPost, live, ana, "", ""); !strings.Contains(got, `"state":"starting"`) {
		t.Fatal(got)
	}

	t.Log("process alone is not live")
	time.Sleep(3 * time.Second)
	if s.output(b).State != broadcast.Starting {
		t.Fatalf("not starting: %+v", s.output(b))
	}

	t.Log("go live: the program arrives")
	pub := s.publish(b, "vp8")
	s.waitState(b, broadcast.Live, 20*time.Second)
	if !s.onAir(b) {
		t.Fatal("not on air")
	}
	for _, d := range []string{dA, dB, dS} {
		s.waitStatus(b, d, destination.Live, 15*time.Second)
	}

	t.Log("one destination refuses")
	s.waitStatus(b, dR, destination.Retrying, 10*time.Second)
	if e := s.status(b, dR).Error; e == "" || strings.Contains(e, "KEYR") || strings.Contains(e, "/live") {
		t.Fatalf("the error is missing or not redacted: %q", e)
	}
	relayA := ffmpegPIDs("a-KEYA")
	if len(relayA) != 1 {
		t.Fatalf("relays of dA: %v", relayA)
	}

	t.Log("clean environment")
	env, err := os.ReadFile("/proc/" + strconv.Itoa(relayA[0]) + "/environ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "STREAMGO_") || len(os.Getenv("STREAMGO_TEST_MEDIAMTX")) == 0 {
		t.Fatalf("relay environment: %q", env)
	}

	t.Log("output format (VP8 program)")
	checkFormat(t, "rtsp://"+s.sink+":8554/live/a-KEYA")

	t.Log("a destination stalls")
	proxy.stalled.Store(true)
	took := s.waitStatus(b, dS, destination.Retrying, 15*time.Second)
	t.Logf("the stalled destination was retrying after %v", took)
	if s.status(b, dA).State != destination.Live || !slices.Equal(ffmpegPIDs("a-KEYA"), relayA) {
		t.Fatal("the working destination was interrupted")
	}
	proxy.stalled.Store(false)
	s.waitStatus(b, dS, destination.Live, 30*time.Second) // it recovers at a later retry

	t.Log("add while live")
	s.choose(ana, b, dA, dB, dR, dS, dC)
	s.waitStatus(b, dC, destination.Live, 15*time.Second)
	if !slices.Equal(ffmpegPIDs("a-KEYA"), relayA) {
		t.Fatal("adding restarted another relay")
	}
	t.Log("remove while live")
	s.choose(ana, b, dA, dR, dS, dC)
	s.waitFor(5*time.Second, "the removed relay to end", func() bool { return len(ffmpegPIDs("b-KEYB")) == 0 })
	if s.status(b, dA).State != destination.Live {
		t.Fatal("dA is not live")
	}
	s.expect(http.StatusConflict, http.MethodPut, "/api/v1/broadcasts/"+b+"/destinations", ana, "application/json", `{"ids":[]}`)

	t.Log("program lost and back")
	stopPublisher(pub)
	s.waitState(b, broadcast.Interrupted, 10*time.Second)
	if s.onAir(b) || s.output(b).Deadline.IsZero() {
		t.Fatalf("interrupted: %+v on air %v", s.output(b), s.onAir(b))
	}
	s.waitFor(5*time.Second, "the relays to stop", func() bool { return len(ffmpegPIDs("/live/")) == 0 })
	pub = s.publish(b, "h264")
	s.waitState(b, broadcast.Live, 20*time.Second)
	s.waitStatus(b, dA, destination.Live, 15*time.Second)
	t.Log("output format (H.264 program)")
	checkFormat(t, "rtsp://"+s.sink+":8554/live/a-KEYA")

	t.Log("graceful stop while live, then restart: the broadcast resumes")
	s.stopOutput()
	if pids := ffmpegPIDs(b); len(pids) != 0 {
		t.Fatalf("ffmpeg left after the stop: %v", pids)
	}
	if st := s.output(b).State; st != broadcast.Live {
		t.Fatalf("the stop stored %s", st)
	}
	s.startOutput()
	s.waitFor(5*time.Second, "recovery", func() bool { return s.output(b).State != broadcast.Live || s.status(b, dA).State != "" })
	s.waitState(b, broadcast.Live, 20*time.Second)
	s.waitStatus(b, dA, destination.Live, 15*time.Second)

	t.Log("end, then go live again at once")
	run := s.output(b).Run
	started := time.Now()
	s.expect(http.StatusOK, http.MethodDelete, live, ana, "", "")
	if o := s.output(b); o.State != broadcast.Offline || s.onAir(b) {
		t.Fatalf("after end: %+v", o)
	}
	s.waitFor(10*time.Second-time.Since(started), "no ffmpeg of the broadcast", func() bool { return len(ffmpegPIDs(b)) == 0 })
	s.expect(http.StatusOK, http.MethodPost, live, ana, "", "")
	deadline := time.Now().Add(20 * time.Second)
	for { // one read per turn: the state may become live between two reads
		o := s.output(b)
		if o.Run != run+1 || (o.State != broadcast.Starting && o.State != broadcast.Live) || time.Now().After(deadline) {
			t.Fatalf("a late event of the first run acted, or no program: %+v", o)
		}
		if o.State == broadcast.Live {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Log("the owner never returns: deadline passed")
	stopPublisher(pub)
	s.waitState(b, broadcast.Interrupted, 10*time.Second)
	s.waitState(b, broadcast.Offline, grace+5*time.Second)
	if o := s.output(b); o.Reason != "program lost" {
		t.Fatalf("%+v", o)
	}
	s.waitFor(5*time.Second, "no ffmpeg of the broadcast", func() bool { return len(ffmpegPIDs(b)) == 0 })

	t.Log("restarts do not extend an outage")
	s.expect(http.StatusOK, http.MethodPost, live, ana, "", "")
	s.stopOutput()
	o := s.output(b)
	o.State, o.Deadline = broadcast.Interrupted, time.Now().UTC().Add(4*time.Second)
	if err := s.broadcasts.SetOutput(context.Background(), b, o); err != nil {
		t.Fatal(err)
	}
	s.startOutput()
	s.waitState(b, broadcast.Offline, 8*time.Second)
	if late := time.Since(o.Deadline); late < 0 || late > 2*time.Second {
		t.Fatalf("ended %v after the stored deadline", late)
	}

	t.Log("command lines and keys are not logged")
	logged := s.log.String()
	for _, secret := range []string{"KEYA", "KEYB", "KEYR", "KEYS", "KEYC", "/live/", mtxSecret} {
		if strings.Contains(logged, secret) {
			t.Fatalf("the log holds %q", secret)
		}
	}
}

// checkFormat: H.264 1280x720 at 30 fps with keyframes 2 s apart, AAC 48 kHz stereo.
func checkFormat(t *testing.T, url string) {
	t.Helper()
	var info struct {
		Streams []struct {
			CodecName  string `json:"codec_name"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			FrameRate  string `json:"r_frame_rate"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(probe(t, "-show_entries", "stream=codec_name,width,height,r_frame_rate,sample_rate,channels", "-of", "json", url), &info); err != nil {
		t.Fatal(err)
	}
	var video, audio bool
	for _, st := range info.Streams {
		switch st.CodecName {
		case "h264":
			video = st.Width == 1280 && st.Height == 720 && st.FrameRate == "30/1"
		case "aac":
			audio = st.SampleRate == "48000" && st.Channels == 2
		}
	}
	if !video || !audio {
		t.Fatalf("format %+v", info.Streams)
	}
	// Keyframes every 2 s are counted in frames, not read from timestamps: a reader of a live RTSP stream
	// re-bases its timestamps when the stream's clock reference arrives, which showed on a slow runner as one
	// interval of 1.75 s between exact 2 s ones. At the 30 fps asserted above, 2 s are 60 frames.
	out := probe(t, "-read_intervals", "%+9", "-select_streams", "v:0",
		"-show_entries", "frame=key_frame", "-of", "csv=p=0", url)
	var gops []int // frames from each keyframe to the next
	last := -1
	n := 0
	for line := range strings.FieldsSeq(string(out)) {
		if strings.HasPrefix(line, "1") {
			if last >= 0 {
				gops = append(gops, n-last)
			}
			last = n
		}
		n++
	}
	if len(gops) < 3 {
		t.Fatalf("%d frames read, keyframe distances %v", n, gops)
	}
	for _, g := range gops {
		if g != 60 {
			t.Fatalf("keyframes %d frames apart, want 60 (%v)", g, gops)
		}
	}
}

// TestDirectAccessRefused: without the internal credentials the media server refuses.
func TestDirectAccessRefused(t *testing.T) {
	mtxHost, _ := hosts(t)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+mtxHost+":8889/program/X/whip", strings.NewReader("v=0"))
	req.Header.Set("Content-Type", "application/sdp")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("WHIP without credentials: %d", res.StatusCode)
	}
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+mtxHost+":9997/v3/paths/list", nil)
	req.Header.Set("Authorization", basicAuth(mtxUser, "wrong"))
	if res, err = http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("API with a wrong password: %d", res.StatusCode)
	}
	cmd := exec.Command("ffprobe", "-v", "error", "-rtsp_transport", "tcp", "rtsp://"+mtxHost+":8554/program/X") //nolint:gosec // test
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "401") {
		t.Fatalf("RTSP read without credentials: %v %s", err, out)
	}
}

// TestServerKilled: the server is killed with SIGKILL during a live broadcast; no ffmpeg it
// started remains, and after a restart the broadcast resumes. The killed server is this test
// binary run again as TestHelperLiveServer on the same database.
func TestServerKilled(t *testing.T) {
	settings := migrated(t)
	s := newStack(t, settings)
	ana := s.user("ana")
	b := s.newBroadcast(ana)
	s.choose(ana, b, s.destination(ana, "YouTube", "rtmp://"+s.sink+":1935/live", "k-KEYK"))
	s.stopOutput() // the helper is the server now
	s.publish(b, "vp8")

	helper := exec.Command(os.Args[0], "-test.run=^TestHelperLiveServer$", "-test.v") //nolint:gosec // this test binary
	helper.Env = append(os.Environ(), "STREAMGO_TEST_HELPER_DB="+settings.SQLitePath, "STREAMGO_TEST_HELPER_BROADCAST="+b+"/"+ana)
	stdout, _ := helper.StdoutPipe()
	helper.Stderr = os.Stderr
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	for {
		line, err := readLine(stdout, 60*time.Second)
		if err != nil {
			_ = helper.Process.Kill()
			t.Fatalf("the helper server did not go live: %v", err)
		}
		if line == "LIVE" {
			break
		}
	}
	pids := ffmpegPIDs(b)
	if len(pids) < 2 {
		t.Fatalf("ffmpeg of the helper: %v", pids)
	}
	_ = helper.Process.Signal(syscall.SIGKILL)
	_ = helper.Wait()
	s.waitFor(3*time.Second, fmt.Sprintf("ffmpeg %v to die with the server", pids), func() bool {
		return !slices.ContainsFunc(pids, alive)
	})
	if st := s.output(b).State; st != broadcast.Live {
		t.Fatalf("stored %s", st)
	}
	s.startOutput()
	s.waitState(b, broadcast.Live, 25*time.Second)
	s.expect(http.StatusOK, http.MethodDelete, "/api/v1/broadcasts/"+b+"/live", ana, "", "")
}

func TestHelperLiveServer(t *testing.T) {
	path, target := os.Getenv("STREAMGO_TEST_HELPER_DB"), os.Getenv("STREAMGO_TEST_HELPER_BROADCAST")
	if path == "" {
		t.Skip("run by TestServerKilled")
	}
	b, owner, _ := strings.Cut(target, "/")
	mtxHost, sink := hosts(t)
	settings := store.Settings{Driver: store.SQLite, SQLitePath: path}
	db, err := store.Open(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	s := assemble(t, mtxHost, sink, settings, db)
	if _, err := s.out.GoLive(context.Background(), owner, b); err != nil {
		t.Fatal(err)
	}
	s.waitState(b, broadcast.Live, 30*time.Second)
	s.waitStatus(b, s.firstDest(b), destination.Live, 15*time.Second)
	fmt.Println("LIVE")
	time.Sleep(time.Minute)
}

func (s *stack) firstDest(b string) string {
	s.t.Helper()
	list, err := s.destRepo.ListByBroadcast(context.Background(), b)
	if err != nil || len(list) == 0 {
		s.t.Fatal("no destination", err)
	}
	return list[0].ID
}
