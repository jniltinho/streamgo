// Package settings holds the server settings an administrator changes in the app (add-server-settings): the
// output's default resolution, its ceiling and the video bitrate. The effective value of each is the environment's
// when it sets it (locked), else the stored one, else the configuration file's (design S1).
package settings

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
)

// The fields of the output settings, as the API names them (and as Locked lists them).
const (
	FieldResolution    = "resolution"
	FieldMaxResolution = "max_resolution"
	FieldVideoKbps     = "video_bitrate_kbps"
)

// The stored names of the settings (design S2).
const (
	keyResolution    = "output.resolution"
	keyMaxResolution = "output.max_resolution"
	keyVideoKbps     = "output.video_bitrate_kbps"
)

// Repository stores the settings by name.
type Repository interface {
	// Load returns every stored setting.
	Load(ctx context.Context) (map[string]string, error)
	// Save writes every given setting or none.
	Save(ctx context.Context, values map[string]string) error
}

// Admins answers core.ErrForbidden unless actorID is an enabled administrator; account.Service implements it.
type Admins interface {
	RequireAdmin(ctx context.Context, actorID string) error
}

// Output is the effective output settings and the fields the environment sets.
type Output struct {
	broadcast.Quality
	Locked []string
}

// Patch is a change to the output settings; a nil field is left as it is.
type Patch struct {
	Resolution    *string
	MaxResolution *string
	VideoKbps     *int
}

// Service serves the settings and is the one source of the output quality (design S3).
type Service struct {
	repo    Repository
	admins  Admins
	base    broadcast.Quality // the configuration's values, the environment's included
	locked  []string
	mu      sync.Mutex // one update at a time
	current atomic.Pointer[broadcast.Quality]
}

// New loads the stored settings over base, the quality the configuration gives (file and environment); locked
// names the fields the environment sets, which keep base's value. A stored combination that breaks the rules
// (an environment value set later can cause it) is ignored with a warning.
func New(ctx context.Context, repo Repository, admins Admins, base broadcast.Quality, locked []string, logger *slog.Logger) (*Service, error) {
	s := &Service{repo: repo, admins: admins, base: base, locked: slices.Clone(locked)}
	stored, err := repo.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	q := base
	var p Patch
	if v, ok := stored[keyResolution]; ok {
		p.Resolution = &v
	}
	if v, ok := stored[keyMaxResolution]; ok {
		p.MaxResolution = &v
	}
	if v, ok := stored[keyVideoKbps]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			n = -1 // refused by the rules below
		}
		p.VideoKbps = &n
	}
	if merged, err := s.apply(q, p, false); err != nil {
		if logger != nil {
			logger.Warn("the stored output settings are ignored: they break the rules with the configuration", "error", err)
		}
	} else {
		q = merged
	}
	s.current.Store(&q)
	return s, nil
}

// Quality is the effective output quality; broadcast.Service and output.Service read it.
func (s *Service) Quality() broadcast.Quality { return *s.current.Load() }

// Get returns the output settings to the administrator actorID (core.ErrForbidden otherwise).
func (s *Service) Get(ctx context.Context, actorID string) (*Output, error) {
	if err := s.admins.RequireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	return s.output(), nil
}

// Update applies the patch for the administrator actorID, all or nothing: a locked field that would change wraps
// core.ErrConflict, a value that breaks the rules is a core.Invalid; then the given values are stored and apply at
// once.
func (s *Service) Update(ctx context.Context, actorID string, p Patch) (*Output, error) {
	if err := s.admins.RequireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := s.apply(s.Quality(), p, true)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if p.Resolution != nil {
		values[keyResolution] = q.Default
	}
	if p.MaxResolution != nil {
		values[keyMaxResolution] = q.Max
	}
	if p.VideoKbps != nil {
		values[keyVideoKbps] = strconv.Itoa(q.VideoKbps)
	}
	if len(values) > 0 {
		if err := s.repo.Save(ctx, values); err != nil {
			return nil, fmt.Errorf("save settings: %w", err)
		}
	}
	s.current.Store(&q)
	return s.output(), nil
}

func (s *Service) output() *Output {
	return &Output{Quality: s.Quality(), Locked: slices.Clone(s.locked)}
}

// apply puts the patch over q and checks the rules (design S4). A locked field keeps its value: with strict, one
// the patch would change is a conflict; without, it is skipped (the stored value under the environment's).
func (s *Service) apply(q broadcast.Quality, p Patch, strict bool) (broadcast.Quality, error) {
	set := func(field string, changed bool) (bool, error) {
		if !slices.Contains(s.locked, field) {
			return true, nil
		}
		if strict && changed {
			return false, fmt.Errorf("%s is set by the server's environment: %w", field, core.ErrConflict)
		}
		return false, nil
	}
	if p.Resolution != nil {
		ok, err := set(FieldResolution, *p.Resolution != q.Default)
		if err != nil {
			return q, err
		}
		if ok {
			q.Default = *p.Resolution
		}
	}
	if p.MaxResolution != nil {
		ok, err := set(FieldMaxResolution, *p.MaxResolution != q.Max)
		if err != nil {
			return q, err
		}
		if ok {
			q.Max = *p.MaxResolution
		}
	}
	if p.VideoKbps != nil {
		ok, err := set(FieldVideoKbps, *p.VideoKbps != q.VideoKbps)
		if err != nil {
			return q, err
		}
		if ok {
			q.VideoKbps = *p.VideoKbps
		}
	}
	return q, check(q)
}

// check applies the configuration loader's rules to q.
func check(q broadcast.Quality) error {
	resolutions := "must be one of " + strings.Join(broadcast.Resolutions(), ", ")
	switch {
	case !broadcast.Known(q.Default):
		return core.Invalid(FieldResolution, resolutions)
	case !broadcast.Known(q.Max):
		return core.Invalid(FieldMaxResolution, resolutions)
	case broadcast.Above(q.Default, q.Max):
		return core.Invalid(FieldResolution, "must not be above the highest resolution "+q.Max)
	case q.VideoKbps != 0 && (q.VideoKbps < 500 || q.VideoKbps > 40000):
		return core.Invalid(FieldVideoKbps, "must be 0 (by resolution) or from 500 to 40000")
	}
	return nil
}
