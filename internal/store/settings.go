package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/jniltinho/streamgo/internal/core/settings"
)

type settingRow struct {
	Name  string `gorm:"primaryKey"`
	Value string
}

func (settingRow) TableName() string { return "settings" }

// ServerSettings implements settings.Repository.
type ServerSettings struct{ db *gorm.DB }

// NewServerSettings returns the settings repository on db.
func NewServerSettings(db *DB) *ServerSettings { return &ServerSettings{db: db.Gorm} }

var _ settings.Repository = (*ServerSettings)(nil)

// Load returns every stored setting.
func (s *ServerSettings) Load(ctx context.Context) (map[string]string, error) {
	var rows []settingRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Name] = r.Value
	}
	return out, nil
}

// Save writes every value in one transaction: an update, and an insert when no row had the name (design S2:
// no upsert syntax). Two first saves of one name racing: the loser's insert fails and its transaction rolls back.
func (s *ServerSettings) Save(ctx context.Context, values map[string]string) error {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for name, value := range values {
			res := tx.Model(&settingRow{}).Where("name = ?", name).Update("value", value)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				continue
			}
			// MariaDB counts a row updated to the value it had as not affected
			var n int64
			if err := tx.Model(&settingRow{}).Where("name = ?", name).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				if err := tx.Create(&settingRow{Name: name, Value: value}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save settings: %w", Translate(err))
	}
	return nil
}
