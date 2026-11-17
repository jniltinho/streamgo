package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/output/outputtest"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/httpserver/handlers"
)

func liveServer(t *testing.T) (*outputtest.Env, http.Handler) {
	t.Helper()
	e := outputtest.New(t)
	srv := httpserver.New(httpserver.Options{
		PublicOrigin: studioOrigin,
		Authenticate: func(c *echo.Context) (string, error) {
			if u := c.Request().Header.Get("X-Test-User"); u != "" {
				return u, nil
			}
			return "", httpserver.ErrUnauthenticated
		},
	})
	handlers.RegisterLive(srv.UserAPI(), handlers.LiveDeps{Output: e.Service})
	handlers.RegisterWHIP(srv.UserAPI(), handlers.LiveDeps{Output: e.Service})
	e.AddDestination(t, "dA", "u1", "YouTube", "")
	e.AddBroadcast(t, "b1", "u1", "dA")
	e.AddBroadcast(t, "empty", "u1")
	return e, srv.Handler()
}

func liveDo(t *testing.T, h http.Handler, method, path, user, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Origin", studioOrigin)
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	return body.Error.Code, body.Error.Message
}

func TestLiveRoutes(t *testing.T) {
	t.Parallel()
	e, h := liveServer(t)
	rec := liveDo(t, h, http.MethodPost, "/api/v1/broadcasts/empty/live", "u1", "", "")
	if code, msg := errorOf(t, rec); rec.Code != http.StatusConflict || code != "conflict" || !strings.Contains(msg, "destination") {
		t.Fatalf("no destination: %d %s", rec.Code, rec.Body)
	}
	if rec := liveDo(t, h, http.MethodPost, "/api/v1/broadcasts/b1/live", "u2", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("another user: %d", rec.Code)
	}
	rec = liveDo(t, h, http.MethodPost, "/api/v1/broadcasts/b1/live", "u1", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	o := got["output"]
	if o["state"] != "starting" || o["reason"] != "" || o["started_at"] != nil || o["deadline"] != nil || len(o) != 4 {
		t.Fatalf("output %v", o)
	}
	rec = liveDo(t, h, http.MethodPost, "/api/v1/broadcasts/b1/live", "u1", "", "")
	if code, msg := errorOf(t, rec); rec.Code != http.StatusConflict || code != "conflict" || !strings.Contains(msg, "already starting") {
		t.Fatalf("double go live: %d %s", rec.Code, rec.Body)
	}
	e.Processes.Last(t, "b1", "").Flow()
	rec = liveDo(t, h, http.MethodDelete, "/api/v1/broadcasts/b1/live", "u1", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"offline"`) {
		t.Fatalf("end: %d %s", rec.Code, rec.Body)
	}
}

func TestWHIPRoutes(t *testing.T) {
	t.Parallel()
	e, h := liveServer(t)
	const path = "/api/v1/broadcasts/b1/program/whip"
	if rec := liveDo(t, h, http.MethodPost, path, "u1", "application/sdp", "v=0"); rec.Code != http.StatusConflict {
		t.Fatalf("offline: %d %s", rec.Code, rec.Body)
	}
	if rec := liveDo(t, h, http.MethodPost, "/api/v1/broadcasts/b1/live", "u1", "", ""); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	for _, c := range []struct {
		user, contentType string
		status            int
	}{
		{"u2", "application/sdp", http.StatusNotFound},
		{"", "application/sdp", http.StatusUnauthorized},
		{"u1", "application/json", http.StatusUnsupportedMediaType},
	} {
		if rec := liveDo(t, h, http.MethodPost, path, c.user, c.contentType, "v=0"); rec.Code != c.status {
			t.Fatalf("%+v: %d %s", c, rec.Code, rec.Body)
		}
	}
	if _, _, calls := e.Ingest.Snapshot(); calls != 0 {
		t.Fatal("a refused offer reached the media server")
	}

	// the client cannot choose the upstream: headers naming another resource are ignored
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path+"?path=program/other", strings.NewReader("v=0 A"))
	req.Header.Set("Origin", studioOrigin)
	req.Header.Set("X-Test-User", "u1")
	req.Header.Set("Content-Type", "application/sdp")
	req.Header.Set("Location", "http://evil.example/program/other/whip/x")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	recA := httptest.NewRecorder()
	h.ServeHTTP(recA, req)
	if recA.Code != http.StatusCreated || recA.Body.String() != "answer to v=0 A" || recA.Header().Get("Content-Type") != "application/sdp" ||
		recA.Header().Get("ETag") != "*" || recA.Header().Get("Link") == "" {
		t.Fatalf("offer A: %d %v %s", recA.Code, recA.Header(), recA.Body)
	}
	locA := recA.Header().Get("Location")
	if !strings.HasPrefix(locA, path+"/sessions/") {
		t.Fatalf("Location %q", locA)
	}
	if e.Ingest.PublisherOf("b1") == "" || e.Ingest.PublisherOf("other") != "" {
		t.Fatal("the offer did not go to the broadcast's own path")
	}

	recB := liveDo(t, h, http.MethodPost, path, "u1", "application/sdp; charset=utf-8", "v=0 B")
	locB := recB.Header().Get("Location")
	if recB.Code != http.StatusCreated || locB == locA {
		t.Fatalf("offer B: %d %s", recB.Code, recB.Body)
	}
	if rec := liveDo(t, h, http.MethodPatch, locB, "u1", "application/trickle-ice-sdpfrag", "a=x"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	if rec := liveDo(t, h, http.MethodDelete, locA, "u1", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("late DELETE of A: %d", rec.Code)
	}
	if rec := liveDo(t, h, http.MethodDelete, locB, "u2", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE by another user: %d", rec.Code)
	}
	if e.Ingest.PublisherOf("b1") == "" {
		t.Fatal("B stopped publishing")
	}
	if rec := liveDo(t, h, http.MethodDelete, locB, "u1", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE of B: %d %s", rec.Code, rec.Body)
	}

	// media server error: 502, and End answers at once
	e.Ingest.Hook = func(string) error { return errors.New("down") }
	rec := liveDo(t, h, http.MethodPost, path, "u1", "application/sdp", "v=0")
	if code, _ := errorOf(t, rec); rec.Code != http.StatusBadGateway || code != "bad_gateway" {
		t.Fatalf("upstream error: %d %s", rec.Code, rec.Body)
	}
	if rec := liveDo(t, h, http.MethodDelete, "/api/v1/broadcasts/b1/live", "u1", "", ""); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	// an offer larger than 64 kB
	if rec := liveDo(t, h, http.MethodPost, path, "u1", "application/sdp", strings.Repeat("a", 65<<10)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large offer: %d", rec.Code)
	}
}
