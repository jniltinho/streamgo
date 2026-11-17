// Package web embeds the built web app (frontend/ → web/dist) into the binary.
// Only web/dist/.gitkeep is versioned; the "all:" prefix includes it so the
// embed pattern matches on a fresh clone, before the frontend is built.
//
// Portions adapted from MeetHub (MIT): web/embed.go
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the web app's files rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil { // only possible if the embed directive changes
		panic(err)
	}
	return sub
}
