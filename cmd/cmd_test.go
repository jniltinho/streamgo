package cmd

// Portions adapted from MeetHub (MIT): cmd/cmd_test.go

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

// execute runs the CLI in-process from an empty directory (no config.toml).
func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

// validEnv sets what a valid configuration needs: a secret key, a data directory and an
// ffmpeg offering every capability; it returns the data directory.
func validEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("STREAMGO_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("STREAMGO_SERVER_DATA_DIR", dir)
	t.Setenv("STREAMGO_OUTPUT_FFMPEG_PATH", fakeFFmpeg(t))
	return dir
}

// fakeFFmpeg writes a script whose output, for any listing, holds every name the output needs.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\nprintf ' ------\\n V..... libx264 x\\n A..... aac x\\n V..... h264 x\\n V..... vp8 x\\n" +
		" A..... opus x\\n DE rtsp x\\n E flv x\\nOutput:\\n  rtmp\\n  rtmps\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	return path
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestHelpListsCommands(t *testing.T) {
	code, out, _ := execute(t, "--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, c := range []string{"healthcheck", "migrate", "server", "version", "--config", "--json"} {
		if !strings.Contains(out, c) {
			t.Errorf("help does not list %q:\n%s", c, out)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	code, out, errOut := execute(t, "nope")
	if code != 1 || !strings.HasPrefix(errOut, "error: ") || !strings.Contains(errOut, `unknown command "nope"`) || out != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestVersionWithoutConfig(t *testing.T) {
	t.Setenv("STREAMGO_SERVER_ADDR", "invalid") // would fail validation if version loaded the config
	code, out, errOut := execute(t, "version")
	if code != 0 || out != "streamgo dev (commit none, built unknown)\n" {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
	code, out, _ = execute(t, "version", "--json")
	var v map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || len(v) != 3 || v["version"] != "dev" || v["commit"] != "none" || v["date"] != "unknown" {
		t.Fatalf("json version: exit %d %q", code, out)
	}
}

func TestHealthcheck(t *testing.T) {
	validEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1") // must be ignored
	t.Setenv("STREAMGO_SERVER_ADDR", ":"+port)   // wildcard → 127.0.0.1
	if code, out, errOut := execute(t, "healthcheck"); code != 0 {
		t.Fatalf("server up: exit %d %q %q", code, out, errOut)
	}
	srv.Close()
	if code, _, _ := execute(t, "healthcheck"); code != 1 {
		t.Fatalf("server not running: exit %d, want 1", code)
	}
}

// TestMigrateIdempotent is the server-cli "Idempotent" scenario.
func TestMigrateIdempotent(t *testing.T) {
	validEnv(t)
	if code, out, errOut := execute(t, "migrate", "up"); code != 0 || !strings.HasPrefix(out, "applied ") {
		t.Fatalf("first: exit %d %q %q", code, out, errOut)
	}
	if code, out, errOut := execute(t, "migrate", "up"); code != 0 || !strings.HasPrefix(out, "nothing pending") {
		t.Fatalf("second: exit %d %q %q", code, out, errOut)
	}
	code, out, _ := execute(t, "migrate", "status", "--json")
	var st []store.MigrationStatus
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || len(st) == 0 {
		t.Fatalf("status: exit %d %q", code, out)
	}
	for _, s := range st {
		if !s.Applied {
			t.Errorf("%s not applied", s.Name)
		}
	}
}

// TestServerInvalidConfiguration is "Invalid configuration": exit 1 naming the setting, not listening.
func TestServerInvalidConfiguration(t *testing.T) {
	validEnv(t)
	addr := freeAddr(t)
	t.Setenv("STREAMGO_SERVER_ADDR", addr)
	t.Setenv("STREAMGO_STUDIO_MAX_ON_STAGE", "0")
	code, _, errOut := execute(t, "server")
	if code != 1 || !strings.Contains(errOut, "studio.max_on_stage") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if conn, err := net.Dial("tcp", addr); err == nil {
		_ = conn.Close()
		t.Fatal("something listens")
	}
}

// TestServerSecondInstance is "Second server on the same data directory".
func TestServerSecondInstance(t *testing.T) {
	dir := validEnv(t)
	first, err := store.LockInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	code, _, errOut := execute(t, "server")
	if code != 1 || !strings.Contains(errOut, "another StreamGo instance is running") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

// TestServerRuns starts the server on the address from the environment, checks its probes,
// the API's JSON 404 and the web app, and stops it as SIGTERM would, on each database: SQLite
// with no database setting (a file under data_dir), MariaDB and PostgreSQL with a DSN.
func TestServerRuns(t *testing.T) {
	for _, driver := range storetest.Drivers() {
		t.Run(driver, func(t *testing.T) { serverRuns(t, driver) })
	}
}

func serverRuns(t *testing.T, driver string) {
	dataDir := validEnv(t)
	if driver != store.SQLite {
		t.Setenv("STREAMGO_DATABASE_DRIVER", driver)
		t.Setenv("STREAMGO_DATABASE_DSN", storetest.NewSettings(t, driver).DSN)
	}
	t.Chdir(t.TempDir())
	addr := freeAddr(t)
	t.Setenv("STREAMGO_SERVER_ADDR", addr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var logs bytes.Buffer
	c := &cli{in: strings.NewReader(""), out: io.Discard, errOut: &logs}
	done := make(chan error, 1)
	go func() { done <- c.runServer(ctx, false, nil) }()

	get := func(path string) (int, string) {
		resp, err := http.Get("http://" + addr + path) //nolint:noctx // a local test request
		if err != nil {
			return 0, err.Error()
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if code, _ := get("/healthz"); code == http.StatusOK {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("server exited: %v\n%s", err, logs.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("not listening on %s", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code, body := get("/readyz"); code != http.StatusOK || !strings.Contains(body, `"database":"ok"`) {
		t.Errorf("readyz: %d %s", code, body)
	}
	if code, body := get("/api/v1/nope"); code != http.StatusNotFound || !strings.Contains(body, `"not_found"`) {
		t.Errorf("unknown API route: %d %s", code, body)
	}
	if code, body := get("/broadcasts"); code != http.StatusOK || !strings.Contains(body, "<html") {
		t.Errorf("client route: %d %s", code, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the server did not stop within 15 s")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "streamgo.db")); (err == nil) != (driver == store.SQLite) {
		t.Fatalf("SQLite file under data_dir: %v", err)
	}
}
