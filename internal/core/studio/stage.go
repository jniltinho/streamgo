package studio

import (
	"context"
	"errors"
	"fmt"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
)

// Patch is the owner's stage patch; a nil field is left as it is. BannerSet with a nil
// BannerID hides the banner.
type Patch struct {
	Layout    *string
	Sources   *[]stage.Source
	BannerSet bool
	BannerID  *string
	Accent    *string
	HideNames *bool
}

// PatchStage applies the owner's patch. Every source must be the owner's or an unremoved
// guest's of this broadcast, and the banner one of its saved banners.
func (s *Service) PatchStage(ctx context.Context, userID, broadcastID string, p Patch) (stage.State, error) {
	if _, err := s.owned(ctx, userID, broadcastID); err != nil {
		return stage.State{}, err
	}
	return s.Stage.Change(ctx, broadcastID, func(ctx context.Context, b *broadcast.Broadcast, st *stage.State) error {
		if p.Layout != nil {
			st.Layout = *p.Layout
		}
		if p.Accent != nil {
			st.Accent = *p.Accent
		}
		if p.HideNames != nil {
			st.HideNames = *p.HideNames
		}
		if p.Sources != nil {
			for _, src := range *p.Sources {
				if src.P == HostIdentity(b.OwnerID) {
					continue
				}
				ok, err := s.knownGuest(ctx, b.ID, src.P)
				if err != nil {
					return err
				}
				if !ok {
					return core.Invalid("sources", "A source is not a participant of this studio.")
				}
			}
			st.Sources = append([]stage.Source{}, *p.Sources...)
		}
		if p.BannerSet {
			st.Banner = nil
			if p.BannerID != nil {
				bn, err := s.Banners.Get(ctx, b.ID, *p.BannerID)
				if errors.Is(err, core.ErrNotFound) {
					return core.Invalid("banner_id", "Unknown banner.")
				}
				if err != nil {
					return err
				}
				st.Banner = &stage.Banner{ID: bn.ID, Text: bn.Text}
			}
		}
		return nil
	})
}

// Banner is a saved banner text.
type Banner struct {
	ID          string `json:"id"`
	BroadcastID string `json:"-"`
	Text        string `json:"text"`
}

// BannerRepository stores banners; internal/store implements it.
type BannerRepository interface {
	// Create stores bn, assigning its ID.
	Create(ctx context.Context, bn *Banner) error
	// Get answers an error wrapping core.ErrNotFound when the broadcast has no such banner.
	Get(ctx context.Context, broadcastID, id string) (*Banner, error)
	List(ctx context.Context, broadcastID string) ([]Banner, error)
	// Delete answers an error wrapping core.ErrNotFound when the broadcast has no such banner.
	Delete(ctx context.Context, broadcastID, id string) error
}

// ListBanners returns the broadcast's saved banners.
func (s *Service) ListBanners(ctx context.Context, userID, broadcastID string) ([]Banner, error) {
	if _, err := s.owned(ctx, userID, broadcastID); err != nil {
		return nil, err
	}
	return s.Banners.List(ctx, broadcastID)
}

// AddBanner saves a banner text of 1 to MaxBannerText characters, at most MaxBanners per broadcast.
func (s *Service) AddBanner(ctx context.Context, userID, broadcastID, txt string) (*Banner, error) {
	if _, err := s.owned(ctx, userID, broadcastID); err != nil {
		return nil, err
	}
	txt, err := text("text", txt, MaxBannerText)
	if err != nil {
		return nil, err
	}
	unlock, err := s.Locks.Lock(ctx, broadcastID) // keeps the count exact
	if err != nil {
		return nil, err
	}
	defer unlock()
	list, err := s.Banners.List(ctx, broadcastID)
	if err != nil {
		return nil, err
	}
	if len(list) >= MaxBanners {
		return nil, core.Invalid("text", fmt.Sprintf("A broadcast keeps at most %d banners.", MaxBanners))
	}
	bn := &Banner{BroadcastID: broadcastID, Text: txt}
	if err := s.Banners.Create(ctx, bn); err != nil {
		return nil, fmt.Errorf("save the banner: %w", err)
	}
	return bn, nil
}

// DeleteBanner deletes a saved banner; deleting the one being shown hides it.
func (s *Service) DeleteBanner(ctx context.Context, userID, broadcastID, bannerID string) (stage.State, error) {
	if _, err := s.owned(ctx, userID, broadcastID); err != nil {
		return stage.State{}, err
	}
	return s.Stage.Change(ctx, broadcastID, func(ctx context.Context, b *broadcast.Broadcast, st *stage.State) error {
		if err := s.Banners.Delete(ctx, b.ID, bannerID); err != nil {
			return err
		}
		if st.Banner != nil && st.Banner.ID == bannerID {
			st.Banner = nil
		}
		return nil
	})
}
