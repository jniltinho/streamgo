package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/httpserver/handlers"
	"github.com/jniltinho/streamgo/internal/ratelimit"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
	"github.com/jniltinho/streamgo/internal/theme"
)

const testOrigin = "https://studio.example.com"

// authEnv is a server with the accounts routes on a fresh SQLite database, a controlled
// clock and two users, Ana and Bruno.
type authEnv struct {
	t        *testing.T
	handler  http.Handler
	accounts *account.Service
	mu       sync.Mutex
	now      time.Time
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	env := &authEnv{t: t, now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	clock := func() time.Time { env.mu.Lock(); defer env.mu.Unlock(); return env.now }
	db := storetest.Open(t, store.SQLite)
	env.accounts = account.New(store.NewAccounts(db), account.Options{Now: clock, Argon2: account.Argon2Params{MemoryKiB: 64, Time: 1, Threads: 1}})
	for _, u := range []account.NewUser{
		{Email: "ana@example.com", Name: "Ana Souza", Password: "correct-horse"},
		{Email: "bruno@example.com", Name: "Bruno", Password: "battery-staple"},
	} {
		if _, err := env.accounts.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	deps := handlers.AuthDeps{Accounts: env.accounts, Limiter: ratelimit.New(10, time.Minute, 1000), SecureCookie: true}
	srv := httpserver.New(httpserver.Options{PublicOrigin: testOrigin, Authenticate: handlers.Authenticator(deps)})
	handlers.RegisterAuth(srv.API(), deps)
	handlers.RegisterThemes(srv.API())
	handlers.RegisterMe(srv.UserAPI(), handlers.MeDeps{Accounts: env.accounts})
	// a signed-in route of another package, to check the session gate
	srv.UserAPI().GET("/whoami", func(c *echo.Context) error { return c.String(http.StatusOK, httpserver.UserID(c)) })
	env.handler = srv.Handler()
	return env
}

func (e *authEnv) advance(d time.Duration) { e.mu.Lock(); e.now = e.now.Add(d); e.mu.Unlock() }

type reply struct {
	status  int
	body    string
	cookies []*http.Cookie
}

// do sends a request from addr with the session cookie (if not empty) and a JSON body.
func (e *authEnv) do(method, path, cookie, body string, mutate ...func(*http.Request)) reply {
	e.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "192.0.2.1:5000"
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: handlers.SessionCookie, Value: cookie})
	}
	for _, m := range mutate {
		m(r)
	}
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, r)
	return reply{status: w.Code, body: w.Body.String(), cookies: w.Result().Cookies()}
}

// login signs in and returns the session cookie's value.
func (e *authEnv) login(email, password string) string {
	e.t.Helper()
	r := e.do(http.MethodPost, "/api/v1/auth/login", "", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	if r.status != http.StatusOK {
		e.t.Fatalf("login %s: %d %s", email, r.status, r.body)
	}
	for _, c := range r.cookies {
		if c.Name == handlers.SessionCookie {
			return c.Value
		}
	}
	e.t.Fatal("no session cookie")
	return ""
}

func TestSignIn(t *testing.T) {
	env := newAuthEnv(t)
	r := env.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"Ana@Example.com","password":"correct-horse"}`)
	if r.status != http.StatusOK {
		t.Fatalf("login: %d %s", r.status, r.body)
	}
	var ck *http.Cookie
	for _, c := range r.cookies {
		if c.Name == handlers.SessionCookie {
			ck = c
		}
	}
	if ck == nil || !ck.HttpOnly || !ck.Secure || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || len(ck.Value) < 40 {
		t.Fatalf("cookie: %+v", ck)
	}
	me := env.do(http.MethodGet, "/api/v1/me", ck.Value, "")
	var u map[string]any
	if err := json.Unmarshal([]byte(me.body), &u); err != nil || me.status != http.StatusOK {
		t.Fatalf("me: %d %s", me.status, me.body)
	}
	for k, want := range map[string]any{"name": "Ana Souza", "email": "ana@example.com", "theme": "", "language": ""} {
		if u[k] != want {
			t.Errorf("me.%s = %v, want %v", k, u[k], want)
		}
	}
	if id, _ := u["id"].(string); len(id) != 26 || r.body != me.body {
		t.Errorf("login body %s, me body %s", r.body, me.body)
	}
}

func TestWrongCredentialsAreIndistinguishable(t *testing.T) {
	env := newAuthEnv(t)
	unknown := env.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"nobody@example.com","password":"correct-horse"}`)
	wrong := env.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"ana@example.com","password":"wrong-horse!"}`)
	if unknown.status != http.StatusUnauthorized || wrong.status != http.StatusUnauthorized || unknown.body != wrong.body {
		t.Fatalf("unknown: %d %s; wrong: %d %s", unknown.status, unknown.body, wrong.status, wrong.body)
	}
	if !strings.Contains(wrong.body, `"invalid_credentials"`) || len(wrong.cookies) != 0 {
		t.Fatalf("body %s, cookies %v", wrong.body, wrong.cookies)
	}
}

func TestSessionExpiryAfterInactivity(t *testing.T) {
	env := newAuthEnv(t)
	ck := env.login("ana@example.com", "correct-horse")
	env.advance(13 * 24 * time.Hour)
	if r := env.do(http.MethodGet, "/api/v1/me", ck, ""); r.status != http.StatusOK {
		t.Fatalf("after 13 days: %d", r.status)
	}
	env.advance(13 * 24 * time.Hour) // renewed by the last use
	if r := env.do(http.MethodGet, "/api/v1/me", ck, ""); r.status != http.StatusOK {
		t.Fatalf("13 days after the renewal: %d", r.status)
	}
	env.advance(14 * 24 * time.Hour)
	if r := env.do(http.MethodGet, "/api/v1/me", ck, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("after 14 idle days: %d", r.status)
	}
}

func TestLogout(t *testing.T) {
	env := newAuthEnv(t)
	ck := env.login("ana@example.com", "correct-horse")
	r := env.do(http.MethodPost, "/api/v1/auth/logout", ck, "")
	if r.status != http.StatusNoContent || len(r.cookies) != 1 || r.cookies[0].MaxAge >= 0 {
		t.Fatalf("logout: %d %v", r.status, r.cookies)
	}
	if r := env.do(http.MethodGet, "/api/v1/me", ck, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("old cookie after logout: %d", r.status)
	}
	if r := env.do(http.MethodPost, "/api/v1/auth/logout", "", ""); r.status != http.StatusNoContent {
		t.Fatalf("logout without a session: %d", r.status)
	}
}

func TestSignedInRoutesNeedASession(t *testing.T) {
	env := newAuthEnv(t)
	for _, c := range []struct{ method, path, cookie string }{
		{http.MethodGet, "/api/v1/me", ""},
		{http.MethodPatch, "/api/v1/me", ""},
		{http.MethodGet, "/api/v1/whoami", ""},
		{http.MethodGet, "/api/v1/whoami", "forged-session-value"},
	} {
		r := env.do(c.method, c.path, c.cookie, `{}`)
		if r.status != http.StatusUnauthorized || !strings.Contains(r.body, `"unauthenticated"`) {
			t.Errorf("%s %s cookie %q: %d %s", c.method, c.path, c.cookie, r.status, r.body)
		}
	}
	ck := env.login("bruno@example.com", "battery-staple")
	me, _, _ := env.accounts.Login(context.Background(), "bruno@example.com", "battery-staple")
	if r := env.do(http.MethodGet, "/api/v1/whoami", ck, ""); r.status != http.StatusOK || r.body != me.ID {
		t.Fatalf("whoami: %d %s, want %s", r.status, r.body, me.ID)
	}
}

func TestDisabledWhileSignedIn(t *testing.T) {
	env := newAuthEnv(t)
	ck := env.login("ana@example.com", "correct-horse")
	if err := env.accounts.SetDisabled(context.Background(), "ana@example.com", true); err != nil {
		t.Fatal(err)
	}
	if r := env.do(http.MethodGet, "/api/v1/me", ck, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("disabled user's next request: %d", r.status)
	}
	if r := env.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"ana@example.com","password":"correct-horse"}`); r.status != http.StatusUnauthorized {
		t.Fatalf("disabled user's login: %d", r.status)
	}
}

func TestNoPublicSignUp(t *testing.T) {
	env := newAuthEnv(t)
	for _, p := range []string{"/api/v1/users", "/api/v1/auth/register", "/api/v1/auth/signup", "/api/v1/signup"} {
		if r := env.do(http.MethodPost, p, "", `{"email":"eve@example.com","name":"Eve","password":"0123456789"}`); r.status != http.StatusNotFound {
			t.Errorf("POST %s: %d", p, r.status)
		}
	}
}

func TestSignInRateLimit(t *testing.T) {
	env := newAuthEnv(t)
	// no trusted proxy: a spoofed X-Forwarded-For does not change the counted address
	spoof := func(i int) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i)) }
	}
	for i := 1; i <= 10; i++ {
		if r := env.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"ana@example.com","password":"wrong-horse!"}`, spoof(i)); r.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, r.status)
		}
	}
	r := env.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"ana@example.com","password":"correct-horse"}`, spoof(11))
	if r.status != http.StatusTooManyRequests || !strings.Contains(r.body, `"rate_limited"`) {
		t.Fatalf("eleventh: %d %s", r.status, r.body)
	}
	other := env.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"ana@example.com","password":"correct-horse"}`,
		func(r *http.Request) { r.RemoteAddr = "192.0.2.2:5000" })
	if other.status != http.StatusOK {
		t.Fatalf("another address: %d", other.status)
	}
}

func TestPreferences(t *testing.T) {
	env := newAuthEnv(t)
	ck := env.login("ana@example.com", "correct-horse")
	r := env.do(http.MethodPatch, "/api/v1/me", ck, `{"theme":"streamgo-light","language":"pt-BR"}`)
	if r.status != http.StatusOK || !strings.Contains(r.body, `"theme":"streamgo-light"`) {
		t.Fatalf("patch: %d %s", r.status, r.body)
	}

	// Follows the user: another browser signs in and gets the saved theme
	other := env.login("ana@example.com", "correct-horse")
	me := env.do(http.MethodGet, "/api/v1/me", other, "")
	if !strings.Contains(me.body, `"theme":"streamgo-light"`) || !strings.Contains(me.body, `"language":"pt-BR"`) {
		t.Fatalf("other browser: %s", me.body)
	}

	for _, c := range []struct{ body, code string }{
		{`{"theme":"no-such-theme"}`, "invalid_theme"},
		{`{"language":"fr"}`, "invalid_language"},
	} {
		r := env.do(http.MethodPatch, "/api/v1/me", ck, c.body)
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, `"`+c.code+`"`) {
			t.Errorf("%s: %d %s", c.body, r.status, r.body)
		}
	}
	if after := env.do(http.MethodGet, "/api/v1/me", ck, ""); after.body != me.body {
		t.Fatalf("an invalid patch changed the preferences: %s", after.body)
	}

	// the display name: trimmed, kept by the other fields, refused when empty
	r = env.do(http.MethodPatch, "/api/v1/me", ck, `{"name":"  Ana Maria  "}`)
	if r.status != http.StatusOK || !strings.Contains(r.body, `"name":"Ana Maria"`) || !strings.Contains(r.body, `"theme":"streamgo-light"`) {
		t.Fatalf("rename: %d %s", r.status, r.body)
	}
	r = env.do(http.MethodPatch, "/api/v1/me", ck, `{"name":"   ","theme":"streamgo-dark"}`)
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, `"field":"name"`) {
		t.Fatalf("empty name: %d %s", r.status, r.body)
	}
	if after := env.do(http.MethodGet, "/api/v1/me", ck, ""); !strings.Contains(after.body, `"name":"Ana Maria"`) || !strings.Contains(after.body, `"theme":"streamgo-light"`) {
		t.Fatalf("a refused name changed something: %s", after.body)
	}
	// a name is printed on the operator's terminal: no control characters
	r = env.do(http.MethodPatch, "/api/v1/me", ck, `{"name":"Ana\u001b[2J\u001b[H"}`)
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.body, `"field":"name"`) {
		t.Fatalf("control characters: %d %s", r.status, r.body)
	}

	// only the given field changes; "" clears a choice
	r = env.do(http.MethodPatch, "/api/v1/me", ck, `{"language":""}`)
	if !strings.Contains(r.body, `"theme":"streamgo-light"`) || !strings.Contains(r.body, `"language":""`) {
		t.Fatalf("partial patch: %s", r.body)
	}
}

func TestThemes(t *testing.T) {
	env := newAuthEnv(t)
	r := env.do(http.MethodGet, "/api/v1/themes", "", "")
	var out struct {
		Themes []theme.Theme `json:"themes"`
	}
	if err := json.Unmarshal([]byte(r.body), &out); err != nil || r.status != http.StatusOK {
		t.Fatalf("themes: %d %s", r.status, r.body)
	}
	if len(out.Themes) != len(theme.All()) || out.Themes[0].ID != theme.Default || !out.Themes[0].Default {
		t.Fatalf("themes: %+v", out.Themes)
	}
}
