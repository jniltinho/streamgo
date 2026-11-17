package handlers_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/httpserver/handlers"
	"github.com/jniltinho/streamgo/internal/sealed"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

const (
	origin    = "https://studio.example.com"
	ana       = "ana00000000000000000000000"
	bruno     = "bruno000000000000000000000"
	publicURL = "https://studio.example.com"
)

// publicResolver resolves every name to a documentation address except localhost, which the
// system resolves (to loopback).
type publicResolver struct{}

func (publicResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if host == "localhost" {
		return net.DefaultResolver.LookupNetIP(ctx, network, host)
	}
	return []netip.Addr{netip.MustParseAddr("203.0.113.7")}, nil
}

// remover records the broadcasts deleted and removes their logo file, as the studio package does.
type remover struct {
	dir     string
	removed []string
}

func (r *remover) RemoveBroadcast(_ context.Context, b broadcast.Broadcast) error {
	r.removed = append(r.removed, b.ID)
	if b.Logo != "" {
		return os.Remove(filepath.Join(r.dir, b.Logo))
	}
	return nil
}

type statuses map[string]destination.Status

func (s statuses) StatusOf(string) map[string]destination.Status { return s }

type notifier struct {
	mu             sync.Mutex
	added, removed []string
	err            error
}

func (n *notifier) SelectionChanged(_ context.Context, _ string, added, removed []string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.added, n.removed = added, removed
	return n.err
}

type env struct {
	t       *testing.T
	db      *store.DB
	srv     *httpserver.Server
	bs      *broadcast.Service
	ds      *destination.Service
	repo    *store.Broadcasts
	remover *remover
}

func newEnv(t *testing.T, mutate ...func(*destination.Service)) *env {
	t.Helper()
	db := storetest.Open(t, store.SQLite)
	for _, id := range []string{ana, bruno} {
		if err := db.Gorm.Exec("INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)",
			id, strings.TrimRight(id, "0")+"@example.com", "U", "x").Error; err != nil {
			t.Fatal(err)
		}
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	box, err := sealed.New(key)
	if err != nil {
		t.Fatal(err)
	}
	locks := &broadcast.Locks{}
	e := &env{t: t, db: db, repo: store.NewBroadcasts(db), remover: &remover{dir: t.TempDir()}}
	e.bs = &broadcast.Service{Repo: e.repo, Locks: locks, Sealer: box, PublicURL: publicURL, Remover: e.remover}
	e.ds = &destination.Service{Repo: store.NewDestinations(db), Broadcasts: e.repo, Locks: locks, Sealer: box,
		Resolver: publicResolver{}, MaxPerBroadcast: 2}
	for _, m := range mutate {
		m(e.ds)
	}
	e.srv = httpserver.New(httpserver.Options{
		PublicOrigin: origin,
		Authenticate: func(c *echo.Context) (string, error) {
			if id := c.Request().Header.Get("X-Test-User"); id != "" {
				return id, nil
			}
			return "", httpserver.ErrUnauthenticated
		},
	})
	handlers.RegisterBroadcasts(e.srv.UserAPI(), handlers.BroadcastsDeps{Broadcasts: e.bs, Destinations: e.ds})
	handlers.RegisterDestinations(e.srv.UserAPI(), handlers.DestinationsDeps{Destinations: e.ds})
	return e
}

// do sends a request as user and returns the status and the body.
func (e *env) do(user, method, path string, body any) (int, []byte) {
	e.t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, "/api/v1"+path, nil)
	} else {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Origin", origin)
	r.Header.Set("X-Test-User", user)
	w := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, r)
	return w.Code, w.Body.Bytes()
}

// call is do that checks the status and decodes the body into out (when not nil).
func (e *env) call(user, method, path string, body any, want int, out any) []byte {
	e.t.Helper()
	code, raw := e.do(user, method, path, body)
	if code != want {
		e.t.Fatalf("%s %s: %d %s, want %d", method, path, code, raw, want)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s %s: %v in %s", method, path, err, raw)
		}
	}
	return raw
}

type detail struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Output struct {
		State     string  `json:"state"`
		Reason    string  `json:"reason"`
		StartedAt *string `json:"started_at"`
		Deadline  *string `json:"deadline"`
	} `json:"output"`
	InviteURL    string          `json:"invite_url"`
	Stage        json.RawMessage `json:"stage"`
	Destinations []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"destinations"`
}

func (e *env) createBroadcast(user, title string) detail {
	e.t.Helper()
	var d detail
	e.call(user, http.MethodPost, "/broadcasts", map[string]string{"title": title}, http.StatusCreated, &d)
	return d
}

func (e *env) setState(id string, state broadcast.OutputState) {
	e.t.Helper()
	if err := e.repo.SetOutput(context.Background(), id, broadcast.Output{State: state}); err != nil {
		e.t.Fatal(err)
	}
}

// assertInvalid checks a 422 invalid_input naming field.
func assertInvalid(t *testing.T, code int, raw []byte, field string) {
	t.Helper()
	var body struct {
		Error struct{ Code, Message, Field string } `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	if code != http.StatusUnprocessableEntity || body.Error.Code != "invalid_input" || body.Error.Field != field {
		t.Fatalf("got %d %s, want 422 naming %q", code, raw, field)
	}
}

// Scenario "Create and list".
func TestCreateAndList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.createBroadcast(ana, "Older")
	d := e.createBroadcast(ana, "  Weekly Product Live ")
	if d.Title != "Weekly Product Live" || d.Output.State != "offline" || d.Output.StartedAt != nil ||
		d.Output.Deadline != nil || string(d.Stage) != broadcast.DefaultStage || len(d.Destinations) != 0 ||
		!strings.HasPrefix(d.InviteURL, publicURL+"/join/") {
		t.Fatalf("created: %+v", d)
	}
	var list struct{ Broadcasts []detail }
	e.call(ana, http.MethodGet, "/broadcasts", nil, http.StatusOK, &list)
	if len(list.Broadcasts) != 2 || list.Broadcasts[0].ID != d.ID || list.Broadcasts[0].Output.State != "offline" {
		t.Fatalf("ana's list: %+v", list)
	}
	e.call(bruno, http.MethodGet, "/broadcasts", nil, http.StatusOK, &list)
	if len(list.Broadcasts) != 0 {
		t.Fatalf("bruno's list: %+v", list)
	}
	// another user's broadcast is not found, whatever the method
	for _, m := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		if code, _ := e.do(bruno, m, "/broadcasts/"+d.ID, map[string]string{"title": "x"}); code != http.StatusNotFound {
			t.Errorf("%s by bruno: %d", m, code)
		}
	}
	if code, _ := e.do(bruno, http.MethodPost, "/broadcasts/"+d.ID+"/invite", nil); code != http.StatusNotFound {
		t.Errorf("rotate by bruno: %d", code)
	}
	var renamed detail
	e.call(ana, http.MethodPatch, "/broadcasts/"+d.ID, map[string]string{"title": "Renamed"}, http.StatusOK, &renamed)
	if renamed.Title != "Renamed" || renamed.InviteURL != d.InviteURL {
		t.Fatalf("renamed: %+v", renamed)
	}
}

// Scenario "Empty title".
func TestEmptyTitle(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	d := e.createBroadcast(ana, "Show")
	for _, title := range []string{"", "   ", strings.Repeat("a", 121)} {
		code, raw := e.do(ana, http.MethodPost, "/broadcasts", map[string]string{"title": title})
		assertInvalid(t, code, raw, "title")
		code, raw = e.do(ana, http.MethodPatch, "/broadcasts/"+d.ID, map[string]string{"title": title})
		assertInvalid(t, code, raw, "title")
	}
	e.createBroadcast(ana, strings.Repeat("é", 120))
}

// Scenario "Delete while live".
func TestDeleteWhileLive(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, state := range []broadcast.OutputState{broadcast.Starting, broadcast.Live, broadcast.Interrupted} {
		d := e.createBroadcast(ana, "Show")
		e.setState(d.ID, state)
		code, raw := e.do(ana, http.MethodDelete, "/broadcasts/"+d.ID, nil)
		if code != http.StatusConflict || !strings.Contains(string(raw), "end it") {
			t.Fatalf("%s: %d %s", state, code, raw)
		}
		e.call(ana, http.MethodGet, "/broadcasts/"+d.ID, nil, http.StatusOK, nil)
	}
	if len(e.remover.removed) != 0 {
		t.Fatalf("removed: %v", e.remover.removed)
	}
}

// Scenario "Delete removes the logo file": the row goes, and the studio's Remover gets the
// broadcast with its logo (the fake deletes the file as the studio package will).
func TestDeleteRemovesTheLogoFile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	d := e.createBroadcast(ana, "Show")
	logo := filepath.Join(e.remover.dir, "f00d.png")
	if err := os.WriteFile(logo, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.repo.SetLogo(context.Background(), d.ID, "f00d.png"); err != nil {
		t.Fatal(err)
	}
	e.call(ana, http.MethodDelete, "/broadcasts/"+d.ID, nil, http.StatusNoContent, nil)
	if _, err := os.Stat(logo); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("logo still there: %v", err)
	}
	if len(e.remover.removed) != 1 || e.remover.removed[0] != d.ID {
		t.Fatalf("removed: %v", e.remover.removed)
	}
	e.call(ana, http.MethodGet, "/broadcasts/"+d.ID, nil, http.StatusNotFound, nil)
}

// Scenarios "Owner reads the link again" and "Rotation".
func TestInviteLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	d := e.createBroadcast(ana, "Show")
	var again detail
	e.call(ana, http.MethodGet, "/broadcasts/"+d.ID, nil, http.StatusOK, &again)
	if again.InviteURL != d.InviteURL {
		t.Fatalf("link changed: %q then %q", d.InviteURL, again.InviteURL)
	}
	oldToken := strings.TrimPrefix(d.InviteURL, publicURL+"/join/")
	if len(oldToken) != 43 { // 32 bytes, base64url without padding
		t.Fatalf("token %q", oldToken)
	}
	if b, err := e.bs.FindByInvite(ctx, oldToken); err != nil || b.ID != d.ID {
		t.Fatalf("find by the link: %v", err)
	}

	var rotated struct {
		InviteURL string `json:"invite_url"`
	}
	e.call(ana, http.MethodPost, "/broadcasts/"+d.ID+"/invite", nil, http.StatusOK, &rotated)
	if rotated.InviteURL == d.InviteURL {
		t.Fatal("the link did not change")
	}
	if _, err := e.bs.FindByInvite(ctx, oldToken); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("old link: %v", err)
	}
	newToken := strings.TrimPrefix(rotated.InviteURL, publicURL+"/join/")
	if b, err := e.bs.FindByInvite(ctx, newToken); err != nil || b.ID != d.ID {
		t.Fatalf("new link: %v", err)
	}
	e.call(ana, http.MethodGet, "/broadcasts/"+d.ID, nil, http.StatusOK, &again)
	if again.InviteURL != rotated.InviteURL {
		t.Fatalf("detail after rotation: %q", again.InviteURL)
	}
}

// Scenario "Token not stored in clear": neither the row nor the database files hold it.
func TestTokenNotStoredInClear(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	d := e.createBroadcast(ana, "Show")
	token := strings.TrimPrefix(d.InviteURL, publicURL+"/join/")
	assertNotInDatabase(t, e.db, token)
}

// assertNotInDatabase checks that secret appears in no column of the broadcasts and
// destinations tables nor in the SQLite files.
func assertNotInDatabase(t *testing.T, db *store.DB, secret string) {
	t.Helper()
	for _, table := range []string{"broadcasts", "destinations"} {
		var rows []map[string]any
		if err := db.Gorm.Table(table).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			for col, v := range row {
				var s string
				switch v := v.(type) {
				case string:
					s = v
				case []byte:
					s = string(v)
				}
				if strings.Contains(s, secret) {
					t.Fatalf("%s.%s holds the secret", table, col)
				}
			}
		}
	}
	var file string
	if err := db.SQL.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&file); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{file, file + "-wal"} {
		b, err := os.ReadFile(f) //nolint:gosec // the test's own database
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("%s holds the secret", filepath.Base(f))
		}
	}
}
