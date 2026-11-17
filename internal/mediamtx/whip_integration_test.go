package mediamtx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
)

// whipPublisher is a WebRTC publisher as the studio is: VP8 video and Opus audio, every ICE
// candidate in the offer (no trickle).
type whipPublisher struct {
	pc           *webrtc.PeerConnection
	video, audio *webrtc.TrackLocalStaticSample
	frames       [][]byte
	stop         chan struct{}
}

// vp8Frames encodes a short VP8 clip with ffmpeg and returns its frames.
func vp8Frames(t *testing.T) [][]byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v.ivf")
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=30", //nolint:gosec // test
		"-t", "4", "-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8", "-g", "30", "-b:v", "800k", "-y", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("encode VP8: %v %s", err, out)
	}
	f, err := os.Open(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	r, _, err := ivfreader.NewWith(f)
	if err != nil {
		t.Fatal(err)
	}
	var frames [][]byte
	for {
		frame, _, err := r.ParseNextFrame()
		if errors.Is(err, io.EOF) {
			return frames
		}
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
}

// newPublisher returns a publisher and its offer.
func newPublisher(t *testing.T, frames [][]byte, withAudio bool) (*whipPublisher, string) {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	p := &whipPublisher{pc: pc, frames: frames, stop: make(chan struct{})}
	t.Cleanup(p.close)
	sendonly := webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}
	if p.video, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "program"); err != nil {
		t.Fatal(err)
	}
	if _, err := pc.AddTransceiverFromTrack(p.video, sendonly); err != nil {
		t.Fatal(err)
	}
	if withAudio {
		p.audio, err = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "program")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pc.AddTransceiverFromTrack(p.audio, sendonly); err != nil {
			t.Fatal(err)
		}
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	return p, pc.LocalDescription().SDP
}

// start applies the answer and sends media until close.
func (p *whipPublisher) start(t *testing.T, answer string) {
	t.Helper()
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}
	go func() {
		video := time.NewTicker(time.Second / 30)
		audio := time.NewTicker(20 * time.Millisecond)
		defer video.Stop()
		defer audio.Stop()
		for i := 0; ; {
			select {
			case <-p.stop:
				return
			case <-video.C:
				_ = p.video.WriteSample(media.Sample{Data: p.frames[i%len(p.frames)], Duration: time.Second / 30})
				i++
			case <-audio.C:
				if p.audio != nil {
					_ = p.audio.WriteSample(media.Sample{Data: []byte{0xf8, 0xff, 0xfe}, Duration: 20 * time.Millisecond}) // Opus silence
				}
			}
		}
	}()
}

func (p *whipPublisher) close() {
	select {
	case <-p.stop:
	default:
		close(p.stop)
		_ = p.pc.Close()
	}
}

// offer posts a publisher's offer through StreamGo's WHIP route.
func (s *stack) offer(owner, b, sdp string) (*http.Response, string) {
	s.t.Helper()
	return s.do(http.MethodPost, "/api/v1/broadcasts/"+b+"/program/whip", owner, "application/sdp", sdp)
}

func (s *stack) publisher(b string) string {
	s.t.Helper()
	id, err := s.mtx.Publisher(context.Background(), b)
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

// TestWHIPProxy: the studio's WHIP publisher through StreamGo to the real MediaMTX.
func TestWHIPProxy(t *testing.T) {
	s := newStack(t, migrated(t))
	frames := vp8Frames(t)
	ana, bia := s.user("ana"), s.user("bia")
	b := s.newBroadcast(ana)
	dW := s.destination(ana, "YouTube", "rtmp://"+s.sink+":1935/live", "w-KEYW")
	s.choose(ana, b, dW)
	live := "/api/v1/broadcasts/" + b + "/live"

	_, sdp := newPublisher(t, frames, true)
	if res, _ := s.offer(ana, b, sdp); res.StatusCode != http.StatusConflict {
		t.Fatalf("offline broadcast: %d", res.StatusCode)
	}
	s.expect(http.StatusOK, http.MethodPost, live, ana, "", "")
	if res, _ := s.offer(bia, b, sdp); res.StatusCode != http.StatusNotFound {
		t.Fatalf("not the owner: %d", res.StatusCode)
	}

	t.Log("owner publishes: 201 with the answer and a session resource; the broadcast goes live")
	p1, sdp := newPublisher(t, frames, true)
	res, answer := s.offer(ana, b, sdp)
	loc1 := res.Header.Get("Location")
	if res.StatusCode != http.StatusCreated || res.Header.Get("Content-Type") != "application/sdp" ||
		!strings.HasPrefix(loc1, "/api/v1/broadcasts/"+b+"/program/whip/sessions/") || !strings.HasPrefix(answer, "v=0") {
		t.Fatalf("%d %v %s", res.StatusCode, res.Header, answer)
	}
	p1.start(t, answer)
	s.waitState(b, broadcast.Live, 30*time.Second)
	s.waitStatus(b, dW, destination.Live, 15*time.Second)
	first := s.publisher(b)
	if first == "" {
		t.Fatal("no publisher on the path")
	}

	t.Log("a second offer replaces the first")
	p2, sdp := newPublisher(t, frames, true)
	res, answer = s.offer(ana, b, sdp)
	loc2 := res.Header.Get("Location")
	if res.StatusCode != http.StatusCreated || loc2 == loc1 {
		t.Fatalf("second offer: %d %s", res.StatusCode, loc2)
	}
	p2.start(t, answer)
	s.waitFor(10*time.Second, "the second session to publish", func() bool { p := s.publisher(b); return p != "" && p != first })
	second := s.publisher(b)
	s.waitState(b, broadcast.Live, 30*time.Second)

	t.Log("late DELETE of the first, PATCH, DELETE by another user")
	if res, _ := s.do(http.MethodDelete, loc1, ana, "", ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("late DELETE: %d", res.StatusCode)
	}
	if res, _ := s.do(http.MethodPatch, loc2, ana, "application/trickle-ice-sdpfrag", "a=ice-ufrag:x"); res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH: %d", res.StatusCode)
	}
	if res, _ := s.do(http.MethodDelete, loc2, bia, "", ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE by another user: %d", res.StatusCode)
	}
	time.Sleep(time.Second)
	if s.publisher(b) != second {
		t.Fatal("the second session stopped publishing")
	}
	s.expect(http.StatusOK, http.MethodDelete, loc2, ana, "", "")
	s.waitFor(5*time.Second, "the session to close", func() bool { return s.publisher(b) == "" })
	p1.close()
	p2.close()

	t.Log("end closes the ingest session")
	p3, sdp := newPublisher(t, frames, true)
	res, answer = s.offer(ana, b, sdp)
	if res.StatusCode != http.StatusCreated {
		t.Fatal(res.StatusCode)
	}
	p3.start(t, answer)
	s.waitFor(10*time.Second, "the third session to publish", func() bool { return s.publisher(b) != "" })
	s.waitState(b, broadcast.Live, 30*time.Second)
	started := time.Now()
	s.expect(http.StatusOK, http.MethodDelete, live, ana, "", "")
	if s.publisher(b) != "" || s.onAir(b) || s.output(b).IngestID != "" {
		t.Fatal("the ingest session survived End")
	}
	s.waitFor(10*time.Second-time.Since(started), "no ffmpeg of the broadcast", func() bool { return len(ffmpegPIDs(b)) == 0 })
	p3.close()

	t.Log("an orphan session of an offline broadcast is closed")
	p4, sdp := newPublisher(t, frames, true)
	a, err := s.mtx.Publish(context.Background(), b, []byte(sdp)) // as if the server died before storing it
	if err != nil {
		t.Fatal(err)
	}
	p4.start(t, string(a.SDP))
	s.waitFor(10*time.Second, "the orphan to publish", func() bool { return s.publisher(b) != "" })
	if err := s.out.CloseOrphans(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.waitFor(5*time.Second, "the orphan to close", func() bool { return s.publisher(b) == "" })
	p4.close()

	t.Log("an offer without audio does not make the broadcast live")
	s.expect(http.StatusOK, http.MethodPost, live, ana, "", "")
	p5, sdp := newPublisher(t, frames, false)
	res, answer = s.offer(ana, b, sdp)
	if res.StatusCode != http.StatusCreated {
		t.Fatal(res.StatusCode)
	}
	p5.start(t, answer)
	s.waitFor(10*time.Second, "the video-only session to publish", func() bool { return s.publisher(b) != "" })
	time.Sleep(8 * time.Second)
	if st := s.output(b).State; st != broadcast.Starting {
		t.Fatalf("a program without audio made the broadcast %s", st)
	}
	s.expect(http.StatusOK, http.MethodDelete, live, ana, "", "")
}
