package broadcast_test

import (
	"slices"
	"testing"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
)

// TestQuality covers "Resolutions and ceiling": the ladder, the default, the ceiling and the bitrate override.
func TestQuality(t *testing.T) {
	t.Parallel()
	if got := broadcast.Resolutions(); !slices.Equal(got, []string{"720p", "1080p", "1440p"}) {
		t.Fatalf("resolutions %v", got)
	}
	p720 := broadcast.Profile{Resolution: "720p", Width: 1280, Height: 720, VideoKbps: 4000}
	p1080 := broadcast.Profile{Resolution: "1080p", Width: 1920, Height: 1080, VideoKbps: 6000}
	p1440 := broadcast.Profile{Resolution: "1440p", Width: 2560, Height: 1440, VideoKbps: 12000}
	defaults := broadcast.Quality{Default: "720p", Max: "1080p"}
	top := broadcast.Quality{Default: "720p", Max: "1440p"}
	tests := []struct {
		name   string
		q      broadcast.Quality
		stored string
		want   broadcast.Profile
	}{
		{"defaults: a new broadcast", defaults, "", p720},
		{"chosen", top, "1440p", p1440},
		{"ceiling lowered later", defaults, "1440p", p1080},
		{"no longer known", defaults, "2160p", p720},
		{"bitrate override", broadcast.Quality{Default: "1080p", Max: "1440p", VideoKbps: 8000}, "",
			broadcast.Profile{Resolution: "1080p", Width: 1920, Height: 1080, VideoKbps: 8000}},
		{"zero value", broadcast.Quality{}, "1440p", p720},
	}
	for _, tt := range tests {
		if got := tt.q.Profile(tt.stored); got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
	if got := defaults.Choices(); !slices.Equal(got, []broadcast.Profile{p720, p1080}) {
		t.Errorf("choices under 1080p: %+v", got)
	}
	for _, c := range (broadcast.Quality{Default: "720p", Max: "1440p", VideoKbps: 8000}).Choices() {
		if c.VideoKbps != 8000 {
			t.Errorf("override not applied to %+v", c)
		}
	}
	for res, want := range map[string]bool{"720p": true, "1080p": true, "1440p": false, "2160p": false, "": false} {
		if defaults.Allowed(res) != want {
			t.Errorf("allowed %q under 1080p: %v", res, !want)
		}
	}
	if !broadcast.Above("1440p", "1080p") || broadcast.Above("720p", "1080p") || !broadcast.Known("1080p") || broadcast.Known("4k") {
		t.Error("ranking")
	}
}
