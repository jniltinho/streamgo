package store

// Portions adapted from MeetHub (MIT): internal/database/errors.go

import (
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
)

// Portable constraint errors, returned by Translate. Repositories turn them into the core's
// errors (a duplicate e-mail, a reference to a missing broadcast).
var (
	// ErrDuplicate is a unique or primary-key violation.
	ErrDuplicate = errors.New("duplicate value")
	// ErrForeignKey is a foreign-key violation.
	ErrForeignKey = errors.New("foreign key violation")
	// ErrCheck is a CHECK constraint violation.
	ErrCheck = errors.New("check constraint violation")
)

// SQLite extended result codes (https://sqlite.org/rescode.html).
const (
	sqliteConstraintCheck      = 275
	sqliteConstraintForeignKey = 787
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// Translate maps the constraint errors of every dialect to ErrDuplicate, ErrForeignKey or
// ErrCheck (wrapping the original); other errors are returned unchanged.
func Translate(err error) error {
	if err == nil {
		return nil
	}
	var (
		sqliteErr *sqlite.Error
		myErr     *mysqldriver.MySQLError
		pgErr     *pgconn.PgError
	)
	switch {
	case errors.As(err, &sqliteErr):
		switch sqliteErr.Code() {
		case sqliteConstraintUnique, sqliteConstraintPrimaryKey:
			return errors.Join(ErrDuplicate, err)
		case sqliteConstraintForeignKey:
			return errors.Join(ErrForeignKey, err)
		case sqliteConstraintCheck:
			return errors.Join(ErrCheck, err)
		}
	case errors.As(err, &myErr):
		switch myErr.Number {
		case 1062: // ER_DUP_ENTRY
			return errors.Join(ErrDuplicate, err)
		case 1451, 1452: // ER_ROW_IS_REFERENCED_2, ER_NO_REFERENCED_ROW_2
			return errors.Join(ErrForeignKey, err)
		case 4025: // ER_CONSTRAINT_FAILED (MariaDB)
			return errors.Join(ErrCheck, err)
		}
	case errors.As(err, &pgErr):
		switch pgErr.Code {
		case "23505": // unique_violation
			return errors.Join(ErrDuplicate, err)
		case "23503": // foreign_key_violation
			return errors.Join(ErrForeignKey, err)
		case "23514": // check_violation
			return errors.Join(ErrCheck, err)
		}
	}
	return err
}
