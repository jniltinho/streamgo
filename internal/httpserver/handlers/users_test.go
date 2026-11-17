package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type adminUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Admin    bool   `json:"admin"`
	Disabled bool   `json:"disabled"`
}

func (e *authEnv) users(cookie string) map[string]adminUser {
	e.t.Helper()
	r := e.do(http.MethodGet, "/api/v1/users", cookie, "")
	var body struct{ Users []adminUser }
	if r.status != http.StatusOK || json.Unmarshal([]byte(r.body), &body) != nil {
		e.t.Fatalf("list: %d %s", r.status, r.body)
	}
	out := map[string]adminUser{}
	for _, u := range body.Users {
		out[u.Email] = u
	}
	return out
}

func errorField(t *testing.T, body string) (code, field, message string) {
	t.Helper()
	var b struct {
		Error struct{ Code, Field, Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("not an error: %s", body)
	}
	return b.Error.Code, b.Error.Field, b.Error.Message
}

func TestMeSaysAdmin(t *testing.T) {
	env := newAuthEnv(t)
	for email, pw := range map[string]string{"ana@example.com": "correct-horse", "bruno@example.com": "battery-staple"} {
		r := env.do(http.MethodGet, "/api/v1/me", env.login(email, pw), "")
		want := fmt.Sprintf(`"admin":%v`, email == "ana@example.com")
		if r.status != http.StatusOK || !strings.Contains(r.body, want) {
			t.Errorf("%s: %d %s, want %s", email, r.status, r.body, want)
		}
	}
}

func TestUsersRoutesNeedAnAdministrator(t *testing.T) {
	env := newAuthEnv(t)
	ana := env.login("ana@example.com", "correct-horse")
	bruno := env.login("bruno@example.com", "battery-staple")
	before := env.users(ana)
	anaID := before["ana@example.com"].ID
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/users", ""},
		{http.MethodPost, "/api/v1/users", `{"email":"eve@example.com","name":"Eve","password":"0123456789","admin":true}`},
		{http.MethodPatch, "/api/v1/users/" + anaID, `{"disabled":true}`},
		{http.MethodPatch, "/api/v1/users/" + before["bruno@example.com"].ID, `{"admin":true}`},
	} {
		r := env.do(c.method, c.path, bruno, c.body)
		if code, _, _ := errorField(t, r.body); r.status != http.StatusForbidden || code != "forbidden" {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.status, r.body)
		}
	}
	if after := env.users(ana); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("a refused request changed something: %v", after)
	}
}

func TestUsersCreate(t *testing.T) {
	env := newAuthEnv(t)
	ana := env.login("ana@example.com", "correct-horse")
	r := env.do(http.MethodPost, "/api/v1/users", ana, `{"email":"Carla@Example.com","name":"Carla","password":"carla-password"}`)
	var carla adminUser
	if r.status != http.StatusCreated || json.Unmarshal([]byte(r.body), &carla) != nil || carla.Email != "carla@example.com" || carla.Admin || carla.Disabled {
		t.Fatalf("create: %d %s", r.status, r.body)
	}
	env.login("carla@example.com", "carla-password")
	r = env.do(http.MethodPost, "/api/v1/users", ana, `{"email":"dan@example.com","name":"Dan","password":"dan-password","admin":true}`)
	if r.status != http.StatusCreated || !strings.Contains(r.body, `"admin":true`) {
		t.Fatalf("create an administrator: %d %s", r.status, r.body)
	}
	for _, c := range []struct {
		body   string
		status int
		field  string
	}{
		{`{"email":"CARLA@example.com","name":"C","password":"0123456789"}`, http.StatusConflict, ""},
		{`{"email":"eve@example.com","name":"Eve","password":"short"}`, http.StatusUnprocessableEntity, "password"},
	} {
		r := env.do(http.MethodPost, "/api/v1/users", ana, c.body)
		if _, field, _ := errorField(t, r.body); r.status != c.status || field != c.field {
			t.Errorf("%s: %d %s", c.body, r.status, r.body)
		}
	}
	if n := len(env.users(ana)); n != 4 {
		t.Fatalf("%d users, want 4", n)
	}
}

func TestUsersEdit(t *testing.T) {
	env := newAuthEnv(t)
	ana := env.login("ana@example.com", "correct-horse")
	brunoSession := env.login("bruno@example.com", "battery-staple")
	id := env.users(ana)["bruno@example.com"].ID
	path := "/api/v1/users/" + id

	r := env.do(http.MethodPatch, path, ana, `{"name":"Bruno Lima"}`)
	if r.status != http.StatusOK || !strings.Contains(r.body, `"name":"Bruno Lima"`) || !strings.Contains(r.body, `"admin":false`) {
		t.Fatalf("name: %d %s", r.status, r.body)
	}
	if r := env.do(http.MethodGet, "/api/v1/me", brunoSession, ""); r.status != http.StatusOK {
		t.Fatalf("a name change ended the session: %d", r.status)
	}
	if r := env.do(http.MethodPatch, path, ana, `{"admin":true}`); r.status != http.StatusOK || !strings.Contains(r.body, `"admin":true`) || !strings.Contains(r.body, `"name":"Bruno Lima"`) {
		t.Fatalf("role: %d %s", r.status, r.body)
	}
	if r := env.do(http.MethodPatch, path, ana, `{"password":"new-bruno-password"}`); r.status != http.StatusOK {
		t.Fatalf("password: %d %s", r.status, r.body)
	}
	if r := env.do(http.MethodGet, "/api/v1/me", brunoSession, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("a new password must end the sessions: %d", r.status)
	}
	brunoSession = env.login("bruno@example.com", "new-bruno-password")
	if r := env.do(http.MethodPatch, path, ana, `{"disabled":true}`); r.status != http.StatusOK || !strings.Contains(r.body, `"disabled":true`) {
		t.Fatalf("disable: %d %s", r.status, r.body)
	}
	if r := env.do(http.MethodGet, "/api/v1/me", brunoSession, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("disabling must end the sessions: %d", r.status)
	}
	if r := env.do(http.MethodPatch, path, ana, `{"disabled":false}`); r.status != http.StatusOK || !strings.Contains(r.body, `"disabled":false`) {
		t.Fatalf("enable: %d %s", r.status, r.body)
	}
	if r := env.do(http.MethodPatch, "/api/v1/users/missing", ana, `{"name":"X"}`); r.status != http.StatusNotFound {
		t.Fatalf("missing: %d %s", r.status, r.body)
	}
	if r := env.do(http.MethodPatch, path, ana, `{"password":"short"}`); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("short password: %d %s", r.status, r.body)
	}
}

func TestUsersNotThemselfAndLastAdministrator(t *testing.T) {
	env := newAuthEnv(t)
	ana := env.login("ana@example.com", "correct-horse")
	list := env.users(ana)
	self := "/api/v1/users/" + list["ana@example.com"].ID
	bruno := "/api/v1/users/" + list["bruno@example.com"].ID
	if r := env.do(http.MethodPatch, bruno, ana, `{"admin":true}`); r.status != http.StatusOK {
		t.Fatal(r.body)
	}
	for _, body := range []string{`{"disabled":true}`, `{"admin":false}`} {
		r := env.do(http.MethodPatch, self, ana, body)
		if _, _, msg := errorField(t, r.body); r.status != http.StatusConflict || !strings.Contains(msg, "your own") {
			t.Errorf("self %s: %d %s", body, r.status, r.body)
		}
	}
	// Last administrator, in sequence: ana demotes bruno, then bruno's demotion of ana (from a
	// browser still open) is refused, since bruno is no longer an administrator. Checked at the
	// same time, the second gets 409 (core and store tests).
	brunoSession := env.login("bruno@example.com", "battery-staple")
	if r := env.do(http.MethodPatch, bruno, ana, `{"admin":false}`); r.status != http.StatusOK {
		t.Fatal(r.body)
	}
	if r := env.do(http.MethodPatch, self, brunoSession, `{"admin":false}`); r.status != http.StatusForbidden {
		t.Fatalf("second demotion: %d %s", r.status, r.body)
	}
	if u := env.users(ana)["ana@example.com"]; !u.Admin || u.Disabled {
		t.Fatalf("ana changed: %+v", u)
	}
}

func TestChangeOwnPassword(t *testing.T) {
	env := newAuthEnv(t)
	current := env.login("bruno@example.com", "battery-staple")
	other := env.login("bruno@example.com", "battery-staple")

	r := env.do(http.MethodPut, "/api/v1/me/password", current, `{"current":"wrong-horse!","password":"new-bruno-password"}`)
	if code, field, _ := errorField(t, r.body); r.status != http.StatusUnprocessableEntity || code != "invalid_input" || field != "current" {
		t.Fatalf("wrong current: %d %s", r.status, r.body)
	}
	r = env.do(http.MethodPut, "/api/v1/me/password", current, `{"current":"battery-staple","password":"short"}`)
	if _, field, _ := errorField(t, r.body); r.status != http.StatusUnprocessableEntity || field != "password" {
		t.Fatalf("short: %d %s", r.status, r.body)
	}
	env.login("bruno@example.com", "battery-staple") // unchanged so far

	r = env.do(http.MethodPut, "/api/v1/me/password", current, `{"current":"battery-staple","password":"new-bruno-password"}`)
	if r.status != http.StatusNoContent {
		t.Fatalf("change: %d %s", r.status, r.body)
	}
	if r := env.do(http.MethodGet, "/api/v1/me", current, ""); r.status != http.StatusOK {
		t.Fatalf("the current session must stay: %d", r.status)
	}
	if r := env.do(http.MethodGet, "/api/v1/me", other, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("the other session must end: %d", r.status)
	}
	env.login("bruno@example.com", "new-bruno-password")
	if r := env.do(http.MethodPost, "/api/v1/auth/login", "", `{"email":"bruno@example.com","password":"battery-staple"}`); r.status != http.StatusUnauthorized {
		t.Fatalf("old password: %d", r.status)
	}
}

func TestChangeOwnPasswordRateLimit(t *testing.T) {
	env := newAuthEnv(t)
	ck := env.login("bruno@example.com", "battery-staple") // one attempt of the shared limiter
	for i := 2; i <= 10; i++ {
		if r := env.do(http.MethodPut, "/api/v1/me/password", ck, `{"current":"wrong-horse!","password":"new-bruno-password"}`); r.status != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: %d", i, r.status)
		}
	}
	r := env.do(http.MethodPut, "/api/v1/me/password", ck, `{"current":"battery-staple","password":"new-bruno-password"}`)
	if r.status != http.StatusTooManyRequests {
		t.Fatalf("eleventh: %d %s", r.status, r.body)
	}
}
