package broadcast

// Profile is an output resolution with its frame size and video bitrate (add-output-quality, Q1). Every
// resolution goes out at 30 fps with a keyframe every 60 frames.
type Profile struct {
	Resolution string
	Width      int
	Height     int
	VideoKbps  int
}

// ladder is the one place that spells a size, lowest first. 2160p is left out: the owner's Chrome did not
// draw and send it at 30 fps (docs/architecture.md, "Output quality").
var ladder = []Profile{
	{"720p", 1280, 720, 4000},
	{"1080p", 1920, 1080, 6000},
	{"1440p", 2560, 1440, 12000},
}

// Resolutions lists the known resolutions, lowest first.
func Resolutions() []string {
	out := make([]string, len(ladder))
	for i, p := range ladder {
		out[i] = p.Resolution
	}
	return out
}

// rank is the position of a resolution on the ladder, -1 when it is not one.
func rank(resolution string) int {
	for i, p := range ladder {
		if p.Resolution == resolution {
			return i
		}
	}
	return -1
}

// Known reports whether resolution is on the ladder.
func Known(resolution string) bool { return rank(resolution) >= 0 }

// Above reports whether resolution a is higher than b; both must be known.
func Above(a, b string) bool { return rank(a) > rank(b) }

// Quality is the operator's output settings: the default of new broadcasts, the ceiling and the bitrate
// override (0: the ladder's bitrate). The configuration loader guarantees Default and Max are known and
// Default is not above Max.
type Quality struct {
	Default   string
	Max       string
	VideoKbps int
}

// The zero Quality is 720p only, at the ladder's bitrate.
func (q Quality) defaultRank() int { return max(rank(q.Default), 0) }
func (q Quality) maxRank() int     { return max(rank(q.Max), q.defaultRank()) }

// Choices are the profiles a broadcast may use: the ladder up to the ceiling.
func (q Quality) Choices() []Profile {
	out := make([]Profile, 0, len(ladder))
	for _, p := range ladder[:q.maxRank()+1] {
		out = append(out, q.withBitrate(p))
	}
	return out
}

// Allowed reports whether a broadcast may choose resolution: known and not above the ceiling.
func (q Quality) Allowed(resolution string) bool {
	r := rank(resolution)
	return r >= 0 && r <= q.maxRank()
}

// Profile is what a broadcast stored with resolution goes out at: the server's default for an empty one (or
// one no longer known) and the ceiling for one above it.
func (q Quality) Profile(resolution string) Profile {
	r := rank(resolution)
	if r < 0 {
		r = q.defaultRank()
	}
	return q.withBitrate(ladder[min(r, q.maxRank())])
}

func (q Quality) withBitrate(p Profile) Profile {
	if q.VideoKbps > 0 {
		p.VideoKbps = q.VideoKbps
	}
	return p
}
