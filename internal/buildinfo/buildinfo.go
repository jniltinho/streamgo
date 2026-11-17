// Package buildinfo exposes build metadata injected with -ldflags at build time.
//
// Portions adapted from MeetHub (MIT): internal/buildinfo/buildinfo.go
package buildinfo

// Build metadata, overridden by the linker (-X) in release builds.
var (
	// Version is the release version or git describe output ("dev" when unset).
	Version = "dev"
	// Commit is the short git commit hash.
	Commit = "none"
	// Date is the UTC build timestamp in RFC 3339 format.
	Date = "unknown"
)
