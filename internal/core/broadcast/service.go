package broadcast

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/jniltinho/streamgo/internal/core"
)

// DefaultStage is the stage state of a new broadcast (design D1). The studio package owns its
// format; this package only stores it and returns it.
const DefaultStage = `{"v":1,"on_air":false,"layout":"grid","sources":[],"banner":null,"logo":null,"accent":"#3d41cf"}`

// MaxTitle is the longest broadcast title, in characters.
const MaxTitle = 120

// Sealer seals and opens stored secrets; internal/sealed.Box implements it.
type Sealer interface {
	Seal(bound string, plain []byte) ([]byte, error)
	Open(bound string, sealed []byte) ([]byte, error)
}

// Remover deletes what a deleted broadcast keeps outside its row: its LiveKit room and its
// logo file. The studio package implements it; it is called after the row is gone.
type Remover interface {
	RemoveBroadcast(ctx context.Context, b Broadcast) error
}

// Service manages broadcasts for their owners: every method that takes an owner answers
// core.ErrNotFound for a broadcast the owner does not have.
type Service struct {
	Repo      Repository
	Locks     *Locks
	Sealer    Sealer
	PublicURL string         // server.public_url; invite links are <PublicURL>/join/<token>
	Remover   Remover        // nil: nothing outside the row is removed
	Quality   func() Quality // the output resolutions: default, ceiling, bitrate (nil: the zero Quality)
	Logger    *slog.Logger
}

// Create creates an offline broadcast with the default stage and a new invite link.
func (s *Service) Create(ctx context.Context, owner, title string) (*Broadcast, error) {
	title, err := validTitle(title)
	if err != nil {
		return nil, err
	}
	b := &Broadcast{OwnerID: owner, Title: title, Stage: DefaultStage, Output: Output{State: Offline}}
	if _, err := s.newInvite(b); err != nil {
		return nil, err
	}
	if err := s.Repo.Create(ctx, b); err != nil {
		return nil, fmt.Errorf("create broadcast: %w", err)
	}
	return b, nil
}

// List lists the owner's broadcasts, newest first.
func (s *Service) List(ctx context.Context, owner string) ([]Broadcast, error) {
	return s.Repo.ListByOwner(ctx, owner)
}

// Get returns the owner's broadcast.
func (s *Service) Get(ctx context.Context, owner, id string) (*Broadcast, error) {
	b, err := s.Repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.OwnerID != owner {
		return nil, fmt.Errorf("broadcast %s: %w", id, core.ErrNotFound)
	}
	return b, nil
}

// Patch is a change to a broadcast; a nil field is left as it is.
type Patch struct {
	Title      *string
	Resolution *string
}

// Update applies the patch to the owner's broadcast under its lock, all or nothing: a resolution must be
// allowed by the ceiling (core.Invalid) and can change only while the output is offline (core.ErrConflict).
func (s *Service) Update(ctx context.Context, owner, id string, p Patch) (*Broadcast, error) {
	if p.Title == nil && p.Resolution == nil {
		return nil, core.Invalid("title", "give a title or a resolution")
	}
	var title string
	if p.Title != nil {
		t, err := validTitle(*p.Title)
		if err != nil {
			return nil, err
		}
		title = t
	}
	q := s.quality()
	if p.Resolution != nil && !q.Allowed(*p.Resolution) {
		return nil, core.Invalid("resolution", "must be one of "+strings.Join(resolutions(q), ", "))
	}
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	b, err := s.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if p.Title != nil {
		b.Title = title
	}
	if p.Resolution != nil {
		// the value it already goes out at is not a change, in any state
		if b.Output.State != Offline && *p.Resolution != q.Profile(b.Resolution).Resolution {
			return nil, fmt.Errorf("change the resolution: the output is %s: %w", b.Output.State, core.ErrConflict)
		}
		if b.Output.State == Offline {
			b.Resolution = *p.Resolution
		}
	}
	if err := s.Repo.Update(ctx, id, b.Title, b.Resolution); err != nil {
		return nil, fmt.Errorf("update broadcast: %w", err)
	}
	return b, nil
}

// Profile is what the broadcast goes out at.
func (s *Service) Profile(b *Broadcast) Profile { return s.quality().Profile(b.Resolution) }

// Choices are the profiles the owner may choose from.
func (s *Service) Choices() []Profile { return s.quality().Choices() }

func (s *Service) quality() Quality {
	if s.Quality == nil {
		return Quality{}
	}
	return s.Quality()
}

func resolutions(q Quality) []string {
	var out []string
	for _, p := range q.Choices() {
		out = append(out, p.Resolution)
	}
	return out
}

// Delete deletes the owner's broadcast with its banners, guests and destination selections,
// then its room and logo; it answers core.ErrConflict unless the output is offline.
func (s *Service) Delete(ctx context.Context, owner, id string) error {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	b, err := s.Get(ctx, owner, id)
	if err != nil {
		return err
	}
	if b.Output.State != Offline {
		return fmt.Errorf("delete broadcast: the output is %s: %w", b.Output.State, core.ErrConflict)
	}
	if err := s.Repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete broadcast: %w", err)
	}
	if s.Remover != nil {
		if err := s.Remover.RemoveBroadcast(ctx, *b); err != nil && s.Logger != nil {
			// the broadcast is gone; what is left behind does not make the request fail
			s.Logger.WarnContext(ctx, "remove the room and logo of a deleted broadcast", "broadcast", id, "error", err)
		}
	}
	return nil
}

// InviteURL returns the broadcast's current invite link.
func (s *Service) InviteURL(b *Broadcast) (string, error) {
	token, err := s.Sealer.Open(inviteBound(b.OwnerID), b.InviteSealed)
	if err != nil {
		return "", fmt.Errorf("open the invite token of broadcast %s: %w", b.ID, err)
	}
	return s.joinURL(string(token)), nil
}

// RotateInvite gives the owner's broadcast a new invite link; the previous one stops working.
func (s *Service) RotateInvite(ctx context.Context, owner, id string) (string, error) {
	b, err := s.Get(ctx, owner, id)
	if err != nil {
		return "", err
	}
	token, err := s.newInvite(b)
	if err != nil {
		return "", err
	}
	if err := s.Repo.SetInvite(ctx, id, b.InviteSealed, b.InviteHash); err != nil {
		return "", fmt.Errorf("rotate the invite link: %w", err)
	}
	return s.joinURL(token), nil
}

// FindByInvite returns the broadcast whose current invite token is token (core.ErrNotFound
// for an unknown or rotated one). The join handlers of the studio package call it.
func (s *Service) FindByInvite(ctx context.Context, token string) (*Broadcast, error) {
	if token == "" {
		return nil, fmt.Errorf("invite: %w", core.ErrNotFound)
	}
	return s.Repo.FindByInviteHash(ctx, InviteHash(token))
}

// InviteHash is the lookup hash of an invite token.
func InviteHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newInvite sets a new sealed token and its hash on b and returns the token.
func (s *Service) newInvite(b *Broadcast) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sealed, err := s.Sealer.Seal(inviteBound(b.OwnerID), []byte(token))
	if err != nil {
		return "", fmt.Errorf("seal the invite token: %w", err)
	}
	b.InviteSealed, b.InviteHash = sealed, InviteHash(token)
	return token, nil
}

func (s *Service) joinURL(token string) string {
	return strings.TrimSuffix(s.PublicURL, "/") + "/join/" + token
}

// inviteBound ties a sealed token to its owner (the row's id does not exist before it is stored).
func inviteBound(owner string) string { return "invite:" + owner }

func validTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if n := utf8.RuneCountInString(title); n == 0 || n > MaxTitle {
		return "", core.Invalid("title", fmt.Sprintf("must be 1 to %d characters", MaxTitle))
	}
	return title, nil
}
