// Package storetest gives tests a fresh, migrated database per test on every driver the
// environment provides: SQLite always, MariaDB and PostgreSQL when make test-db runs their
// throwaway containers (STREAMGO_TEST_MARIADB_ADDR, STREAMGO_TEST_POSTGRES_ADDR).
//
// Portions adapted from MeetHub (MIT): internal/database/dbtest/dbtest.go
package storetest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/store"
)

// The test stack's fixture credentials (deployments/compose/compose.test.yml): never a real deployment's.
const (
	testUser     = "streamgo"
	testPassword = "streamgo-test" //nolint:gosec // fixture of the throwaway test containers
	mariaRoot    = "streamgo-test-root"
)

// Drivers lists every database driver.
func Drivers() []string { return []string{store.SQLite, store.MariaDB, store.Postgres} }

// Each runs fn as a subtest per driver (skipping unavailable servers) with a fresh migrated database.
func Each(t *testing.T, fn func(t *testing.T, db *store.DB)) {
	t.Helper()
	for _, d := range Drivers() {
		t.Run(d, func(t *testing.T) { fn(t, Open(t, d)) })
	}
}

// Open returns a fresh, migrated database of the driver, closed and dropped when the test ends.
func Open(t *testing.T, driver string) *store.DB {
	t.Helper()
	db := OpenEmpty(t, NewSettings(t, driver))
	m, err := store.NewMigrator(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// OpenEmpty opens the database described by s without migrating it; closed when the test ends.
func OpenEmpty(t *testing.T, s store.Settings) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), s)
	if err != nil {
		t.Fatalf("open %s: %v", s.Driver, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// NewSettings creates an empty database of the driver (skipping the test when its server is
// not available) and returns the settings that reach it.
func NewSettings(t *testing.T, driver string) store.Settings {
	t.Helper()
	s := store.Settings{Driver: driver}
	name := "t_" + randomHex(t)
	switch driver {
	case store.SQLite:
		s.SQLitePath = filepath.Join(t.TempDir(), "streamgo.db")
	case store.MariaDB:
		addr := server(t, "MARIADB")
		admin := fmt.Sprintf("root:%s@tcp(%s)/", mariaRoot, addr)
		adminExec(t, "mysql", admin,
			"CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
			"GRANT ALL ON `"+name+"`.* TO '"+testUser+"'@'%'")
		t.Cleanup(func() { adminExec(t, "mysql", admin, "DROP DATABASE IF EXISTS `"+name+"`") })
		s.DSN = fmt.Sprintf("%s:%s@tcp(%s)/%s", testUser, testPassword, addr, name)
	case store.Postgres:
		addr := server(t, "POSTGRES")
		admin := fmt.Sprintf("postgres://%s:%s@%s/streamgo?sslmode=disable", testUser, testPassword, addr)
		adminExec(t, "pgx", admin, `CREATE DATABASE "`+name+`"`)
		t.Cleanup(func() { adminExec(t, "pgx", admin, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`) })
		s.DSN = fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", testUser, testPassword, addr, name)
	default:
		t.Fatalf("unknown driver %q", driver)
	}
	return s
}

func server(t *testing.T, kind string) string {
	t.Helper()
	addr := os.Getenv("STREAMGO_TEST_" + kind + "_ADDR")
	if addr == "" {
		t.Skipf("STREAMGO_TEST_%s_ADDR not set (run make test-db)", kind)
	}
	return addr
}

func adminExec(t *testing.T, driver, dsn string, stmts ...string) {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
