// Package stage is the stage state of a broadcast (design D1): the JSON document every studio
// client renders from, its validation, and the single writer through which every change goes.
package stage

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

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

// NameStyles are the styles of the name tags (design N1); the first is the default.
var NameStyles = []string{"bar", "solid", "strip", "text", "two-line"}

// Limits of the second lines (design N2).
const (
	MaxTaglines     = 50
	MaxTaglineRunes = 60
)

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
	// HideNames takes the name tags off the tiles; a state stored before it existed shows them.
	HideNames bool `json:"hide_names"`
	// NameStyle is one of NameStyles; a state stored before it existed draws "bar".
	NameStyle string `json:"name_style"`
	// HideScreenNames takes the name tags off the tiles of shared screens.
	HideScreenNames bool `json:"hide_screen_names"`
	// Taglines maps a participant's identity to the second line the owner wrote for them.
	Taglines map[string]string `json:"taglines"`
}

// Default is the stage state of a new broadcast.
func Default() State {
	return State{V: 1, Layout: "grid", Sources: []Source{}, Accent: DefaultAccent,
		NameStyle: NameStyles[0], Taglines: map[string]string{}}
}

// RoomPrefix starts the name of every studio room.
const RoomPrefix = "bc_"

// Room is the LiveKit room name of a broadcast.
func Room(broadcastID string) string { return RoomPrefix + broadcastID }

// JSON encodes the state; sources is always a list and taglines always an object.
func (s State) JSON() string {
	if s.Sources == nil {
		s.Sources = []Source{}
	}
	if s.Taglines == nil {
		s.Taglines = map[string]string{}
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
	if s.NameStyle == "" {
		s.NameStyle = NameStyles[0]
	}
	if s.Taglines == nil {
		s.Taglines = map[string]string{}
	}
	return s, nil
}

// SetTagline sets identity's second line to text trimmed, or removes it when text is blank
// (design N2). A new entry past MaxTaglines first drops the entries of identities with no
// source on the stage; it is refused when the stage is still full. Whether identity belongs
// to the broadcast is the caller's check.
func (s *State) SetTagline(identity, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		delete(s.Taglines, identity)
		return nil
	}
	if utf8.RuneCountInString(text) > MaxTaglineRunes || !utf8.ValidString(text) ||
		strings.ContainsFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return core.Invalid("tagline", fmt.Sprintf("Use at most %d characters, without control characters.", MaxTaglineRunes))
	}
	if s.Taglines == nil {
		s.Taglines = map[string]string{}
	}
	if _, ok := s.Taglines[identity]; !ok && len(s.Taglines) >= MaxTaglines {
		kept := maps.Clone(s.Taglines)
		maps.DeleteFunc(kept, func(p, _ string) bool {
			return !slices.ContainsFunc(s.Sources, func(src Source) bool { return src.P == p })
		})
		if len(kept) >= MaxTaglines {
			return core.Invalid("tagline", fmt.Sprintf("The stage keeps at most %d second lines.", MaxTaglines))
		}
		s.Taglines = kept
	}
	s.Taglines[identity] = text
	return nil
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
	if !slices.Contains(NameStyles, s.NameStyle) {
		return core.Invalid("name_style", "Unknown name tag style.")
	}
	if len(s.Taglines) > MaxTaglines {
		return core.Invalid("tagline", fmt.Sprintf("The stage keeps at most %d second lines.", MaxTaglines))
	}
	return nil
}
