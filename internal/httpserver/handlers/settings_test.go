package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/settings"
	"github.com/jniltinho/streamgo/internal/httpserver/handlers"
	"github.com/jniltinho/streamgo/internal/store"
)

// fileQuality is what config.toml says in these tests: a ceiling of 1080p and a fixed bitrate of 4000.
var fileQuality = broadcast.Quality{Default: "720p", Max: "1080p", VideoKbps: 4000}

type outputSettings struct {
	Resolution       string   `json:"resolution"`
	MaxResolution    string   `json:"max_resolution"`
	VideoBitrateKbps int      `json:"video_bitrate_kbps"`
	Resolutions      []string `json:"resolutions"`
	Locked           []string `json:"locked"`
}

// newSettingsEnv is the broadcasts env with ana an administrator and the settings routes; the broadcasts read
// their quality from the settings, as in cmd/server.go.
func newSettingsEnv(t *testing.T, locked ...string) (*env, func() *settings.Service) {
	t.Helper()
	e := newEnv(t)
	if err := e.db.Gorm.Exec("UPDATE users SET is_admin = TRUE WHERE id = ?", ana).Error; err != nil {
		t.Fatal(err)
	}
	accounts := account.New(store.NewAccounts(e.db), account.Options{})
	start := func() *settings.Service { // a server start on the same database
		s, err := settings.New(context.Background(), store.NewServerSettings(e.db), accounts, fileQuality, locked, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := start()
	e.bs.Quality = s.Quality
	handlers.RegisterSettings(e.srv.UserAPI(), handlers.SettingsDeps{Settings: s})
	return e, start
}

func (e *env) settings(user string, body any, want int) outputSettings {
	e.t.Helper()
	var out struct{ Output outputSettings }
	method := http.MethodGet
	if body != nil {
		method = http.MethodPut
	}
	e.call(user, method, "/settings", body, want, &out)
	return out.Output
}

// Requirement "Stored output settings": raise the ceiling, bitrate by resolution.
func TestSettingsRaiseTheCeiling(t *testing.T) {
	t.Parallel()
	e, restart := newSettingsEnv(t)
	d := e.createBroadcast(ana, "Show")
	if got := e.settings(ana, nil, http.StatusOK); got.MaxResolution != "1080p" || got.VideoBitrateKbps != 4000 ||
		!slices.Equal(got.Resolutions, []string{"720p", "1080p", "1440p"}) || len(got.Locked) != 0 {
		t.Fatalf("read: %+v", got)
	}
	if got := e.settings(ana, map[string]any{"output": map[string]any{"max_resolution": "1440p"}}, http.StatusOK); got.MaxResolution != "1440p" || got.Resolution != "720p" {
		t.Fatalf("put: %+v", got)
	}
	var x detail
	e.call(ana, http.MethodGet, "/broadcasts/"+d.ID, nil, http.StatusOK, &x)
	if !slices.Equal(x.Studio.Resolutions, []string{"720p", "1080p", "1440p"}) {
		t.Fatalf("the detail's choices: %v", x.Studio.Resolutions)
	}
	if q := restart().Quality(); q.Max != "1440p" {
		t.Fatalf("after a restart: %+v", q)
	}

	// bitrate by resolution: a 1080p broadcast goes out at 6000
	e.call(ana, http.MethodPatch, "/broadcasts/"+d.ID, map[string]string{"resolution": "1080p"}, http.StatusOK, nil)
	e.settings(ana, map[string]any{"output": map[string]any{"video_bitrate_kbps": 0}}, http.StatusOK)
	e.call(ana, http.MethodGet, "/broadcasts/"+d.ID, nil, http.StatusOK, &x)
	if x.Studio.VideoBitrateKbps != 6000 {
		t.Fatalf("1080p by resolution: %d kbit/s", x.Studio.VideoBitrateKbps)
	}
}

// Requirement "Stored output settings": the rules and all or nothing.
func TestSettingsRules(t *testing.T) {
	t.Parallel()
	e, restart := newSettingsEnv(t)
	for _, c := range []struct{ body, field string }{
		{`{"output":{"resolution":"1440p"}}`, "resolution"},
		{`{"output":{"max_resolution":"2160p"}}`, "max_resolution"},
		{`{"output":{"video_bitrate_kbps":300}}`, "video_bitrate_kbps"},
		{`{"output":{"max_resolution":"1440p","video_bitrate_kbps":300}}`, "video_bitrate_kbps"},
	} {
		code, raw := e.do(ana, http.MethodPut, "/settings", json.RawMessage(c.body))
		assertInvalid(t, code, raw, c.field)
	}
	if got := e.settings(ana, nil, http.StatusOK); got.MaxResolution != "1080p" || got.VideoBitrateKbps != 4000 || got.Resolution != "720p" {
		t.Fatalf("a refused put changed %+v", got)
	}
	if q := restart().Quality(); q != fileQuality {
		t.Fatalf("a refused put stored %+v", q)
	}
}

// Requirement "Precedence": locked by the environment.
func TestSettingsLocked(t *testing.T) {
	t.Parallel()
	e, restart := newSettingsEnv(t, settings.FieldMaxResolution)
	if got := e.settings(ana, nil, http.StatusOK); !slices.Equal(got.Locked, []string{"max_resolution"}) {
		t.Fatalf("read: %+v", got)
	}
	code, raw := e.do(ana, http.MethodPut, "/settings", map[string]any{"output": map[string]any{"max_resolution": "1440p", "video_bitrate_kbps": 0}})
	if code != http.StatusConflict {
		t.Fatalf("put a locked ceiling: %d %s", code, raw)
	}
	if q := restart().Quality(); q != fileQuality {
		t.Fatalf("a refused put stored %+v", q)
	}
}

// Requirement "Precedence": stored wins over the file.
func TestSettingsStoredWinsOverTheFile(t *testing.T) {
	t.Parallel()
	e, restart := newSettingsEnv(t)
	e.settings(ana, map[string]any{"output": map[string]any{"resolution": "1080p"}}, http.StatusOK)
	e.bs.Quality = restart().Quality
	if d := e.createBroadcast(ana, "New"); d.Studio.Resolution != "1080p" {
		t.Fatalf("a new broadcast after a restart: %+v", d.Studio)
	}
}

// Requirement "Administrators only".
func TestSettingsNeedAnAdministrator(t *testing.T) {
	t.Parallel()
	e, restart := newSettingsEnv(t)
	for _, body := range []any{nil, map[string]any{"output": map[string]any{"max_resolution": "1440p"}}} {
		method := http.MethodGet
		if body != nil {
			method = http.MethodPut
		}
		code, raw := e.do(bruno, method, "/settings", body)
		if c, _, _ := errorField(t, string(raw)); code != http.StatusForbidden || c != "forbidden" {
			t.Fatalf("%s: %d %s", method, code, raw)
		}
	}
	if q := restart().Quality(); q != fileQuality {
		t.Fatalf("a refused put stored %+v", q)
	}
}
