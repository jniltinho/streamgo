package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

func TestAccountsUsers(t *testing.T) {
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		repo := store.NewAccounts(db)
		ana := &account.User{Email: "ana@example.com", Name: "Ana Souza", PasswordHash: "h1"}
		if err := repo.CreateUser(ctx, ana); err != nil || len(ana.ID) != 26 || !ana.Admin {
			t.Fatalf("create the first user (an administrator): %v, %+v", err, ana)
		}
		if err := repo.CreateUser(ctx, &account.User{Email: "ana@example.com", Name: "Other", PasswordHash: "h"}); !errors.Is(err, core.ErrConflict) {
			t.Fatalf("duplicate: %v, want ErrConflict", err)
		}
		// the database itself refuses an e-mail that is not lower-cased
		if err := repo.CreateUser(ctx, &account.User{Email: "Bob@example.com", Name: "Bob", PasswordHash: "h"}); err == nil {
			t.Fatal("upper-case e-mail stored")
		}
		bob := &account.User{Email: "bob@example.com", Name: "Bob", PasswordHash: "h", Admin: true}
		if err := repo.CreateUser(ctx, bob); err != nil {
			t.Fatal(err)
		}

		got, err := repo.UserByEmail(ctx, "ana@example.com")
		if err != nil || got.ID != ana.ID || got.Name != "Ana Souza" || got.Disabled() || !got.Admin {
			t.Fatalf("by e-mail: %+v %v", got, err)
		}
		if got, _ = repo.UserByID(ctx, bob.ID); !got.Admin {
			t.Fatalf("created as administrator: %+v", got)
		}
		carla := &account.User{Email: "carla@example.com", Name: "Carla", PasswordHash: "h"}
		if err := repo.CreateUser(ctx, carla); err != nil || carla.Admin {
			t.Fatalf("not the first: %+v %v", carla, err)
		}
		if err := db.Gorm.Exec("DELETE FROM users WHERE id = ?", carla.ID).Error; err != nil {
			t.Fatal(err)
		}
		for _, admin := range []bool{true, true, false} { // the same value twice must not fail
			if err := repo.SetAdmin(ctx, ana.ID, admin); err != nil {
				t.Fatal(err)
			}
			if got, _ = repo.UserByID(ctx, ana.ID); got.Admin != admin {
				t.Fatalf("admin = %v, want %v", got.Admin, admin)
			}
		}
		if _, err := repo.UserByID(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
		list, err := repo.ListUsers(ctx)
		if err != nil || len(list) != 2 || list[0].Email != "ana@example.com" {
			t.Fatalf("list: %+v %v", list, err)
		}

		for range 2 { // the same values twice must not fail (MariaDB reports 0 changed rows)
			if err := repo.UpdateProfile(ctx, ana.ID, nil, ptr("streamgo-light"), ptr("pt-BR")); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.SetPasswordHash(ctx, ana.ID, "h2"); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpdateProfile(ctx, ana.ID, ptr("Ana Maria"), nil, nil); err != nil {
			t.Fatal(err)
		}
		at := time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC)
		if err := repo.SetDisabledAt(ctx, ana.ID, at); err != nil {
			t.Fatal(err)
		}
		got, _ = repo.UserByID(ctx, ana.ID)
		if got.Name != "Ana Maria" || got.Theme != "streamgo-light" || got.Language != "pt-BR" || got.PasswordHash != "h2" || !got.DisabledAt.Equal(at) {
			t.Fatalf("after updates: %+v", got)
		}
		if err := repo.SetDisabledAt(ctx, ana.ID, time.Time{}); err != nil {
			t.Fatal(err)
		}
		if got, _ = repo.UserByID(ctx, ana.ID); got.Disabled() {
			t.Fatal("still disabled")
		}
	})
}

func ptr[T any](v T) *T { return &v }

// TestAccountsUpdateUser: one transaction writes only the supplied columns, ends the sessions on
// a new password or a disabled state, and refuses to leave no enabled administrator.
func TestAccountsUpdateUser(t *testing.T) {
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		repo := store.NewAccounts(db)
		ana := &account.User{Email: "ana@example.com", Name: "Ana", PasswordHash: "h", Theme: "matrix"}
		bruno := &account.User{Email: "bruno@example.com", Name: "Bruno", PasswordHash: "h", Admin: true}
		for _, u := range []*account.User{ana, bruno} {
			if err := repo.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
		}
		used := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
		session := func(h string) {
			if err := repo.CreateSession(ctx, account.Session{IDHash: h, UserID: bruno.ID, LastUsedAt: used}); err != nil {
				t.Fatal(err)
			}
		}
		alive := func(h string) bool { _, err := repo.ActiveSession(ctx, h); return err == nil }
		session("s1")

		u, err := repo.UpdateUser(ctx, bruno.ID, account.UserChanges{Name: ptr("Bruno Lima")})
		if err != nil || u.Name != "Bruno Lima" || !u.Admin || u.PasswordHash != "h" || !alive("s1") {
			t.Fatalf("name only: %+v %v", u, err)
		}
		if u, err = repo.UpdateUser(ctx, bruno.ID, account.UserChanges{PasswordHash: ptr("h2")}); err != nil || u.PasswordHash != "h2" || alive("s1") {
			t.Fatalf("password: %+v %v, session alive %v", u, err, alive("s1"))
		}
		session("s2")
		at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
		if u, err = repo.UpdateUser(ctx, bruno.ID, account.UserChanges{DisabledAt: &at}); err != nil || !u.DisabledAt.Equal(at) || alive("s2") {
			t.Fatalf("disable: %+v %v", u, err)
		}
		// bruno is disabled: ana is the last enabled administrator
		for _, c := range []account.UserChanges{{Admin: ptr(false)}, {DisabledAt: &at, Name: ptr("Gone")}} {
			if _, err := repo.UpdateUser(ctx, ana.ID, c); !errors.Is(err, core.ErrConflict) {
				t.Fatalf("last administrator %+v: %v", c, err)
			}
		}
		if got, _ := repo.UserByID(ctx, ana.ID); !got.Admin || got.Disabled() || got.Name != "Ana" || got.Theme != "matrix" {
			t.Fatalf("a refused update wrote: %+v", got)
		}
		if u, err = repo.UpdateUser(ctx, bruno.ID, account.UserChanges{DisabledAt: &time.Time{}}); err != nil || u.Disabled() {
			t.Fatalf("enable: %+v %v", u, err)
		}
		// two administrators: the first demotion passes, the second would leave none
		if _, err := repo.UpdateUser(ctx, bruno.ID, account.UserChanges{Admin: ptr(false)}); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateUser(ctx, ana.ID, account.UserChanges{Admin: ptr(false)}); !errors.Is(err, core.ErrConflict) {
			t.Fatalf("second demotion: %v", err)
		}
		if _, err := repo.UpdateUser(ctx, "missing", account.UserChanges{Name: ptr("X")}); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
	})
}

// TestAccountsLastAdministratorRace: two administrators demoting each other at the same time, as
// two processes would, leave one enabled administrator.
func TestAccountsLastAdministratorRace(t *testing.T) {
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		repo := store.NewAccounts(db)
		for round := range 5 {
			ids := make([]string, 2)
			for i := range ids {
				u := &account.User{Email: fmt.Sprintf("r%d-%d@example.com", round, i), Name: "A", PasswordHash: "h", Admin: true}
				if err := repo.CreateUser(ctx, u); err != nil {
					t.Fatal(err)
				}
				ids[i] = u.ID
			}
			if round > 0 { // only this round's pair stays an administrator
				if err := db.Gorm.Exec("UPDATE users SET is_admin = ? WHERE email NOT LIKE ?", false, fmt.Sprintf("r%d-%%", round)).Error; err != nil {
					t.Fatal(err)
				}
			}
			errs := make(chan error, 2)
			for _, id := range ids {
				go func() {
					_, err := store.NewAccounts(db).UpdateUser(ctx, id, account.UserChanges{Admin: ptr(false)})
					errs <- err
				}()
			}
			conflicts := 0
			for range 2 {
				if err := <-errs; errors.Is(err, core.ErrConflict) {
					conflicts++
				} else if err != nil {
					t.Fatal(err)
				}
			}
			list, _ := repo.ListUsers(ctx)
			admins := 0
			for _, u := range list {
				if u.Admin {
					admins++
				}
			}
			if conflicts != 1 || admins != 1 {
				t.Fatalf("round %d: %d conflicts, %d administrators; want 1 and 1", round, conflicts, admins)
			}
		}
	})
}

// TestAccountsFirstUserRace: concurrent first creations leave at least one administrator.
func TestAccountsFirstUserRace(t *testing.T) {
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		repo := store.NewAccounts(db)
		errs := make(chan error, 4)
		for i := range 4 {
			go func() {
				errs <- repo.CreateUser(ctx, &account.User{Email: fmt.Sprintf("u%d@example.com", i), Name: "U", PasswordHash: "h"})
			}()
		}
		for range 4 {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		list, err := repo.ListUsers(ctx)
		if err != nil || len(list) != 4 {
			t.Fatalf("list: %v %v", list, err)
		}
		if !slices.ContainsFunc(list, func(u account.User) bool { return u.Admin }) {
			t.Fatal("no administrator")
		}
	})
}

func TestAccountsSessions(t *testing.T) {
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		repo := store.NewAccounts(db)
		ana := &account.User{Email: "ana@example.com", Name: "Ana", PasswordHash: "h"}
		if err := repo.CreateUser(ctx, ana); err != nil {
			t.Fatal(err)
		}
		used := time.Date(2026, 11, 1, 8, 0, 0, 123456000, time.UTC)
		for _, h := range []string{"a", "b"} {
			if err := repo.CreateSession(ctx, account.Session{IDHash: h + "1234", UserID: ana.ID, LastUsedAt: used}); err != nil {
				t.Fatal(err)
			}
		}
		s, err := repo.ActiveSession(ctx, "a1234")
		if err != nil || s.UserID != ana.ID || !s.LastUsedAt.Equal(used) {
			t.Fatalf("active: %+v %v", s, err)
		}
		later := used.Add(time.Hour)
		if err := repo.TouchSession(ctx, "a1234", later); err != nil {
			t.Fatal(err)
		}
		if s, _ = repo.ActiveSession(ctx, "a1234"); !s.LastUsedAt.Equal(later) {
			t.Fatalf("touched: %v", s.LastUsedAt)
		}

		// a disabled user's sessions are not active
		_ = repo.SetDisabledAt(ctx, ana.ID, later)
		if _, err := repo.ActiveSession(ctx, "a1234"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("disabled user's session: %v", err)
		}
		_ = repo.SetDisabledAt(ctx, ana.ID, time.Time{})

		if err := repo.DeleteSession(ctx, "a1234"); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ActiveSession(ctx, "a1234"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("deleted: %v", err)
		}
		for _, h := range []string{"c1234", "d1234"} {
			if err := repo.CreateSession(ctx, account.Session{IDHash: h, UserID: ana.ID, LastUsedAt: used}); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.DeleteOtherSessions(ctx, ana.ID, "c1234"); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ActiveSession(ctx, "c1234"); err != nil {
			t.Fatalf("kept session: %v", err)
		}
		for _, h := range []string{"b1234", "d1234"} {
			if _, err := repo.ActiveSession(ctx, h); !errors.Is(err, core.ErrNotFound) {
				t.Fatalf("other session %s: %v", h, err)
			}
		}
		if err := repo.DeleteUserSessions(ctx, ana.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ActiveSession(ctx, "c1234"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("after deleting the user's sessions: %v", err)
		}
	})
}
