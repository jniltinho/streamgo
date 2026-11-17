// Package compose_test checks the Compose stack's files, make init and the deployment guide (build-and-deploy spec:
// "Media servers configured", "Media server not exposed", "Init keeps existing secrets", "Guide covers the list").
// The running stack is checked by hand as docs/deployment.md describes.
package compose_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func readYAML(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

type composeFile struct {
	Services map[string]struct {
		Ports       []string          `yaml:"ports"`
		Environment map[string]string `yaml:"environment"`
	} `yaml:"services"`
}

func TestMediaServersConfigured(t *testing.T) {
	var lk struct {
		Room struct {
			AutoCreate *bool `yaml:"auto_create"`
		} `yaml:"room"`
		Redis any `yaml:"redis"`
	}
	readYAML(t, "livekit.yaml", &lk)
	if lk.Room.AutoCreate == nil || *lk.Room.AutoCreate {
		t.Error("livekit.yaml: room.auto_create must be false")
	}
	if lk.Redis != nil {
		t.Error("livekit.yaml: single node, no Redis")
	}

	var mtx map[string]any
	readYAML(t, "mediamtx.yml", &mtx)
	if mtx["authMethod"] != "internal" {
		t.Errorf("mediamtx.yml: authMethod = %v, want internal", mtx["authMethod"])
	}
	for _, on := range []string{"rtsp", "webrtc", "api"} {
		if mtx[on] != true {
			t.Errorf("mediamtx.yml: %s must be true", on)
		}
	}
	for _, off := range []string{"rtmp", "hls", "srt", "moq", "metrics", "pprof", "playback"} {
		if mtx[off] != false {
			t.Errorf("mediamtx.yml: %s must be false", off)
		}
	}
	if users, _ := mtx["authInternalUsers"].([]any); len(users) != 1 {
		t.Errorf("mediamtx.yml: want exactly one internal user, got %d", len(users))
	}

	// The credentials come from .env, never from the files.
	var c composeFile
	readYAML(t, "compose.yml", &c)
	env := c.Services["mediamtx"].Environment
	if env["MTX_AUTHINTERNALUSERS_0_USER"] != "${STREAMGO_MEDIAMTX_USER:?run make init}" ||
		env["MTX_AUTHINTERNALUSERS_0_PASS"] != "${STREAMGO_MEDIAMTX_SECRET:?run make init}" {
		t.Errorf("compose.yml: MediaMTX's user must come from .env, got %v", env)
	}
	if !strings.Contains(c.Services["livekit"].Environment["LIVEKIT_KEYS"], "${STREAMGO_LIVEKIT_API_SECRET:?") {
		t.Error("compose.yml: LiveKit's key must come from .env")
	}
}

func TestMediaServerNotExposed(t *testing.T) {
	want := map[string][]string{
		"streamgo": {"${HTTP_PORT:-8080}:8080"},
		"livekit": {
			"${LIVEKIT_PORT:-7880}:7880",
			"${LIVEKIT_TCP_PORT:-7881}:${LIVEKIT_TCP_PORT:-7881}/tcp",
			"${LIVEKIT_UDP_PORT:-7882}:${LIVEKIT_UDP_PORT:-7882}/udp",
		},
		"mediamtx": {
			"${MEDIAMTX_WEBRTC_PORT:-8189}:${MEDIAMTX_WEBRTC_PORT:-8189}/udp",
			"${MEDIAMTX_WEBRTC_PORT:-8189}:${MEDIAMTX_WEBRTC_PORT:-8189}/tcp",
		},
	}
	got := map[string][]string{}
	for _, f := range []string{"compose.yml", "compose.mariadb.yml", "compose.postgres.yml"} {
		var c composeFile
		readYAML(t, f, &c)
		for name, s := range c.Services {
			got[name] = append(got[name], s.Ports...)
		}
	}
	for name, ports := range got {
		if !slices.Equal(ports, want[name]) {
			t.Errorf("service %s publishes %q, want %q", name, ports, want[name])
		}
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("service %s is missing", name)
		}
	}
}

func TestInitKeepsExistingSecrets(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	dir := t.TempDir()
	for _, f := range []string{"Makefile", "config.toml.example"} {
		data, err := os.ReadFile(filepath.Join("..", "..", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	initRun := func() {
		t.Helper()
		cmd := exec.Command("make", "-s", "init")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("make init: %v\n%s", err, out)
		}
	}
	envPath := filepath.Join(dir, ".env")

	// A new checkout gets every variable, readable only by its owner.
	initRun()
	first, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"STREAMGO_SECRET_KEY", "STREAMGO_LIVEKIT_API_KEY", "STREAMGO_LIVEKIT_API_SECRET",
		"STREAMGO_MEDIAMTX_USER", "STREAMGO_MEDIAMTX_SECRET", "STREAMGO_DATABASE_PASSWORD"} {
		if !regexp.MustCompile(`(?m)^` + v + `=\S+$`).Match(first) {
			t.Errorf(".env lacks a value for %s", v)
		}
	}
	if fi, err := os.Stat(envPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf(".env mode: %v %v, want 0600", fi.Mode().Perm(), err)
	}

	// Running it again changes nothing.
	initRun()
	if again, _ := os.ReadFile(envPath); !bytes.Equal(again, first) {
		t.Error("a second make init changed .env")
	}

	// An older .env keeps its key; only the missing variables are added after it.
	old := []byte("STREAMGO_SECRET_KEY=a2VlcC1tZS1rZWVwLW1lLWtlZXAtbWUta2VlcC1tZSE=\n")
	if err := os.WriteFile(envPath, old, 0o600); err != nil {
		t.Fatal(err)
	}
	initRun()
	got, _ := os.ReadFile(envPath)
	if !bytes.HasPrefix(got, old) || bytes.Count(got, []byte("STREAMGO_SECRET_KEY=")) != 1 {
		t.Error("make init changed the existing secret key")
	}
}

func TestGuideCoversTheList(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "deployment.md"))
	if err != nil {
		t.Fatal(err)
	}
	var headings []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			headings = append(headings, strings.ToLower(line))
		}
	}
	items := map[string]*regexp.Regexp{
		"HTTPS in front of the app and secure WebSocket for LiveKit": regexp.MustCompile(`https.*secure websocket`),
		"the addresses LiveKit and MediaMTX advertise behind NAT":    regexp.MustCompile(`advertised addresses behind nat`),
		"no TURN, the owner needs MediaMTX's WebRTC port":            regexp.MustCompile(`no turn.*mediamtx's webrtc port`),
		"the reverse proxy not logging the join route":               regexp.MustCompile(`join route.*not be logged`),
		"one StreamGo instance per deployment":                       regexp.MustCompile(`one streamgo instance per deployment`),
		"backup and restore":                                         regexp.MustCompile(`backup and restore`),
	}
	for item, re := range items {
		if !slices.ContainsFunc(headings, re.MatchString) {
			t.Errorf("docs/deployment.md has no section for %s; headings: %q", item, headings)
		}
	}
}
