package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
)

type destinationRow struct {
	ID            string `gorm:"column:id;primaryKey"`
	OwnerID       string `gorm:"column:owner_id"`
	Name          string `gorm:"column:name"`
	Scheme        string `gorm:"column:scheme"`
	Host          string `gorm:"column:host"`
	AddressSealed []byte `gorm:"column:address_sealed"`
	HasKey        bool   `gorm:"column:has_key"`
}

func (destinationRow) TableName() string { return "destinations" }

type selectionRow struct {
	BroadcastID   string `gorm:"column:broadcast_id;primaryKey"`
	DestinationID string `gorm:"column:destination_id;primaryKey"`
}

func (selectionRow) TableName() string { return "broadcast_destinations" }

func destinationFromCore(d *destination.Destination) destinationRow {
	return destinationRow{ID: d.ID, OwnerID: d.OwnerID, Name: d.Name, Scheme: d.Scheme, Host: d.Host,
		AddressSealed: d.Sealed, HasKey: d.HasKey}
}

func (r destinationRow) toCore() destination.Destination {
	return destination.Destination{ID: r.ID, OwnerID: r.OwnerID, Name: r.Name, Scheme: r.Scheme, Host: r.Host,
		HasKey: r.HasKey, Sealed: r.AddressSealed}
}

// Destinations implements destination.Repository.
type Destinations struct{ db *gorm.DB }

// NewDestinations returns the destination repository of db.
func NewDestinations(db *DB) *Destinations { return &Destinations{db: db.Gorm} }

var _ destination.Repository = (*Destinations)(nil)

// Create inserts d with a new ULID.
func (r *Destinations) Create(ctx context.Context, d *destination.Destination) error {
	d.ID = ulid.Make().String()
	row := destinationFromCore(d)
	return Translate(r.db.WithContext(ctx).Create(&row).Error)
}

// Get returns the destination with id.
func (r *Destinations) Get(ctx context.Context, id string) (*destination.Destination, error) {
	var row destinationRow
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("destination %s: %w", id, core.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	d := row.toCore()
	return &d, nil
}

// ListByOwner lists the owner's destinations in creation order.
func (r *Destinations) ListByOwner(ctx context.Context, ownerID string) ([]destination.Destination, error) {
	return r.list(r.db.WithContext(ctx).Where("owner_id = ?", ownerID).Order("id"))
}

// ListByBroadcast lists the destinations a broadcast chose, in creation order.
func (r *Destinations) ListByBroadcast(ctx context.Context, broadcastID string) ([]destination.Destination, error) {
	return r.list(r.db.WithContext(ctx).
		Where("id IN (?)", r.db.Model(&selectionRow{}).Select("destination_id").Where("broadcast_id = ?", broadcastID)).
		Order("id"))
}

func (r *Destinations) list(q *gorm.DB) ([]destination.Destination, error) {
	var rows []destinationRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]destination.Destination, len(rows))
	for i, row := range rows {
		out[i] = row.toCore()
	}
	return out, nil
}

// Update stores the name, scheme, host, sealed address and has_key of d.
func (r *Destinations) Update(ctx context.Context, d *destination.Destination) error {
	return Translate(r.db.WithContext(ctx).Model(&destinationRow{}).Where("id = ?", d.ID).Updates(map[string]any{
		"name": d.Name, "scheme": d.Scheme, "host": d.Host, "address_sealed": d.Sealed, "has_key": d.HasKey,
	}).Error)
}

// Delete removes the destination; the selections go with it (ON DELETE CASCADE).
func (r *Destinations) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&destinationRow{})
	if res.Error != nil {
		return Translate(res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("destination %s: %w", id, core.ErrNotFound)
	}
	return nil
}

// InUse reports whether a broadcast that is not offline chose the destination.
func (r *Destinations) InUse(ctx context.Context, id string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&selectionRow{}).
		Joins("JOIN broadcasts ON broadcasts.id = broadcast_destinations.broadcast_id").
		Where("broadcast_destinations.destination_id = ? AND broadcasts.output_state <> ?", id, string(broadcast.Offline)).
		Count(&n).Error
	return n > 0, err
}

// SetSelection replaces the destinations a broadcast chose.
func (r *Destinations) SetSelection(ctx context.Context, broadcastID string, ids []string) error {
	return Translate(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("broadcast_id = ?", broadcastID).Delete(&selectionRow{}).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		rows := make([]selectionRow, len(ids))
		for i, id := range ids {
			rows[i] = selectionRow{BroadcastID: broadcastID, DestinationID: id}
		}
		return tx.Create(&rows).Error
	}))
}
