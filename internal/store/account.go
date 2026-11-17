package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/account"
)

// Accounts implements account.Repository.
type Accounts struct{ db *gorm.DB }

// NewAccounts returns the users and sessions repository.
func NewAccounts(db *DB) *Accounts { return &Accounts{db: db.Gorm} }

var _ account.Repository = (*Accounts)(nil)

type userRow struct {
	ID           string `gorm:"primaryKey"`
	Email        string
	Name         string
	PasswordHash string
	Theme        string
	Language     string
	DisabledAt   *time.Time
}

func (userRow) TableName() string { return "users" }

func (r userRow) user() account.User {
	u := account.User{ID: r.ID, Email: r.Email, Name: r.Name, PasswordHash: r.PasswordHash, Theme: r.Theme, Language: r.Language}
	if r.DisabledAt != nil {
		u.DisabledAt = r.DisabledAt.UTC()
	}
	return u
}

type sessionRow struct {
	IDHash     string `gorm:"column:id_hash;primaryKey"`
	UserID     string
	LastUsedAt time.Time
}

func (sessionRow) TableName() string { return "sessions" }

// CreateUser stores u with a new id; a duplicate e-mail wraps core.ErrConflict.
func (a *Accounts) CreateUser(ctx context.Context, u *account.User) error {
	row := userRow{ID: ulid.Make().String(), Email: u.Email, Name: u.Name, PasswordHash: u.PasswordHash, Theme: u.Theme, Language: u.Language}
	if err := Translate(a.db.WithContext(ctx).Create(&row).Error); err != nil {
		if errors.Is(err, ErrDuplicate) {
			return fmt.Errorf("create user: %w", core.ErrConflict)
		}
		return fmt.Errorf("create user: %w", err)
	}
	u.ID = row.ID
	return nil
}

// UserByID finds a user by id.
func (a *Accounts) UserByID(ctx context.Context, id string) (*account.User, error) {
	return a.findUser(ctx, "id = ?", id)
}

// UserByEmail finds a user by lower-cased e-mail.
func (a *Accounts) UserByEmail(ctx context.Context, email string) (*account.User, error) {
	return a.findUser(ctx, "email = ?", email)
}

func (a *Accounts) findUser(ctx context.Context, where string, arg string) (*account.User, error) {
	var row userRow
	err := a.db.WithContext(ctx).Where(where, arg).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("user: %w", core.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	u := row.user()
	return &u, nil
}

// ListUsers returns every user ordered by e-mail.
func (a *Accounts) ListUsers(ctx context.Context) ([]account.User, error) {
	var rows []userRow
	if err := a.db.WithContext(ctx).Order("email").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make([]account.User, len(rows))
	for i, r := range rows {
		out[i] = r.user()
	}
	return out, nil
}

func (a *Accounts) updateUser(ctx context.Context, id string, values map[string]any) error {
	if err := a.db.WithContext(ctx).Model(&userRow{}).Where("id = ?", id).Updates(values).Error; err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

// SetPasswordHash replaces the user's password hash.
func (a *Accounts) SetPasswordHash(ctx context.Context, id, hash string) error {
	return a.updateUser(ctx, id, map[string]any{"password_hash": hash})
}

// SetDisabledAt disables the user at t, or enables them when t is zero.
func (a *Accounts) SetDisabledAt(ctx context.Context, id string, t time.Time) error {
	var at *time.Time
	if !t.IsZero() {
		utc := t.UTC()
		at = &utc
	}
	return a.updateUser(ctx, id, map[string]any{"disabled_at": at})
}

// SetName stores the user's display name.
func (a *Accounts) SetName(ctx context.Context, id, name string) error {
	return a.updateUser(ctx, id, map[string]any{"name": name})
}

// SetPreferences stores the user's theme and language.
func (a *Accounts) SetPreferences(ctx context.Context, id, theme, language string) error {
	return a.updateUser(ctx, id, map[string]any{"theme": theme, "language": language})
}

// CreateSession stores a session.
func (a *Accounts) CreateSession(ctx context.Context, s account.Session) error {
	row := sessionRow{IDHash: s.IDHash, UserID: s.UserID, LastUsedAt: s.LastUsedAt.UTC()}
	if err := Translate(a.db.WithContext(ctx).Create(&row).Error); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// ActiveSession returns the session while its user is enabled.
func (a *Accounts) ActiveSession(ctx context.Context, idHash string) (*account.Session, error) {
	var row sessionRow
	err := a.db.WithContext(ctx).
		Joins("JOIN users ON users.id = sessions.user_id AND users.disabled_at IS NULL").
		Where("sessions.id_hash = ?", idHash).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("session: %w", core.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("find session: %w", err)
	}
	return &account.Session{IDHash: row.IDHash, UserID: row.UserID, LastUsedAt: row.LastUsedAt.UTC()}, nil
}

// TouchSession records a use of the session.
func (a *Accounts) TouchSession(ctx context.Context, idHash string, at time.Time) error {
	if err := a.db.WithContext(ctx).Model(&sessionRow{}).Where("id_hash = ?", idHash).Update("last_used_at", at.UTC()).Error; err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// DeleteSession removes one session (none is not an error).
func (a *Accounts) DeleteSession(ctx context.Context, idHash string) error {
	if err := a.db.WithContext(ctx).Where("id_hash = ?", idHash).Delete(&sessionRow{}).Error; err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions removes every session of the user.
func (a *Accounts) DeleteUserSessions(ctx context.Context, userID string) error {
	if err := a.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&sessionRow{}).Error; err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}
