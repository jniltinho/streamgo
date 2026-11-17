// Package stagetest holds in-memory fakes of the broadcast repository for the studio's tests;
// the real one is internal/store's.
package stagetest

import (
	"context"
	"fmt"
	"sync"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
)

// Broadcasts is an in-memory broadcast.Repository holding the methods the studio uses; the
// others panic through the nil embedded interface.
type Broadcasts struct {
	broadcast.Repository
	mu   sync.Mutex
	rows map[string]broadcast.Broadcast
}

// Put stores b as it is.
func (f *Broadcasts) Put(b broadcast.Broadcast) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = map[string]broadcast.Broadcast{}
	}
	f.rows[b.ID] = b
}

// Get returns a copy of the broadcast.
func (f *Broadcasts) Get(_ context.Context, id string) (*broadcast.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("broadcast %s: %w", id, core.ErrNotFound)
	}
	return &b, nil
}

// FindByInviteHash finds the broadcast whose invite hash is hash.
func (f *Broadcasts) FindByInviteHash(_ context.Context, hash string) (*broadcast.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.rows {
		if b.InviteHash == hash {
			return &b, nil
		}
	}
	return nil, fmt.Errorf("invite: %w", core.ErrNotFound)
}

// SetStage stores the stage state.
func (f *Broadcasts) SetStage(_ context.Context, id, stage string, version int64) error {
	return f.update(id, func(b *broadcast.Broadcast) { b.Stage, b.StageVersion = stage, version })
}

// SetLogo stores the logo's file name.
func (f *Broadcasts) SetLogo(_ context.Context, id, logo string) error {
	return f.update(id, func(b *broadcast.Broadcast) { b.Logo = logo })
}

// Delete removes the broadcast.
func (f *Broadcasts) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

func (f *Broadcasts) update(id string, fn func(*broadcast.Broadcast)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok {
		return fmt.Errorf("broadcast %s: %w", id, core.ErrNotFound)
	}
	fn(&b)
	f.rows[id] = b
	return nil
}
