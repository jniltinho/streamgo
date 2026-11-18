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
	// LiveKitSHA256 and MediaMTXSHA256 are the SHA-256 of the two media servers the release pins (LIVEKIT_SHA256 and
	// MEDIAMTX_SHA256 of the Dockerfile). `streamgo up` runs a media server of its own payload only when the unpacked
	// file has that digest. Empty (a development build): the digest the payload's trailer records is used instead.
	LiveKitSHA256  = ""
	MediaMTXSHA256 = ""
)
