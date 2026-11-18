// Package singlecheck is "A working studio" of packaging/single-smoke.sh: compiled with `go test -c` in the tools
// image and run inside the smoke container, as the user that runs `streamgo up`, against that running `up`. It
// skips unless SMOKE_URL is set, so `go test ./...` only compiles it.
package singlecheck

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
)

// env is the smoke script's input: the server, its public origin, the account the script created, the data
// directory of `up` (its streamgo.env and generated media/mediamtx.yml) and the RTMP address of a local sink.
type env struct {
	url, origin, email, password, dataDir, sink string
}

func readEnv(t *testing.T) env {
	t.Helper()
	e := env{os.Getenv("SMOKE_URL"), os.Getenv("SMOKE_ORIGIN"), os.Getenv("SMOKE_EMAIL"), os.Getenv("SMOKE_PASSWORD"),
		os.Getenv("SMOKE_DATA_DIR"), os.Getenv("SMOKE_SINK")}
	if e.url == "" {
		t.Skip("SMOKE_URL not set: this check runs inside packaging/single-smoke.sh")
	}
	return e
}

type client struct {
	t    *testing.T
	e    env
	http *http.Client
}

func (c *client) do(method, path, contentType, body string) (int, []byte) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, c.e.url+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Origin", c.e.origin)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

// json sends v as JSON, wants status and decodes the answer into out (when not nil).
func (c *client) json(status int, method, path string, v, out any) {
	c.t.Helper()
	body := ""
	if v != nil {
		b, _ := json.Marshal(v)
		body = string(b)
	}
	got, b := c.do(method, path, "application/json", body)
	if got != status {
		c.t.Fatalf("%s %s: %d %s, want %d", method, path, got, b, status)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			c.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

// envFile reads NAME=value lines of streamgo.env (the format config.EnsureEnvFile writes). Never printed.
func envFile(t *testing.T, dir string) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "streamgo.env")) //nolint:gosec // the smoke test's data directory
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok && !strings.HasPrefix(k, "#") {
			vals[k] = strings.Trim(v, `'"`)
		}
	}
	return vals
}

// rtspPort is MediaMTX's RTSP listener, from the configuration up generated for this start.
func rtspPort(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "media", "mediamtx.yml")) //nolint:gosec // the smoke test's data directory
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^rtspAddress: 127\.0\.0\.1:(\d+)$`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no rtspAddress in media/mediamtx.yml")
	}
	return string(m[1])
}

// vp8Frames encodes a short VP8 clip with the system's ffmpeg.
func vp8Frames(t *testing.T) [][]byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v.ivf")
	cmd := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=30", //nolint:noctx // a test
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

// publish sends the studio's program as the browser does (VP8 and Opus, every candidate in the offer) through
// StreamGo's WHIP route, and keeps sending until the test ends.
func publish(t *testing.T, c *client, broadcastID string) {
	t.Helper()
	frames := vp8Frames(t)
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop); _ = pc.Close() })
	sendonly := webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}
	video, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "program")
	if err != nil {
		t.Fatal(err)
	}
	audio, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "program")
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range []webrtc.TrackLocal{video, audio} {
		if _, err := pc.AddTransceiverFromTrack(tr, sendonly); err != nil {
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
	status, answer := c.do(http.MethodPost, "/api/v1/broadcasts/"+broadcastID+"/program/whip", "application/sdp", pc.LocalDescription().SDP)
	if status != http.StatusCreated || !bytes.HasPrefix(answer, []byte("v=0")) {
		t.Fatalf("WHIP offer through StreamGo: %d %s", status, answer)
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(answer)}); err != nil {
		t.Fatal(err)
	}
	go func() {
		v, a := time.NewTicker(time.Second/30), time.NewTicker(20*time.Millisecond)
		defer v.Stop()
		defer a.Stop()
		for i := 0; ; {
			select {
			case <-stop:
				return
			case <-v.C:
				_ = video.WriteSample(media.Sample{Data: frames[i%len(frames)], Duration: time.Second / 30})
				i++
			case <-a.C:
				_ = audio.WriteSample(media.Sample{Data: []byte{0xf8, 0xff, 0xfe}, Duration: 20 * time.Millisecond}) // Opus silence
			}
		}
	}()
}

// probe reads the program at MediaMTX's RTSP address with ffprobe and returns the codecs it found. The password
// is on ffprobe's command line inside the throwaway container only and is removed from anything printed.
func probe(ctx context.Context, rtsp, secret string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-rtsp_transport", "tcp", "-show_entries", "stream=codec_name",
		"-of", "csv=p=0", rtsp)
	out, err := cmd.CombinedOutput()
	return strings.ReplaceAll(strings.TrimSpace(string(out)), secret, "***"), err
}

// TestStudio: a client joins the broadcast's room with a token of the server, and a WHIP publish sent through
// StreamGo is readable at MediaMTX's RTSP address. The broadcast then goes live to a local RTMP sink.
func TestStudio(t *testing.T) {
	e := readEnv(t)
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, e: e, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	c.json(http.StatusOK, http.MethodPost, "/api/v1/auth/login", map[string]string{"email": e.email, "password": e.password}, nil)
	t.Log("signed in")

	var b struct{ ID string }
	c.json(http.StatusCreated, http.MethodPost, "/api/v1/broadcasts", map[string]string{"title": "Smoke"}, &b)

	var tok struct{ URL, Token string }
	c.json(http.StatusOK, http.MethodPost, "/api/v1/broadcasts/"+b.ID+"/studio-token", nil, &tok)
	room, err := lksdk.ConnectToRoomWithToken(tok.URL, tok.Token, lksdk.NewRoomCallback(),
		lksdk.WithAutoSubscribe(false), lksdk.WithConnectTimeout(15*time.Second))
	if err != nil {
		t.Fatalf("join the room at %s: %v", tok.URL, err)
	}
	t.Cleanup(room.Disconnect)
	t.Logf("joined room %s at %s as %s", room.Name(), tok.URL, room.LocalParticipant.Identity())

	// a local RTMP sink as the destination: going live needs one, and a WHIP publish needs a live broadcast
	sink, err := url.Parse(e.sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	listen := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-loglevel", "error", "-listen", "1", "-i", e.sink+"/smoke-key", "-f", "null", "-")
	if err := listen.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = listen.Wait() })
	var d struct{ ID string }
	c.json(http.StatusCreated, http.MethodPost, "/api/v1/destinations",
		map[string]string{"name": "Sink", "address": sink.String(), "key": "smoke-key"}, &d)
	c.json(http.StatusOK, http.MethodPut, "/api/v1/broadcasts/"+b.ID+"/destinations", map[string][]string{"ids": {d.ID}}, nil)
	c.json(http.StatusOK, http.MethodPost, "/api/v1/broadcasts/"+b.ID+"/live", nil, nil)

	publish(t, c, b.ID)
	t.Log("WHIP offer answered through StreamGo")

	vals := envFile(t, e.dataDir)
	user, secret := vals["STREAMGO_MEDIAMTX_USER"], vals["STREAMGO_MEDIAMTX_SECRET"]
	if user == "" || secret == "" {
		t.Fatal("streamgo.env lacks the MediaMTX credentials")
	}
	rtsp := (&url.URL{Scheme: "rtsp", User: url.UserPassword(user, secret), Host: "127.0.0.1:" + rtspPort(t, e.dataDir),
		Path: "/program/" + b.ID}).String()
	deadline := time.Now().Add(30 * time.Second)
	for {
		pctx, pcancel := context.WithTimeout(context.Background(), 15*time.Second)
		codecs, err := probe(pctx, rtsp, secret)
		pcancel()
		if err == nil && strings.Contains(codecs, "vp8") && strings.Contains(codecs, "opus") {
			t.Logf("RTSP read at 127.0.0.1:%s/program/%s: %s", rtspPort(t, e.dataDir), b.ID, strings.ReplaceAll(codecs, "\n", ", "))
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the program is not readable at MediaMTX's RTSP address: %v %s", err, codecs)
		}
		time.Sleep(time.Second)
	}

	for start := time.Now(); ; time.Sleep(time.Second) {
		var got struct {
			Output struct{ State, Reason string }
		}
		c.json(http.StatusOK, http.MethodGet, "/api/v1/broadcasts/"+b.ID, nil, &got)
		if got.Output.State == "live" {
			t.Logf("the broadcast is live to the RTMP sink after %s", time.Since(start).Round(time.Second))
			break
		}
		if time.Since(start) > 45*time.Second {
			t.Fatalf("the broadcast did not go live: %s %s", got.Output.State, got.Output.Reason)
		}
	}
	c.json(http.StatusOK, http.MethodDelete, "/api/v1/broadcasts/"+b.ID+"/live", nil, nil)
	fmt.Println("studio: join, WHIP through StreamGo, RTSP read and live to RTMP all passed")
}
