// Package store opens the one relational database StreamGo uses (SQLite by default, pure Go
// through modernc.org/sqlite; MariaDB or PostgreSQL), applies its migrations and implements
// the core's repositories with GORM. It is the only package that knows GORM or a dialect.
//
// Portions adapted from MeetHub (MIT): internal/database/database.go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver
)

// Database drivers (the values of database.driver).
const (
	SQLite   = "sqlite"
	MariaDB  = "mariadb"
	Postgres = "postgres"
)

// sqlitePragmas apply to every SQLite connection: write-ahead logging, foreign keys, a writer
// waits up to 30 s for the lock instead of failing at once, and write transactions take the
// lock up front (_txlock=immediate) so two of them never deadlock upgrading a read lock.
const sqlitePragmas = "_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)" +
	"&_pragma=synchronous(NORMAL)&_txlock=immediate&_time_format=sqlite"

// Settings say which database to open.
type Settings struct {
	Driver     string // SQLite, MariaDB or Postgres
	SQLitePath string
	DSN        string // MariaDB and PostgreSQL; from STREAMGO_DATABASE_DSN only
}

// DB is an open database: the GORM handle, its *sql.DB and the driver.
type DB struct {
	Gorm   *gorm.DB
	SQL    *sql.DB
	Driver string
}

// Open connects to the database and checks it answers. Errors never contain the connection
// string or its password.
func Open(ctx context.Context, s Settings) (*DB, error) {
	sqlDB, password, err := s.openSQL()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(16)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, redact(fmt.Errorf("connect to %s: %w", s.describe(), err), password)
	}
	var dialector gorm.Dialector
	switch s.Driver {
	case MariaDB:
		dialector = mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true})
	case Postgres:
		dialector = postgres.New(postgres.Config{Conn: sqlDB})
	default:
		dialector = gormsqlite.Dialector{DriverName: "sqlite", Conn: sqlDB}
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger:                 logger.Discard,
		NowFunc:                func() time.Time { return time.Now().UTC() },
		SkipDefaultTransaction: true,
		DisableAutomaticPing:   true,
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, redact(fmt.Errorf("open %s: %w", s.describe(), err), password)
	}
	return &DB{Gorm: gdb, SQL: sqlDB, Driver: s.Driver}, nil
}

// openSQL creates the database/sql pool (no connection yet) and returns the DSN's password,
// for redaction.
func (s Settings) openSQL() (*sql.DB, string, error) {
	switch s.Driver {
	case SQLite:
		if err := os.MkdirAll(filepath.Dir(s.SQLitePath), 0o750); err != nil {
			return nil, "", fmt.Errorf("create the database directory: %w", err)
		}
		db, err := sql.Open("sqlite", "file:"+s.SQLitePath+"?"+sqlitePragmas)
		return db, "", err
	case MariaDB:
		mc, err := mysqldriver.ParseDSN(s.DSN)
		if err != nil {
			return nil, "", fmt.Errorf("STREAMGO_DATABASE_DSN is not a valid MariaDB connection string")
		}
		mc.ParseTime, mc.Loc = true, time.UTC
		mc.Collation = "utf8mb4_unicode_ci"
		if mc.Params == nil {
			mc.Params = map[string]string{}
		}
		mc.Params["time_zone"] = "'+00:00'"
		conn, err := mysqldriver.NewConnector(mc)
		if err != nil {
			return nil, "", redact(fmt.Errorf("mariadb: %w", err), mc.Passwd)
		}
		return sql.OpenDB(conn), mc.Passwd, nil
	case Postgres:
		pc, err := pgx.ParseConfig(s.DSN)
		if err != nil {
			return nil, "", fmt.Errorf("STREAMGO_DATABASE_DSN is not a valid PostgreSQL connection string")
		}
		pc.RuntimeParams["timezone"] = "UTC"
		return stdlib.OpenDB(*pc), pc.Password, nil
	default:
		return nil, "", fmt.Errorf("unsupported database driver %q", s.Driver)
	}
}

func (s Settings) describe() string {
	if s.Driver == SQLite {
		return "sqlite " + s.SQLitePath
	}
	return s.Driver
}

// Ping checks the database answers (readiness).
func (db *DB) Ping(ctx context.Context) error { return db.SQL.PingContext(ctx) }

// Close closes the connection pool.
func (db *DB) Close() error { return db.SQL.Close() }

// redact removes password from err's message, keeping err in the chain.
func redact(err error, password string) error {
	if err == nil || password == "" || !strings.Contains(err.Error(), password) {
		return err
	}
	return &redactedError{msg: strings.ReplaceAll(err.Error(), password, "***"), err: err}
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }
