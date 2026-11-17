package stage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
)

// PushTimeout bounds the push of the metadata to the room.
const PushTimeout = 3 * time.Second

// Pusher copies a stage state to a room's metadata; the studio's Rooms port implements it.
// A room that does not exist answers an error wrapping core.ErrNotFound.
type Pusher interface {
	UpdateMetadata(ctx context.Context, room, metadata string) error
}

// Writer is the single writer of the stage state (design D1). Every change goes through
// Change or ChangeLocked: it is applied under the broadcast's lock, validated, its version
// incremented, stored, and then pushed to the room. A failed push is logged; the room
// reconciler repairs it.
type Writer struct {
	Broadcasts broadcast.Repository
	Locks      *broadcast.Locks
	Rooms      Pusher
	MaxOnStage int
	Logger     *slog.Logger
}

// Mutation changes s for broadcast b; an error leaves the stored state unchanged.
type Mutation func(ctx context.Context, b *broadcast.Broadcast, s *State) error

// Change takes broadcast id's lock and applies fn (see ChangeLocked).
func (w *Writer) Change(ctx context.Context, id string, fn Mutation) (State, error) {
	unlock, err := w.Locks.Lock(ctx, id)
	if err != nil {
		return State{}, err
	}
	defer unlock()
	return w.ChangeLocked(ctx, id, fn)
}

// ChangeLocked applies fn to broadcast id's stage for a caller that already holds its lock
// (the live output turning on_air on and off). It returns the new state.
func (w *Writer) ChangeLocked(ctx context.Context, id string, fn Mutation) (State, error) {
	b, err := w.Broadcasts.Get(ctx, id)
	if err != nil {
		return State{}, err
	}
	s, err := Parse(b.Stage)
	if err != nil {
		return State{}, err
	}
	if err := fn(ctx, b, &s); err != nil {
		return State{}, err
	}
	if err := s.Validate(w.MaxOnStage); err != nil {
		return State{}, err
	}
	s.V++
	text := s.JSON()
	if err := w.Broadcasts.SetStage(ctx, id, text, s.V); err != nil {
		return State{}, fmt.Errorf("store the stage state: %w", err)
	}
	w.push(ctx, id, text)
	return s, nil
}

// SetOnAir turns on_air on or off for a caller holding the broadcast's lock (the live output).
func (w *Writer) SetOnAir(ctx context.Context, id string, on bool) (State, error) {
	return w.ChangeLocked(ctx, id, func(_ context.Context, _ *broadcast.Broadcast, s *State) error {
		s.OnAir = on
		return nil
	})
}

// push copies a stored state to the room, logging a failure; the caller holds the lock so
// pushes reach the room in version order.
func (w *Writer) push(ctx context.Context, id, text string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), PushTimeout)
	defer cancel()
	err := w.Rooms.UpdateMetadata(ctx, Room(id), text)
	switch {
	case err == nil:
	case errors.Is(err, core.ErrNotFound): // no room open: the next one is created with the stored state
	default:
		w.logger().Warn("push the stage state to the room", "broadcast", id, "error", err)
	}
}

func (w *Writer) logger() *slog.Logger {
	if w.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return w.Logger
}
