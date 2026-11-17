package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

const origin = "https://studio.example.com"

var dist = fstest.MapFS{
	"index.html":           {Data: []byte(`<!doctype html><title>StreamGo</title>`)},
	"assets/app-abc123.js": {Data: []byte("console.log(1)")},
	"favicon.svg":          {Data: []byte("<svg/>")},
}

// testServer is a server with test-only routes and a log buffer; created counts the
// broadcasts the test route "created".
type testServer struct {
	*httpserver.Server
	log     *bytes.Buffer
	created atomic.Int32
}

func newServer(t *testing.T, mutate ...func(*httpserver.Options)) *testServer {
	t.Helper()
	ts := &testServer{log: &bytes.Buffer{}}
	o := httpserver.Options{
		PublicOrigin: origin,
		Dist:         dist,
		Logger:       slog.New(slog.NewJSONHandler(ts.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Authenticate: func(*echo.Context) (string, error) { return "ana", nil },
		BodyLimits:   map[string]int64{"POST /api/v1/broadcasts/:id/logo": 2 << 20},
	}
	for _, m := range mutate {
		m(&o)
	}
	ts.Server = httpserver.New(o)
	ts.UserAPI().POST("/broadcasts", func(c *echo.Context) error {
		var body map[string]any
		if err := c.Bind(&body); err != nil {
			return err
		}
		ts.created.Add(1)
		return c.JSON(http.StatusCreated, map[string]string{"owner": httpserver.UserID(c)})
	})
	ts.UserAPI().POST("/broadcasts/:id/logo", func(c *echo.Context) error {
		n, err := io.Copy(io.Discard, c.Request().Body)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]int64{"bytes": n})
	})
	ts.API().GET("/join/:token", func(c *echo.Context) error { return c.JSON(http.StatusOK, "Show") })
	ts.API().GET("/ip", func(c *echo.Context) error { return c.String(http.StatusOK, c.RealIP()) })
	ts.API().GET("/fail/:kind", func(c *echo.Context) error {
		switch c.Param("kind") {
		case "missing":
			return fmt.Errorf("broadcast b1: %w", core.ErrNotFound)
		case "conflict":
			return fmt.Errorf("delete: %w", core.ErrConflict)
		case "invalid":
			return core.Invalid("title", "must not be empty")
		}
		return errors.New("boom")
	})
	return ts
}

func (ts *testServer) do(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	ts.Handler().ServeHTTP(w, r)
	return w
}

func request(method, target, body string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return r
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Message == "" {
		t.Fatalf("not the error envelope (%d): %q", w.Code, w.Body.String())
	}
	return body.Error.Code
}

func TestRoutes(t *testing.T) {
	t.Parallel()
	ts := newServer(t)
	tests := []struct {
		name     string
		req      *http.Request
		status   int
		code     string // error code; "" for success
		contains string // in the body, for success
	}{
		{"unknown API route", request(http.MethodGet, "/api/v1/nope", ""), 404, "not_found", ""},
		{"unknown API route, post", request(http.MethodPost, "/api/v1/nope", "{}", "Origin", origin), 404, "not_found", ""},
		{"unknown API version", request(http.MethodGet, "/api/v2/broadcasts", ""), 404, "not_found", ""},
		{"wrong method", request(http.MethodDelete, "/api/v1/ip", "", "Origin", origin), 405, "method_not_allowed", ""},
		{"not found from the core", request(http.MethodGet, "/api/v1/fail/missing", ""), 404, "not_found", ""},
		{"conflict from the core", request(http.MethodGet, "/api/v1/fail/conflict", ""), 409, "conflict", ""},
		{"invalid input from the core", request(http.MethodGet, "/api/v1/fail/invalid", ""), 422, "invalid_input", ""},
		{"internal error", request(http.MethodGet, "/api/v1/fail/other", ""), 500, "internal", ""},
		{"client route", request(http.MethodGet, "/studio/01JABC", ""), 200, "", "<title>StreamGo</title>"},
		{"join route", request(http.MethodGet, "/join/sometoken", ""), 200, "", "<title>StreamGo</title>"},
		{"root", request(http.MethodGet, "/", ""), 200, "", "<title>StreamGo</title>"},
		{"asset", request(http.MethodGet, "/assets/app-abc123.js", ""), 200, "", "console.log"},
		{"missing asset", request(http.MethodGet, "/assets/gone.js", ""), 404, "not_found", ""},
		{"healthz", request(http.MethodGet, "/healthz", ""), 200, "", `"ok"`},
		{"readyz", request(http.MethodGet, "/readyz", ""), 200, "", `"ready"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := ts.do(tt.req)
			if w.Code != tt.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tt.status, w.Body)
			}
			if tt.code != "" {
				if got := errorCode(t, w); got != tt.code {
					t.Fatalf("code %q, want %q", got, tt.code)
				}
			} else if !strings.Contains(w.Body.String(), tt.contains) {
				t.Fatalf("body %q lacks %q", w.Body, tt.contains)
			}
		})
	}
}

// TestCrossOrigin covers "Cross-origin post" and "Missing or null origin".
func TestCrossOrigin(t *testing.T) {
	t.Parallel()
	ts := newServer(t)
	for _, o := range []string{"https://evil.example", "", "null", "http://studio.example.com"} {
		r := request(http.MethodPost, "/api/v1/broadcasts", `{"title":"x"}`)
		if o != "" {
			r.Header.Set("Origin", o)
		}
		w := ts.do(r)
		if w.Code != http.StatusForbidden || errorCode(t, w) != "cross_origin" {
			t.Errorf("origin %q: %d %s", o, w.Code, w.Body)
		}
	}
	for _, m := range []string{http.MethodPatch, http.MethodDelete, http.MethodPut} {
		if w := ts.do(request(m, "/api/v1/broadcasts/b1", "{}")); w.Code != http.StatusForbidden {
			t.Errorf("%s without origin: %d", m, w.Code)
		}
	}
	if n := ts.created.Load(); n != 0 {
		t.Fatalf("%d created by refused requests", n)
	}
	if w := ts.do(request(http.MethodPost, "/api/v1/broadcasts", `{"title":"x"}`, "Origin", origin)); w.Code != http.StatusCreated ||
		!strings.Contains(w.Body.String(), `"ana"`) {
		t.Fatalf("same origin: %d %s", w.Code, w.Body)
	}
}

// TestBodyLimit covers "Oversized body": over 64 kB is refused with 413 before the handler
// processes it, declared or not; a route with its own limit takes more.
func TestBodyLimit(t *testing.T) {
	t.Parallel()
	ts := newServer(t)
	big := `{"title":"` + strings.Repeat("x", httpserver.DefaultBodyLimit) + `"}`
	declared := request(http.MethodPost, "/api/v1/broadcasts", big, "Origin", origin)
	chunked := request(http.MethodPost, "/api/v1/broadcasts", big, "Origin", origin)
	chunked.ContentLength = -1
	unknown := request(http.MethodPost, "/api/v1/nope", big, "Origin", origin)
	for name, r := range map[string]*http.Request{"declared": declared, "chunked": chunked} {
		if w := ts.do(r); w.Code != http.StatusRequestEntityTooLarge || errorCode(t, w) != "too_large" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body)
		}
	}
	if w := ts.do(unknown); w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusNotFound {
		t.Errorf("unknown route: %d", w.Code)
	}
	if n := ts.created.Load(); n != 0 {
		t.Fatalf("%d processed", n)
	}
	logo := request(http.MethodPost, "/api/v1/broadcasts/b1/logo", strings.Repeat("x", 1<<20), "Origin", origin)
	if w := ts.do(logo); w.Code != http.StatusOK {
		t.Fatalf("1 MB logo: %d %s", w.Code, w.Body)
	}
	logo = request(http.MethodPost, "/api/v1/broadcasts/b1/logo", strings.Repeat("x", 3<<20), "Origin", origin)
	if w := ts.do(logo); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("3 MB logo: %d", w.Code)
	}
}

// TestClientAddress covers "Spoofed forwarding header": without a trusted proxy the
// connection's own address counts; behind a trusted one, the forwarded address does.
func TestClientAddress(t *testing.T) {
	t.Parallel()
	ask := func(ts *testServer) string {
		r := request(http.MethodGet, "/api/v1/ip", "", "X-Forwarded-For", "203.0.113.9")
		r.RemoteAddr = "192.0.2.1:4321"
		return ts.do(r).Body.String()
	}
	if got := ask(newServer(t)); got != "192.0.2.1" {
		t.Fatalf("no trusted proxy: %q", got)
	}
	trusted := newServer(t, func(o *httpserver.Options) { o.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")} })
	if got := ask(trusted); got != "203.0.113.9" {
		t.Fatalf("trusted proxy: %q", got)
	}
}

// TestNothingSensitive covers "Nothing sensitive in logs or caches": the log names the route
// pattern, never the token, path or query, and API responses (errors too) are not cached.
func TestNothingSensitive(t *testing.T) {
	t.Parallel()
	ts := newServer(t)
	const token, query = "inv1teT0kenSecret", "qu3rySecret"
	for _, target := range []string{
		"/api/v1/join/" + token + "?s=" + query,
		"/join/" + token + "?s=" + query, // the client route of the same link
		"/api/v1/nope/" + token,
	} {
		w := ts.do(request(http.MethodGet, target, ""))
		if strings.HasPrefix(target, "/api/") && w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control %q", target, w.Header().Get("Cache-Control"))
		}
	}
	w := ts.do(request(http.MethodPost, "/api/v1/broadcasts", "{}", "Origin", "https://evil.example"))
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("error response: Cache-Control %q", w.Header().Get("Cache-Control"))
	}
	log := ts.log.String()
	if strings.Contains(log, token) || strings.Contains(log, query) {
		t.Fatalf("the log carries the token or the query:\n%s", log)
	}
	if !strings.Contains(log, `"route":"/api/v1/join/:token"`) {
		t.Fatalf("the log does not name the route pattern:\n%s", log)
	}
}

// TestReadiness: /readyz names what is not ready and answers 503; /healthz still answers 200.
func TestReadiness(t *testing.T) {
	t.Parallel()
	ts := newServer(t, func(o *httpserver.Options) {
		o.ReadyChecks = []httpserver.Check{
			{Name: "database", Check: func(context.Context) error { return nil }},
			{Name: "mediamtx", Check: func(context.Context) error { return errors.New("connection refused") }},
		}
	})
	w := ts.do(request(http.MethodGet, "/readyz", ""))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"mediamtx":"connection refused"`) {
		t.Fatalf("readyz: %d %s", w.Code, w.Body)
	}
	if w := ts.do(request(http.MethodGet, "/healthz", "")); w.Code != http.StatusOK {
		t.Fatalf("healthz: %d", w.Code)
	}
}

// TestSession: UserAPI routes run the authenticator; without one, or when it refuses, 401.
func TestSession(t *testing.T) {
	t.Parallel()
	post := func() *http.Request { return request(http.MethodPost, "/api/v1/broadcasts", "{}", "Origin", origin) }
	none := newServer(t, func(o *httpserver.Options) { o.Authenticate = nil })
	if w := none.do(post()); w.Code != http.StatusUnauthorized || errorCode(t, w) != "unauthenticated" {
		t.Fatalf("no authenticator: %d %s", w.Code, w.Body)
	}
	refused := newServer(t, func(o *httpserver.Options) {
		o.Authenticate = func(*echo.Context) (string, error) { return "", httpserver.ErrUnauthenticated }
	})
	if w := refused.do(post()); w.Code != http.StatusUnauthorized {
		t.Fatalf("refused: %d", w.Code)
	}
	if w := refused.do(request(http.MethodGet, "/api/v1/nope", "")); w.Code != http.StatusNotFound {
		t.Fatalf("an unknown route behind no session: %d", w.Code)
	}
}

// TestNoReferrer: every response, the join page's included, forbids the Referer header, so the invite
// token in the page's address is not sent along with the requests the page makes.
func TestNoReferrer(t *testing.T) {
	t.Parallel()
	ts := newServer(t)
	for _, target := range []string{"/join/inv1teT0ken", "/api/v1/join/inv1teT0ken", "/broadcasts", "/healthz"} {
		w := ts.do(request(http.MethodGet, target, ""))
		if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy %q, want no-referrer", target, got)
		}
	}
}
