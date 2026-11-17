package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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
	IsAdmin      bool
	DisabledAt   *time.Time
}

func (userRow) TableName() string { return "users" }

func (r userRow) user() account.User {
	u := account.User{ID: r.ID, Email: r.Email, Name: r.Name, PasswordHash: r.PasswordHash, Theme: r.Theme, Language: r.Language, Admin: r.IsAdmin}
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

// CreateUser stores u with a new id; it is an administrator when u.Admin is set or no user
// existed, counted in the same transaction. Two racing first creations may both become
// administrators (neither count sees the other's uncommitted row), never neither. A duplicate
// e-mail wraps core.ErrConflict.
func (a *Accounts) CreateUser(ctx context.Context, u *account.User) error {
	row := userRow{ID: ulid.Make().String(), Email: u.Email, Name: u.Name, PasswordHash: u.PasswordHash, Theme: u.Theme, Language: u.Language, IsAdmin: u.Admin}
	err := Translate(a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !row.IsAdmin {
			var n int64
			if err := tx.Model(&userRow{}).Count(&n).Error; err != nil {
				return err
			}
			row.IsAdmin = n == 0
		}
		return tx.Create(&row).Error
	}))
	if errors.Is(err, ErrDuplicate) {
		return fmt.Errorf("create user: %w", core.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	u.ID, u.Admin = row.ID, row.IsAdmin
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

// SetAdmin grants or revokes the administrator role.
func (a *Accounts) SetAdmin(ctx context.Context, id string, admin bool) error {
	return a.updateUser(ctx, id, map[string]any{"is_admin": admin})
}

// UpdateProfile writes the supplied name, theme and language (nil: unchanged).
func (a *Accounts) UpdateProfile(ctx context.Context, id string, name, theme, language *string) error {
	values := map[string]any{}
	for col, v := range map[string]*string{"name": name, "theme": theme, "language": language} {
		if v != nil {
			values[col] = *v
		}
	}
	if len(values) == 0 {
		return nil
	}
	return a.updateUser(ctx, id, values)
}

// UpdateUser applies c in one transaction: it locks the enabled administrators and the user,
// refuses (core.ErrConflict) a change that would leave no enabled administrator, writes only the
// supplied columns and, when c sets a password or disables the account, deletes its sessions.
func (a *Accounts) UpdateUser(ctx context.Context, id string, c account.UserChanges) (*account.User, error) {
	var out account.User
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Row locks (GORM leaves them out on SQLite, whose transactions take the write lock up
		// front): a concurrent change of an administrator waits, then reads this one's result.
		var admins []string
		if err := tx.Model(&userRow{}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("is_admin = ? AND disabled_at IS NULL", true).Order("id").Pluck("id", &admins).Error; err != nil {
			return err
		}
		var row userRow
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("user: %w", core.ErrNotFound)
		}
		if err != nil {
			return err
		}
		if c.RemovesAdmin() && len(admins) == 1 && admins[0] == id {
			return fmt.Errorf("the last enabled administrator: %w", core.ErrConflict)
		}
		values := map[string]any{}
		if c.Name != nil {
			values["name"], row.Name = *c.Name, *c.Name
		}
		if c.Admin != nil {
			values["is_admin"], row.IsAdmin = *c.Admin, *c.Admin
		}
		if c.DisabledAt != nil {
			var at *time.Time
			if !c.DisabledAt.IsZero() {
				utc := c.DisabledAt.UTC()
				at = &utc
			}
			values["disabled_at"], row.DisabledAt = at, at
		}
		if c.PasswordHash != nil {
			values["password_hash"], row.PasswordHash = *c.PasswordHash, *c.PasswordHash
		}
		if len(values) > 0 {
			if err := tx.Model(&userRow{}).Where("id = ?", id).Updates(values).Error; err != nil {
				return err
			}
		}
		if c.EndsSessions() {
			if err := tx.Where("user_id = ?", id).Delete(&sessionRow{}).Error; err != nil {
				return err
			}
		}
		out = row.user()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	return &out, nil
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

// DeleteOtherSessions removes every session of the user except the one with keepIDHash.
func (a *Accounts) DeleteOtherSessions(ctx context.Context, userID, keepIDHash string) error {
	if err := a.db.WithContext(ctx).Where("user_id = ? AND id_hash <> ?", userID, keepIDHash).Delete(&sessionRow{}).Error; err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}
	return nil
}
