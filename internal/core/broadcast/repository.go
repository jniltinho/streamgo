// Package broadcast is the broadcast domain: management, the invite link and ownership
// (work package B). The foundation declares here what the studio (C) and the live output (D)
// share with it: the broadcast row, its repository and the per-broadcast lock.
package broadcast

import (
	"context"
	"time"
)

// OutputState is the state of a broadcast's live output (design D5).
type OutputState string

// Output states.
const (
	Offline     OutputState = "offline"
	Starting    OutputState = "starting"
	Live        OutputState = "live"
	Interrupted OutputState = "interrupted"
)

// Broadcast is one row of the broadcasts table. Ids are ULID strings; times are UTC.
type Broadcast struct {
	ID      string
	OwnerID string
	Title   string
	// InviteSealed is the invite token sealed with the secret key; InviteHash finds it.
	InviteSealed []byte
	InviteHash   string
	// Stage is the stage state as JSON text (design D1), changed only under the broadcast's lock.
	Stage        string
	StageVersion int64
	Logo         string // file name under <data_dir>/assets; empty: none
	// Resolution is the output resolution the owner chose; empty: the server's default (Quality.Profile).
	Resolution string
	Output     Output
}

// Output is the stored state of the live output (design D5).
type Output struct {
	State    OutputState
	Reason   string
	Run      int64
	Deadline time.Time // zero unless State is Interrupted
	// IngestID and IngestURL identify the program's WHIP session in MediaMTX.
	IngestID  string
	IngestURL string
	StartedAt time.Time // zero unless the broadcast went live
}

// Repository stores broadcasts; internal/store implements it. Methods that find one broadcast
// return an error wrapping core.ErrNotFound when it does not exist. Writes to the stage and
// the output are made while holding the broadcast's lock (Locks).
type Repository interface {
	Create(ctx context.Context, b *Broadcast) error
	Get(ctx context.Context, id string) (*Broadcast, error)
	FindByInviteHash(ctx context.Context, hash string) (*Broadcast, error)
	ListByOwner(ctx context.Context, ownerID string) ([]Broadcast, error)
	// ListByOutputState lists the broadcasts in any of the states (restart recovery, D5).
	ListByOutputState(ctx context.Context, states ...OutputState) ([]Broadcast, error)
	// Update writes the title and the resolution in one statement (made under the broadcast's lock).
	Update(ctx context.Context, id, title, resolution string) error
	SetInvite(ctx context.Context, id string, sealed []byte, hash string) error
	SetStage(ctx context.Context, id, stage string, version int64) error
	SetLogo(ctx context.Context, id, logo string) error
	SetOutput(ctx context.Context, id string, o Output) error
	// Delete removes the broadcast with its banners, guests and destination selections.
	Delete(ctx context.Context, id string) error
}
