package httpserver_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jniltinho/streamgo/internal/httpserver"
)

// seen is what the fake LiveKit received.
type seen struct{ path, query, host, cookie, auth string }

// fakeUpstream is LiveKit: it records each request and echoes a WebSocket-style upgraded stream.
func fakeUpstream(t *testing.T) (*url.URL, chan seen) {
	t.Helper()
	got := make(chan seen, 8)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- seen{r.URL.Path, r.URL.RawQuery, r.Host, r.Header.Get("Cookie"), r.Header.Get("Authorization")}
		if r.Header.Get("Upgrade") != "websocket" {
			_, _ = io.WriteString(w, "livekit")
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		_, _ = io.Copy(conn, rw) // echo
	}))
	t.Cleanup(up.Close)
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u, got
}

func withLiveKit(u *url.URL) func(*httpserver.Options) {
	return func(o *httpserver.Options) { o.LiveKitAPIURL = u }
}

// TestLiveKitProxy is "Only the configured LiveKit": whatever the path, Host or headers name, the request reaches
// livekit.api_url with the /livekit prefix stripped, and never the web app.
func TestLiveKitProxy(t *testing.T) {
	t.Parallel()
	up, got := fakeUpstream(t)
	ts := newServer(t, withLiveKit(up))
	tests := []struct {
		name, target, wantPath string
		headers                []string
	}{
		{"signalling", "/livekit/rtc?access_token=tok", "/rtc", nil},
		{"another host named", "http://evil.example/livekit/rtc?access_token=tok", "/rtc",
			[]string{"X-Forwarded-Host", "evil.example", "Forwarded", "host=evil.example"}},
		{"host in the path", "/livekit//evil.example/rtc", "//evil.example/rtc", nil},
		{"dot segments", "/livekit/../api/v1/me", "/../api/v1/me", nil},
		{"credentials pass, the session does not", "/livekit/twirp/livekit.RoomService/ListRooms", "/twirp/livekit.RoomService/ListRooms",
			[]string{"Cookie", "streamgo_session=s3ss10n", "Authorization", "Bearer lk"}},
	}
	for _, tt := range tests {
		r := request(http.MethodGet, tt.target, "", tt.headers...)
		w := ts.do(r)
		if w.Code != http.StatusOK || w.Body.String() != "livekit" {
			t.Errorf("%s: %d %q, want LiveKit's answer", tt.name, w.Code, w.Body.String())
			continue
		}
		s := <-got
		if s.path != tt.wantPath || s.host != up.Host || s.cookie != "" {
			t.Errorf("%s: upstream saw %+v, want path %q on %s without cookie", tt.name, s, tt.wantPath, up.Host)
		}
		if tt.name == "signalling" && s.query != "access_token=tok" {
			t.Errorf("query not passed: %q", s.query)
		}
		if strings.HasPrefix(tt.name, "credentials") && s.auth != "Bearer lk" {
			t.Errorf("LiveKit's own credentials not passed: %q", s.auth)
		}
	}
	if strings.Contains(ts.log.String(), "tok") {
		t.Errorf("the log carries the token:\n%s", ts.log.String())
	}

	// without a LiveKit the path is a client route of the web app, as any other
	plain := newServer(t)
	if w := plain.do(request(http.MethodGet, "/livekit/rtc", "")); !strings.Contains(w.Body.String(), "<title>StreamGo") {
		t.Errorf("no proxy configured: %d %q", w.Code, w.Body.String())
	}
}

// TestLiveKitProxyUpgrade: a WebSocket upgrade passes through, both ways.
func TestLiveKitProxyUpgrade(t *testing.T) {
	t.Parallel()
	up, got := fakeUpstream(t)
	srv := httptest.NewServer(newServer(t, withLiveKit(up)).Handler())
	defer srv.Close()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = io.WriteString(conn, "GET /livekit/rtc?access_token=tok HTTP/1.1\r\nHost: studio.example.com\r\n"+
		"Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade: %v %v", resp, err)
	}
	_ = resp.Body.Close() // empty for a 101: the stream continues on conn
	if s := <-got; s.path != "/rtc" {
		t.Errorf("upstream path %q", s.path)
	}
	_, _ = io.WriteString(conn, "ping")
	buf := make([]byte, 4)
	if _, err := io.ReadFull(br, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo through the upgraded connection: %q %v", buf, err)
	}
}

// TestLiveKitProxyDown: LiveKit unreachable answers 502 and logs the cause, never the URL with its token.
func TestLiveKitProxyDown(t *testing.T) {
	t.Parallel()
	up, _ := fakeUpstream(t)
	down := *up
	down.Host = "127.0.0.1:1"
	ts := newServer(t, withLiveKit(&down))
	if w := ts.do(request(http.MethodGet, "/livekit/rtc?access_token=s3cr3tT0ken", "")); w.Code != http.StatusBadGateway {
		t.Errorf("status %d", w.Code)
	}
	if log := ts.log.String(); strings.Contains(log, "s3cr3tT0ken") || !strings.Contains(log, "livekit proxy") {
		t.Errorf("log:\n%s", log)
	}
}
