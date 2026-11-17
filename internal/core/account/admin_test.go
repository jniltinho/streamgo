package account

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
)

func ptr[T any](v T) *T { return &v }

// adminEnv is ana (the first account, an administrator) and bruno (a user), with a session each.
func adminEnv(t *testing.T) (s *Service, repo *fakeRepo, ana, bruno *User) {
	t.Helper()
	s, repo, _ = newTestService(t)
	ctx := context.Background()
	ana, err := s.repo.UserByEmail(ctx, "ana@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bruno, err = s.Create(ctx, NewUser{Email: "bruno@example.com", Name: "Bruno", Password: "bruno-password"})
	if err != nil {
		t.Fatal(err)
	}
	return s, repo, ana, bruno
}

func TestFirstAccountIsAdministrator(t *testing.T) {
	s, _, ana, bruno := adminEnv(t)
	if !ana.Admin || bruno.Admin {
		t.Fatalf("ana admin %v, bruno admin %v; want true, false", ana.Admin, bruno.Admin)
	}
	carla, err := s.Create(context.Background(), NewUser{Email: "carla@example.com", Name: "Carla", Password: "carla-password", Admin: true})
	if err != nil || !carla.Admin {
		t.Fatalf("created with Admin: %+v %v", carla, err)
	}
}

func TestSetAdminFromTheCLI(t *testing.T) {
	s, repo, _, bruno := adminEnv(t)
	ctx := context.Background()
	for _, admin := range []bool{true, false} {
		if err := s.SetAdmin(ctx, "BRUNO@example.com", admin); err != nil {
			t.Fatal(err)
		}
		if got := repo.users[bruno.ID].Admin; got != admin {
			t.Fatalf("admin = %v, want %v", got, admin)
		}
	}
	if err := s.SetAdmin(ctx, "nobody@example.com", true); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestNotAnAdministrator(t *testing.T) {
	s, repo, ana, bruno := adminEnv(t)
	ctx := context.Background()
	disabledAdmin, _ := s.Create(ctx, NewUser{Email: "carla@example.com", Name: "Carla", Password: "carla-password", Admin: true})
	_ = repo.SetDisabledAt(ctx, disabledAdmin.ID, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	for _, actor := range []string{bruno.ID, disabledAdmin.ID, "missing", ""} {
		before := len(repo.users)
		if _, err := s.ListFor(ctx, actor); !errors.Is(err, core.ErrForbidden) {
			t.Errorf("%s list: %v", actor, err)
		}
		if _, err := s.CreateBy(ctx, actor, NewUser{Email: "dan@example.com", Name: "Dan", Password: "dan-password"}); !errors.Is(err, core.ErrForbidden) {
			t.Errorf("%s create: %v", actor, err)
		}
		if _, err := s.UpdateBy(ctx, actor, ana.ID, Changes{Name: ptr("Hacked"), Disabled: ptr(true)}); !errors.Is(err, core.ErrForbidden) {
			t.Errorf("%s update: %v", actor, err)
		}
		if len(repo.users) != before || repo.users[ana.ID].Name != "Ana Souza" || repo.users[ana.ID].Disabled() {
			t.Fatalf("%s changed something: %+v", actor, repo.users[ana.ID])
		}
	}
}

func TestCreateBy(t *testing.T) {
	s, _, ana, _ := adminEnv(t)
	ctx := context.Background()
	list, err := s.ListFor(ctx, ana.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %d %v", len(list), err)
	}
	tests := []struct {
		name  string
		in    NewUser
		admin bool
		field string
		err   error
	}{
		{"user", NewUser{Email: "Dan@Example.com", Name: "Dan", Password: "dan-password"}, false, "", nil},
		{"administrator", NewUser{Email: "eva@example.com", Name: "Eva", Password: "eva-password", Admin: true}, true, "", nil},
		{"duplicate in another case", NewUser{Email: "BRUNO@example.com", Name: "B", Password: "0123456789"}, false, "", core.ErrConflict},
		{"short password", NewUser{Email: "fred@example.com", Name: "Fred", Password: "short"}, false, "password", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := s.CreateBy(ctx, ana.ID, tt.in)
			var inv *core.InvalidError
			switch {
			case tt.err != nil:
				if !errors.Is(err, tt.err) {
					t.Fatalf("err = %v, want %v", err, tt.err)
				}
			case tt.field != "":
				if !errors.As(err, &inv) || inv.Field != tt.field {
					t.Fatalf("err = %v, want invalid %s", err, tt.field)
				}
			default:
				if err != nil || u.Admin != tt.admin {
					t.Fatalf("created %+v %v", u, err)
				}
				if _, _, err := s.Login(ctx, tt.in.Email, tt.in.Password); err != nil {
					t.Fatalf("sign in: %v", err)
				}
			}
		})
	}
}

func TestUpdateBy(t *testing.T) {
	s, repo, ana, bruno := adminEnv(t)
	ctx := context.Background()
	_, token, _ := s.Login(ctx, "bruno@example.com", "bruno-password")

	u, err := s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Name: ptr(" Bruno Lima "), Admin: ptr(true)})
	if err != nil || u.Name != "Bruno Lima" || !u.Admin || u.Disabled() {
		t.Fatalf("name and role: %+v %v", u, err)
	}
	if got := repo.users[bruno.ID]; got.Name != "Bruno Lima" || !got.Admin || got.PasswordHash != bruno.PasswordHash {
		t.Fatalf("stored: %+v", got)
	}
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatalf("a name or role change must keep the sessions: %v", err)
	}

	if _, err := s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Password: ptr("new-bruno-password")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("a new password must end the sessions: %v", err)
	}
	if _, token, err = s.Login(ctx, "bruno@example.com", "new-bruno-password"); err != nil {
		t.Fatalf("new password: %v", err)
	}

	if u, err = s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Disabled: ptr(true)}); err != nil || !u.Disabled() {
		t.Fatalf("disable: %+v %v", u, err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("disabling must end the sessions: %v", err)
	}
	if u, err = s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Disabled: ptr(false)}); err != nil || u.Disabled() {
		t.Fatalf("enable: %+v %v", u, err)
	}

	var inv *core.InvalidError
	for _, c := range []struct {
		field string
		ch    Changes
	}{{"name", Changes{Name: ptr(" ")}}, {"password", Changes{Password: ptr("short"), Name: ptr("Changed")}}} {
		if _, err := s.UpdateBy(ctx, ana.ID, bruno.ID, c.ch); !errors.As(err, &inv) || inv.Field != c.field {
			t.Errorf("invalid %s: %v", c.field, err)
		}
	}
	if repo.users[bruno.ID].Name != "Bruno Lima" {
		t.Fatal("an invalid request changed the name")
	}
	if _, err := s.UpdateBy(ctx, ana.ID, "missing", Changes{Name: ptr("X")}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestUpdateByRefusesSelf(t *testing.T) {
	s, repo, ana, bruno := adminEnv(t)
	ctx := context.Background()
	if _, err := s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Admin: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	// another enabled administrator exists, still refused, and nothing of the request is written
	for _, c := range []Changes{{Disabled: ptr(true)}, {Admin: ptr(false)}, {Admin: ptr(false), Name: ptr("Other")}} {
		if _, err := s.UpdateBy(ctx, ana.ID, ana.ID, c); !errors.Is(err, ErrSelf) || !errors.Is(err, core.ErrConflict) {
			t.Fatalf("self %+v: %v", c, err)
		}
	}
	if got := repo.users[ana.ID]; !got.Admin || got.Disabled() || got.Name != "Ana Souza" {
		t.Fatalf("self change applied: %+v", got)
	}
	// an administrator may still edit their own name and keep their role
	if _, err := s.UpdateBy(ctx, ana.ID, ana.ID, Changes{Name: ptr("Ana Maria"), Admin: ptr(true), Disabled: ptr(false)}); err != nil {
		t.Fatalf("own name: %v", err)
	}
}

// TestUpdateByLastAdministrator: ana and bruno are the enabled administrators; ana demotes bruno
// and bruno demotes ana. Each alone is allowed; together they would leave none.
func TestUpdateByLastAdministrator(t *testing.T) {
	t.Run("in sequence: the second actor is no longer an administrator", func(t *testing.T) {
		s, repo, ana, bruno := adminEnv(t)
		ctx := context.Background()
		_, _ = s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Admin: ptr(true)})
		if _, err := s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Admin: ptr(false)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateBy(ctx, bruno.ID, ana.ID, Changes{Admin: ptr(false)}); !errors.Is(err, core.ErrForbidden) {
			t.Fatalf("second: %v, want ErrForbidden", err)
		}
		if !repo.users[ana.ID].Admin {
			t.Fatal("ana demoted")
		}
	})
	for _, c := range []Changes{{Admin: ptr(false)}, {Disabled: ptr(true), Name: ptr("Gone")}} {
		t.Run(fmt.Sprintf("checked together: %+v", c), func(t *testing.T) {
			s, repo, ana, bruno := adminEnv(t)
			ctx := context.Background()
			_, _ = s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Admin: ptr(true)})
			// bruno's request passed its check; ana's demotion of bruno is written first
			repo.beforeUpdate = func() {
				repo.beforeUpdate = nil
				if _, err := repo.UpdateUser(ctx, bruno.ID, UserChanges{Admin: ptr(false)}); err != nil {
					t.Error(err)
				}
			}
			_, err := s.UpdateBy(ctx, bruno.ID, ana.ID, c)
			if !errors.Is(err, ErrLastAdmin) || !errors.Is(err, core.ErrConflict) {
				t.Fatalf("second: %v, want ErrLastAdmin", err)
			}
			if got := repo.users[ana.ID]; !got.Admin || got.Disabled() || got.Name != "Ana Souza" {
				t.Fatalf("ana changed: %+v", got)
			}
		})
	}
	t.Run("demoting a disabled administrator is allowed", func(t *testing.T) {
		s, _, ana, bruno := adminEnv(t)
		ctx := context.Background()
		_, _ = s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Admin: ptr(true), Disabled: ptr(true)})
		if u, err := s.UpdateBy(ctx, ana.ID, bruno.ID, Changes{Admin: ptr(false)}); err != nil || u.Admin {
			t.Fatalf("demote disabled bruno: %+v %v", u, err)
		}
	})
}

func TestChangePassword(t *testing.T) {
	s, repo, ana, _ := adminEnv(t)
	ctx := context.Background()
	_, current, _ := s.Login(ctx, "ana@example.com", "correct-horse")
	_, other, _ := s.Login(ctx, "ana@example.com", "correct-horse")

	var inv *core.InvalidError
	if err := s.ChangePassword(ctx, ana.ID, current, "wrong-horse", "battery-staple"); !errors.As(err, &inv) || inv.Field != "current" {
		t.Fatalf("wrong current: %v", err)
	}
	if err := s.ChangePassword(ctx, ana.ID, current, "correct-horse", "short"); !errors.As(err, &inv) || inv.Field != "password" {
		t.Fatalf("short: %v", err)
	}
	if repo.users[ana.ID].PasswordHash != ana.PasswordHash {
		t.Fatal("a refused change stored a password")
	}
	if _, err := s.Authenticate(ctx, other); err != nil {
		t.Fatalf("a refused change ended a session: %v", err)
	}

	if err := s.ChangePassword(ctx, ana.ID, current, "correct-horse", "battery-staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, current); err != nil {
		t.Fatalf("the current session must stay: %v", err)
	}
	if _, err := s.Authenticate(ctx, other); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("the other session must end: %v", err)
	}
	if _, _, err := s.Login(ctx, "ana@example.com", "battery-staple"); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if _, _, err := s.Login(ctx, "ana@example.com", "correct-horse"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: %v", err)
	}
}
