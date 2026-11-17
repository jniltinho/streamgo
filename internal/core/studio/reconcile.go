package studio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/stage"
)

// ReconcileEvery is the period of the room reconciler (design D1).
const ReconcileEvery = 15 * time.Second

// Step is one job of a reconciler pass; other packages add theirs (the live output closes
// orphan ingest sessions).
type Step func(ctx context.Context) error

// Reconciler brings the studio rooms in line with the database at startup and every
// ReconcileEvery: it deletes expired guests, deletes rooms whose broadcast is gone, pushes the
// stored stage to rooms whose metadata version differs, removes participants who are removed
// guests or unknown, and runs the extra Steps.
type Reconciler struct {
	Studio *Service
	Steps  []Step
}

// Run reconciles at once and then every ReconcileEvery until ctx ends. A failed pass is
// logged; the next one tries again.
func (r *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(ReconcileEvery)
	defer t.Stop()
	for {
		if err := r.Once(ctx); err != nil && ctx.Err() == nil {
			r.Studio.logger().Warn("room reconciler", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once runs one pass; every part runs even when another fails, and the errors are joined.
func (r *Reconciler) Once(ctx context.Context) error {
	s := r.Studio
	var errs []error
	if _, err := s.Guests.DeleteBefore(ctx, s.now().Add(-GuestLifetime)); err != nil {
		errs = append(errs, fmt.Errorf("delete expired guests: %w", err))
	}
	rooms, err := s.Rooms.ListRooms(ctx)
	if err != nil {
		errs = append(errs, err)
	}
	for _, room := range rooms {
		if id, ok := strings.CutPrefix(room.Name, stage.RoomPrefix); ok {
			if err := r.room(ctx, id, room); err != nil {
				errs = append(errs, fmt.Errorf("room %s: %w", room.Name, err))
			}
		}
	}
	for _, step := range r.Steps {
		if err := step(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Reconciler) room(ctx context.Context, id string, room Room) error {
	s := r.Studio
	unlock, err := s.Locks.Lock(ctx, id) // a push here must not overtake the writer's
	if err != nil {
		return err
	}
	defer unlock()
	b, err := s.Broadcasts.Get(ctx, id)
	if errors.Is(err, core.ErrNotFound) {
		return s.Rooms.DeleteRoom(ctx, room.Name)
	}
	if err != nil {
		return err
	}
	var md struct {
		V int64 `json:"v"`
	}
	if json.Unmarshal([]byte(room.Metadata), &md) != nil || md.V != b.StageVersion {
		if err := s.Rooms.UpdateMetadata(ctx, room.Name, b.Stage); err != nil && !errors.Is(err, core.ErrNotFound) {
			return err
		}
	}
	ps, err := s.Rooms.ListParticipants(ctx, room.Name)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.Identity == HostIdentity(b.OwnerID) {
			continue
		}
		ok, err := s.knownGuest(ctx, b.ID, p.Identity)
		if err != nil {
			return err
		}
		if !ok {
			if err := s.Rooms.RemoveParticipant(ctx, room.Name, p.Identity); err != nil {
				return err
			}
		}
	}
	return nil
}
