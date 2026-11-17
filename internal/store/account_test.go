package store_test

import (
	"context"
	"errors"
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
		if err := repo.CreateUser(ctx, ana); err != nil || len(ana.ID) != 26 {
			t.Fatalf("create: %v, id %q", err, ana.ID)
		}
		if err := repo.CreateUser(ctx, &account.User{Email: "ana@example.com", Name: "Other", PasswordHash: "h"}); !errors.Is(err, core.ErrConflict) {
			t.Fatalf("duplicate: %v, want ErrConflict", err)
		}
		// the database itself refuses an e-mail that is not lower-cased
		if err := repo.CreateUser(ctx, &account.User{Email: "Bob@example.com", Name: "Bob", PasswordHash: "h"}); err == nil {
			t.Fatal("upper-case e-mail stored")
		}
		if err := repo.CreateUser(ctx, &account.User{Email: "bob@example.com", Name: "Bob", PasswordHash: "h"}); err != nil {
			t.Fatal(err)
		}

		got, err := repo.UserByEmail(ctx, "ana@example.com")
		if err != nil || got.ID != ana.ID || got.Name != "Ana Souza" || got.Disabled() {
			t.Fatalf("by e-mail: %+v %v", got, err)
		}
		if _, err := repo.UserByID(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
		list, err := repo.ListUsers(ctx)
		if err != nil || len(list) != 2 || list[0].Email != "ana@example.com" {
			t.Fatalf("list: %+v %v", list, err)
		}

		for range 2 { // the same values twice must not fail (MariaDB reports 0 changed rows)
			if err := repo.SetPreferences(ctx, ana.ID, "streamgo-light", "pt-BR"); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.SetPasswordHash(ctx, ana.ID, "h2"); err != nil {
			t.Fatal(err)
		}
		if err := repo.SetName(ctx, ana.ID, "Ana Maria"); err != nil {
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
		if err := repo.DeleteUserSessions(ctx, ana.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ActiveSession(ctx, "b1234"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("after deleting the user's sessions: %v", err)
		}
	})
}
