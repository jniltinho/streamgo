// Package packaging holds the Linux packages' build and smoke scripts; its tests read them.
package packaging

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// buildFiles are everything that builds an image or a package.
var buildFiles = []string{
	"../Dockerfile", "build.sh", "init-env.sh", "postinstall.sh", "preremove.sh", "postremove.sh", "streamgo.spec",
	"control", "../docker/allinone/entrypoint.sh", "../docker/allinone/streamgo.sh",
}

var (
	// a fetch of ffmpeg or of a codec's source
	fetch = regexp.MustCompile(`(?i)(curl|wget|git clone|^\s*ADD\s+https?:)`)
	codec = regexp.MustCompile(`(?i)(ffmpeg|x264|x265|opus|vpx|fdk-aac|lame|dav1d|aom)`)
	// a source build, or a source tree of ffmpeg or its codecs
	compile = regexp.MustCompile(`(?i)(\./configure|make\s+(-j\S*\s+)?install|--enable-(gpl|nonfree|lib)|ffmpeg\.org|videolan\.org|FFMPEG_(VERSION|SHA256))`)
)

// ffmpegBuildLines returns the lines of text that download or compile ffmpeg or its codecs.
func ffmpegBuildLines(text string) []string {
	var bad []string
	for _, l := range strings.Split(text, "\n") {
		if (fetch.MatchString(l) && codec.MatchString(l)) || compile.MatchString(l) {
			bad = append(bad, strings.TrimSpace(l))
		}
	}
	return bad
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Scenario "No ffmpeg build" (build-and-deploy): no stage or script downloads or compiles ffmpeg or its codecs; the
// images install the distribution's package, the .deb depends on it and the .rpm recommends it.
func TestNoFFmpegBuild(t *testing.T) {
	for _, f := range buildFiles {
		if bad := ffmpegBuildLines(read(t, f)); len(bad) > 0 {
			t.Errorf("%s downloads or compiles ffmpeg or a codec:\n  %s", f, strings.Join(bad, "\n  "))
		}
	}
	docker := read(t, "../Dockerfile")
	for _, want := range []string{"apk add --no-cache ffmpeg", "apt-get install -y --no-install-recommends ffmpeg"} {
		if !strings.Contains(docker, want) {
			t.Errorf("the Dockerfile does not install the distribution's ffmpeg (%q)", want)
		}
	}
	if !regexp.MustCompile(`(?m)^Depends:.*\bffmpeg\b`).MatchString(read(t, "control")) {
		t.Error("the .deb does not depend on the distribution's ffmpeg")
	}
	if !strings.Contains(read(t, "streamgo.spec"), "Recommends:     (ffmpeg or ffmpeg-free)") {
		t.Error("the .rpm does not recommend ffmpeg or ffmpeg-free")
	}
}

// The check itself catches the builds it is meant to catch.
func TestFFmpegBuildLines(t *testing.T) {
	for _, tc := range []struct {
		line string
		bad  bool
	}{
		{"RUN curl -fsSL -o ffmpeg.txz https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz", true},
		{"RUN wget https://code.videolan.org/videolan/x264/-/archive/stable/x264-stable.tar.bz2", true},
		{"RUN git clone https://github.com/xiph/opus", true},
		{"RUN cd ffmpeg && ./configure --enable-libx264 && make -j8 install", true},
		{"ARG FFMPEG_VERSION=9.0.2", true},
		{"RUN apk add --no-cache ffmpeg", false},
		{"RUN apt-get install -y --no-install-recommends ffmpeg supervisor", false},
		{"Depends: passwd, ffmpeg", false},
	} {
		if got := len(ffmpegBuildLines(tc.line)) > 0; got != tc.bad {
			t.Errorf("%q: flagged %v, want %v", tc.line, got, tc.bad)
		}
	}
}

// Scenario "Third-party notices": THIRD_PARTY names the media servers the Dockerfile ships, with their image
// digests and the checksums the build verifies, and they are the Compose stack's versions.
func TestThirdPartyMatchesDockerfile(t *testing.T) {
	docker, third, compose := read(t, "../Dockerfile"), read(t, "../THIRD_PARTY"), read(t, "../deployments/compose/compose.yml")
	for _, arg := range []string{"LIVEKIT_IMAGE", "LIVEKIT_SHA256", "MEDIAMTX_IMAGE", "MEDIAMTX_SHA256"} {
		m := regexp.MustCompile(`(?m)^ARG ` + arg + `=(\S+)$`).FindStringSubmatch(docker)
		if m == nil {
			t.Fatalf("the Dockerfile has no ARG %s", arg)
		}
		if !strings.Contains(third, m[1]) {
			t.Errorf("THIRD_PARTY does not carry %s %s", arg, m[1])
		}
		if strings.HasSuffix(arg, "_IMAGE") && !strings.Contains(compose, "image: "+m[1]) {
			t.Errorf("the Compose stack does not run %s", m[1])
		}
	}
	for _, want := range []string{"LiveKit server 1.13.10", "MediaMTX 1.21.4", "Apache License", "MIT License"} {
		if !strings.Contains(third, want) {
			t.Errorf("THIRD_PARTY lacks %q", want)
		}
	}
}
