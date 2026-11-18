package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestNoticesEmbedded is the inventory of "Notices: Printed": the executable carries StreamGo's licence and notice,
// THIRD_PARTY, LiveKit's notice and the six font licences, the latter identical to those beside the fonts.
func TestNoticesEmbedded(t *testing.T) {
	want := []string{"LICENSE", "NOTICE", "THIRD_PARTY", "third_party/livekit/NOTICE"}
	fonts, err := filepath.Glob("frontend/src/assets/fonts/LICENSE-*.txt")
	if err != nil || len(fonts) != 6 {
		t.Fatalf("font licences %v %v", fonts, err)
	}
	for _, f := range fonts {
		embedded := "third_party/fonts/" + filepath.Base(f)
		want = append(want, embedded)
		original, _ := os.ReadFile(f) //nolint:gosec // a repository file
		copied, err := fs.ReadFile(notices, embedded)
		if err != nil || !bytes.Equal(original, copied) {
			t.Errorf("%s is not a copy of %s: %v", embedded, f, err)
		}
	}
	for _, name := range want {
		if b, err := fs.ReadFile(notices, name); err != nil || len(b) == 0 {
			t.Errorf("%s is not embedded: %v", name, err)
		}
	}
	thirdParty, _ := fs.ReadFile(notices, "THIRD_PARTY")
	for _, s := range []string{"UPX", "third_party/livekit/NOTICE"} {
		if !bytes.Contains(thirdParty, []byte(s)) {
			t.Errorf("THIRD_PARTY does not mention %s", s)
		}
	}
}
