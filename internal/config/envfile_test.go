package config_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/jniltinho/streamgo/internal/config"
)

// The first-start-secrets and settings scenarios that need no process. Values in these tests are made up: never
// a real secret.

func lookupOf(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func readEnvFile(t *testing.T, dir string) map[string]string {
	t.Helper()
	vars, err := config.ReadEnvFile(config.EnvFilePath(dir), os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	return vars
}

func TestParseEnv(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text string
		want       map[string]string
		badLine    string // "": no error
	}{
		{name: "assignments, comments and blank lines", text: "# c\n\nA=1\nB='x y'\nC=\"z\"\r\nD=\nE=a=b\n",
			want: map[string]string{"A": "1", "B": "x y", "C": "z", "D": "", "E": "a=b"}},
		{name: "a command", text: "A=1\n#\nrm -rf /tmp/x\n", badLine: "line 3"},
		{name: "export", text: "export A=1\n", badLine: "line 1"},
		{name: "a name with a blank", text: "A B=1\n", badLine: "line 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := config.ParseEnv("f.env", []byte(tc.text))
			if tc.badLine != "" {
				if err == nil || !strings.Contains(err.Error(), "f.env "+tc.badLine+":") {
					t.Fatalf("error %v, want f.env %s", err, tc.badLine)
				}
				return
			}
			if err != nil || !maps.Equal(got, tc.want) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

// TestFileNotAssignments is "A file that is not assignments": the command exits naming the file and line 3, and
// nothing ran.
func TestFileNotAssignments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	victim := filepath.Join(dir, "x")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	text := "STREAMGO_SECRET_KEY=" + validKey + "\n# then\nrm -rf " + victim + "\n"
	if err := os.WriteFile(config.EnvFilePath(dir), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadWith("", lookupOf(map[string]string{"STREAMGO_SERVER_DATA_DIR": dir}), os.ReadFile)
	if err == nil || !strings.Contains(err.Error(), config.EnvFilePath(dir)+" line 3") {
		t.Fatalf("error %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("the line ran:", err)
	}
}

// TestOtherCommandsReadTheFile is the configuration side of "Other commands": with no secret in the environment,
// a command given the data directory reads the secrets of its streamgo.env, and the environment wins over it.
func TestOtherCommandsReadTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := config.EnsureEnvFile(dir, lookupOf(nil), nil); err != nil {
		t.Fatal(err)
	}
	stored := readEnvFile(t, dir)
	// --data-dir is an override, as the flags are: the file of that directory is read
	cfg, err := config.Source{Lookup: lookupOf(nil), ReadFile: os.ReadFile,
		Override: func(c *config.Config) { c.Server.DataDir = dir }}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Secret.Key != stored["STREAMGO_SECRET_KEY"] || cfg.LiveKit.APISecret != stored["STREAMGO_LIVEKIT_API_SECRET"] ||
		cfg.MediaMTX.Secret != stored["STREAMGO_MEDIAMTX_SECRET"] || cfg.Server.DataDir != dir {
		t.Fatal("the stored values were not read")
	}
	cfg, err = config.Source{Lookup: lookupOf(map[string]string{
		"STREAMGO_SERVER_DATA_DIR": dir, "STREAMGO_LIVEKIT_API_SECRET": "from-the-environment",
	}), ReadFile: os.ReadFile}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LiveKit.APISecret != "from-the-environment" {
		t.Fatal("the file won over the environment")
	}
}

// TestCreatedOnce is "Created once": mode 0600 and the same five values after a second start, the file untouched.
func TestCreatedOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	added, err := config.EnsureEnvFile(dir, lookupOf(nil), nil)
	if err != nil || len(added) != 5 {
		t.Fatalf("added %v, %v", added, err)
	}
	first := readEnvFile(t, dir)
	st1, _ := os.Stat(config.EnvFilePath(dir))
	if st1.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st1.Mode())
	}
	if added, err = config.EnsureEnvFile(dir, lookupOf(nil), nil); err != nil || len(added) != 0 {
		t.Fatalf("second start added %v, %v", added, err)
	}
	st2, _ := os.Stat(config.EnvFilePath(dir))
	if second := readEnvFile(t, dir); len(first) != 5 || !maps.Equal(first, second) || !st2.ModTime().Equal(st1.ModTime()) {
		t.Fatal("the second start changed the file")
	}
	cfg, err := config.LoadWith("", lookupOf(map[string]string{"STREAMGO_SERVER_DATA_DIR": dir}), os.ReadFile)
	if err != nil || cfg.Secret.Bytes() == nil {
		t.Fatalf("the generated key does not load: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("a temporary file was left: %v", entries)
	}
}

// TestKeyGivenIsKept is "A key given on the first start is the one kept", on the file: a start without the variable
// then reads the same key, so what it sealed stays readable.
func TestKeyGivenIsKept(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	given := strings.Repeat("A", 43) + "=" // 32 bytes in base64
	if _, err := config.EnsureEnvFile(dir, lookupOf(map[string]string{"STREAMGO_SECRET_KEY": given}), nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWith("", lookupOf(map[string]string{"STREAMGO_SERVER_DATA_DIR": dir}), os.ReadFile)
	if err != nil || cfg.Secret.Key != given {
		t.Fatalf("the key given was not kept: %v", err)
	}
}

// TestEnvironmentWinsFileUnchanged is "The environment wins", on the file: a value in the environment does not
// replace the stored one.
func TestEnvironmentWinsFileUnchanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := config.EnsureEnvFile(dir, lookupOf(nil), nil); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(config.EnvFilePath(dir))
	if _, err := config.EnsureEnvFile(dir, lookupOf(map[string]string{"STREAMGO_LIVEKIT_API_SECRET": "other"}), nil); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(config.EnvFilePath(dir)); !bytes.Equal(before, after) {
		t.Fatal("the file changed")
	}
}

// TestFileWithSomeValues is "A file with some values": the four missing variables are added, the key unchanged.
func TestFileWithSomeValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(config.EnvFilePath(dir), []byte("# mine\nSTREAMGO_SECRET_KEY="+validKey), 0o600); err != nil {
		t.Fatal(err)
	}
	added, err := config.EnsureEnvFile(dir, lookupOf(nil), nil)
	if err != nil || len(added) != 4 {
		t.Fatalf("added %v, %v", added, err)
	}
	vars := readEnvFile(t, dir)
	if len(vars) != 5 || vars["STREAMGO_SECRET_KEY"] != validKey {
		t.Fatalf("%d variables, key kept: %v", len(vars), vars["STREAMGO_SECRET_KEY"] == validKey)
	}
	if b, _ := os.ReadFile(config.EnvFilePath(dir)); !strings.HasPrefix(string(b), "# mine\n") {
		t.Fatal("the existing lines were not kept")
	}
	if st, _ := os.Stat(config.EnvFilePath(dir)); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestEnvValueQuoting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env := map[string]string{"STREAMGO_MEDIAMTX_SECRET": "with space$x"}
	if _, err := config.EnsureEnvFile(dir, lookupOf(env), nil); err != nil {
		t.Fatal(err)
	}
	if got := readEnvFile(t, dir)["STREAMGO_MEDIAMTX_SECRET"]; got != "with space$x" {
		t.Fatal("the value does not read back")
	}
	env["STREAMGO_MEDIAMTX_SECRET"] = "it's"
	if _, err := config.EnsureEnvFile(t.TempDir(), lookupOf(env), nil); err == nil || strings.Contains(err.Error(), "it's") {
		t.Fatalf("a quote: %v", err)
	}
}

// TestExampleListsMediaSettings is "The example lists the settings": the four media settings with their defaults.
func TestExampleListsMediaSettings(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Media config.Media }
	if err := toml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Media != config.Default().Media || doc.Media.WebRTCPort != 8189 || doc.Media.LiveKitTCPPort != 7881 ||
		doc.Media.LiveKitUDPPort != 7882 || doc.Media.NodeIP != "" {
		t.Fatalf("media in the example: %+v", doc.Media)
	}
}

func TestMediaSettingsChecked(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"STREAMGO_MEDIA_WEBRTC_PORT": "70000"}, "media.webrtc_port"},
		{map[string]string{"STREAMGO_MEDIA_LIVEKIT_TCP_PORT": "0"}, "media.livekit_tcp_port"},
		{map[string]string{"STREAMGO_MEDIA_LIVEKIT_UDP_PORT": "8189"}, "media.webrtc_port: must differ"},
		{map[string]string{"STREAMGO_MEDIA_NODE_IP": "not-an-address"}, "media.node_ip"},
	} {
		if _, err := load(t, "", tc.env); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %v", tc.env, err)
		}
	}
	if cfg, err := load(t, "", map[string]string{"STREAMGO_MEDIA_NODE_IP": "192.0.2.10"}); err != nil || cfg.Media.NodeIP != "192.0.2.10" {
		t.Errorf("a node address: %v", err)
	}
}
