// Package account is the accounts domain (work package A): users created from the CLI or by
// an administrator, argon2id passwords, server-side sessions that expire after 14 days without
// use, and disabling an account, which ends its sessions at once.
package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jniltinho/streamgo/internal/core"
)

// Account rules.
const (
	MinPasswordChars = 10
	MaxPasswordBytes = 256
	MaxNameChars     = 200
	MaxEmailBytes    = 320
	// SessionIdle is how long a session lives without use.
	SessionIdle = 14 * 24 * time.Hour
	// touchEvery bounds session writes: a session used again within it is not rewritten.
	touchEvery = time.Minute
)

// Errors of the accounts domain.
var (
	// ErrInvalidCredentials is a failed sign-in, the same for an unknown e-mail, a wrong
	// password and a disabled account.
	ErrInvalidCredentials = errors.New("invalid e-mail or password")
	// ErrUnauthenticated is a missing, unknown or expired session.
	ErrUnauthenticated = errors.New("no valid session")
	// ErrSelf is an administrator disabling or demoting their own account; it wraps core.ErrConflict.
	ErrSelf = fmt.Errorf("an administrator cannot disable or demote their own account: %w", core.ErrConflict)
	// ErrLastAdmin is a change that would leave no enabled administrator; it wraps core.ErrConflict.
	ErrLastAdmin = fmt.Errorf("an install needs an enabled administrator: %w", core.ErrConflict)
)

// User is one account. Theme and Language are "" when not chosen.
type User struct {
	ID           string
	Email        string // lower-cased
	Name         string
	PasswordHash string
	Theme        string
	Language     string
	Admin        bool
	DisabledAt   time.Time // zero while enabled
}

// Disabled reports whether the account is disabled.
func (u User) Disabled() bool { return !u.DisabledAt.IsZero() }

// Session is a signed-in browser; only the hash of its cookie value is stored.
type Session struct {
	IDHash     string
	UserID     string
	LastUsedAt time.Time
}

// Repository stores users and sessions; internal/store implements it. Lookups return an
// error wrapping core.ErrNotFound when nothing matches.
type Repository interface {
	// CreateUser stores u and sets u.ID and u.Admin: the account is an administrator when u.Admin
	// is set or when no account existed, read and written in one transaction (two racing first
	// creations may both become administrators, never neither). A duplicate e-mail wraps
	// core.ErrConflict.
	CreateUser(ctx context.Context, u *User) error
	UserByID(ctx context.Context, id string) (*User, error)
	UserByEmail(ctx context.Context, email string) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	SetPasswordHash(ctx context.Context, id, hash string) error
	// SetDisabledAt disables the user at t, or enables them when t is zero.
	SetDisabledAt(ctx context.Context, id string, t time.Time) error
	// UpdateProfile writes the supplied columns (nil: unchanged).
	UpdateProfile(ctx context.Context, id string, name, theme, language *string) error
	// SetAdmin grants or revokes the role without any check (the CLI's).
	SetAdmin(ctx context.Context, id string, admin bool) error
	// UpdateUser applies c in one transaction and returns the user after it: only the supplied
	// columns change, and the user's sessions end in the same transaction when c sets a password
	// or disables the account. A change that would leave no enabled administrator wraps
	// core.ErrConflict, checked under row locks so two processes cannot both pass it.
	UpdateUser(ctx context.Context, id string, c UserChanges) (*User, error)

	CreateSession(ctx context.Context, s Session) error
	// ActiveSession returns the session only while its user is enabled.
	ActiveSession(ctx context.Context, idHash string) (*Session, error)
	TouchSession(ctx context.Context, idHash string, at time.Time) error
	DeleteSession(ctx context.Context, idHash string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	// DeleteOtherSessions removes every session of the user except the one with keepIDHash.
	DeleteOtherSessions(ctx context.Context, userID, keepIDHash string) error
}

// UserChanges are the columns of Repository.UpdateUser; nil leaves a column as is.
type UserChanges struct {
	Name         *string
	Admin        *bool
	DisabledAt   *time.Time // a zero time enables the account
	PasswordHash *string
}

// RemovesAdmin reports whether c takes an account out of the enabled administrators.
func (c UserChanges) RemovesAdmin() bool {
	return (c.Admin != nil && !*c.Admin) || (c.DisabledAt != nil && !c.DisabledAt.IsZero())
}

// EndsSessions reports whether c ends the account's sessions: a new password or disabling it.
func (c UserChanges) EndsSessions() bool {
	return c.PasswordHash != nil || (c.DisabledAt != nil && !c.DisabledAt.IsZero())
}

// Options tune a Service; the zero value is the production setting.
type Options struct {
	Now    func() time.Time // the clock (time.Now)
	Argon2 Argon2Params     // the hashing cost (64 MiB, 3 passes, 2 lanes)
}

// Service manages accounts and sessions.
type Service struct {
	repo Repository
	pw   *hasher
	now  func() time.Time
}

// New returns the accounts service.
func New(repo Repository, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Argon2 == (Argon2Params{}) {
		o.Argon2 = defaultArgon2()
	}
	return &Service{repo: repo, pw: newHasher(o.Argon2, runtime.GOMAXPROCS(0)), now: o.Now}
}

// NewUser is the input of Create.
type NewUser struct {
	Email    string
	Name     string
	Password string
	Admin    bool
}

// Create adds an account; an e-mail that exists in any letter case wraps core.ErrConflict.
// The first account of an install is an administrator whatever in.Admin says.
func (s *Service) Create(ctx context.Context, in NewUser) (*User, error) {
	return s.create(ctx, "", in)
}

// CreateBy is Create done by the administrator actorID (core.ErrForbidden otherwise).
func (s *Service) CreateBy(ctx context.Context, actorID string, in NewUser) (*User, error) {
	if actorID == "" {
		return nil, core.ErrForbidden
	}
	return s.create(ctx, actorID, in)
}

// create checks the actor (none: the CLI) twice: first, so that someone who is not an administrator learns
// nothing and costs no password hash, and again after the slow hashing, right before the write.
func (s *Service) create(ctx context.Context, actorID string, in NewUser) (*User, error) {
	if actorID != "" {
		if err := s.RequireAdmin(ctx, actorID); err != nil {
			return nil, err
		}
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, err
	}
	if err := checkPassword(in.Password); err != nil {
		return nil, err
	}
	hash, err := s.pw.hash(ctx, in.Password)
	if err != nil {
		return nil, err
	}
	if actorID != "" {
		if err := s.RequireAdmin(ctx, actorID); err != nil {
			return nil, err
		}
	}
	u := &User{Email: email, Name: name, PasswordHash: hash, Admin: in.Admin}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		if errors.Is(err, core.ErrConflict) {
			return nil, fmt.Errorf("a user with the e-mail %s already exists: %w", email, core.ErrConflict)
		}
		return nil, err
	}
	return u, nil
}

// cleanName trims a display name and checks it: 1 to MaxNameChars characters, valid UTF-8 and no control
// characters (a name is printed by `streamgo user list` on the operator's terminal).
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameChars || !utf8.ValidString(name) ||
		strings.ContainsFunc(name, unicode.IsControl) {
		return "", core.Invalid("name", fmt.Sprintf("must have 1 to %d characters, none of them a control character", MaxNameChars))
	}
	return name, nil
}

// Rename changes the user's display name (the name tag in the studio) and returns the user.
func (s *Service) Rename(ctx context.Context, id, name string) (*User, error) {
	return s.UpdateProfile(ctx, id, &name, nil, nil)
}

// UpdateProfile changes the display name, the theme and the language (nil: unchanged) in one update and
// returns the user: nothing is stored when the name is refused, and a field that was not given is not written.
// The caller validates the theme and the language against the theme registry and the catalogs.
func (s *Service) UpdateProfile(ctx context.Context, id string, name, theme, language *string) (*User, error) {
	if name != nil {
		clean, err := cleanName(*name)
		if err != nil {
			return nil, err
		}
		name = &clean
	}
	if _, err := s.repo.UserByID(ctx, id); err != nil {
		return nil, err
	}
	if name != nil || theme != nil || language != nil {
		if err := s.repo.UpdateProfile(ctx, id, name, theme, language); err != nil {
			return nil, err
		}
	}
	return s.repo.UserByID(ctx, id)
}

// RequireAdmin answers core.ErrForbidden unless actorID is an enabled administrator.
func (s *Service) RequireAdmin(ctx context.Context, actorID string) error {
	a, err := s.repo.UserByID(ctx, actorID)
	if errors.Is(err, core.ErrNotFound) || (err == nil && (!a.Admin || a.Disabled())) {
		return fmt.Errorf("this needs an administrator: %w", core.ErrForbidden)
	}
	return err
}

// ListFor returns every account to the administrator actorID (core.ErrForbidden otherwise).
func (s *Service) ListFor(ctx context.Context, actorID string) ([]User, error) {
	if err := s.RequireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	return s.repo.ListUsers(ctx)
}

// Changes are the fields of an account an administrator changes; nil leaves a field as is.
type Changes struct {
	Name     *string
	Admin    *bool
	Disabled *bool
	Password *string
}

// UpdateBy applies c to the account id for the administrator actorID, in one repository update,
// and returns the account. It answers core.ErrForbidden to anyone else, ErrSelf when the
// administrator would disable or demote themself and ErrLastAdmin when no enabled administrator
// would remain (the repository checks it under row locks, so it holds between processes). A new
// password or a disabled state ends the account's sessions in the same update.
func (s *Service) UpdateBy(ctx context.Context, actorID, id string, c Changes) (*User, error) {
	// authorization first: someone who is not an administrator learns nothing and costs no password hash
	if err := s.RequireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	var ch UserChanges
	if c.Name != nil {
		name, err := cleanName(*c.Name)
		if err != nil {
			return nil, err
		}
		ch.Name = &name
	}
	if c.Password != nil {
		if err := checkPassword(*c.Password); err != nil {
			return nil, err
		}
		hash, err := s.pw.hash(ctx, *c.Password)
		if err != nil {
			return nil, err
		}
		ch.PasswordHash = &hash
	}
	ch.Admin = c.Admin
	if c.Disabled != nil {
		var at time.Time
		if *c.Disabled {
			at = s.now().UTC()
		}
		ch.DisabledAt = &at
	}

	if id == actorID && ch.RemovesAdmin() {
		return nil, ErrSelf
	}
	u, err := s.repo.UpdateUser(ctx, id, ch)
	if errors.Is(err, core.ErrConflict) {
		return nil, ErrLastAdmin
	}
	return u, err
}

// ChangePassword sets the user's own password after checking the current one (an invalid
// "current" field when it is wrong) and ends their other sessions; the session with the cookie
// value keepToken stays.
func (s *Service) ChangePassword(ctx context.Context, id, keepToken, current, password string) error {
	if err := checkPassword(password); err != nil {
		return err
	}
	u, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return err
	}
	ok, err := s.pw.verify(ctx, u.PasswordHash, current)
	if err != nil {
		return err
	}
	if !ok {
		return core.Invalid("current", "is not your current password")
	}
	hash, err := s.pw.hash(ctx, password)
	if err != nil {
		return err
	}
	if err := s.repo.SetPasswordHash(ctx, id, hash); err != nil {
		return err
	}
	return s.repo.DeleteOtherSessions(ctx, id, hashToken(keepToken))
}

// SetAdmin grants or revokes the administrator role of the account with email. It is the
// CLI's: it may leave an install without an administrator, since the operator can undo it.
func (s *Service) SetAdmin(ctx context.Context, email string, admin bool) error {
	u, err := s.byEmail(ctx, email)
	if err != nil {
		return err
	}
	return s.repo.SetAdmin(ctx, u.ID, admin)
}

// List returns every account.
func (s *Service) List(ctx context.Context) ([]User, error) { return s.repo.ListUsers(ctx) }

// User returns the account with id.
func (s *Service) User(ctx context.Context, id string) (*User, error) {
	return s.repo.UserByID(ctx, id)
}

// SetPassword changes the password of the account with email and ends its sessions.
func (s *Service) SetPassword(ctx context.Context, email, password string) error {
	if err := checkPassword(password); err != nil {
		return err
	}
	u, err := s.byEmail(ctx, email)
	if err != nil {
		return err
	}
	hash, err := s.pw.hash(ctx, password)
	if err != nil {
		return err
	}
	if err := s.repo.SetPasswordHash(ctx, u.ID, hash); err != nil {
		return err
	}
	return s.repo.DeleteUserSessions(ctx, u.ID)
}

// SetDisabled disables the account with email, ending its sessions at once, or enables it.
func (s *Service) SetDisabled(ctx context.Context, email string, disabled bool) error {
	u, err := s.byEmail(ctx, email)
	if err != nil {
		return err
	}
	var at time.Time
	if disabled {
		at = s.now().UTC()
	}
	if err := s.repo.SetDisabledAt(ctx, u.ID, at); err != nil {
		return err
	}
	if !disabled {
		return nil
	}
	return s.repo.DeleteUserSessions(ctx, u.ID)
}

// SetPreferences changes the theme and the language (nil: unchanged) and returns the user.
// The caller validates the values against the theme registry and the catalogs.
func (s *Service) SetPreferences(ctx context.Context, id string, theme, language *string) (*User, error) {
	return s.UpdateProfile(ctx, id, nil, theme, language)
}

// Login checks the e-mail and password and opens a session, returning the user and the
// cookie value. Every failure is ErrInvalidCredentials, after the same hashing work.
func (s *Service) Login(ctx context.Context, email, password string) (*User, string, error) {
	u, err := s.repo.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, core.ErrNotFound) {
		if _, err := s.pw.verify(ctx, s.pw.dummy, password); err != nil {
			return nil, "", err
		}
		return nil, "", ErrInvalidCredentials
	}
	if err != nil {
		return nil, "", err
	}
	ok, err := s.pw.verify(ctx, u.PasswordHash, password)
	if err != nil {
		return nil, "", err
	}
	if !ok || u.Disabled() {
		return nil, "", ErrInvalidCredentials
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	if err := s.repo.CreateSession(ctx, Session{IDHash: hashToken(token), UserID: u.ID, LastUsedAt: s.now().UTC()}); err != nil {
		return nil, "", err
	}
	return u, token, nil
}

// Authenticate returns the user id of the session with the cookie value token and renews
// it; a missing, unknown or idle session, or a disabled user, is ErrUnauthenticated.
func (s *Service) Authenticate(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", ErrUnauthenticated
	}
	h := hashToken(token)
	sess, err := s.repo.ActiveSession(ctx, h)
	if errors.Is(err, core.ErrNotFound) {
		return "", ErrUnauthenticated
	}
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	idle := now.Sub(sess.LastUsedAt)
	if idle >= SessionIdle {
		if err := s.repo.DeleteSession(ctx, h); err != nil {
			return "", err
		}
		return "", ErrUnauthenticated
	}
	if idle >= touchEvery {
		if err := s.repo.TouchSession(ctx, h, now); err != nil {
			return "", err
		}
	}
	return sess.UserID, nil
}

// Logout deletes the session with the cookie value token, if any.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.repo.DeleteSession(ctx, hashToken(token))
}

func (s *Service) byEmail(ctx context.Context, email string) (*User, error) {
	u, err := s.repo.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, core.ErrNotFound) {
		return nil, fmt.Errorf("no user with the e-mail %s: %w", email, core.ErrNotFound)
	}
	return u, err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > MaxEmailBytes {
		return "", core.Invalid("email", "must be a valid e-mail address")
	}
	return email, nil
}

func checkPassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordChars || len(pw) > MaxPasswordBytes {
		return core.Invalid("password", fmt.Sprintf("must have at least %d characters (at most %d bytes)", MinPasswordChars, MaxPasswordBytes))
	}
	return nil
}
