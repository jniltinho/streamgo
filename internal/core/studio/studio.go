// Package studio is the live studio around LiveKit (design D1 and D6): the owner's and the
// guests' tokens, guest sessions, capacity with the owner's priority, removing a guest,
// banners, the logo, and the room reconciler. LiveKit is reached through the Rooms port.
package studio

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
)

// Limits of the studio (spec "studio").
const (
	MaxGuests     = 200            // unexpired guests per broadcast
	GuestLifetime = 24 * time.Hour // after the last join
	MaxNameRunes  = 40
	MaxBanners    = 50
	MaxBannerText = 120
)

// Identity prefixes of the room's participants.
const (
	HostPrefix  = "host-"
	GuestPrefix = "guest-"
)

// ErrFull is a join that would exceed MaxGuests; the HTTP layer answers 429.
var ErrFull = errors.New("the studio has too many guests")

// Room is a LiveKit room.
type Room struct {
	Name     string
	Metadata string
}

// Participant is someone connected to a room.
type Participant struct {
	Identity string
	JoinedAt time.Time
}

// Rooms is LiveKit as the studio uses it (internal/livekit implements it). Methods on a room
// that does not exist answer an error wrapping core.ErrNotFound, except RemoveParticipant and
// DeleteRoom, for which a missing room or participant is not an error, and ListParticipants,
// which answers none.
type Rooms interface {
	stage.Pusher
	// Token signs a 10-minute token to join room as identity, publishing and subscribing,
	// without room administration or metadata updates.
	Token(room, identity, name string) (string, error)
	// CreateRoom opens room with the metadata unless it exists.
	CreateRoom(ctx context.Context, room, metadata string, maxParticipants int) error
	ListRooms(ctx context.Context) ([]Room, error)
	ListParticipants(ctx context.Context, room string) ([]Participant, error)
	RemoveParticipant(ctx context.Context, room, identity string) error
	DeleteRoom(ctx context.Context, room string) error
}

// Join is what a client needs to connect: LiveKit's URL for browsers, a token and the identity.
type Join struct {
	URL      string `json:"url"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

// Service is the studio. Every field is required except Logger, Steps and Now.
type Service struct {
	Broadcasts broadcast.Repository
	Locks      *broadcast.Locks
	Stage      *stage.Writer
	Rooms      Rooms
	Guests     GuestRepository
	Banners    BannerRepository
	// OwnerName returns a user's display name (the accounts package).
	OwnerName func(ctx context.Context, userID string) (string, error)
	// URL is LiveKit's URL for browsers (livekit.url).
	URL             string
	MaxParticipants int
	AssetsDir       string // <data_dir>/assets
	Now             func() time.Time
	Logger          *slog.Logger
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

func (s *Service) logger() *slog.Logger {
	if s.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Logger
}

// owned returns the broadcast when userID owns it; core.ErrNotFound otherwise.
func (s *Service) owned(ctx context.Context, userID, id string) (*broadcast.Broadcast, error) {
	b, err := s.Broadcasts.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.OwnerID != userID {
		return nil, fmt.Errorf("broadcast %s: %w", id, core.ErrNotFound)
	}
	return b, nil
}

// HostIdentity is the owner's identity in the room.
func HostIdentity(userID string) string { return HostPrefix + userID }

// StudioToken opens the broadcast's room when it is not open (with the stored stage state) and
// returns the owner's token. When the room is full without the owner, the guest who joined
// last is removed first.
func (s *Service) StudioToken(ctx context.Context, userID, broadcastID string) (*Join, error) {
	b, err := s.owned(ctx, userID, broadcastID)
	if err != nil {
		return nil, err
	}
	name, err := s.OwnerName(ctx, userID)
	if err != nil {
		return nil, err
	}
	room, identity := stage.Room(b.ID), HostIdentity(userID)
	if err := s.Rooms.CreateRoom(ctx, room, b.Stage, s.MaxParticipants); err != nil {
		return nil, fmt.Errorf("open the studio room: %w", err)
	}
	if err := s.makeRoomForOwner(ctx, room, identity); err != nil {
		return nil, err
	}
	return s.token(room, identity, name)
}

// makeRoomForOwner removes the guest who joined last when the room is full and the owner is
// not in it. That frees a slot without reserving it: the studio retries with a new token.
func (s *Service) makeRoomForOwner(ctx context.Context, room, owner string) error {
	ps, err := s.Rooms.ListParticipants(ctx, room)
	if err != nil {
		return fmt.Errorf("list the studio's participants: %w", err)
	}
	if len(ps) < s.MaxParticipants || slices.ContainsFunc(ps, func(p Participant) bool { return p.Identity == owner }) {
		return nil
	}
	var last *Participant
	for i, p := range ps {
		if strings.HasPrefix(p.Identity, GuestPrefix) && (last == nil || !p.JoinedAt.Before(last.JoinedAt)) {
			last = &ps[i]
		}
	}
	if last == nil {
		return nil
	}
	s.logger().Info("studio full: removing the guest who joined last for the owner", "room", room, "identity", last.Identity)
	return s.Rooms.RemoveParticipant(ctx, room, last.Identity)
}

func (s *Service) token(room, identity, name string) (*Join, error) {
	tok, err := s.Rooms.Token(room, identity, name)
	if err != nil {
		return nil, fmt.Errorf("sign the studio token: %w", err)
	}
	return &Join{URL: s.URL, Token: tok, Identity: identity}, nil
}

// Teardown closes the broadcast's room and deletes its logo file; the broadcasts package
// calls it when it deletes a broadcast (logo is Broadcast.Logo before the delete).
func (s *Service) Teardown(ctx context.Context, broadcastID, logo string) error {
	s.removeLogoFile(logo)
	if err := s.Rooms.DeleteRoom(ctx, stage.Room(broadcastID)); err != nil {
		return fmt.Errorf("close the studio room: %w", err)
	}
	return nil
}

// Hash is the hex SHA-256 of a secret (invite tokens, guest cookies).
func Hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// text trims s and checks it has 1 to max characters without control characters.
func text(field, s string, maxRunes int) (string, error) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n == 0 || n > maxRunes || !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", core.Invalid(field, fmt.Sprintf("Use 1 to %d characters.", maxRunes))
	}
	return s, nil
}
