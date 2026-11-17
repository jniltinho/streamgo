package handlers_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/core/studio/studiotest"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/httpserver/handlers"
)

const studioOrigin = "https://studio.example.test"

// limiter is a minimal fixed-window stand-in for internal/ratelimit.
type limiter struct {
	mu  sync.Mutex
	n   map[string]int
	max int
}

func (l *limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n == nil {
		l.n = map[string]int{}
	}
	l.n[key]++
	return l.n[key] <= l.max
}

type harness struct {
	*studiotest.Env
	h http.Handler
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	e := studiotest.New(t)
	srv := httpserver.New(httpserver.Options{
		PublicOrigin: studioOrigin,
		BodyLimits:   handlers.LogoBodyLimits,
		Authenticate: func(c *echo.Context) (string, error) {
			if u := c.Request().Header.Get("X-Test-User"); u != "" {
				return u, nil
			}
			return "", httpserver.ErrUnauthenticated
		},
	})
	handlers.RegisterStudio(srv.UserAPI(), handlers.StudioDeps{Studio: e.Service})
	handlers.RegisterJoin(srv.API(), handlers.JoinDeps{Studio: e.Service, Limiter: &limiter{max: 20}, SecureCookies: true})
	handlers.RegisterAssets(srv.Handler().(*echo.Echo), e.Service)
	return &harness{Env: e, h: srv.Handler()}
}

type call struct {
	method, path, user, body, contentType string
	cookies                               []*http.Cookie
	ip                                    string
}

func (h *harness) do(t *testing.T, c call) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), c.method, c.path, strings.NewReader(c.body))
	req.Header.Set("Origin", studioOrigin)
	if c.contentType == "" {
		c.contentType = "application/json"
	}
	req.Header.Set("Content-Type", c.contentType)
	if c.user != "" {
		req.Header.Set("X-Test-User", c.user)
	}
	if c.ip != "" {
		req.RemoteAddr = c.ip + ":1234"
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

type stageBody struct {
	Stage stage.State `json:"stage"`
}

type errBody struct {
	Error struct{ Code, Message, Field string } `json:"error"`
}

func want(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	if code != "" {
		if got := decode[errBody](t, rec).Error.Code; got != code {
			t.Fatalf("code %q, want %q", got, code)
		}
	}
}

func wantField(t *testing.T, rec *httptest.ResponseRecorder, field string) {
	t.Helper()
	want(t, rec, http.StatusUnprocessableEntity, "invalid_input")
	if got := decode[errBody](t, rec).Error.Field; got != field {
		t.Fatalf("field %q, want %q", got, field)
	}
}

func TestStudioToken(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	rec := h.do(t, call{method: "POST", path: "/api/v1/broadcasts/b1/studio-token", user: "u1"})
	want(t, rec, 200, "")
	var j map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &j)
	if j["url"] != "wss://lk.example.test" || j["identity"] != "host-u1" || j["token"] == "" || len(j) != 3 {
		t.Fatalf("join %v", j)
	}
	want(t, h.do(t, call{method: "POST", path: "/api/v1/broadcasts/b1/studio-token"}), 401, "unauthenticated")
	want(t, h.do(t, call{method: "POST", path: "/api/v1/broadcasts/b1/studio-token", user: "u2"}), 404, "not_found")
}

// Requirement "Changing the stage" through the route.
func TestPatchStageRoute(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	rafael := h.JoinGuest(t, "invite-b1", "Rafael", "")
	patch := func(user, body string) *httptest.ResponseRecorder {
		return h.do(t, call{method: "PATCH", path: "/api/v1/broadcasts/b1/stage", user: user, body: body})
	}

	rec := patch("u1", `{"sources":[{"p":"host-u1","k":"camera"},{"p":"`+rafael.Identity+`","k":"camera"}],"layout":"spotlight"}`)
	want(t, rec, 200, "")
	var raw map[string]map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	for _, k := range []string{"v", "on_air", "layout", "sources", "banner", "logo", "accent", "hide_names"} {
		if _, ok := raw["stage"][k]; !ok {
			t.Fatalf("stage without %q: %s", k, rec.Body.String())
		}
	}
	s := decode[stageBody](t, rec).Stage
	if s.V != 2 || len(s.Sources) != 2 || s.Layout != "spotlight" {
		t.Fatalf("stage %+v", s)
	}
	// "Name tags off": the patch hides them, and nothing else changes
	rec = patch("u1", `{"hide_names":true}`)
	want(t, rec, 200, "")
	if s = decode[stageBody](t, rec).Stage; !s.HideNames || s.V != 3 || len(s.Sources) != 2 || s.Layout != "spotlight" {
		t.Fatalf("hide names: %+v", s)
	}

	for body, field := range map[string]string{
		`{"logo":"/assets/x.png"}`:                   "logo",
		`{"layout":"mosaic"}`:                        "layout",
		`{"layout":null}`:                            "layout",
		`{"accent":"#abc"}`:                          "accent",
		`{"hide_names":"yes"}`:                       "hide_names",
		`{"hide_names":null}`:                        "hide_names",
		`{"banner_id":"nope"}`:                       "banner_id",
		`{"sources":[{"p":"guest-x","k":"camera"}]}`: "sources",
		`{"sources":[{"p":"host-u1","k":"camera"},{"p":"host-u1","k":"camera"}]}`: "sources",
		`{"sources":"all"}`: "sources",
		`{"v":99}`:          "v",
		`{"on_air":true}`:   "on_air",
		`{"sources":[{"p":"host-u1","k":"camera"},{"p":"host-u1","k":"screen"},{"p":"` + rafael.Identity + `","k":"camera"},{"p":"` + rafael.Identity + `","k":"screen"}]}`: "sources",
	} {
		wantField(t, patch("u1", body), field)
	}
	want(t, patch("u1", `not json`), 400, "bad_request")
	// a guest, or anyone without the owner's session, cannot change it
	want(t, patch("", `{"layout":"solo"}`), 401, "unauthenticated")
	want(t, patch("u2", `{"layout":"solo"}`), 404, "not_found")
	if got := h.Stored(t, "b1"); got.JSON() != s.JSON() {
		t.Fatalf("refused patches changed the stage: %s", got.JSON())
	}

	// removing a guest answers the new stage
	rec = h.do(t, call{method: "DELETE", path: "/api/v1/broadcasts/b1/participants/" + rafael.Identity, user: "u1"})
	want(t, rec, 200, "")
	if s := decode[stageBody](t, rec).Stage; len(s.Sources) != 1 || s.V != 4 || !s.HideNames {
		t.Fatalf("after removal %+v", s)
	}
	wantField(t, h.do(t, call{method: "DELETE", path: "/api/v1/broadcasts/b1/participants/host-u1", user: "u1"}), "identity")
}

// Requirement "Banners" through the routes.
func TestBannerRoutes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	rec := h.do(t, call{method: "POST", path: "/api/v1/broadcasts/b1/banners", user: "u1", body: `{"text":"Q3 roadmap: what ships next"}`})
	want(t, rec, 201, "")
	bn := decode[studio.Banner](t, rec)
	if bn.ID == "" || bn.Text != "Q3 roadmap: what ships next" || strings.Contains(rec.Body.String(), "BroadcastID") {
		t.Fatalf("banner %s", rec.Body.String())
	}
	wantField(t, h.do(t, call{method: "POST", path: "/api/v1/broadcasts/b1/banners", user: "u1", body: `{"text":""}`}), "text")
	rec = h.do(t, call{method: "GET", path: "/api/v1/broadcasts/b1/banners", user: "u1"})
	want(t, rec, 200, "")
	if list := decode[map[string][]studio.Banner](t, rec)["banners"]; len(list) != 1 || list[0] != bn {
		t.Fatalf("list %s", rec.Body.String())
	}

	rec = h.do(t, call{method: "PATCH", path: "/api/v1/broadcasts/b1/stage", user: "u1", body: `{"banner_id":"` + bn.ID + `"}`})
	if s := decode[stageBody](t, rec).Stage; s.Banner == nil || *s.Banner != (stage.Banner{ID: bn.ID, Text: bn.Text}) {
		t.Fatalf("shown %s", rec.Body.String())
	}
	rec = h.do(t, call{method: "DELETE", path: "/api/v1/broadcasts/b1/banners/" + bn.ID, user: "u1"})
	want(t, rec, 200, "")
	if s := decode[stageBody](t, rec).Stage; s.Banner != nil {
		t.Fatalf("still shown %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"banner":null`) {
		t.Fatalf("banner key %s", rec.Body.String())
	}
	want(t, h.do(t, call{method: "DELETE", path: "/api/v1/broadcasts/b1/banners/" + bn.ID, user: "u1"}), 404, "not_found")
}

func pngOf(t *testing.T, w, h int, noise bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if noise {
		r := rand.New(rand.NewPCG(1, 2))
		for i := range img.Pix {
			img.Pix[i] = byte(r.Uint32())
		}
	} else {
		img.Set(0, 0, color.White)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func multipartBody(t *testing.T, field, filename string, data []byte) (string, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	return b.String(), mw.FormDataContentType()
}

func (h *harness) upload(t *testing.T, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBody(t, "file", filename, data)
	return h.do(t, call{method: "POST", path: "/api/v1/broadcasts/b1/logo", user: "u1", body: body, contentType: ct})
}

// A 1x1 lossless WebP.
const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

// Requirement "Logo": "Upload", "Not an image", "Too large", "Replace".
func TestLogoRoutes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	big := pngOf(t, 280, 280, true)
	if len(big) < 300_000 {
		t.Fatalf("the test PNG has %d bytes", len(big))
	}
	rec := h.upload(t, "logo.png", big)
	want(t, rec, 200, "")
	s := decode[stageBody](t, rec).Stage
	if s.Logo == nil || !strings.HasPrefix(*s.Logo, "/assets/") {
		t.Fatalf("logo %v", s.Logo)
	}
	first := *s.Logo
	get := h.do(t, call{method: "GET", path: first})
	want(t, get, 200, "")
	if get.Header().Get("Content-Type") != "image/png" || get.Header().Get("X-Content-Type-Options") != "nosniff" || !bytes.Equal(get.Body.Bytes(), big) {
		t.Fatalf("served %s, %s, %d bytes", get.Header().Get("Content-Type"), get.Header().Get("X-Content-Type-Options"), get.Body.Len())
	}

	truncated := pngOf(t, 64, 64, true)
	var jpg bytes.Buffer
	_ = jpeg.Encode(&jpg, image.NewGray(image.Rect(0, 0, 4, 4)), nil)
	for name, tc := range map[string]struct {
		filename string
		data     []byte
		status   int
	}{
		"svg":                 {"logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 422},
		"text named png":      {"logo.png", []byte("not an image at all"), 422},
		"gif":                 {"logo.gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), 422},
		"truncated png":       {"logo.png", truncated[:len(truncated)/2], 422},
		"3000 pixels wide":    {"logo.png", pngOf(t, 3000, 1, false), 422},
		"3000 pixels high":    {"logo.png", pngOf(t, 1, 3000, false), 422},
		"over 2 MB, the file": {"logo.png", append(pngOf(t, 1, 1, false), make([]byte, handlers.MaxLogoBytes)...), 413},
		"over the body limit": {"logo.png", make([]byte, handlers.MaxLogoBytes+64<<10), 413},
	} {
		rec := h.upload(t, tc.filename, tc.data)
		if rec.Code != tc.status {
			t.Fatalf("%s: status %d, want %d: %s", name, rec.Code, tc.status, rec.Body.String())
		}
		if st := h.Stored(t, "b1"); st.Logo == nil || *st.Logo != first {
			t.Fatalf("%s replaced the logo: %v", name, st.Logo)
		}
	}
	want(t, h.do(t, call{method: "POST", path: "/api/v1/broadcasts/b1/logo", user: "u1", body: "{}"}), 415, "unsupported_media_type")
	body, ct := multipartBody(t, "other", "x.png", big)
	wantField(t, h.do(t, call{method: "POST", path: "/api/v1/broadcasts/b1/logo", user: "u1", body: body, contentType: ct}), "file")

	// JPEG and WebP are accepted with their own content type; each replaces the previous file
	webp, _ := base64.StdEncoding.DecodeString(webp1x1)
	prev := first
	for _, tc := range []struct {
		data []byte
		ct   string
	}{{jpg.Bytes(), "image/jpeg"}, {webp, "image/webp"}} {
		rec := h.upload(t, "logo", tc.data)
		want(t, rec, 200, "")
		url := *decode[stageBody](t, rec).Stage.Logo
		if got := h.do(t, call{method: "GET", path: url}); got.Code != 200 || got.Header().Get("Content-Type") != tc.ct {
			t.Fatalf("%s served as %d %s", tc.ct, got.Code, got.Header().Get("Content-Type"))
		}
		want(t, h.do(t, call{method: "GET", path: prev}), 404, "")
		prev = url
	}

	rec = h.do(t, call{method: "DELETE", path: "/api/v1/broadcasts/b1/logo", user: "u1"})
	want(t, rec, 200, "")
	if !strings.Contains(rec.Body.String(), `"logo":null`) {
		t.Fatalf("after delete %s", rec.Body.String())
	}
	want(t, h.do(t, call{method: "GET", path: prev}), 404, "")
	want(t, h.do(t, call{method: "GET", path: "/assets/logos/..%2Fstreamgo.db"}), 404, "")
}

// Requirements "Guests join by invite link" and "Guest sessions" through the routes.
func TestJoinRoutes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.AddBroadcast("b2", "u1", "invite-b2")
	join := func(token, name, ip string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		return h.do(t, call{method: "POST", path: "/api/v1/join/" + token, body: fmt.Sprintf(`{"name":%q}`, name), ip: ip, cookies: cookies})
	}

	rec := h.do(t, call{method: "GET", path: "/api/v1/join/invite-b1"})
	want(t, rec, 200, "")
	if rec.Body.String() != "{\"title\":\"Show b1\"}\n" {
		t.Fatalf("info %s", rec.Body.String())
	}
	// "Invalid link": unknown and rotated tokens answer 404 the same way
	unknownGet, unknownPost := h.do(t, call{method: "GET", path: "/api/v1/join/nope"}), join("nope", "Rafael", "")
	want(t, unknownGet, 404, "not_found")
	want(t, unknownPost, 404, "not_found")

	rec = join("invite-b1", "Rafael Lima", "")
	want(t, rec, 200, "")
	var j map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &j)
	if j["title"] != "Show b1" || j["url"] == "" || j["token"] == "" || !strings.HasPrefix(j["identity"], "guest-") {
		t.Fatalf("join %v", j)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	ck := cookies[0]
	if ck.Name != "streamgo_guest_b1" || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode ||
		ck.Path != "/api/v1/join" || ck.MaxAge != 12*3600 || ck.Value == "" {
		t.Fatalf("cookie %+v", ck)
	}
	if g, _ := h.Guests.FindByCookie(context.Background(), "b1", studio.Hash(ck.Value)); g == nil || g.CookieHash == ck.Value {
		t.Fatal("the cookie is not stored as a hash")
	}

	// "Reload keeps the identity"; "Two broadcasts"
	rec = join("invite-b1", "Rafael Lima", "", ck)
	if decode[map[string]string](t, rec)["identity"] != j["identity"] {
		t.Fatalf("reload gave %s", rec.Body.String())
	}
	rec = join("invite-b2", "Rafael Lima", "", ck)
	if id := decode[map[string]string](t, rec)["identity"]; id == j["identity"] || rec.Result().Cookies()[0].Name != "streamgo_guest_b2" {
		t.Fatalf("b2 join %s", rec.Body.String())
	}

	wantField(t, join("invite-b1", strings.Repeat("x", 41), ""), "name")

	// at most 20 joins a minute per address
	for range 20 {
		want(t, join("invite-b1", "Flood", "203.0.113.9"), 200, "")
	}
	want(t, join("invite-b1", "Flood", "203.0.113.9"), 429, "rate_limited")
	want(t, join("invite-b1", "Other address", "203.0.113.10"), 200, "")
}

// Scenario "Too many guests" through the route.
// Scenario "Studio full": the join route says so, and creates no guest.
func TestJoinRouteRoomFull(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for i := range h.MaxInStudio {
		j := h.JoinGuest(t, "invite-b1", fmt.Sprint("guest ", i), "")
		if err := h.Rooms.Connect("bc_b1", j.Identity, h.Clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	n := h.Guests.Len()
	rec := h.do(t, call{method: "POST", path: "/api/v1/join/invite-b1", body: `{"name":"late"}`})
	want(t, rec, 409, "studio_full")
	if h.Guests.Len() != n {
		t.Fatalf("%d guests, want %d", h.Guests.Len(), n)
	}
}

func TestJoinRouteFull(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	b, _ := h.Service.Invite(context.Background(), "invite-b1")
	for i := h.Guests.Len(); i < studio.MaxGuests; i++ {
		if _, err := h.Service.JoinAsGuest(context.Background(), b, "g", ""); err != nil {
			t.Fatal(err)
		}
	}
	rec := h.do(t, call{method: "POST", path: "/api/v1/join/invite-b1", body: `{"name":"one more"}`})
	want(t, rec, 429, "rate_limited")
	if n := h.Guests.Len(); n != studio.MaxGuests {
		t.Fatalf("%d guests", n)
	}
}
