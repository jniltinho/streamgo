package stage_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/stage/stagetest"
)

// The broadcasts package creates new broadcasts with this document; the default must match it.
const newBroadcastStage = `{"v":1,"on_air":false,"layout":"grid","sources":[],"banner":null,"logo":null,"accent":"#3d41cf","hide_names":false}`

func TestDefaultMatchesNewBroadcast(t *testing.T) {
	t.Parallel()
	if got := stage.Default().JSON(); got != newBroadcastStage {
		t.Fatalf("default stage\n got %s\nwant %s", got, newBroadcastStage)
	}
	s, err := stage.Parse(newBroadcastStage)
	if err != nil || s.V != 1 || s.OnAir || s.Layout != "grid" || len(s.Sources) != 0 || s.Banner != nil || s.Logo != nil {
		t.Fatalf("parsed %+v, %v", s, err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	cam := stage.Source{P: "host-1", K: stage.Camera}
	tests := []struct {
		name  string
		edit  func(*stage.State)
		field string
	}{
		{"default", func(*stage.State) {}, ""},
		{"every layout", func(s *stage.State) { s.Layout = "pip" }, ""},
		{"unknown layout", func(s *stage.State) { s.Layout = "mosaic" }, "layout"},
		{"too many sources", func(s *stage.State) {
			s.Sources = []stage.Source{cam, {P: "a", K: "camera"}, {P: "b", K: "camera"}}
		}, "sources"},
		{"duplicate source", func(s *stage.State) { s.Sources = []stage.Source{cam, cam} }, "sources"},
		{"camera and screen of one person", func(s *stage.State) {
			s.Sources = []stage.Source{cam, {P: "host-1", K: stage.Screen}}
		}, ""},
		{"unknown kind", func(s *stage.State) { s.Sources = []stage.Source{{P: "a", K: "mic"}} }, "sources"},
		{"short accent", func(s *stage.State) { s.Accent = "#fff" }, "accent"},
		{"named accent", func(s *stage.State) { s.Accent = "red" }, "accent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := stage.Default()
			tt.edit(&s)
			err := s.Validate(2)
			var inv *core.InvalidError
			switch {
			case tt.field == "" && err != nil:
				t.Fatalf("unexpected %v", err)
			case tt.field != "" && (!errors.As(err, &inv) || inv.Field != tt.field):
				t.Fatalf("got %v, want invalid %s", err, tt.field)
			}
		})
	}
}

type pushes struct {
	mu   sync.Mutex
	last map[string]string
	fail error
}

func (p *pushes) UpdateMetadata(_ context.Context, room, md string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail != nil {
		return p.fail
	}
	if p.last == nil {
		p.last = map[string]string{}
	}
	p.last[room] = md
	return nil
}

func newWriter(t *testing.T) (*stage.Writer, *stagetest.Broadcasts, *pushes) {
	t.Helper()
	repo := &stagetest.Broadcasts{}
	repo.Put(broadcast.Broadcast{ID: "b1", OwnerID: "u1", Stage: stage.Default().JSON(), StageVersion: 1})
	p := &pushes{}
	return &stage.Writer{Broadcasts: repo, Locks: &broadcast.Locks{}, Rooms: p, MaxOnStage: 6}, repo, p
}

func TestChangeStoresAndPushes(t *testing.T) {
	t.Parallel()
	w, repo, p := newWriter(t)
	ctx := context.Background()
	s, err := w.Change(ctx, "b1", func(_ context.Context, _ *broadcast.Broadcast, s *stage.State) error {
		s.Layout = "spotlight"
		return nil
	})
	if err != nil || s.V != 2 || s.Layout != "spotlight" {
		t.Fatalf("got %+v, %v", s, err)
	}
	b, _ := repo.Get(ctx, "b1")
	if b.Stage != s.JSON() || b.StageVersion != 2 || p.last["bc_b1"] != s.JSON() {
		t.Fatalf("stored %q v%d, pushed %q", b.Stage, b.StageVersion, p.last["bc_b1"])
	}

	// an invalid change leaves everything as it was
	_, err = w.Change(ctx, "b1", func(_ context.Context, _ *broadcast.Broadcast, s *stage.State) error {
		s.Accent = "blue"
		return nil
	})
	if after, _ := repo.Get(ctx, "b1"); err == nil || after.Stage != b.Stage {
		t.Fatalf("invalid change: %v, stored %s", err, after.Stage)
	}

	// the room not open, or a failed push: the change is still stored and returned
	for _, fail := range []error{core.ErrNotFound, errors.New("livekit down")} {
		p.fail = fail
		s, err = w.SetOnAir(ctx, "b1", true)
		if err != nil || !s.OnAir {
			t.Fatalf("push failing with %v: %+v, %v", fail, s, err)
		}
	}
	if b, _ := repo.Get(ctx, "b1"); b.StageVersion != 4 {
		t.Fatalf("version %d, want 4", b.StageVersion)
	}
}

func TestConcurrentChanges(t *testing.T) {
	t.Parallel()
	w, repo, _ := newWriter(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := w.Change(ctx, "b1", func(context.Context, *broadcast.Broadcast, *stage.State) error { return nil }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if b, _ := repo.Get(ctx, "b1"); b.StageVersion != 21 {
		t.Fatalf("version %d, want 21", b.StageVersion)
	}
}

func TestChangeMissingBroadcast(t *testing.T) {
	t.Parallel()
	w, _, _ := newWriter(t)
	_, err := w.Change(context.Background(), "nope", func(context.Context, *broadcast.Broadcast, *stage.State) error { return nil })
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
