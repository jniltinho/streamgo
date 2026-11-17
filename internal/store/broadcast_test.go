package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

// seedUser inserts a user whose id is name padded to a ULID's 26 characters (CHAR(26) pads on PostgreSQL).
func seedUser(t *testing.T, db *store.DB, name string) string {
	t.Helper()
	id := name + strings.Repeat("0", 26-len(name))
	err := db.Gorm.Exec("INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)",
		id, name+"@example.com", name, "x").Error
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// pad64 pads s to the 64 characters of an invite hash (CHAR(64) pads on PostgreSQL).
func pad64(s string) string { return s + strings.Repeat("0", 64-len(s)) }

func newBroadcast(t *testing.T, r *store.Broadcasts, owner, hash string) *broadcast.Broadcast {
	t.Helper()
	b := &broadcast.Broadcast{OwnerID: owner, Title: "Show " + hash, InviteSealed: []byte{1, 2, 3}, InviteHash: pad64(hash),
		Stage: broadcast.DefaultStage}
	if err := r.Create(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBroadcastRepository: the whole row round-trips on every database.
func TestBroadcastRepository(t *testing.T) {
	t.Parallel()
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		r := store.NewBroadcasts(db)
		ana, bruno := seedUser(t, db, "ana"), seedUser(t, db, "bruno")
		first := newBroadcast(t, r, ana, "h1")
		second := newBroadcast(t, r, ana, "h2")
		newBroadcast(t, r, bruno, "h3")

		list, err := r.ListByOwner(ctx, ana)
		if err != nil || len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
			t.Fatalf("list: %+v %v", list, err)
		}
		if got, err := r.FindByInviteHash(ctx, pad64("h2")); err != nil || got.ID != second.ID {
			t.Fatalf("by hash: %+v %v", got, err)
		}
		if _, err := r.Get(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}

		deadline := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
		out := broadcast.Output{State: broadcast.Interrupted, Reason: "program_lost", Run: 3, Deadline: deadline,
			IngestID: "s1", IngestURL: "http://m/whip", StartedAt: deadline.Add(-time.Hour)}
		for _, err := range []error{
			r.SetTitle(ctx, first.ID, "Renamed"),
			r.SetInvite(ctx, first.ID, []byte{9}, pad64("h9")),
			r.SetStage(ctx, first.ID, `{"v":1}`, 7),
			r.SetLogo(ctx, first.ID, "abc.png"),
			r.SetOutput(ctx, first.ID, out),
		} {
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := r.Get(ctx, first.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Renamed" || string(got.InviteSealed) != "\x09" || got.InviteHash != pad64("h9") ||
			got.Stage != `{"v":1}` || got.StageVersion != 7 || got.Logo != "abc.png" ||
			got.Output.State != out.State || got.Output.Reason != out.Reason || got.Output.Run != 3 ||
			!got.Output.Deadline.Equal(deadline) || !got.Output.StartedAt.Equal(out.StartedAt) ||
			got.Output.IngestID != "s1" || got.Output.IngestURL != out.IngestURL {
			t.Fatalf("round trip: %+v", got)
		}
		active, err := r.ListByOutputState(ctx, broadcast.Live, broadcast.Interrupted)
		if err != nil || len(active) != 1 || active[0].ID != first.ID {
			t.Fatalf("by state: %+v %v", active, err)
		}
		if err := r.SetOutput(ctx, first.ID, broadcast.Output{State: broadcast.Offline}); err != nil {
			t.Fatal(err)
		}
		if got, _ := r.Get(ctx, first.ID); !got.Output.Deadline.IsZero() || !got.Output.StartedAt.IsZero() {
			t.Fatalf("times not cleared: %+v", got.Output)
		}

		// banners, guests and selections go with the row
		ds := store.NewDestinations(db)
		d := &destination.Destination{OwnerID: ana, Name: "YT", Scheme: "rtmp", Host: "h", Sealed: []byte{1}}
		if err := ds.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
		if err := ds.SetSelection(ctx, first.ID, []string{d.ID}); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"INSERT INTO banners (id, broadcast_id, text) VALUES (?, ?, ?)", []any{ulid.Make().String(), first.ID, "Hi"}},
			{"INSERT INTO guests (id, broadcast_id, name, cookie_hash, joined_at, last_join_at) VALUES (?, ?, ?, ?, ?, ?)",
				[]any{ulid.Make().String(), first.ID, "Gil", strings.Repeat("c", 64), now, now}},
		} {
			if err := db.Gorm.Exec(q.sql, q.args...).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := r.Delete(ctx, first.ID); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"banners", "guests", "broadcast_destinations"} {
			var n int64
			if err := db.Gorm.Table(table).Where("broadcast_id = ?", first.ID).Count(&n).Error; err != nil || n != 0 {
				t.Errorf("%s left: %d %v", table, n, err)
			}
		}
		if err := r.Delete(ctx, first.ID); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("delete twice: %v", err)
		}
	})
}

// TestDestinationRepository: rows, selections, the in-use query and the cascade on delete.
func TestDestinationRepository(t *testing.T) {
	t.Parallel()
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		bs, ds := store.NewBroadcasts(db), store.NewDestinations(db)
		ana := seedUser(t, db, "ana")
		b := newBroadcast(t, bs, ana, "h1")
		var ids []string
		for _, name := range []string{"A", "B"} {
			d := &destination.Destination{OwnerID: ana, Name: name, Scheme: "rtmp", Host: "h", Sealed: []byte{1}}
			if err := ds.Create(ctx, d); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, d.ID)
		}
		d, err := ds.Get(ctx, ids[0])
		if err != nil {
			t.Fatal(err)
		}
		d.Name, d.Scheme, d.Host, d.Sealed, d.HasKey = "A2", "rtmps", "h2:443", []byte{2}, true
		if err := ds.Update(ctx, d); err != nil {
			t.Fatal(err)
		}
		if got, _ := ds.Get(ctx, d.ID); got.Name != "A2" || got.Scheme != "rtmps" || got.Host != "h2:443" ||
			string(got.Sealed) != "\x02" || !got.HasKey {
			t.Fatalf("update: %+v", got)
		}
		if err := ds.SetSelection(ctx, b.ID, ids); err != nil {
			t.Fatal(err)
		}
		if err := ds.SetSelection(ctx, b.ID, ids[1:]); err != nil {
			t.Fatal(err)
		}
		if list, err := ds.ListByBroadcast(ctx, b.ID); err != nil || len(list) != 1 || list[0].ID != ids[1] {
			t.Fatalf("selection: %+v %v", list, err)
		}
		if used, err := ds.InUse(ctx, ids[1]); err != nil || used {
			t.Fatalf("offline in use: %v %v", used, err)
		}
		if err := bs.SetOutput(ctx, b.ID, broadcast.Output{State: broadcast.Interrupted}); err != nil {
			t.Fatal(err)
		}
		if used, err := ds.InUse(ctx, ids[1]); err != nil || !used {
			t.Fatalf("interrupted not in use: %v %v", used, err)
		}
		if used, _ := ds.InUse(ctx, ids[0]); used {
			t.Fatal("unselected in use")
		}
		if err := ds.Delete(ctx, ids[1]); err != nil {
			t.Fatal(err)
		}
		if list, _ := ds.ListByBroadcast(ctx, b.ID); len(list) != 0 {
			t.Fatalf("selection survives: %+v", list)
		}
		if list, _ := ds.ListByOwner(ctx, ana); len(list) != 1 {
			t.Fatalf("owner list: %+v", list)
		}
	})
}
