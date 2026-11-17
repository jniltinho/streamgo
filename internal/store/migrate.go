package store

// Portions adapted from MeetHub (MIT): internal/database/migrate.go

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"time"

	"github.com/pressly/goose/v3"
)

// Migrations are goose SQL files per dialect with the same version numbers in each directory
// (design D11). A new migration is added to the three directories with the same number.
//
//go:embed migrations
var migrationsFS embed.FS

// MigrationDir is the directory under migrations/ of a driver.
func MigrationDir(driver string) string {
	if driver == MariaDB {
		return "mysql"
	}
	return driver
}

// Migrations returns the embedded migration files of a driver.
func Migrations(driver string) (fs.FS, error) {
	return fs.Sub(migrationsFS, "migrations/"+MigrationDir(driver))
}

// MigrationStatus describes one migration.
type MigrationStatus struct {
	Version   int64     `json:"version"`
	Name      string    `json:"name"`
	Applied   bool      `json:"applied"`
	AppliedAt time.Time `json:"applied_at,omitzero"`
}

// Migrator applies the embedded migrations of the database's dialect.
// shortcut: no cross-process migration lock; one server runs per deployment (D12) and
// migrate up is run by hand. Add goose's session lockers if migrations ever run concurrently.
type Migrator struct {
	provider *goose.Provider
}

// NewMigrator builds a Migrator for db; logger (optional) logs each applied migration.
func NewMigrator(db *DB, logger *slog.Logger) (*Migrator, error) {
	sources, err := Migrations(db.Driver)
	if err != nil {
		return nil, err
	}
	dialect := map[string]goose.Dialect{SQLite: goose.DialectSQLite3, MariaDB: goose.DialectMySQL, Postgres: goose.DialectPostgres}[db.Driver]
	opts := []goose.ProviderOption{goose.WithDisableGlobalRegistry(true)}
	if logger != nil {
		opts = append(opts, goose.WithSlog(logger))
	}
	p, err := goose.NewProvider(dialect, db.SQL, sources, opts...)
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return &Migrator{provider: p}, nil
}

// Up applies every pending migration and returns how many ran.
func (m *Migrator) Up(ctx context.Context) (int, error) {
	res, err := m.provider.Up(ctx)
	if err != nil {
		return len(res), fmt.Errorf("migrate up: %w", err)
	}
	return len(res), nil
}

// Status lists every migration and whether it is applied.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	st, err := m.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate status: %w", err)
	}
	out := make([]MigrationStatus, 0, len(st))
	for _, s := range st {
		out = append(out, MigrationStatus{
			Version:   s.Source.Version,
			Name:      path.Base(s.Source.Path),
			Applied:   s.State == goose.StateApplied,
			AppliedAt: s.AppliedAt.UTC(),
		})
	}
	return out, nil
}

// HasPending reports whether any migration is not applied.
func (m *Migrator) HasPending(ctx context.Context) (bool, error) {
	return m.provider.HasPending(ctx)
}
