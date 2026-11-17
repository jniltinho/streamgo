package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/studio"
)

type bannerRow struct {
	ID          string `gorm:"primaryKey"`
	BroadcastID string
	Text        string
}

func (bannerRow) TableName() string { return "banners" }

// Banners implements studio.BannerRepository.
type Banners struct{ db *gorm.DB }

// NewBanners returns the banner repository on db.
func NewBanners(db *DB) *Banners { return &Banners{db: db.Gorm} }

// Create stores bn with a new ULID.
func (s *Banners) Create(ctx context.Context, bn *studio.Banner) error {
	row := bannerRow{ID: ulid.Make().String(), BroadcastID: bn.BroadcastID, Text: bn.Text}
	if err := Translate(s.db.WithContext(ctx).Create(&row).Error); err != nil {
		if errors.Is(err, ErrForeignKey) {
			return fmt.Errorf("broadcast %s: %w", bn.BroadcastID, core.ErrNotFound)
		}
		return fmt.Errorf("create banner: %w", err)
	}
	bn.ID = row.ID
	return nil
}

// Get finds a banner of the broadcast.
func (s *Banners) Get(ctx context.Context, broadcastID, id string) (*studio.Banner, error) {
	var row bannerRow
	err := s.db.WithContext(ctx).Where("broadcast_id = ? AND id = ?", broadcastID, id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("banner: %w", core.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("find banner: %w", err)
	}
	return &studio.Banner{ID: row.ID, BroadcastID: row.BroadcastID, Text: row.Text}, nil
}

// List lists the broadcast's banners, oldest first (ULIDs sort by creation).
func (s *Banners) List(ctx context.Context, broadcastID string) ([]studio.Banner, error) {
	var rows []bannerRow
	if err := s.db.WithContext(ctx).Where("broadcast_id = ?", broadcastID).Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list banners: %w", err)
	}
	out := make([]studio.Banner, 0, len(rows))
	for _, r := range rows {
		out = append(out, studio.Banner{ID: r.ID, BroadcastID: r.BroadcastID, Text: r.Text})
	}
	return out, nil
}

// Delete removes a banner of the broadcast.
func (s *Banners) Delete(ctx context.Context, broadcastID, id string) error {
	res := s.db.WithContext(ctx).Where("broadcast_id = ? AND id = ?", broadcastID, id).Delete(&bannerRow{})
	if res.Error != nil {
		return fmt.Errorf("delete banner: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("banner: %w", core.ErrNotFound)
	}
	return nil
}
