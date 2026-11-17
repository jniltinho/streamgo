package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/studio"
)

type guestRow struct {
	ID          string `gorm:"primaryKey"`
	BroadcastID string
	Name        string
	CookieHash  string
	JoinedAt    time.Time
	LastJoinAt  time.Time
	RemovedAt   *time.Time
}

func (guestRow) TableName() string { return "guests" }

func (r *guestRow) guest() *studio.Guest {
	g := &studio.Guest{
		ID: r.ID, BroadcastID: r.BroadcastID, Name: r.Name, CookieHash: r.CookieHash,
		JoinedAt: r.JoinedAt.UTC(), LastJoinAt: r.LastJoinAt.UTC(),
	}
	if r.RemovedAt != nil {
		g.RemovedAt = r.RemovedAt.UTC()
	}
	return g
}

// Guests implements studio.GuestRepository.
type Guests struct{ db *gorm.DB }

// NewGuests returns the guest repository on db.
func NewGuests(db *DB) *Guests { return &Guests{db: db.Gorm} }

// Create stores g with a new ULID.
func (s *Guests) Create(ctx context.Context, g *studio.Guest) error {
	row := guestRow{
		ID: ulid.Make().String(), BroadcastID: g.BroadcastID, Name: g.Name, CookieHash: g.CookieHash,
		JoinedAt: g.JoinedAt.UTC(), LastJoinAt: g.LastJoinAt.UTC(),
	}
	if err := Translate(s.db.WithContext(ctx).Create(&row).Error); err != nil {
		if errors.Is(err, ErrForeignKey) {
			return fmt.Errorf("broadcast %s: %w", g.BroadcastID, core.ErrNotFound)
		}
		return fmt.Errorf("create guest: %w", err)
	}
	g.ID = row.ID
	return nil
}

func (s *Guests) first(ctx context.Context, query string, args ...any) (*studio.Guest, error) {
	var row guestRow
	err := s.db.WithContext(ctx).Where(query, args...).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("guest: %w", core.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("find guest: %w", err)
	}
	return row.guest(), nil
}

// Get finds a guest of the broadcast.
func (s *Guests) Get(ctx context.Context, broadcastID, id string) (*studio.Guest, error) {
	return s.first(ctx, "broadcast_id = ? AND id = ?", broadcastID, id)
}

// FindByCookie finds a guest of the broadcast by the hash of its cookie.
func (s *Guests) FindByCookie(ctx context.Context, broadcastID, cookieHash string) (*studio.Guest, error) {
	return s.first(ctx, "broadcast_id = ? AND cookie_hash = ?", broadcastID, cookieHash)
}

func (s *Guests) update(ctx context.Context, id string, values map[string]any) error {
	res := s.db.WithContext(ctx).Model(&guestRow{}).Where("id = ?", id).Updates(values)
	if res.Error != nil {
		return fmt.Errorf("update guest: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("guest %s: %w", id, core.ErrNotFound)
	}
	return nil
}

// Rejoin sets the guest's name and last join time.
func (s *Guests) Rejoin(ctx context.Context, id, name string, at time.Time) error {
	return s.update(ctx, id, map[string]any{"name": name, "last_join_at": at.UTC()})
}

// MarkRemoved marks the guest removed at at.
func (s *Guests) MarkRemoved(ctx context.Context, id string, at time.Time) error {
	return s.update(ctx, id, map[string]any{"removed_at": at.UTC()})
}

// CountSince counts the broadcast's guests whose last join is after since.
func (s *Guests) CountSince(ctx context.Context, broadcastID string, since time.Time) (int, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&guestRow{}).
		Where("broadcast_id = ? AND last_join_at > ?", broadcastID, since.UTC()).Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("count guests: %w", err)
	}
	return int(n), nil
}

// DeleteBefore deletes every guest whose last join is before t.
func (s *Guests) DeleteBefore(ctx context.Context, t time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Where("last_join_at < ?", t.UTC()).Delete(&guestRow{})
	if res.Error != nil {
		return 0, fmt.Errorf("delete expired guests: %w", res.Error)
	}
	return res.RowsAffected, nil
}
