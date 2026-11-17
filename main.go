// Command streamgo is the StreamGo server and administration CLI: a self-hosted live-streaming studio in a
// single binary. It delegates to cmd.Execute (Cobra).
//
// Portions adapted from MeetHub (MIT): main.go
package main

import "github.com/jniltinho/streamgo/cmd"

func main() {
	cmd.Execute()
}
