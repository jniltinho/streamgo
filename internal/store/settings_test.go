package store_test

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

// TestSettings: an empty table, the first save inserts, a later one updates (the same value included), and a
// save that fails half-way stores nothing.
func TestSettings(t *testing.T) {
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		repo := store.NewServerSettings(db)
		load := func() map[string]string {
			t.Helper()
			got, err := repo.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		if got := load(); len(got) != 0 {
			t.Fatalf("empty: %v", got)
		}
		steps := []struct {
			save, want map[string]string
		}{
			{map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1", "b": "2"}},
			{map[string]string{"a": "3"}, map[string]string{"a": "3", "b": "2"}},
			{map[string]string{"a": "3", "c": "x"}, map[string]string{"a": "3", "b": "2", "c": "x"}},
		}
		for _, s := range steps {
			if err := repo.Save(ctx, s.save); err != nil {
				t.Fatalf("save %v: %v", s.save, err)
			}
			if got := load(); !maps.Equal(got, s.want) {
				t.Fatalf("after %v: %v, want %v", s.save, got, s.want)
			}
		}
		// a name longer than the column fails on MariaDB and PostgreSQL (SQLite does not check lengths)
		if db.Driver != store.SQLite {
			long := strings.Repeat("n", 65)
			if err := repo.Save(ctx, map[string]string{"a": "9", long: "y"}); err == nil {
				t.Fatal("a too long name was stored")
			}
			if got := load(); got["a"] != "3" || len(got) != 3 {
				t.Fatalf("a failed save changed %v", got)
			}
		}
	})
}
