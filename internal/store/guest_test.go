package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

// seedBroadcasts inserts a user and broadcasts b1 and b2 (the broadcast repository is another package's).
func seedBroadcasts(t *testing.T, db *store.DB) {
	t.Helper()
	g := db.Gorm
	if err := g.Exec(`INSERT INTO users (id, email, name, password_hash) VALUES ('u1', 'u1@example.test', 'U', 'x')`).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"b1", "b2"} {
		if err := g.Exec(`INSERT INTO broadcasts (id, owner_id, title, invite_sealed, invite_hash, stage) VALUES (?, 'u1', 'T', ?, ?, '{}')`,
			id, []byte("sealed"), "hash-"+id).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuests(t *testing.T) {
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		seedBroadcasts(t, db)
		ctx := context.Background()
		repo := store.NewGuests(db)
		t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

		g := &studio.Guest{BroadcastID: "b1", Name: "Rafael", CookieHash: "c1", JoinedAt: t0, LastJoinAt: t0}
		if err := repo.Create(ctx, g); err != nil || len(g.ID) != 26 {
			t.Fatalf("create: %v, id %q", err, g.ID)
		}
		if err := repo.Create(ctx, &studio.Guest{BroadcastID: "nope", Name: "x", CookieHash: "c9", JoinedAt: t0, LastJoinAt: t0}); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("missing broadcast: %v", err)
		}
		old := &studio.Guest{BroadcastID: "b1", Name: "Old", CookieHash: "c2", JoinedAt: t0.Add(-48 * time.Hour), LastJoinAt: t0.Add(-25 * time.Hour)}
		other := &studio.Guest{BroadcastID: "b2", Name: "Other", CookieHash: "c3", JoinedAt: t0, LastJoinAt: t0}
		for _, x := range []*studio.Guest{old, other} {
			if err := repo.Create(ctx, x); err != nil {
				t.Fatal(err)
			}
		}

		got, err := repo.FindByCookie(ctx, "b1", "c1")
		if err != nil || got.ID != g.ID || got.Name != "Rafael" || !got.LastJoinAt.Equal(t0) || !got.RemovedAt.IsZero() {
			t.Fatalf("find by cookie: %+v, %v", got, err)
		}
		if _, err := repo.FindByCookie(ctx, "b2", "c1"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("cookie of another broadcast: %v", err)
		}
		if _, err := repo.Get(ctx, "b2", g.ID); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("guest of another broadcast: %v", err)
		}

		if err := repo.Rejoin(ctx, g.ID, "Rafael Lima", t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := repo.MarkRemoved(ctx, g.ID, t0.Add(2*time.Hour)); err != nil {
			t.Fatal(err)
		}
		got, err = repo.Get(ctx, "b1", g.ID)
		if err != nil || got.Name != "Rafael Lima" || !got.LastJoinAt.Equal(t0.Add(time.Hour)) || !got.RemovedAt.Equal(t0.Add(2*time.Hour)) {
			t.Fatalf("after rejoin and removal: %+v, %v", got, err)
		}
		if err := repo.Rejoin(ctx, "nope", "x", t0); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("rejoin missing: %v", err)
		}

		if n, err := repo.CountSince(ctx, "b1", t0.Add(-24*time.Hour)); err != nil || n != 1 {
			t.Fatalf("count: %d, %v", n, err)
		}
		if n, err := repo.DeleteBefore(ctx, t0.Add(-24*time.Hour)); err != nil || n != 1 {
			t.Fatalf("delete expired: %d, %v", n, err)
		}
		if _, err := repo.Get(ctx, "b1", old.ID); !errors.Is(err, core.ErrNotFound) {
			t.Fatal("the expired guest is still there")
		}
	})
}

func TestBanners(t *testing.T) {
	storetest.Each(t, func(t *testing.T, db *store.DB) {
		seedBroadcasts(t, db)
		ctx := context.Background()
		repo := store.NewBanners(db)
		var ids []string
		for _, txt := range []string{"first", "second", "third"} {
			bn := &studio.Banner{BroadcastID: "b1", Text: txt}
			if err := repo.Create(ctx, bn); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, bn.ID)
		}
		if err := repo.Create(ctx, &studio.Banner{BroadcastID: "nope", Text: "x"}); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("missing broadcast: %v", err)
		}
		list, err := repo.List(ctx, "b1")
		if err != nil || len(list) != 3 || list[0].Text != "first" || list[2].Text != "third" {
			t.Fatalf("list %+v, %v", list, err)
		}
		if list, _ := repo.List(ctx, "b2"); len(list) != 0 {
			t.Fatalf("b2 has %v", list)
		}
		if bn, err := repo.Get(ctx, "b1", ids[1]); err != nil || bn.Text != "second" {
			t.Fatalf("get %+v, %v", bn, err)
		}
		if _, err := repo.Get(ctx, "b2", ids[1]); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("banner of another broadcast: %v", err)
		}
		if err := repo.Delete(ctx, "b2", ids[1]); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("delete from another broadcast: %v", err)
		}
		if err := repo.Delete(ctx, "b1", ids[1]); err != nil {
			t.Fatal(err)
		}
		if list, _ := repo.List(ctx, "b1"); len(list) != 2 {
			t.Fatalf("after delete %v", list)
		}
	})
}
