package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
)

type broadcastRow struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OwnerID        string     `gorm:"column:owner_id"`
	Title          string     `gorm:"column:title"`
	InviteSealed   []byte     `gorm:"column:invite_sealed"`
	InviteHash     string     `gorm:"column:invite_hash"`
	Stage          string     `gorm:"column:stage"`
	StageVersion   int64      `gorm:"column:stage_version"`
	Logo           string     `gorm:"column:logo"`
	Resolution     string     `gorm:"column:resolution"`
	OutputState    string     `gorm:"column:output_state"`
	OutputReason   string     `gorm:"column:output_reason"`
	OutputRun      int64      `gorm:"column:output_run"`
	OutputDeadline *time.Time `gorm:"column:output_deadline"`
	IngestID       string     `gorm:"column:ingest_id"`
	IngestURL      string     `gorm:"column:ingest_url"`
	StartedAt      *time.Time `gorm:"column:started_at"`
}

func (broadcastRow) TableName() string { return "broadcasts" }

func (r *broadcastRow) toCore() broadcast.Broadcast {
	return broadcast.Broadcast{
		ID: r.ID, OwnerID: r.OwnerID, Title: r.Title, InviteSealed: r.InviteSealed, InviteHash: r.InviteHash,
		Stage: r.Stage, StageVersion: r.StageVersion, Logo: r.Logo, Resolution: r.Resolution,
		Output: broadcast.Output{
			State: broadcast.OutputState(r.OutputState), Reason: r.OutputReason, Run: r.OutputRun,
			Deadline: fromNullTime(r.OutputDeadline), IngestID: r.IngestID, IngestURL: r.IngestURL,
			StartedAt: fromNullTime(r.StartedAt),
		},
	}
}

func outputColumns(o broadcast.Output) map[string]any {
	return map[string]any{
		"output_state": string(o.State), "output_reason": o.Reason, "output_run": o.Run,
		"output_deadline": toNullTime(o.Deadline), "ingest_id": o.IngestID, "ingest_url": o.IngestURL,
		"started_at": toNullTime(o.StartedAt),
	}
}

func toNullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func fromNullTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

// Broadcasts implements broadcast.Repository.
type Broadcasts struct{ db *gorm.DB }

// NewBroadcasts returns the broadcast repository of db.
func NewBroadcasts(db *DB) *Broadcasts { return &Broadcasts{db: db.Gorm} }

var _ broadcast.Repository = (*Broadcasts)(nil)

// Create inserts b, giving it a new ULID when b.ID is empty.
func (r *Broadcasts) Create(ctx context.Context, b *broadcast.Broadcast) error {
	if b.ID == "" {
		b.ID = ulid.Make().String()
	}
	if b.Output.State == "" {
		b.Output.State = broadcast.Offline
	}
	o := b.Output
	row := broadcastRow{
		ID: b.ID, OwnerID: b.OwnerID, Title: b.Title, InviteSealed: b.InviteSealed, InviteHash: b.InviteHash,
		Stage: b.Stage, StageVersion: b.StageVersion, Logo: b.Logo, Resolution: b.Resolution,
		OutputState: string(o.State), OutputReason: o.Reason, OutputRun: o.Run, OutputDeadline: toNullTime(o.Deadline),
		IngestID: o.IngestID, IngestURL: o.IngestURL, StartedAt: toNullTime(o.StartedAt),
	}
	return Translate(r.db.WithContext(ctx).Create(&row).Error)
}

// Get returns the broadcast with id.
func (r *Broadcasts) Get(ctx context.Context, id string) (*broadcast.Broadcast, error) {
	return r.find(ctx, "id = ?", id)
}

// FindByInviteHash returns the broadcast whose invite token hashes to hash.
func (r *Broadcasts) FindByInviteHash(ctx context.Context, hash string) (*broadcast.Broadcast, error) {
	return r.find(ctx, "invite_hash = ?", hash)
}

func (r *Broadcasts) find(ctx context.Context, where string, arg string) (*broadcast.Broadcast, error) {
	var row broadcastRow
	err := r.db.WithContext(ctx).Where(where, arg).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("broadcast: %w", core.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	b := row.toCore()
	return &b, nil
}

// ListByOwner lists the owner's broadcasts, newest first (ULIDs sort by creation time).
func (r *Broadcasts) ListByOwner(ctx context.Context, ownerID string) ([]broadcast.Broadcast, error) {
	return r.list(r.db.WithContext(ctx).Where("owner_id = ?", ownerID).Order("id DESC"))
}

// ListByOutputState lists the broadcasts in any of the states.
func (r *Broadcasts) ListByOutputState(ctx context.Context, states ...broadcast.OutputState) ([]broadcast.Broadcast, error) {
	s := make([]string, len(states))
	for i, st := range states {
		s[i] = string(st)
	}
	return r.list(r.db.WithContext(ctx).Where("output_state IN ?", s).Order("id"))
}

func (r *Broadcasts) list(q *gorm.DB) ([]broadcast.Broadcast, error) {
	var rows []broadcastRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]broadcast.Broadcast, len(rows))
	for i := range rows {
		out[i] = rows[i].toCore()
	}
	return out, nil
}

func (r *Broadcasts) update(ctx context.Context, id string, cols map[string]any) error {
	return Translate(r.db.WithContext(ctx).Model(&broadcastRow{}).Where("id = ?", id).Updates(cols).Error)
}

// Update writes the title and the resolution in one statement.
func (r *Broadcasts) Update(ctx context.Context, id, title, resolution string) error {
	return r.update(ctx, id, map[string]any{"title": title, "resolution": resolution})
}

// SetInvite replaces the invite token.
func (r *Broadcasts) SetInvite(ctx context.Context, id string, sealed []byte, hash string) error {
	return r.update(ctx, id, map[string]any{"invite_sealed": sealed, "invite_hash": hash})
}

// SetStage stores the stage state and its version.
func (r *Broadcasts) SetStage(ctx context.Context, id, stage string, version int64) error {
	return r.update(ctx, id, map[string]any{"stage": stage, "stage_version": version})
}

// SetLogo stores the logo's file name.
func (r *Broadcasts) SetLogo(ctx context.Context, id, logo string) error {
	return r.update(ctx, id, map[string]any{"logo": logo})
}

// SetOutput stores the output state.
func (r *Broadcasts) SetOutput(ctx context.Context, id string, o broadcast.Output) error {
	return r.update(ctx, id, outputColumns(o))
}

// Delete removes the broadcast; its banners, guests and selections go with it (ON DELETE CASCADE).
func (r *Broadcasts) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&broadcastRow{})
	if res.Error != nil {
		return Translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("broadcast %s: %w", id, core.ErrNotFound)
	}
	return nil
}
