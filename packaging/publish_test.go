package packaging

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeGH is the part of the GitHub CLI publish-release.sh uses, on a release kept in $FAKE_GH: assets/ (absent: no
// release), draft, and fail-* files that make a call fail. Every call is appended to calls.
const fakeGH = `#!/bin/sh
set -eu
s=$FAKE_GH
echo "$*" >>"$s/calls"
case "$1 $2" in
"release view")
  [ -d "$s/assets" ] || { echo "release not found" >&2; exit 1; }
  [ ! -f "$s/fail-view" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
  case "$*" in
  *isDraft*) cat "$s/draft" ;;
  *'select(.name == "SHA256SUMS.next")'*) [ ! -f "$s/assets/SHA256SUMS.next" ] || echo 42 ;;
  *'.assets[].name'*) ls "$s/assets" ;;
  esac ;;
"release create")
  shift 3
  mkdir "$s/assets"
  echo false >"$s/draft"
  for a; do case "$a" in --*) break ;; esac; cp "$a" "$s/assets/"; done ;;
"release upload")
  shift 3
  clobber=no
  for a; do [ "$a" != --clobber ] || clobber=yes; done
  for a; do
    case "$a" in --*) continue ;; esac
    n=$(basename "$a")
    if [ -f "$s/fail-upload-$n" ]; then
      [ $clobber = no ] || rm -f "$s/assets/$n" # gh deletes the old asset first
      echo "upload failed" >&2
      exit 1
    fi
    [ ! -f "$s/assets/$n" ] || [ $clobber = yes ] || { echo "asset under the same name already exists" >&2; exit 1; }
    cp "$a" "$s/assets/$n"
  done ;;
"release download")
  [ -f "$s/assets/$5" ] || { echo "no assets match the file pattern" >&2; exit 1; }
  cp "$s/assets/$5" "$7/" ;;
"release delete-asset") rm "$s/assets/$4" ;;
"release edit") echo false >"$s/draft" ;;
"api -X")
  [ ! -f "$s/fail-rename" ] || { echo "HTTP 500" >&2; exit 1; }
  case "$4" in */releases/assets/42) ;; *) echo "unexpected $4" >&2; exit 1 ;; esac
  [ ! -f "$s/assets/SHA256SUMS" ] || { echo "HTTP 422: already_exists" >&2; exit 1; }
  mv "$s/assets/SHA256SUMS.next" "$s/assets/SHA256SUMS" ;;
*) echo "fake gh: unexpected $*" >&2; exit 1 ;;
esac
`

var releaseFiles = []string{"streamgo_0.4.0_amd64.deb", "streamgo-0.4.0-1.x86_64.rpm", "streamgo_0.4.0_linux_amd64.tar.gz",
	"streamgo_0.4.0_linux_amd64", "streamgo-server_0.4.0_linux_amd64.tar.gz"}

// manifest is SHA256SUMS of files, in the order of packaging/build.sh.
func manifest(files map[string]string) string {
	var b strings.Builder
	for _, n := range releaseFiles {
		data, ok := files[n]
		if !ok {
			continue
		}
		sum := sha256.Sum256([]byte(data))
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return b.String()
}

// TestPublishRelease runs packaging/publish-release.sh against a fake gh: each state of a release it may meet.
func TestPublishRelease(t *testing.T) {
	built := map[string]string{} // this build's five files
	for _, n := range releaseFiles {
		built[n] = "built " + n
	}
	builtSums := manifest(built)
	old := map[string]string{} // the four-file release of an earlier version of the workflow: three of the five
	for _, n := range releaseFiles[:3] {
		old[n] = "old " + n
	}
	withSums := func(files map[string]string, sums string) map[string]string {
		out := maps.Clone(files)
		out["SHA256SUMS"] = sums
		return out
	}
	completed := maps.Clone(old) // the four-file release once completed: its three files kept, two added
	for _, n := range releaseFiles[3:] {
		completed[n] = built[n]
	}
	tampered := withSums(old, manifest(old))
	tampered[releaseFiles[0]] = "replaced by someone"

	for _, tc := range []struct {
		name  string
		tag   string
		draft bool
		have  map[string]string // nil: no release
		fail  []string          // fail-* files of the fake
		ok    bool
		want  map[string]string // the assets after the run; nil: as before
	}{
		{name: "no release", have: nil, ok: true, want: withSums(built, builtSums)},
		{name: "complete", have: withSums(built, builtSums), ok: true},
		{name: "a release of four files", have: withSums(old, manifest(old)), ok: true, want: withSums(completed, manifest(completed))},
		{name: "a kept file that does not match", have: tampered},
		{name: "a missing file that does not upload", have: withSums(old, manifest(old)), fail: []string{"fail-upload-" + releaseFiles[3]}},
		{name: "the manifest that does not upload", have: withSums(old, manifest(old)), fail: []string{"fail-upload-SHA256SUMS.next"},
			want: withSums(completed, manifest(old))},
		{name: "the rename that fails", have: withSums(old, manifest(old)), fail: []string{"fail-rename"},
			want: map[string]string{"SHA256SUMS.next": manifest(completed), releaseFiles[0]: old[releaseFiles[0]],
				releaseFiles[1]: old[releaseFiles[1]], releaseFiles[2]: old[releaseFiles[2]], releaseFiles[3]: built[releaseFiles[3]],
				releaseFiles[4]: built[releaseFiles[4]]}},
		{name: "after the rename that failed", have: map[string]string{"SHA256SUMS.next": manifest(completed),
			releaseFiles[0]: old[releaseFiles[0]], releaseFiles[1]: old[releaseFiles[1]], releaseFiles[2]: old[releaseFiles[2]],
			releaseFiles[3]: built[releaseFiles[3]], releaseFiles[4]: built[releaseFiles[4]]}, ok: true,
			want: withSums(completed, manifest(completed))},
		{name: "an interrupted run of this build", have: withSums(map[string]string{releaseFiles[0]: old[releaseFiles[0]],
			releaseFiles[3]: built[releaseFiles[3]]}, manifest(map[string]string{releaseFiles[0]: old[releaseFiles[0]]})), ok: true,
			want: func() map[string]string {
				m := maps.Clone(built)
				m[releaseFiles[0]] = old[releaseFiles[0]]
				return withSums(m, manifest(m))
			}()},
		{name: "a draft", draft: true, have: map[string]string{releaseFiles[0]: "partial"}, ok: true, want: withSums(built, builtSums)},
		{name: "a release that cannot be read", have: withSums(old, manifest(old)), fail: []string{"fail-view"}},
		{name: "a tag that is not a version", tag: "v0.4.0;id", have: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bin, state, dir := t.TempDir(), t.TempDir(), t.TempDir()
			write := func(path, data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(bin, "gh"), fakeGH)
			if err := os.Chmod(filepath.Join(bin, "gh"), 0o700); err != nil { //nolint:gosec // a test script
				t.Fatal(err)
			}
			for n, data := range withSums(built, builtSums) {
				write(filepath.Join(dir, n), data)
			}
			write(filepath.Join(dir, "CHANGELOG.md"), "# Changelog\n\n## [0.4.0] - 2026-11-18\n\n- the single file\n\n## [0.3.0]\n\n- older\n")
			if tc.have != nil {
				if err := os.Mkdir(filepath.Join(state, "assets"), 0o700); err != nil {
					t.Fatal(err)
				}
				for n, data := range tc.have {
					write(filepath.Join(state, "assets", n), data)
				}
			}
			write(filepath.Join(state, "draft"), fmt.Sprint(tc.draft))
			for _, f := range tc.fail {
				write(filepath.Join(state, f), "")
			}
			tag := tc.tag
			if tag == "" {
				tag = "v0.4.0"
			}
			cmd := exec.Command("sh", "publish-release.sh", tag, dir, filepath.Join(dir, "CHANGELOG.md")) //nolint:noctx // a test
			cmd.Env = append(os.Environ(), "GH="+filepath.Join(bin, "gh"), "FAKE_GH="+state, "PUBLISH_RETRY_DELAY=0")
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok {
				t.Fatalf("error %v, want ok %v:\n%s", err, tc.ok, out)
			}
			want := tc.want
			if want == nil {
				want = tc.have
			}
			got := map[string]string{}
			entries, _ := os.ReadDir(filepath.Join(state, "assets"))
			for _, e := range entries {
				b, _ := os.ReadFile(filepath.Join(state, "assets", e.Name())) //nolint:gosec // a test file
				got[e.Name()] = string(b)
			}
			if want == nil {
				want = map[string]string{}
			}
			if !maps.Equal(got, want) {
				t.Errorf("assets %v\nwant   %v\n%s", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)), out)
				for n := range want {
					if got[n] != want[n] {
						t.Errorf("%s: %q, want %q", n, got[n], want[n])
					}
				}
			}
			if tc.draft {
				if d, _ := os.ReadFile(filepath.Join(state, "draft")); strings.TrimSpace(string(d)) != "false" {
					t.Error("the draft was not published")
				}
			}
		})
	}
}
