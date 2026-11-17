package store_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"

	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

// versions lists the migration versions of a driver's directory.
func versions(t *testing.T, driver string) []string {
	t.Helper()
	sources, err := store.Migrations(driver)
	if err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(sources, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		v, _, _ := strings.Cut(f, "_")
		out = append(out, v)
	}
	return out
}

// TestMigrationDirectoriesMatch: the three directories list the same versions (design D11).
func TestMigrationDirectoriesMatch(t *testing.T) {
	t.Parallel()
	want := versions(t, store.SQLite)
	if len(want) == 0 {
		t.Fatal("no migrations")
	}
	for _, d := range []string{store.MariaDB, store.Postgres} {
		if got := versions(t, d); !slices.Equal(got, want) {
			t.Errorf("%s migrations %v, sqlite %v", d, got, want)
		}
	}
}

// TestMigrate covers "Idempotent" and "Same version everywhere": up applies everything once,
// a second up applies nothing, and status lists every migration of the directory as applied.
func TestMigrate(t *testing.T) {
	t.Parallel()
	for _, d := range storetest.Drivers() {
		t.Run(d, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			db := storetest.OpenEmpty(t, storetest.NewSettings(t, d))
			m, err := store.NewMigrator(db, nil)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := m.Up(ctx); err != nil || n != len(versions(t, d)) {
				t.Fatalf("first up: %d, %v", n, err)
			}
			if n, err := m.Up(ctx); err != nil || n != 0 {
				t.Fatalf("second up: %d, %v", n, err)
			}
			st, err := m.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var applied []string
			for _, s := range st {
				if s.Applied {
					applied = append(applied, fmt.Sprintf("%05d", s.Version))
				}
			}
			if want := versions(t, store.SQLite); !slices.Equal(applied, want) {
				t.Fatalf("applied %v, want %v", applied, want)
			}
			if pending, err := m.HasPending(ctx); err != nil || pending {
				t.Fatalf("pending %v, %v", pending, err)
			}
		})
	}
}

// TestMigrationUserAdmin is "Existing install": migration 00002_user_admin makes the oldest enabled
// account (the smallest ULID) an administrator, or the oldest account when none is enabled, and no other.
func TestMigrationUserAdmin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                       string
		anaDisabled, brunoDisabled bool
		admin                      string
	}{
		{"oldest enabled", false, false, "ana@example.com"},
		{"oldest is disabled", true, false, "bruno@example.com"},
		{"none enabled", true, true, "ana@example.com"},
	}
	for _, d := range storetest.Drivers() {
		for _, tt := range tests {
			t.Run(d+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				db := storetest.OpenEmpty(t, storetest.NewSettings(t, d))
				sources, err := store.Migrations(d)
				if err != nil {
					t.Fatal(err)
				}
				dialect := map[string]goose.Dialect{store.SQLite: goose.DialectSQLite3, store.MariaDB: goose.DialectMySQL, store.Postgres: goose.DialectPostgres}[d]
				p, err := goose.NewProvider(dialect, db.SQL, sources, goose.WithDisableGlobalRegistry(true))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := p.UpTo(ctx, 1); err != nil {
					t.Fatal(err)
				}
				// bruno is inserted first, but ana has the smaller (older) id
				disabledAt := func(disabled bool) *time.Time {
					if !disabled {
						return nil
					}
					at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
					return &at
				}
				for _, u := range []struct {
					id, email string
					disabled  bool
				}{{"01JUSER2000000000000000000", "bruno@example.com", tt.brunoDisabled}, {"01JUSER1000000000000000000", "ana@example.com", tt.anaDisabled}} {
					if err := store.Translate(db.Gorm.WithContext(ctx).Exec(
						"INSERT INTO users (id, email, name, password_hash, disabled_at) VALUES (?, ?, ?, ?, ?)",
						u.id, u.email, "N", "x", disabledAt(u.disabled)).Error); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := p.UpTo(ctx, 2); err != nil {
					t.Fatal(err)
				}
				users, err := store.NewAccounts(db).ListUsers(ctx)
				if err != nil || len(users) != 2 {
					t.Fatalf("list: %+v %v", users, err)
				}
				for _, u := range users {
					if u.Admin != (u.Email == tt.admin) {
						t.Errorf("%s admin = %v, want only %s", u.Email, u.Admin, tt.admin)
					}
				}
			})
		}
	}
}

func addUser(ctx context.Context, db *store.DB, id, email string) error {
	return store.Translate(db.Gorm.WithContext(ctx).Exec(
		"INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)", id, email, "Ana", "x").Error)
}

// TestForeignKeys is "Foreign keys enforced": a row referencing a missing broadcast is refused.
func TestForeignKeys(t *testing.T) {
	t.Parallel()
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		err := store.Translate(db.Gorm.Exec("INSERT INTO banners (id, broadcast_id, text) VALUES (?, ?, ?)",
			"01JBANNER00000000000000000", "01JMISSING0000000000000000", "Hello").Error)
		if !errors.Is(err, store.ErrForeignKey) {
			t.Fatalf("got %v, want ErrForeignKey", err)
		}
	})
}

// TestEmailCase is "E-mail case": e-mails are stored lower-cased, so Ana@Example.com cannot be
// stored as such, and lower-cased it collides with ana@example.com.
func TestEmailCase(t *testing.T) {
	t.Parallel()
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		if err := addUser(ctx, db, "01JUSER1000000000000000000", "ana@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := addUser(ctx, db, "01JUSER2000000000000000000", "Ana@Example.com"); !errors.Is(err, store.ErrCheck) {
			t.Fatalf("mixed case: got %v, want ErrCheck", err)
		}
		if err := addUser(ctx, db, "01JUSER3000000000000000000", strings.ToLower("Ana@Example.com")); !errors.Is(err, store.ErrDuplicate) {
			t.Fatalf("same e-mail: got %v, want ErrDuplicate", err)
		}
	})
}

// TestSQLiteConcurrentWrites is "Concurrent requests": fifty simultaneous writers, none locked out.
func TestSQLiteConcurrentWrites(t *testing.T) {
	t.Parallel()
	db := storetest.Open(t, store.SQLite)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := range 50 {
		wg.Go(func() {
			errs <- db.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				id := fmt.Sprintf("01JUSER%019d", i)
				if err := tx.Exec("INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)",
					id, fmt.Sprintf("u%d@example.com", i), "U", "x").Error; err != nil {
					return err
				}
				return tx.Exec("INSERT INTO sessions (id_hash, user_id, last_used_at) VALUES (?, ?, ?)",
					fmt.Sprintf("%064d", i), id, time.Now().UTC()).Error
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	var n int64
	if err := db.Gorm.Raw("SELECT COUNT(*) FROM sessions").Scan(&n).Error; err != nil || n != 50 {
		t.Fatalf("%d sessions, %v", n, err)
	}
	var mode string
	if err := db.SQL.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q, %v", mode, err)
	}
}

// TestOpenNeverPrintsTheDSN: connection and parse errors keep the connection string and its password out.
func TestOpenNeverPrintsTheDSN(t *testing.T) {
	t.Parallel()
	const password = "hunter2-very-secret"
	tests := []store.Settings{
		{Driver: store.MariaDB, DSN: "streamgo:" + password + "@tcp(127.0.0.1:1)/streamgo?timeout=1s"},
		{Driver: store.Postgres, DSN: "postgres://streamgo:" + password + "@127.0.0.1:1/streamgo?connect_timeout=1"},
		{Driver: store.MariaDB, DSN: "streamgo:" + password + "@tcp(127.0.0.1:1/broken"},
		{Driver: store.Postgres, DSN: "postgres://streamgo:" + password + "@%zz"},
	}
	for _, s := range tests {
		_, err := store.Open(context.Background(), s)
		if err == nil {
			t.Fatalf("%s opened", s.Driver)
		}
		if strings.Contains(err.Error(), password) {
			t.Errorf("%s error prints the password: %v", s.Driver, err)
		}
	}
}

// TestSecondInstance is "Second server on the same data directory".
func TestSecondInstance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := store.LockInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LockInstance(dir); !errors.Is(err, store.ErrInstanceRunning) {
		t.Fatalf("second lock: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := store.LockInstance(dir)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	_ = again.Close()
}

// TestLockReleasedWhenKilled is "Lock released": a process holding the lock is killed and a new
// lock is taken without cleanup.
func TestLockReleasedWhenKilled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHolder$") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), "STREAMGO_LOCK_HOLDER_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "locked\n" {
		_ = cmd.Process.Kill()
		t.Fatalf("holder said %q", line)
	}
	if _, err := store.LockInstance(dir); !errors.Is(err, store.ErrInstanceRunning) {
		t.Fatalf("while held: %v", err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	l, err := store.LockInstance(dir)
	if err != nil {
		t.Fatalf("after kill: %v", err)
	}
	_ = l.Close()
}

// TestLockHolder is the child process of TestLockReleasedWhenKilled.
func TestLockHolder(t *testing.T) {
	dir := os.Getenv("STREAMGO_LOCK_HOLDER_DIR")
	if dir == "" {
		t.Skip("helper process")
	}
	if _, err := store.LockInstance(dir); err != nil {
		t.Fatal(err)
	}
	fmt.Println("locked")
	time.Sleep(time.Minute)
}
