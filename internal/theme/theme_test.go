package theme_test

// Portions adapted from MeetHub (MIT): internal/theme/theme_test.go

import (
	"testing"

	"github.com/jniltinho/streamgo/internal/theme"
)

// TestCatalog is the ui-themes "Catalog" scenario: the 28 ids in order, one default.
func TestCatalog(t *testing.T) {
	t.Parallel()
	want := []string{
		"streamgo-dark", "streamgo-light", "default", "admanager", "breeze", "breeze-dark",
		"violet", "violet-dark", "aurora", "aurora-dark", "crystal", "crystal-dark", "cone", "classic-blue",
		"classic-gray", "graphite", "grafana", "rabbitmq", "matrix", "winamp", "pink-panther", "high-contrast",
		"macos", "macos-dark", "macos-retro", "ubuntu", "obs-dark", "obs-light",
	}
	all := theme.All()
	if len(all) != len(want) {
		t.Fatalf("%d themes, want %d", len(all), len(want))
	}
	defaults := 0
	for i, th := range all {
		if th.ID != want[i] {
			t.Errorf("position %d: %q, want %q", i, th.ID, want[i])
		}
		if th.Scheme != "light" && th.Scheme != "dark" {
			t.Errorf("%s: scheme %q", th.ID, th.Scheme)
		}
		if th.Label == "" {
			t.Errorf("%s: no label", th.ID)
		}
		if th.Default {
			defaults++
		}
	}
	if defaults != 1 || !all[0].Default || all[0].ID != theme.Default {
		t.Fatal("streamgo-dark must be the single default")
	}
	if !theme.Valid("classic-blue") || theme.Valid("neon") || theme.Valid("") {
		t.Fatal("Valid")
	}
	all[0].ID = "changed" // All returns a copy
	if theme.All()[0].ID != theme.Default {
		t.Fatal("All shares the registry")
	}
}
