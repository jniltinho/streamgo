// Command streamgo is the StreamGo server and administration CLI: a self-hosted live-streaming studio in a
// single binary. It delegates to cmd.Execute (Cobra).
//
// Portions adapted from MeetHub (MIT): main.go
package main

import (
	"embed"

	"github.com/jniltinho/streamgo/cmd"
)

// notices are the licence texts `streamgo licenses` prints: StreamGo's, the media servers' and the fonts'.
// frontend/ is a module of its own, which go:embed cannot reach: third_party/fonts holds copies of the font
// licences of frontend/src/assets/fonts (TestNoticesEmbedded keeps them identical).
//
//go:embed LICENSE NOTICE THIRD_PARTY third_party/livekit/NOTICE third_party/fonts/LICENSE-*.txt
var notices embed.FS

func main() {
	cmd.Execute(notices)
}
