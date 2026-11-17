// Package stage is the stage state of a broadcast (design D1): the JSON document every studio
// client renders from, its validation, and the single writer through which every change goes.
package stage

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/jniltinho/streamgo/internal/core"
)

// Layouts accepted in State.Layout.
var Layouts = []string{"solo", "grid", "spotlight", "sidebar", "screen", "pip"}

// Source kinds.
const (
	Camera = "camera"
	Screen = "screen"
)

// DefaultAccent is the accent colour of a new broadcast.
const DefaultAccent = "#3d41cf"

// Source is one participant's camera or screen on stage.
type Source struct {
	P string `json:"p"` // participant identity
	K string `json:"k"` // Camera or Screen
}

// Banner is the banner being shown.
type Banner struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// State is the stage state; its JSON is the only shape clients parse.
type State struct {
	V       int64    `json:"v"`
	OnAir   bool     `json:"on_air"`
	Layout  string   `json:"layout"`
	Sources []Source `json:"sources"`
	Banner  *Banner  `json:"banner"`
	Logo    *string  `json:"logo"`
	Accent  string   `json:"accent"`
}

// Default is the stage state of a new broadcast.
func Default() State {
	return State{V: 1, Layout: "grid", Sources: []Source{}, Accent: DefaultAccent}
}

// RoomPrefix starts the name of every studio room.
const RoomPrefix = "bc_"

// Room is the LiveKit room name of a broadcast.
func Room(broadcastID string) string { return RoomPrefix + broadcastID }

// JSON encodes the state; sources is always a list.
func (s State) JSON() string {
	if s.Sources == nil {
		s.Sources = []Source{}
	}
	b, _ := json.Marshal(s) // cannot fail: plain fields only
	return string(b)
}

// Parse decodes a stored stage state.
func Parse(text string) (State, error) {
	var s State
	if err := json.Unmarshal([]byte(text), &s); err != nil {
		return State{}, fmt.Errorf("parse the stage state: %w", err)
	}
	if s.Sources == nil {
		s.Sources = []Source{}
	}
	return s, nil
}

var accentRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate checks the state's shape: a known layout, at most maxOnStage sources of a known
// kind without duplicates, and a #rrggbb accent. Whether the identities belong to the
// broadcast is the caller's check.
func (s State) Validate(maxOnStage int) error {
	known := false
	for _, l := range Layouts {
		known = known || s.Layout == l
	}
	if !known {
		return core.Invalid("layout", "Unknown layout.")
	}
	if len(s.Sources) > maxOnStage {
		return core.Invalid("sources", fmt.Sprintf("At most %d sources can be on stage.", maxOnStage))
	}
	seen := map[Source]bool{}
	for _, src := range s.Sources {
		if src.K != Camera && src.K != Screen {
			return core.Invalid("sources", "A source is a camera or a screen.")
		}
		if seen[src] {
			return core.Invalid("sources", "A source is on stage twice.")
		}
		seen[src] = true
	}
	if !accentRE.MatchString(s.Accent) {
		return core.Invalid("accent", "The accent must be a #rrggbb colour.")
	}
	return nil
}
