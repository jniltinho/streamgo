package config_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/jniltinho/streamgo/internal/config"
)

// validKey is a well-formed secret key (32 zero bytes): never a real one.
var validKey = base64.StdEncoding.EncodeToString(make([]byte, 32))

// load runs LoadWith over an in-memory file (empty: none) and environment; the secret key is
// set unless env says otherwise.
func load(t *testing.T, file string, env map[string]string) (*config.Config, error) {
	t.Helper()
	full := map[string]string{"STREAMGO_SECRET_KEY": validKey}
	for k, v := range env {
		full[k] = v
	}
	lookup := func(k string) (string, bool) { v, ok := full[k]; return v, ok }
	read := func(p string) ([]byte, error) {
		if file == "" || p != "config.toml" {
			return nil, fs.ErrNotExist
		}
		return []byte(file), nil
	}
	return config.LoadWith("", lookup, read)
}

func TestEnvironmentWins(t *testing.T) {
	t.Parallel()
	cfg, err := load(t, "[server]\naddr = \":8080\"\n", map[string]string{"STREAMGO_SERVER_ADDR": ":9090"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Fatalf("addr %q, want :9090", cfg.Server.Addr)
	}
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := load(t, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s, d, o := cfg.Server, cfg.Database, cfg.Output
	checks := []struct {
		name string
		ok   bool
	}{
		{"server", s.Addr == ":8080" && s.PublicURL == "http://localhost:8080" && s.DataDir == "./data" && len(s.TrustedProxies) == 0 &&
			s.CertFile == "" && s.KeyFile == "" && !s.SelfSigned},
		{"database", d.Driver == "sqlite" && cfg.SQLitePath() == filepath.Join("data", "streamgo.db")},
		{"livekit", cfg.LiveKit.URL != "" && cfg.LiveKit.APIURL != ""},
		{"mediamtx", cfg.MediaMTX.WHIPURL != "" && cfg.MediaMTX.RTSPURL != ""},
		{"studio", cfg.Studio.MaxParticipants == 10 && cfg.Studio.MaxOnStage == 6},
		{"output", o.FFmpegPath == "ffmpeg" && o.Resolution == "720p" && o.VideoBitrateKbps > 0 && o.AudioBitrateKbps > 0 &&
			o.MaxDestinations == 5 && o.MaxConcurrent == 2 && o.StartTimeout.Std() == 30*time.Second &&
			o.ReconnectGrace.Std() == 120*time.Second && !o.AllowPrivateDestinations},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s defaults: %+v", c.name, cfg)
		}
	}
}

func TestInvalid(t *testing.T) {
	t.Parallel()
	const dsn = "streamgo:hunter2-dsn@tcp(db:3306)/streamgo"
	tests := []struct {
		name  string
		file  string
		env   map[string]string
		want  []string // every one in the error
		never string   // never in the error
	}{
		{name: "unknown key", file: "[server]\nlisten = \":1\"\n", want: []string{"unknown settings", "listen"}},
		{name: "unknown section", file: "[redis]\naddr = \"x\"\n", want: []string{"redis"}},
		{name: "secret in the file", file: "[livekit]\napi_secret = \"s3cr3t-in-file\"\n",
			want: []string{"livekit.api_secret", "STREAMGO_LIVEKIT_API_SECRET", "environment"}, never: "s3cr3t-in-file"},
		{name: "secret key in the file", file: "[secret]\nkey = \"" + validKey + "\"\n", want: []string{"STREAMGO_SECRET_KEY"}},
		{name: "no stage", env: map[string]string{"STREAMGO_STUDIO_MAX_ON_STAGE": "0"}, want: []string{"studio.max_on_stage", "between 1 and 6"}},
		{name: "stage too large", file: "[studio]\nmax_on_stage = 7\n", want: []string{"studio.max_on_stage", "between 1 and 6"}},
		{name: "resolution", env: map[string]string{"STREAMGO_OUTPUT_RESOLUTION": "4k"}, want: []string{"output.resolution", `"720p"`, `"1080p"`}},
		{name: "grace", env: map[string]string{"STREAMGO_OUTPUT_RECONNECT_GRACE": "1s"}, want: []string{"output.reconnect_grace"}},
		{name: "not a number", env: map[string]string{"STREAMGO_OUTPUT_MAX_CONCURRENT": "two"}, want: []string{"STREAMGO_OUTPUT_MAX_CONCURRENT", "not an integer"}},
		{name: "missing secret key", env: map[string]string{"STREAMGO_SECRET_KEY": ""}, want: []string{"STREAMGO_SECRET_KEY is required"}},
		{name: "short secret key", env: map[string]string{"STREAMGO_SECRET_KEY": "c2hvcnQta2V5"},
			want: []string{"STREAMGO_SECRET_KEY must be 32 bytes"}, never: "c2hvcnQta2V5"},
		{name: "plain http on a public host", env: map[string]string{"STREAMGO_SERVER_PUBLIC_URL": "http://studio.example.com"},
			want: []string{"server.public_url", "browsers need HTTPS for camera and microphone"}},
		{name: "relative public url", env: map[string]string{"STREAMGO_SERVER_PUBLIC_URL": "/studio"}, want: []string{"server.public_url"}},
		{name: "unknown driver", env: map[string]string{"STREAMGO_DATABASE_DRIVER": "oracle"},
			want: []string{"database.driver", `"sqlite"`, `"mariadb"`, `"postgres"`}},
		{name: "mariadb without dsn", env: map[string]string{"STREAMGO_DATABASE_DRIVER": "mariadb"}, want: []string{"STREAMGO_DATABASE_DSN is required"}},
		{name: "postgres without dsn", env: map[string]string{"STREAMGO_DATABASE_DRIVER": "postgres"}, want: []string{"STREAMGO_DATABASE_DSN is required"}},
		{name: "dsn in the file", file: "[database]\ndsn = \"" + dsn + "\"\n", want: []string{"STREAMGO_DATABASE_DSN"}, never: "hunter2"},
		{name: "trusted proxy", env: map[string]string{"STREAMGO_SERVER_TRUSTED_PROXIES": "10.0.0.0/8, nope"}, want: []string{"server.trusted_proxies", "nope"}},
		{name: "livekit url", env: map[string]string{"STREAMGO_LIVEKIT_URL": "http://lk"}, want: []string{"livekit.url"}},
		{name: "self-signed on http", env: map[string]string{"STREAMGO_SERVER_SELF_SIGNED": "true"},
			want: []string{"server.public_url", "must be https://"}},
		{name: "certificate on http", env: map[string]string{"STREAMGO_SERVER_CERT_FILE": "/c.pem", "STREAMGO_SERVER_KEY_FILE": "/k.pem"},
			want: []string{"server.public_url", "must be https://"}},
		{name: "certificate without key", env: map[string]string{"STREAMGO_SERVER_CERT_FILE": "/c.pem", "STREAMGO_SERVER_PUBLIC_URL": "https://s.example.com"},
			want: []string{"server.cert_file", "server.key_file"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := load(t, tt.file, tt.env)
			if err == nil {
				t.Fatal("loaded, want an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
			if tt.never != "" && strings.Contains(err.Error(), tt.never) {
				t.Errorf("error %q prints %q", err, tt.never)
			}
		})
	}
}

func TestValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"local development", map[string]string{"STREAMGO_SERVER_PUBLIC_URL": "http://localhost:8080"}},
		{"loopback v4", map[string]string{"STREAMGO_SERVER_PUBLIC_URL": "http://127.0.0.1:18080"}},
		{"loopback v6", map[string]string{"STREAMGO_SERVER_PUBLIC_URL": "http://[::1]:8080"}},
		{"https", map[string]string{"STREAMGO_SERVER_PUBLIC_URL": "https://studio.example.com"}},
		{"mariadb with dsn", map[string]string{"STREAMGO_DATABASE_DRIVER": "mariadb", "STREAMGO_DATABASE_DSN": "u:p@tcp(db)/s"}},
		{"bounds", map[string]string{"STREAMGO_STUDIO_MAX_ON_STAGE": "1", "STREAMGO_OUTPUT_RESOLUTION": "1080p"}},
		{"proxies", map[string]string{"STREAMGO_SERVER_TRUSTED_PROXIES": "10.0.0.0/8,::1"}},
		{"self-signed", map[string]string{"STREAMGO_SERVER_SELF_SIGNED": "true", "STREAMGO_SERVER_PUBLIC_URL": "https://localhost:8443"}},
		{"livekit through the server", map[string]string{"STREAMGO_LIVEKIT_URL": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := load(t, "", tt.env); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestExampleComplete loads config.toml.example and checks it names every setting: each
// non-secret key set in the file, each secret's environment variable in a comment.
func TestExampleComplete(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadWith("../../config.toml.example",
		func(k string) (string, bool) { return validKey, k == "STREAMGO_SECRET_KEY" }, os.ReadFile); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, s := range config.Settings() {
		if s.Secret {
			if !strings.Contains(string(data), s.Env) {
				t.Errorf("the example does not mention %s", s.Env)
			}
			continue
		}
		section, key, _ := strings.Cut(s.Key, ".")
		if m, ok := doc[section].(map[string]any); !ok || m[key] == nil {
			t.Errorf("the example does not set %s", s.Key)
		}
	}
}

func TestDataDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "new")
	if err := config.CheckDataDir(dir); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	ro := t.TempDir()
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	if err := config.CheckDataDir(ro); err == nil || !strings.Contains(err.Error(), "server.data_dir") {
		t.Fatalf("read-only data_dir: %v", err)
	}
}

// fakeListings are ffmpeg listings with everything the output needs.
var fakeListings = map[string]string{
	"-encoders":  "Encoders:\n V..... = Video\n ------\n V....D libx264              libx264 H.264\n A....D aac                  AAC\n",
	"-decoders":  "Decoders:\n ------\n V....D h264  H.264\n V....D vp8  On2 VP8\n A....D opus  Opus\n",
	"-demuxers":  "Formats:\n D. = Demuxing supported\n ---\n D  rtsp            RTSP input\n D  mov,mp4,m4a     QuickTime\n",
	"-muxers":    "Formats:\n .E = Muxing supported\n ---\n  E rtsp            RTSP output\n  E flv             FLV\n",
	"-protocols": "Supported file protocols:\nInput:\n  rtmp\n  rtmps\n  rtsp\nOutput:\n  file\n  rtmp\n  rtmps\n",
}

// fakeFFmpeg writes a script that prints the listing named by its second argument, with
// drop removed from it.
func fakeFFmpeg(t *testing.T, drop string) string {
	t.Helper()
	dir := t.TempDir()
	for flag, text := range fakeListings {
		if drop != "" {
			text = strings.ReplaceAll(text, "  "+drop+"\n", "\n")
			text = strings.ReplaceAll(text, " "+drop+" ", " x ")
		}
		if err := os.WriteFile(filepath.Join(dir, "list"+flag), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nexec cat \"" + dir + "/list$2\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	return path
}

// TestCheckFFmpeg is not parallel, nor are its cases: each writes a script and runs it, and a fork from
// another goroutine in between would hold the script open for writing ("text file busy"). Sequential tests
// finish before the parallel ones resume, so nothing else in the package forks meanwhile.
func TestCheckFFmpeg(t *testing.T) {
	tests := []struct {
		name, drop, want string
	}{
		{"complete", "", ""},
		{"no libx264", "libx264", "the libx264 encoder"},
		{"no rtmps", "rtmps", "the rtmps output protocol"},
		{"no flv", "flv", "the flv muxer"},
		{"no vp8", "vp8", "the vp8 decoder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := config.CheckFFmpeg(context.Background(), fakeFFmpeg(t, tt.drop))
			switch {
			case tt.want == "" && err != nil:
				t.Fatal(err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("error %v, want %q", err, tt.want)
			}
		})
	}
	if err := config.CheckFFmpeg(context.Background(), filepath.Join(t.TempDir(), "none")); err == nil {
		t.Fatal("a missing ffmpeg passed")
	}
}

// TestStockFFmpeg checks the ffmpeg on PATH (the tools image installs Debian's): a stock build passes.
func TestStockFFmpeg(t *testing.T) {
	t.Parallel()
	path, err := exec.LookPath("ffmpeg")
	if errors.Is(err, exec.ErrNotFound) {
		t.Skip("no ffmpeg on PATH")
	}
	if err := config.CheckFFmpeg(context.Background(), path); err != nil {
		t.Fatal(err)
	}
}

func TestTLSFilesAndLiveKitURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		env               map[string]string
		wantCert, wantKey string
		wantLiveKit       string
	}{
		{name: "plain http", env: map[string]string{"STREAMGO_LIVEKIT_URL": ""}, wantLiveKit: "ws://localhost:8080/livekit"},
		{name: "self-signed", env: map[string]string{
			"STREAMGO_SERVER_SELF_SIGNED": "true", "STREAMGO_SERVER_PUBLIC_URL": "https://studio.example.com:8443",
			"STREAMGO_SERVER_DATA_DIR": "/var/lib/streamgo", "STREAMGO_LIVEKIT_URL": "",
		}, wantCert: "/var/lib/streamgo/tls/selfsigned.crt", wantKey: "/var/lib/streamgo/tls/selfsigned.key",
			wantLiveKit: "wss://studio.example.com:8443/livekit"},
		{name: "files win over self-signed", env: map[string]string{
			"STREAMGO_SERVER_SELF_SIGNED": "true", "STREAMGO_SERVER_PUBLIC_URL": "https://studio.example.com",
			"STREAMGO_SERVER_CERT_FILE": "/etc/streamgo/fullchain.pem", "STREAMGO_SERVER_KEY_FILE": "/etc/streamgo/privkey.pem",
			"STREAMGO_LIVEKIT_URL": "wss://lk.example.com",
		}, wantCert: "/etc/streamgo/fullchain.pem", wantKey: "/etc/streamgo/privkey.pem", wantLiveKit: "wss://lk.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := load(t, "", tt.env)
			if err != nil {
				t.Fatal(err)
			}
			if cert, key := cfg.TLSFiles(); cert != tt.wantCert || key != tt.wantKey {
				t.Errorf("TLSFiles() = %q, %q; want %q, %q", cert, key, tt.wantCert, tt.wantKey)
			}
			if got := cfg.BrowserLiveKitURL(); got != tt.wantLiveKit {
				t.Errorf("BrowserLiveKitURL() = %q, want %q", got, tt.wantLiveKit)
			}
		})
	}
}
