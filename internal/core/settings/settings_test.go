package settings_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/settings"
)

type memRepo struct{ values map[string]string }

func (r *memRepo) Load(context.Context) (map[string]string, error) { return maps.Clone(r.values), nil }
func (r *memRepo) Save(_ context.Context, v map[string]string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	maps.Copy(r.values, v)
	return nil
}

// admins: "admin" is an enabled administrator, anyone else is refused.
type admins struct{}

func (admins) RequireAdmin(_ context.Context, id string) error {
	if id != "admin" {
		return core.ErrForbidden
	}
	return nil
}

var file = broadcast.Quality{Default: "720p", Max: "1080p", VideoKbps: 4000}

func ptr[T any](v T) *T { return &v }

func newService(t *testing.T, repo *memRepo, locked ...string) *settings.Service {
	t.Helper()
	s, err := settings.New(context.Background(), repo, admins{}, file, locked, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// "Stored output settings" (raise the ceiling and restart, bitrate by resolution, rules, all or nothing) and
// "Precedence" (locked by the environment).
func TestUpdate(t *testing.T) {
	cases := []struct {
		name    string
		locked  []string
		patch   settings.Patch
		want    broadcast.Quality // after the update and after a restart
		wantErr error
		field   string
	}{
		{name: "raise the ceiling", patch: settings.Patch{MaxResolution: ptr("1440p")},
			want: broadcast.Quality{Default: "720p", Max: "1440p", VideoKbps: 4000}},
		{name: "bitrate by resolution", patch: settings.Patch{VideoKbps: ptr(0)},
			want: broadcast.Quality{Default: "720p", Max: "1080p"}},
		{name: "all three", patch: settings.Patch{Resolution: ptr("1440p"), MaxResolution: ptr("1440p"), VideoKbps: ptr(12000)},
			want: broadcast.Quality{Default: "1440p", Max: "1440p", VideoKbps: 12000}},
		{name: "default above the ceiling", patch: settings.Patch{Resolution: ptr("1440p")}, field: "resolution"},
		{name: "ceiling below the default", patch: settings.Patch{Resolution: ptr("1080p"), MaxResolution: ptr("720p")}, field: "resolution"},
		{name: "unknown resolution", patch: settings.Patch{MaxResolution: ptr("2160p")}, field: "max_resolution"},
		{name: "unknown default", patch: settings.Patch{Resolution: ptr("")}, field: "resolution"},
		{name: "bitrate of 300", patch: settings.Patch{VideoKbps: ptr(300)}, field: "video_bitrate_kbps"},
		{name: "bitrate above 40000", patch: settings.Patch{VideoKbps: ptr(40001)}, field: "video_bitrate_kbps"},
		{name: "all or nothing", patch: settings.Patch{MaxResolution: ptr("1440p"), VideoKbps: ptr(300)}, field: "video_bitrate_kbps"},
		{name: "locked by the environment", locked: []string{"max_resolution"}, patch: settings.Patch{MaxResolution: ptr("1440p")},
			wantErr: core.ErrConflict},
		{name: "locked, the same value", locked: []string{"max_resolution"},
			patch: settings.Patch{MaxResolution: ptr("1080p"), VideoKbps: ptr(0)},
			want:  broadcast.Quality{Default: "720p", Max: "1080p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memRepo{}
			s := newService(t, repo, tc.locked...)
			got, err := s.Update(context.Background(), "admin", tc.patch)
			var invalid *core.InvalidError
			switch {
			case tc.field != "":
				if !errors.As(err, &invalid) || invalid.Field != tc.field {
					t.Fatalf("got %v, want invalid %s", err, tc.field)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v, want %v", err, tc.wantErr)
				}
			case err != nil:
				t.Fatal(err)
			}
			if err != nil {
				if len(repo.values) != 0 || s.Quality() != file {
					t.Fatalf("a refused update changed %v, %+v", repo.values, s.Quality())
				}
				return
			}
			if got.Quality != tc.want || s.Quality() != tc.want {
				t.Fatalf("got %+v, source %+v, want %+v", got.Quality, s.Quality(), tc.want)
			}
			if again := newService(t, repo, tc.locked...); again.Quality() != tc.want {
				t.Fatalf("after a restart %+v, want %+v", again.Quality(), tc.want)
			}
		})
	}
}

// "Precedence": the environment wins over the stored value, the stored one over the file; a stored combination
// that breaks the rules is ignored.
func TestPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		stored map[string]string
		locked []string
		want   broadcast.Quality
	}{
		{"file only", nil, nil, file},
		{"stored wins over the file", map[string]string{"output.resolution": "1080p"}, nil,
			broadcast.Quality{Default: "1080p", Max: "1080p", VideoKbps: 4000}},
		{"the environment wins over the stored value", map[string]string{"output.max_resolution": "1440p", "output.video_bitrate_kbps": "0"},
			[]string{"max_resolution"}, broadcast.Quality{Default: "720p", Max: "1080p"}},
		{"a stored combination that breaks the rules", map[string]string{"output.resolution": "1440p"}, nil, file},
		{"a stored bitrate that is not a number", map[string]string{"output.video_bitrate_kbps": "fast"}, nil, file},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newService(t, &memRepo{values: tc.stored}, tc.locked...)
			if s.Quality() != tc.want {
				t.Fatalf("got %+v, want %+v", s.Quality(), tc.want)
			}
			out, err := s.Get(context.Background(), "admin")
			if err != nil || out.Quality != tc.want || len(out.Locked) != len(tc.locked) {
				t.Fatalf("get: %+v, %v", out, err)
			}
		})
	}
}

// "Administrators only".
func TestNotAnAdministrator(t *testing.T) {
	repo := &memRepo{}
	s := newService(t, repo)
	if _, err := s.Get(context.Background(), "bruno"); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("get: %v", err)
	}
	if _, err := s.Update(context.Background(), "bruno", settings.Patch{MaxResolution: ptr("1440p")}); !errors.Is(err, core.ErrForbidden) {
		t.Fatalf("update: %v", err)
	}
	if len(repo.values) != 0 || s.Quality() != file {
		t.Fatal("a refused update changed something")
	}
}
