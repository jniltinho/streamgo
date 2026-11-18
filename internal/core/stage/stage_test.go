package stage_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/stage/stagetest"
)

// The broadcasts package creates new broadcasts with this document; the default must match it.
const newBroadcastStage = `{"v":1,"on_air":false,"layout":"grid","sources":[],"banner":null,"logo":null,"accent":"#3d41cf","hide_names":false,"name_style":"bar","hide_screen_names":false,"taglines":{}}`

// Scenario "Bar by default": a new broadcast, and a state stored before the name styles.
func TestBarByDefault(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		newBroadcastStage,
		`{"v":7,"on_air":false,"layout":"grid","sources":[],"banner":null,"logo":null,"accent":"#3d41cf","hide_names":false}`,
		`{"v":3,"on_air":false,"layout":"grid","sources":[],"banner":null,"logo":null,"accent":"#3d41cf"}`,
	} {
		s, err := stage.Parse(text)
		if err != nil || s.NameStyle != "bar" || s.HideScreenNames || s.Taglines == nil || len(s.Taglines) != 0 {
			t.Fatalf("%s: parsed %+v, %v", text, s, err)
		}
		if err := s.Validate(2); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
		if !strings.Contains(s.JSON(), `"name_style":"bar","hide_screen_names":false,"taglines":{}`) {
			t.Fatalf("encoded %s", s.JSON())
		}
	}
}

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
		// scenario "Only a known style"
		{"every name style", func(s *stage.State) { s.NameStyle = "two-line" }, ""},
		{"unknown name style", func(s *stage.State) { s.NameStyle = "neon" }, "name_style"},
		{"empty name style", func(s *stage.State) { s.NameStyle = "" }, "name_style"},
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

// Scenarios "Rules", "Remove it" and "One at a time" of the second line.
func TestSetTagline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, identity, text string
		field                string
		want                 map[string]string
	}{
		{"set", "guest-a", "Head of Product", "", map[string]string{"host-u1": "Host", "guest-a": "Head of Product"}},
		{"trimmed", "guest-a", "  Head of Product \t", "", map[string]string{"host-u1": "Host", "guest-a": "Head of Product"}},
		{"replaced", "host-u1", "CEO", "", map[string]string{"host-u1": "CEO"}},
		{"sixty characters", "guest-a", strings.Repeat("é", 60), "", map[string]string{"host-u1": "Host", "guest-a": strings.Repeat("é", 60)}},
		{"sixty-one characters", "guest-a", strings.Repeat("a", 61), "tagline", nil},
		{"control character", "guest-a", "Head\nof Product", "tagline", nil},
		{"delete character", "guest-a", "Head\x7f", "tagline", nil},
		{"invalid UTF-8", "guest-a", "Head \xff", "tagline", nil},
		{"removed by an empty text", "host-u1", "", "", map[string]string{}},
		{"removed by blanks", "host-u1", "   ", "", map[string]string{}},
		{"removing an absent entry", "guest-z", "", "", map[string]string{"host-u1": "Host"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := stage.Default()
			s.Taglines["host-u1"] = "Host"
			err := s.SetTagline(tt.identity, tt.text)
			var inv *core.InvalidError
			if tt.field != "" {
				if !errors.As(err, &inv) || inv.Field != tt.field {
					t.Fatalf("got %v, want invalid %s", err, tt.field)
				}
				if len(s.Taglines) != 1 || s.Taglines["host-u1"] != "Host" {
					t.Fatalf("refused change left %v", s.Taglines)
				}
				return
			}
			if err != nil || !maps.Equal(s.Taglines, tt.want) {
				t.Fatalf("got %v, %v; want %v", s.Taglines, err, tt.want)
			}
		})
	}
}

// Scenario "The limit".
func TestTaglineLimit(t *testing.T) {
	t.Parallel()
	full := func(onStage int) stage.State {
		s := stage.Default()
		for i := range stage.MaxTaglines {
			id := fmt.Sprintf("guest-%02d", i)
			s.Taglines[id] = "Role"
			if i < onStage {
				s.Sources = append(s.Sources, stage.Source{P: id, K: stage.Camera})
			}
		}
		return s
	}
	tests := []struct {
		name     string
		onStage  int
		identity string
		field    string
		wantLen  int
	}{
		{"off-stage entries are dropped", 6, "guest-new", "", 7},
		{"every entry off stage", 0, "guest-new", "", 1},
		{"all on stage", stage.MaxTaglines, "guest-new", "tagline", stage.MaxTaglines},
		{"editing an entry is not a new one", stage.MaxTaglines, "guest-07", "", stage.MaxTaglines},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := full(tt.onStage)
			err := s.SetTagline(tt.identity, "Speaker")
			var inv *core.InvalidError
			switch {
			case tt.field != "" && (!errors.As(err, &inv) || inv.Field != tt.field):
				t.Fatalf("got %v, want invalid %s", err, tt.field)
			case tt.field == "" && (err != nil || s.Taglines[tt.identity] != "Speaker"):
				t.Fatalf("got %v, %v", err, s.Taglines)
			case len(s.Taglines) != tt.wantLen:
				t.Fatalf("%d taglines, want %d", len(s.Taglines), tt.wantLen)
			}
			for i := range tt.onStage {
				if id := fmt.Sprintf("guest-%02d", i); s.Taglines[id] == "" {
					t.Fatalf("on-stage %s dropped", id)
				}
			}
			if err := s.Validate(stage.MaxTaglines); err != nil {
				t.Fatalf("validate: %v", err)
			}
		})
	}
	s := full(0)
	s.Taglines["guest-extra"] = "x"
	var inv *core.InvalidError
	if err := s.Validate(2); !errors.As(err, &inv) || inv.Field != "tagline" {
		t.Fatalf("51 taglines: %v", err)
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
