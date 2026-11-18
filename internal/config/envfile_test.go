package config_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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

// TestEnvValueQuoting is "A key given on the first start is the one kept" for any value: what the environment gives
// is stored and reads back the same, quotes and backslashes too; only a line break cannot be stored.
func TestEnvValueQuoting(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"with space$x", "it's", `a"b'c`, `back\slash'`, `end\`, `'quoted'`, `"double"`, "#hash ' x"} {
		dir := t.TempDir()
		env := map[string]string{"STREAMGO_MEDIAMTX_SECRET": v}
		if _, err := config.EnsureEnvFile(dir, lookupOf(env), nil); err != nil {
			t.Fatal(err)
		}
		if got := readEnvFile(t, dir)["STREAMGO_MEDIAMTX_SECRET"]; got != v {
			t.Errorf("a value of %d bytes does not read back", len(v))
		}
	}
	env := map[string]string{"STREAMGO_MEDIAMTX_SECRET": "made-up-part\nother"}
	if _, err := config.EnsureEnvFile(t.TempDir(), lookupOf(env), nil); err == nil || strings.Contains(err.Error(), "made-up-part") {
		t.Fatalf("a line break: %v", err)
	}
}

// TestAnEnvFileThatIsNotSafe: streamgo.env holds secrets, so it is read only as a regular file of this user, never
// through a link or from a FIFO; a mode wider than 0600 is taken back. No error holds a value.
func TestAnEnvFileThatIsNotSafe(t *testing.T) {
	t.Parallel()
	const value = "made-up-value-0123456789"
	load := func(dir string) error {
		_, err := config.Source{Lookup: lookupOf(map[string]string{"STREAMGO_SERVER_DATA_DIR": dir, "STREAMGO_SECRET_KEY": validKey})}.Load()
		return err
	}
	ensure := func(dir string) error { _, err := config.EnsureEnvFile(dir, lookupOf(nil), nil); return err }
	for name, tc := range map[string]struct {
		make func(t *testing.T, path string)
		want string // in the error; "": no error
	}{
		"a symbolic link": {func(t *testing.T, path string) {
			target := filepath.Join(t.TempDir(), "elsewhere.env")
			if err := os.WriteFile(target, []byte("STREAMGO_MEDIAMTX_SECRET="+value+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}, "symbolic link"},
		"a FIFO": {func(t *testing.T, path string) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "not a regular file"},
		"a directory": {func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, "not a regular file"},
		"readable by others": {func(t *testing.T, path string) {
			if _, err := config.EnsureEnvFile(filepath.Dir(path), lookupOf(nil), nil); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		}, ""},
	} {
		for how, run := range map[string]func(string) error{"load": load, "first start": ensure} {
			t.Run(name+" at "+how, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				path := config.EnvFilePath(dir)
				tc.make(t, path)
				done := make(chan error, 1)
				go func() { done <- run(dir) }()
				var err error
				select {
				case err = <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("blocked")
				}
				if tc.want == "" {
					st, _ := os.Stat(path)
					if err != nil || st.Mode().Perm() != 0o600 {
						t.Fatalf("error %v, mode %v", err, st.Mode())
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), value) {
					t.Fatalf("error %v", err)
				}
			})
		}
	}
}

// TestAFlagWinsOverAMalformedValue is "A flag wins" when the environment's value of the same setting is not even
// valid: the flag's value is used and the environment's is not read.
func TestAFlagWinsOverAMalformedValue(t *testing.T) {
	t.Parallel()
	env := lookupOf(map[string]string{"STREAMGO_MEDIA_WEBRTC_PORT": "invalid", "STREAMGO_SECRET_KEY": validKey})
	src := config.Source{Lookup: env, ReadFile: os.ReadFile, Override: func(c *config.Config) { c.Media.WebRTCPort = 18189 }}
	if _, err := src.Load(); err == nil || !strings.Contains(err.Error(), "STREAMGO_MEDIA_WEBRTC_PORT") {
		t.Fatalf("without the flag: %v", err)
	}
	src.Flags = []string{"media.webrtc_port"}
	cfg, err := src.Load()
	if err != nil || cfg.Media.WebRTCPort != 18189 {
		t.Fatalf("with the flag: %v", err)
	}
}

// TestADataDirectoryOthersCanReplace: up refuses a data directory another user could write to or replace, itself
// or a directory above it, but a sticky directory above it (as /tmp) is fine.
func TestADataDirectoryOthersCanReplace(t *testing.T) {
	t.Parallel()
	mk := func(t *testing.T, mode os.FileMode, path ...string) string {
		t.Helper()
		p := filepath.Join(path...)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	base := t.TempDir()
	if err := config.CheckPrivateDir(mk(t, 0o700, base, "ok")); err != nil {
		t.Fatalf("a private directory: %v", err)
	}
	if err := config.CheckPrivateDir(mk(t, 0o700, mk(t, 0o777|os.ModeSticky, base, "sticky"), "data")); err != nil {
		t.Fatalf("below a sticky directory: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "ok"), link); err != nil {
		t.Fatal(err)
	}
	if err := config.CheckPrivateDir(link); err != nil {
		t.Fatalf("a link to a private directory: %v", err)
	}
	for name, dir := range map[string]string{
		"writable by others":          mk(t, 0o777, base, "open"),
		"writable by its group":       mk(t, 0o770, base, "group"),
		"below a directory of others": mk(t, 0o700, mk(t, 0o777, base, "shared"), "data"),
		"sticky itself":               mk(t, 0o777|os.ModeSticky, base, "sticky-data"),
	} {
		if err := config.CheckPrivateDir(dir); err == nil || !strings.Contains(err.Error(), "--data-dir") {
			t.Errorf("%s: %v", name, err)
		}
	}
	shared := mk(t, 0o777, base, "shared2")
	target := mk(t, 0o700, base, "target")
	if err := os.Symlink(target, filepath.Join(shared, "data")); err != nil {
		t.Fatal(err)
	}
	if err := config.CheckPrivateDir(filepath.Join(shared, "data")); err == nil {
		t.Error("a link in a directory others can write to")
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
