package account

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
)

// fakeRepo is an in-memory Repository.
type fakeRepo struct {
	mu       sync.Mutex
	users    map[string]User
	sessions map[string]Session
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{users: map[string]User{}, sessions: map[string]Session{}}
}

func (f *fakeRepo) CreateUser(_ context.Context, u *User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.users {
		if o.Email == u.Email {
			return core.ErrConflict
		}
	}
	u.ID = fmt.Sprintf("u%d", len(f.users)+1)
	f.users[u.ID] = *u
	return nil
}

func (f *fakeRepo) UserByID(_ context.Context, id string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return nil, core.ErrNotFound
	}
	return &u, nil
}

func (f *fakeRepo) UserByEmail(_ context.Context, email string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.Email == email {
			return &u, nil
		}
	}
	return nil, core.ErrNotFound
}

func (f *fakeRepo) ListUsers(context.Context) ([]User, error) { return nil, nil }

func (f *fakeRepo) update(id string, fn func(*User)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.users[id]
	fn(&u)
	f.users[id] = u
	return nil
}

func (f *fakeRepo) SetPasswordHash(_ context.Context, id, hash string) error {
	return f.update(id, func(u *User) { u.PasswordHash = hash })
}

func (f *fakeRepo) SetDisabledAt(_ context.Context, id string, t time.Time) error {
	return f.update(id, func(u *User) { u.DisabledAt = t })
}

func (f *fakeRepo) SetPreferences(_ context.Context, id, theme, language string) error {
	return f.update(id, func(u *User) { u.Theme, u.Language = theme, language })
}

func (f *fakeRepo) CreateSession(_ context.Context, s Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.IDHash] = s
	return nil
}

func (f *fakeRepo) ActiveSession(_ context.Context, h string) (*Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[h]
	if !ok || f.users[s.UserID].Disabled() {
		return nil, core.ErrNotFound
	}
	return &s, nil
}

func (f *fakeRepo) TouchSession(_ context.Context, h string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[h]
	s.LastUsedAt = at
	f.sessions[h] = s
	return nil
}

func (f *fakeRepo) DeleteSession(_ context.Context, h string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, h)
	return nil
}

func (f *fakeRepo) DeleteUserSessions(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for h, s := range f.sessions {
		if s.UserID == id {
			delete(f.sessions, h)
		}
	}
	return nil
}

// clock is a controlled time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// cheap keeps the tests fast; production uses the default cost.
var cheap = Argon2Params{MemoryKiB: 64, Time: 1, Threads: 1}

func newTestService(t *testing.T) (*Service, *fakeRepo, *clock) {
	t.Helper()
	repo, clk := newFakeRepo(), &clock{t: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	s := New(repo, Options{Now: clk.now, Argon2: cheap})
	if _, err := s.Create(context.Background(), NewUser{Email: "Ana@Example.com", Name: "Ana Souza", Password: "correct-horse"}); err != nil {
		t.Fatal(err)
	}
	return s, repo, clk
}

func TestCreateValidates(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	tests := []struct {
		name  string
		in    NewUser
		field string
		err   error
	}{
		{"short password", NewUser{Email: "b@example.com", Name: "B", Password: "123456789"}, "password", nil},
		{"bad e-mail", NewUser{Email: "not an e-mail", Name: "B", Password: "0123456789"}, "email", nil},
		{"display-name e-mail", NewUser{Email: "B <b@example.com>", Name: "B", Password: "0123456789"}, "email", nil},
		{"empty name", NewUser{Email: "b@example.com", Name: "  ", Password: "0123456789"}, "name", nil},
		{"duplicate in another case", NewUser{Email: "ANA@example.COM", Name: "A", Password: "0123456789"}, "", core.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Create(ctx, tt.in)
			var inv *core.InvalidError
			switch {
			case tt.err != nil && !errors.Is(err, tt.err):
				t.Fatalf("err = %v, want %v", err, tt.err)
			case tt.field != "" && (!errors.As(err, &inv) || inv.Field != tt.field):
				t.Fatalf("err = %v, want invalid %s", err, tt.field)
			}
		})
	}
}

func TestCreateStoresLowerCaseAndArgon2id(t *testing.T) {
	_, repo, _ := newTestService(t)
	u := repo.users["u1"]
	if u.Email != "ana@example.com" {
		t.Errorf("email = %q", u.Email)
	}
	if _, _, _, err := parsePHC(u.PasswordHash); err != nil || u.PasswordHash[:10] != "$argon2id$" {
		t.Errorf("hash = %q, %v", u.PasswordHash, err)
	}
}

func TestLogin(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	u, token, err := s.Login(ctx, " ANA@example.com", "correct-horse")
	if err != nil || u.Email != "ana@example.com" || token == "" {
		t.Fatalf("login: %v %v %q", u, err, token)
	}
	if id, err := s.Authenticate(ctx, token); err != nil || id != u.ID {
		t.Fatalf("authenticate: %q %v", id, err)
	}
	for _, c := range []struct{ email, pw string }{{"bob@example.com", "correct-horse"}, {"ana@example.com", "wrong-horse"}} {
		if _, _, err := s.Login(ctx, c.email, c.pw); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("login %s: %v, want ErrInvalidCredentials", c.email, err)
		}
	}
}

func TestSessionExpiresAfterIdleDays(t *testing.T) {
	s, _, clk := newTestService(t)
	ctx := context.Background()
	_, token, err := s.Login(ctx, "ana@example.com", "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(13 * 24 * time.Hour)
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatalf("after 13 days: %v", err)
	}
	clk.t = clk.t.Add(13 * 24 * time.Hour) // renewed: 13 more days are fine
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatalf("13 days after the renewal: %v", err)
	}
	clk.t = clk.t.Add(SessionIdle)
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("after 14 idle days: %v", err)
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	_, token, _ := s.Login(ctx, "ana@example.com", "correct-horse")
	if err := s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("after logout: %v", err)
	}
}

func TestDisableEndsSessionsAndSignIn(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	_, token, _ := s.Login(ctx, "ana@example.com", "correct-horse")
	if err := s.SetDisabled(ctx, "ANA@example.com", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("disabled session: %v", err)
	}
	if _, _, err := s.Login(ctx, "ana@example.com", "correct-horse"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled login: %v", err)
	}
	if err := s.SetDisabled(ctx, "ana@example.com", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("enabling must not revive old sessions: %v", err)
	}
	if _, _, err := s.Login(ctx, "ana@example.com", "correct-horse"); err != nil {
		t.Fatalf("enabled login: %v", err)
	}
}

func TestSetPasswordEndsSessions(t *testing.T) {
	s, _, _ := newTestService(t)
	ctx := context.Background()
	_, token, _ := s.Login(ctx, "ana@example.com", "correct-horse")
	if err := s.SetPassword(ctx, "ana@example.com", "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := s.SetPassword(ctx, "ana@example.com", "battery-staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old session: %v", err)
	}
	if _, _, err := s.Login(ctx, "ana@example.com", "battery-staple"); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if err := s.SetPassword(ctx, "nobody@example.com", "battery-staple"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}
