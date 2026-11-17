package studio

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
)

// Guest is someone who joined a broadcast by its invite link (design D6).
type Guest struct {
	ID          string
	BroadcastID string
	Name        string
	CookieHash  string // hex SHA-256 of the guest cookie's value
	JoinedAt    time.Time
	LastJoinAt  time.Time
	RemovedAt   time.Time // zero unless removed
}

// Identity is the guest's identity in the room.
func (g *Guest) Identity() string { return GuestPrefix + g.ID }

// GuestRepository stores guests; internal/store implements it. Finders answer an error
// wrapping core.ErrNotFound when nothing matches.
type GuestRepository interface {
	// Create stores g, assigning its ID.
	Create(ctx context.Context, g *Guest) error
	Get(ctx context.Context, broadcastID, id string) (*Guest, error)
	FindByCookie(ctx context.Context, broadcastID, cookieHash string) (*Guest, error)
	// Rejoin sets the guest's name and last join time.
	Rejoin(ctx context.Context, id, name string, at time.Time) error
	MarkRemoved(ctx context.Context, id string, at time.Time) error
	// CountSince counts the broadcast's guests whose last join is after since.
	CountSince(ctx context.Context, broadcastID string, since time.Time) (int, error)
	// DeleteBefore deletes every guest whose last join is before t.
	DeleteBefore(ctx context.Context, t time.Time) (int64, error)
}

// Invite returns the broadcast whose invite link carries token; core.ErrNotFound otherwise.
func (s *Service) Invite(ctx context.Context, token string) (*broadcast.Broadcast, error) {
	if token == "" {
		return nil, fmt.Errorf("invite: %w", core.ErrNotFound)
	}
	return s.Broadcasts.FindByInviteHash(ctx, Hash(token))
}

// GuestJoin is the result of a join: the guest's token and the cookie value to set.
type GuestJoin struct {
	Join
	Title  string
	Cookie string
}

// JoinAsGuest joins broadcast b (from Invite) as name. A cookie of an unexpired, unremoved
// guest of b returns that guest's identity; otherwise a new guest is created, backstage,
// unless the broadcast already has MaxGuests unexpired guests (ErrFull).
func (s *Service) JoinAsGuest(ctx context.Context, b *broadcast.Broadcast, name, cookie string) (*GuestJoin, error) {
	name, err := text("name", name, MaxNameRunes)
	if err != nil {
		return nil, err
	}
	unlock, err := s.Locks.Lock(ctx, b.ID) // keeps the guest count exact
	if err != nil {
		return nil, err
	}
	g, cookie, err := s.guestFor(ctx, b.ID, name, cookie)
	unlock()
	if err != nil {
		return nil, err
	}
	room := stage.Room(b.ID)
	if err := s.Rooms.CreateRoom(ctx, room, b.Stage, s.MaxParticipants); err != nil {
		return nil, fmt.Errorf("open the studio room: %w", err)
	}
	j, err := s.token(room, g.Identity(), name)
	if err != nil {
		return nil, err
	}
	return &GuestJoin{Join: *j, Title: b.Title, Cookie: cookie}, nil
}

func (s *Service) guestFor(ctx context.Context, broadcastID, name, cookie string) (*Guest, string, error) {
	now := s.now()
	if cookie != "" {
		g, err := s.Guests.FindByCookie(ctx, broadcastID, Hash(cookie))
		switch {
		case err == nil && g.RemovedAt.IsZero() && g.LastJoinAt.After(now.Add(-GuestLifetime)):
			if err := s.Guests.Rejoin(ctx, g.ID, name, now); err != nil {
				return nil, "", fmt.Errorf("rejoin: %w", err)
			}
			return g, cookie, nil
		case err != nil && !errors.Is(err, core.ErrNotFound):
			return nil, "", err
		}
	}
	n, err := s.Guests.CountSince(ctx, broadcastID, now.Add(-GuestLifetime))
	if err != nil {
		return nil, "", err
	}
	if n >= MaxGuests {
		return nil, "", ErrFull
	}
	if cookie, err = randomToken(); err != nil {
		return nil, "", err
	}
	g := &Guest{BroadcastID: broadcastID, Name: name, CookieHash: Hash(cookie), JoinedAt: now, LastJoinAt: now}
	if err := s.Guests.Create(ctx, g); err != nil {
		return nil, "", fmt.Errorf("create the guest: %w", err)
	}
	return g, cookie, nil
}

// RemoveGuest takes a guest off the stage, invalidates their cookie and disconnects them. The
// returned state is what the owner's studio applies, whether or not the room was updated.
func (s *Service) RemoveGuest(ctx context.Context, userID, broadcastID, identity string) (stage.State, error) {
	b, err := s.owned(ctx, userID, broadcastID)
	if err != nil {
		return stage.State{}, err
	}
	id, ok := strings.CutPrefix(identity, GuestPrefix)
	if !ok {
		return stage.State{}, core.Invalid("identity", "Only guests can be removed.")
	}
	g, err := s.Guests.Get(ctx, b.ID, id)
	if err != nil {
		return stage.State{}, err
	}
	st, err := s.Stage.Change(ctx, b.ID, func(ctx context.Context, _ *broadcast.Broadcast, st *stage.State) error {
		if err := s.Guests.MarkRemoved(ctx, g.ID, s.now()); err != nil {
			return fmt.Errorf("remove the guest: %w", err)
		}
		st.Sources = slices.DeleteFunc(st.Sources, func(src stage.Source) bool { return src.P == identity })
		return nil
	})
	if err != nil {
		return stage.State{}, err
	}
	if err := s.Rooms.RemoveParticipant(ctx, stage.Room(b.ID), identity); err != nil {
		s.logger().Warn("disconnect a removed guest", "broadcast", b.ID, "error", err) // the reconciler retries
	}
	return st, nil
}

// knownGuest reports whether identity is an unremoved, unexpired guest of the broadcast.
func (s *Service) knownGuest(ctx context.Context, broadcastID, identity string) (bool, error) {
	id, ok := strings.CutPrefix(identity, GuestPrefix)
	if !ok {
		return false, nil
	}
	g, err := s.Guests.Get(ctx, broadcastID, id)
	if errors.Is(err, core.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return g.RemovedAt.IsZero() && g.LastJoinAt.After(s.now().Add(-GuestLifetime)), nil
}
