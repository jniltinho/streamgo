package output_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/output/outputtest"
)

// TestTranscoderProfile is "1440p reaches the destination" on the core's side and "Ceiling lowered later": the
// transcoder gets the broadcast's profile, cut to the ceiling of the server that runs it.
func TestTranscoderProfile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		stored, max string
		kbps        int
		want        broadcast.Profile
	}{
		{"", "1080p", 0, broadcast.Profile{Resolution: "720p", Width: 1280, Height: 720, VideoKbps: 4000}},
		{"1440p", "1440p", 0, broadcast.Profile{Resolution: "1440p", Width: 2560, Height: 1440, VideoKbps: 12000}},
		{"1440p", "1080p", 0, broadcast.Profile{Resolution: "1080p", Width: 1920, Height: 1080, VideoKbps: 6000}},
		{"1440p", "1440p", 8000, broadcast.Profile{Resolution: "1440p", Width: 2560, Height: 1440, VideoKbps: 8000}},
	} {
		t.Run(fmt.Sprintf("%s under %s at %d", tc.stored, tc.max, tc.kbps), func(t *testing.T) {
			t.Parallel()
			e := outputtest.New(t)
			e.Service.Quality = func() broadcast.Quality { return broadcast.Quality{Default: "720p", Max: tc.max, VideoKbps: tc.kbps} }
			e.AddDestination(t, "dA", "u1", "YouTube", "")
			e.AddBroadcast(t, "b1", "u1", "dA")
			b, _ := e.Broadcasts.Get(ctx, "b1")
			b.Resolution = tc.stored
			e.Broadcasts.Put(*b)
			if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
				t.Fatal(err)
			}
			if got := e.Processes.Last(t, "b1", "").Profile; got != tc.want {
				t.Fatalf("transcoder at %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestResolutionRacesGoLive: a resolution patch and a go-live of the same broadcast are ordered by the
// broadcast's lock. Either the patch lands first and the run goes out at the new resolution, or the go-live
// lands first and the patch is refused with nothing changed.
func TestResolutionRacesGoLive(t *testing.T) {
	t.Parallel()
	e := outputtest.New(t)
	q := broadcast.Quality{Default: "720p", Max: "1440p"}
	e.Service.Quality = func() broadcast.Quality { return q }
	svc := &broadcast.Service{Repo: e.Broadcasts, Locks: e.Locks, Quality: e.Service.Quality}
	e.AddDestination(t, "dA", "u1", "YouTube", "")
	res := "1440p"
	seen := map[bool]int{}
	for i := range 60 {
		id := fmt.Sprintf("b%02d", i)
		e.AddBroadcast(t, id, "u1", "dA")
		var wg sync.WaitGroup
		var patchErr, liveErr error
		wg.Go(func() { _, patchErr = svc.Update(ctx, "u1", id, broadcast.Patch{Resolution: &res}) })
		wg.Go(func() { _, liveErr = e.Service.GoLive(ctx, "u1", id) })
		wg.Wait()
		if liveErr != nil {
			t.Fatalf("%s go live: %v", id, liveErr)
		}
		got := e.Processes.Last(t, id, "").Profile.Resolution
		stored, _ := e.Broadcasts.Get(ctx, id)
		switch {
		case patchErr == nil && got == "1440p" && stored.Resolution == "1440p":
		case errors.Is(patchErr, core.ErrConflict) && got == "720p" && stored.Resolution == "":
		default:
			t.Fatalf("%s: patch %v, transcoder at %s, stored %q", id, patchErr, got, stored.Resolution)
		}
		seen[patchErr == nil]++
		if _, err := e.Service.End(ctx, "u1", id); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("patch first %d times, go-live first %d times", seen[true], seen[false])
}

// TestRunKeepsItsProfile is design S3 of add-server-settings: a run keeps the profile it started with when the
// server's quality changes; the next go-live takes the new one.
func TestRunKeepsItsProfile(t *testing.T) {
	t.Parallel()
	e := outputtest.New(t)
	var mu sync.Mutex
	q := broadcast.Quality{Default: "720p", Max: "1440p"}
	e.Service.Quality = func() broadcast.Quality { mu.Lock(); defer mu.Unlock(); return q }
	e.AddDestination(t, "dA", "u1", "YouTube", "")
	e.AddBroadcast(t, "b1", "u1", "dA")
	b, _ := e.Broadcasts.Get(ctx, "b1")
	b.Resolution = "1440p"
	e.Broadcasts.Put(*b)
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	q.Max = "1080p"
	mu.Unlock()
	first := e.Processes.Last(t, "b1", "")
	first.Flow()
	first.Exit("lost")
	e.Tick(time.Second)
	if retry := e.Processes.Last(t, "b1", ""); retry == first || retry.Profile.Resolution != "1440p" {
		t.Fatalf("the retry of the run went out at %+v", retry.Profile)
	}
	if _, err := e.Service.End(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	if got := e.Processes.Last(t, "b1", "").Profile.Resolution; got != "1080p" {
		t.Fatalf("the next run went out at %s", got)
	}
}
