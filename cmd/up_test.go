package cmd

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jniltinho/streamgo/internal/config"
	"github.com/jniltinho/streamgo/internal/mediaservers/mediatest"
	"github.com/jniltinho/streamgo/internal/tlsutil"
	"github.com/jniltinho/streamgo/internal/tlsutil/tlstest"
)

// The scenarios of "The up command", "Settings and flags", "HTTPS", "ffmpeg", "One instance", "First-start secrets"
// and "Supervision" that need up itself. A long-running up is this test binary re-executed as the CLI
// (STREAMGO_TEST_CLI=1), with fake media servers in PATH (mediatest) and a fake ffmpeg; the test stays alive to
// observe it. No secret is ever printed: the tests compare values, they do not log them.

func TestMain(m *testing.M) {
	mediatest.Main()
	if os.Getenv("STREAMGO_TEST_CLI") == "1" {
		os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// upEnv is what an up process runs with: fakes in PATH, a home that steers them, a fake ffmpeg.
type upEnv struct {
	home, bin, ffmpeg string
}

func newUpEnv(t *testing.T) upEnv {
	t.Helper()
	e := upEnv{home: t.TempDir(), bin: t.TempDir(), ffmpeg: fakeFFmpeg(t)}
	mediatest.Install(t, e.bin)
	return e
}

func (e upEnv) environ(extra ...string) []string {
	return append([]string{"STREAMGO_TEST_CLI=1", "PATH=" + e.bin + ":" + os.Getenv("PATH"), "HOME=" + e.home,
		"STREAMGO_OUTPUT_FFMPEG_PATH=" + e.ffmpeg}, extra...)
}

// setInProcess gives this process the environment of e, for an up run in-process.
func (e upEnv) setInProcess(t *testing.T) {
	t.Helper()
	for _, kv := range e.environ() {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
}

// mediaFlags are media ports free over TCP and UDP, so tests do not meet each other or the host's services.
func mediaFlags(t *testing.T) []string {
	t.Helper()
	p := mediatest.FreePorts(t, 3)
	return []string{"--livekit-tcp-port", strconv.Itoa(p[0]), "--livekit-udp-port", strconv.Itoa(p[1]), "--webrtc-port", strconv.Itoa(p[2])}
}

type upProc struct {
	cmd  *exec.Cmd
	logs *lockedBuffer
	done chan struct{}
	err  error
}

// startUp runs up in a process of its own (its own process group, as a shell would start it).
func startUp(t *testing.T, dir string, env []string, args ...string) *upProc {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := &upProc{logs: &lockedBuffer{}, done: make(chan struct{})}
	p.cmd = exec.Command(self, append([]string{"up"}, args...)...) //nolint:noctx // stopped by the test
	p.cmd.Dir, p.cmd.Env, p.cmd.Stdout, p.cmd.Stderr = dir, env, p.logs, p.logs
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		p.err = p.cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		_ = p.cmd.Process.Kill()
		<-p.done
		if t.Failed() {
			t.Logf("up's log:\n%.30000s", p.logs.String())
		}
	})
	return p
}

func (p *upProc) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// waitReady waits until GET base/readyz answers 200.
func (p *upProc) waitReady(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		if p.exited() {
			t.Fatalf("up exited: %v", p.err)
		}
		if code, body := get(client, base+"/readyz"); code == http.StatusOK {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatal("up did not become ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stop sends sig and returns the exit code.
func (p *upProc) stop(t *testing.T, sig syscall.Signal, within time.Duration) int {
	t.Helper()
	if err := p.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	case <-time.After(within):
		t.Fatalf("up did not exit within %v", within)
	}
	return p.cmd.ProcessState.ExitCode()
}

func get(client *http.Client, u string) (int, string) {
	resp, err := client.Get(u) //nolint:noctx // a local test request
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func childPids(home string) []int {
	var out []int
	for _, c := range []string{mediatest.LiveKit, mediatest.MediaMTX} {
		for _, r := range mediatest.Records(home, c) {
			out = append(out, r.Pid)
		}
	}
	return out
}

func processGone(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true
	}
	_, after, _ := strings.Cut(string(b), ") ")
	return strings.HasPrefix(after, "Z") // a zombie has exited
}

func waitGone(t *testing.T, pids []int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for _, pid := range pids {
		for !processGone(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("process %d is still there after %v", pid, within)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// snapshot is every file under dir with its size and modification time, but the database the running instance
// writes.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), "streamgo.db") {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		out[path] = fmt.Sprintf("%d %d %v", st.Size(), st.ModTime().UnixNano(), st.Mode())
		return nil
	})
	return out
}

func login(t *testing.T, client *http.Client, base, email, password string) int {
	t.Helper()
	code, _ := apiCall(t, client, base, http.MethodPost, "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	return code
}

// apiCall sends a JSON request from the public origin and returns the status and the body.
func apiCall(t *testing.T, client *http.Client, base, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body)) //nolint:noctx // a local test request
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// noSecretsInProcess clears the secrets from this process's environment, as for an operator's shell.
func noSecretsInProcess(t *testing.T) {
	t.Helper()
	for _, k := range []string{"STREAMGO_SECRET_KEY", "STREAMGO_LIVEKIT_API_KEY", "STREAMGO_LIVEKIT_API_SECRET",
		"STREAMGO_MEDIAMTX_USER", "STREAMGO_MEDIAMTX_SECRET", "STREAMGO_SERVER_DATA_DIR"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
}

// TestUpLifecycle follows one data directory through two starts of up, with the scenarios each step proves.
func TestUpLifecycle(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	work := t.TempDir()
	dataDir := filepath.Join(work, "data")
	// "A host with the internal ports taken": the usual internal ports are held while up runs
	for _, port := range []int{7880, 8889, 8554, 9997} {
		if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			defer func() { _ = l.Close() }()
		}
	}
	// "Managed media settings" and "An explicit false": a config.toml up overrides
	toml := "[server]\nself_signed = true\npublic_url = \"https://localhost:8443\"\n[livekit]\nurl = \"wss://lk.example.com\"\n" +
		"api_url = \"http://lk.example.com:7880\"\n[mediamtx]\nwhip_url = \"http://mtx.example.com:8889\"\n" +
		"rtsp_url = \"rtsp://mtx.example.com:8554\"\napi_url = \"http://mtx.example.com:9997\"\n"
	if err := os.WriteFile(filepath.Join(work, "config.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	given := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)) // a made-up key
	addr := freeAddr(t)
	base := "http://" + addr
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 5 * time.Second, Jar: jar}
	args := append([]string{"--addr", addr, "--public-url", base, "--self-signed=false"}, mediaFlags(t)...)
	const private = "STREAMGO_OUTPUT_ALLOW_PRIVATE_DESTINATIONS=true" // a destination on the loopback address
	first := startUp(t, work, e.environ("STREAMGO_SECRET_KEY="+given, private), args...)
	ready := first.waitReady(t, client, base)

	t.Run("First start in an empty directory", func(t *testing.T) {
		for _, want := range []string{`"database":"ok"`, `"livekit":"ok"`, `"mediamtx":"ok"`} {
			if !strings.Contains(ready, want) {
				t.Errorf("readyz %s lacks %s", ready, want)
			}
		}
		for _, f := range []string{"streamgo.db", "streamgo.env"} {
			if _, err := os.Stat(filepath.Join(dataDir, f)); err != nil {
				t.Error(err)
			}
		}
	})
	t.Run("No configuration file", func(t *testing.T) {
		// the config.toml here holds no secret: the rest are defaults, flags and streamgo.env
		if strings.Contains(first.logs.String(), "invalid configuration") {
			t.Fatal("the configuration did not load")
		}
	})
	t.Run("An explicit false", func(t *testing.T) {
		if code, _ := get(client, base+"/healthz"); code != http.StatusOK {
			t.Fatalf("plain HTTP: %d", code)
		}
	})
	t.Run("Managed media settings", func(t *testing.T) {
		if !strings.Contains(first.logs.String(), `"settings":"livekit.url, livekit.api_url, mediamtx.whip_url, mediamtx.rtsp_url, mediamtx.api_url"`) {
			t.Fatal("no log line names the replaced settings")
		}
	})
	t.Run("Plain build", func(t *testing.T) {
		for _, name := range []string{"livekit-server", "mediamtx"} {
			if !strings.Contains(first.logs.String(), `"path":"`+filepath.Join(e.bin, name)+`"`) {
				t.Errorf("the start line does not name %s", name)
			}
		}
	})
	t.Run("Other commands", func(t *testing.T) {
		t.Chdir(t.TempDir()) // another directory, no secret in the environment
		code, out, errOut := executeIn(t, "correct-horse-battery\n", "user", "add", "--data-dir", dataDir,
			"--email", "ana@example.com", "--name", "Ana", "--password-stdin")
		if code != 0 {
			t.Fatalf("user add: exit %d %q %q", code, out, errOut)
		}
		if code := login(t, client, base, "ana@example.com", "correct-horse-battery"); code != http.StatusOK {
			t.Fatalf("sign-in: %d", code)
		}
	})
	var destination string // encrypted with the key given on the first start
	t.Run("A destination is saved", func(t *testing.T) {
		code, body := apiCall(t, client, base, http.MethodPost, "/api/v1/destinations",
			`{"name":"Made up","address":"rtmp://127.0.0.1/live","key":"made-up-stream-key"}`)
		var d struct{ ID string }
		if code != http.StatusCreated || json.Unmarshal([]byte(body), &d) != nil || d.ID == "" {
			t.Fatalf("create: %d %s", code, body)
		}
		destination = d.ID
	})
	t.Run("What a child receives", func(t *testing.T) {
		lk := mediatest.Records(e.home, mediatest.LiveKit)[0]
		mtx := mediatest.Records(e.home, mediatest.MediaMTX)[0]
		if !strings.Contains(strings.Join(lk.Env, "\n"), "LIVEKIT_KEYS=") {
			t.Error("LiveKit has no LIVEKIT_KEYS")
		}
		for _, env := range append(lk.Env, mtx.Env...) {
			if strings.HasPrefix(env, "STREAMGO_SECRET_KEY=") || strings.HasPrefix(env, "STREAMGO_DATABASE_DSN=") {
				t.Errorf("a child has %s", strings.SplitN(env, "=", 2)[0])
			}
		}
	})
	t.Run("A second start", func(t *testing.T) {
		before := snapshot(t, dataDir)
		e.setInProcess(t)
		code, _, errOut := execute(t, append([]string{"up", "--data-dir", dataDir}, args...)...)
		if code != 1 || !strings.Contains(errOut, "another StreamGo instance is running") {
			t.Fatalf("exit %d, stderr %q", code, errOut)
		}
		after := snapshot(t, dataDir)
		if fmt.Sprint(before) != fmt.Sprint(after) {
			t.Errorf("files changed:\nbefore %v\nafter  %v", before, after)
		}
		if code, _ := get(client, base+"/readyz"); code != http.StatusOK {
			t.Errorf("the first is not ready: %d", code)
		}
	})
	t.Run("A media server that exits is started again", func(t *testing.T) {
		for _, c := range []string{mediatest.LiveKit, mediatest.MediaMTX} {
			records := mediatest.Records(e.home, c)
			if err := syscall.Kill(records[len(records)-1].Pid, syscall.SIGKILL); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			sawFailing := false
			for {
				code, body := get(client, base+"/readyz")
				sawFailing = sawFailing || (code == http.StatusServiceUnavailable && !strings.Contains(body, `"`+c+`":"ok"`))
				if sawFailing && code == http.StatusOK && len(mediatest.Records(e.home, c)) == len(records)+1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s: failing seen %v, last readyz %d %s", c, sawFailing, code, body)
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	})
	t.Run("Stop", func(t *testing.T) {
		pids := childPids(e.home)
		if code := first.stop(t, syscall.SIGTERM, 50*time.Second); code != 0 {
			t.Fatalf("exit %d", code)
		}
		waitGone(t, pids, 0)
		logs := first.logs.String()
		if s, m := strings.Index(logs, `"msg":"streamgo stopped"`), strings.Index(logs, `"msg":"media servers stopped"`); s < 0 || m < s {
			t.Fatalf("the server did not stop before the media servers (%d, %d)", s, m)
		}
	})

	envBefore, err := os.ReadFile(filepath.Join(dataDir, "streamgo.env"))
	if err != nil {
		t.Fatal(err)
	}
	// the second start: no key in the environment, another LiveKit secret there, the address from a flag over the
	// environment and MediaMTX's port from the environment
	webrtc := mediatest.FreePorts(t, 1)
	second := startUp(t, work, e.environ("STREAMGO_LIVEKIT_API_SECRET=from-the-environment-0123456789",
		"STREAMGO_SERVER_ADDR=127.0.0.1:1", "STREAMGO_MEDIA_WEBRTC_PORT="+strconv.Itoa(webrtc[0]), private),
		append([]string{"--addr", addr, "--public-url", base, "--self-signed=false"}, mediaFlags(t)[:4]...)...)
	second.waitReady(t, client, base)

	t.Run("Created once", func(t *testing.T) {
		envAfter, _ := os.ReadFile(filepath.Join(dataDir, "streamgo.env"))
		st, _ := os.Stat(filepath.Join(dataDir, "streamgo.env"))
		if !bytes.Equal(envBefore, envAfter) || st.Mode().Perm() != 0o600 || bytes.Count(envAfter, []byte("\n")) != 5 {
			t.Fatal("streamgo.env changed, or is not 0600 with five values")
		}
	})
	t.Run("A key given on the first start is the one kept", func(t *testing.T) {
		if !bytes.Contains(envBefore, []byte("STREAMGO_SECRET_KEY="+given+"\n")) {
			t.Fatal("the key given is not the one stored")
		}
		if code := login(t, client, base, "ana@example.com", "correct-horse-battery"); code != http.StatusOK {
			t.Fatalf("sign-in after the restart: %d", code)
		}
		// moving the destination opens its sealed address and stream key: only the key of the first start can
		if destination == "" {
			t.Fatal("no destination was saved")
		}
		code, body := apiCall(t, client, base, http.MethodPatch, "/api/v1/destinations/"+destination, `{"address":"rtmp://127.0.0.1/other"}`)
		if code != http.StatusOK || !strings.Contains(body, `"has_key":true`) {
			t.Fatalf("the stored stream key does not open after the restart: %d %s", code, body)
		}
	})
	t.Run("The environment wins", func(t *testing.T) {
		records := mediatest.Records(e.home, mediatest.LiveKit)
		if !strings.Contains(strings.Join(records[len(records)-1].Env, "\n"), `"streamgo":"from-the-environment-0123456789"`) {
			t.Fatal("LiveKit did not get the environment's secret")
		}
	})
	t.Run("A flag wins", func(t *testing.T) {
		conf, _ := os.ReadFile(filepath.Join(dataDir, "media", "mediamtx.yml"))
		if !strings.Contains(string(conf), fmt.Sprintf("webrtcLocalUDPAddress: :%d\n", webrtc[0])) {
			t.Fatal("MediaMTX's media port is not the environment's")
		}
		// ready on the flag's address (above), not the environment's
	})
	t.Run("The parent is killed", func(t *testing.T) {
		all := childPids(e.home) // the earlier ones are gone already
		if err := second.cmd.Process.Signal(syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		<-second.done
		waitGone(t, all, 5*time.Second)
	})
}

func TestUpAsRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	c := &cli{in: strings.NewReader(""), out: &out, errOut: &errOut, geteuid: func() int { return 0 }}
	if code := runCLI(c, []string{"up"}); code != 1 || !strings.Contains(errOut.String(), "unprivileged user") {
		t.Fatalf("exit %d, %q", code, errOut.String())
	}
	if entries, _ := os.ReadDir("."); len(entries) != 0 {
		t.Fatalf("created %v", entries)
	}
}

func TestUpAWrongValue(t *testing.T) {
	for _, tc := range []struct{ args, flag string }{
		{"--webrtc-port 70000", "--webrtc-port"},
		{"--livekit-tcp-port 0", "--livekit-tcp-port"},
		{"--node-ip not-an-address", "--node-ip"},
	} {
		code, _, errOut := execute(t, append([]string{"up"}, strings.Fields(tc.args)...)...)
		if code != 1 || !strings.Contains(errOut, tc.flag) {
			t.Errorf("%s: exit %d, %q", tc.args, code, errOut)
		}
		if entries, _ := os.ReadDir("."); len(entries) != 0 {
			t.Errorf("%s: created %v", tc.args, entries)
		}
	}
}

// failsBeforeAnyChild runs up in-process with the fakes and checks it exits 1 with want, no child started.
func failsBeforeAnyChild(t *testing.T, e upEnv, want string, args ...string) {
	t.Helper()
	code, _, errOut := execute(t, append([]string{"up", "--data-dir", t.TempDir()}, args...)...)
	if code != 1 || !strings.Contains(errOut, want) {
		t.Fatalf("exit %d, stderr %q, want %q", code, errOut, want)
	}
	if pids := childPids(e.home); len(pids) != 0 {
		t.Fatalf("a child started: %v", pids)
	}
}

func TestUpNoFFmpeg(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	e.setInProcess(t)
	t.Setenv("STREAMGO_OUTPUT_FFMPEG_PATH", filepath.Join(t.TempDir(), "ffmpeg"))
	failsBeforeAnyChild(t, e, "output.ffmpeg_path: cannot run", mediaFlags(t)...)
}

func TestUpHalfAPair(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	e.setInProcess(t)
	authority := tlstest.NewAuthority(t)
	cert, _ := authority.Issue(t, t.TempDir(), "localhost")
	https := []string{"--public-url", "https://localhost:8443"}
	failsBeforeAnyChild(t, e, "server.key_file", append(append([]string{"--cert-file", cert}, https...), mediaFlags(t)...)...)
	failsBeforeAnyChild(t, e, "--key-file", append(append([]string{"--cert-file", cert, "--key-file",
		filepath.Join(t.TempDir(), "absent.pem")}, https...), mediaFlags(t)...)...)
}

// TestUpACertificatePairFromTwoSources is the order of B8: --cert-file from a flag and server.key_file from
// streamgo.env make a pair, checked once the whole bootstrap is done (the start goes on to ffmpeg, which is absent).
func TestUpACertificatePairFromTwoSources(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	e.setInProcess(t)
	t.Setenv("STREAMGO_OUTPUT_FFMPEG_PATH", filepath.Join(t.TempDir(), "ffmpeg"))
	authority := tlstest.NewAuthority(t)
	cert, key := authority.Issue(t, t.TempDir(), "localhost")
	dataDir := t.TempDir()
	if _, err := config.EnsureEnvFile(dataDir, func(string) (string, bool) { return "", false }, nil); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(config.EnvFilePath(dataDir), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(f, "STREAMGO_SERVER_KEY_FILE=%s\n", key)
	_ = f.Close()
	code, _, errOut := execute(t, append([]string{"up", "--data-dir", dataDir, "--cert-file", cert,
		"--public-url", "https://localhost:8443"}, mediaFlags(t)...)...)
	if code != 1 || !strings.Contains(errOut, "output.ffmpeg_path: cannot run") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

// TestUpAFlagOverAMalformedEnvironment is "A flag wins" when the environment's value of the same setting is not a
// number: the start goes on with the flag's (to ffmpeg, which is absent).
func TestUpAFlagOverAMalformedEnvironment(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	e.setInProcess(t)
	t.Setenv("STREAMGO_OUTPUT_FFMPEG_PATH", filepath.Join(t.TempDir(), "ffmpeg"))
	t.Setenv("STREAMGO_MEDIA_WEBRTC_PORT", "invalid")
	failsBeforeAnyChild(t, e, "output.ffmpeg_path: cannot run", mediaFlags(t)...)
}

// TestUpADataDirectoryOthersCanWrite: up runs programs from its data directory, so it refuses one another user could
// write to, before it stores a secret there.
func TestUpADataDirectoryOthersCanWrite(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	e.setInProcess(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dataDir, 0o777); err != nil { //nolint:gosec // the case under test
		t.Fatal(err)
	}
	code, _, errOut := execute(t, append([]string{"up", "--data-dir", dataDir}, mediaFlags(t)...)...)
	if code != 1 || !strings.Contains(errOut, "--data-dir: other users can write to "+dataDir) {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(config.EnvFilePath(dataDir)); err == nil {
		t.Fatal("a secret was stored there")
	}
}

func TestUpAMediaPortInUse(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	e.setInProcess(t)
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	u, err := net.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = u.Close() }()
	flags := mediaFlags(t)
	failsBeforeAnyChild(t, e, "--livekit-tcp-port", append(flags, "--livekit-tcp-port", strconv.Itoa(l.Addr().(*net.TCPAddr).Port))...)
	failsBeforeAnyChild(t, e, "--webrtc-port", append(flags, "--webrtc-port", strconv.Itoa(u.LocalAddr().(*net.UDPAddr).Port))...)
}

// TestUpAFailureAfterAMediaServerStarted: the StreamGo server cannot listen; up exits 1 and its children are gone.
func TestUpAFailureAfterAMediaServerStarted(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	e.setInProcess(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	addr := l.Addr().String()
	code, _, errOut := execute(t, append([]string{"up", "--data-dir", t.TempDir(), "--addr", addr,
		"--public-url", "http://" + addr}, mediaFlags(t)...)...)
	if code != 1 || !strings.Contains(errOut, "address already in use") {
		t.Fatalf("exit %d, stderr %.3000s", code, errOut)
	}
	pids := childPids(e.home)
	if len(pids) != 2 {
		t.Fatalf("children %v", pids)
	}
	waitGone(t, pids, 15*time.Second)
}

// TestUpHTTPS is "Self-signed" (twice, the same certificate, signalling through /livekit/ on that origin) and "A
// certificate of the operator".
func TestUpHTTPS(t *testing.T) {
	noSecretsInProcess(t)
	e := newUpEnv(t)
	work := t.TempDir()
	served := func(t *testing.T, pool *x509.CertPool, args ...string) (*x509.Certificate, string) {
		t.Helper()
		addr := freeAddr(t)
		_, port, _ := net.SplitHostPort(addr)
		base := "https://localhost:" + port
		client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs: pool, InsecureSkipVerify: pool == nil, MinVersion: tls.VersionTLS12, //nolint:gosec // the self-signed one is compared below
		}}}
		p := startUp(t, work, e.environ(), append(append([]string{"--addr", addr, "--public-url", base}, args...), mediaFlags(t)...)...)
		p.waitReady(t, client, base)
		resp, err := client.Get(base + "/healthz") //nolint:noctx // a local test request
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		code, _ := get(client, base+"/livekit/rtc/validate")
		if c := p.stop(t, syscall.SIGTERM, 50*time.Second); c != 0 {
			t.Fatalf("exit %d", c)
		}
		return resp.TLS.PeerCertificates[0], strconv.Itoa(code)
	}
	t.Run("Self-signed", func(t *testing.T) {
		first, proxied := served(t, nil, "--self-signed")
		second, _ := served(t, nil, "--self-signed")
		if tlsutil.Fingerprint(first) != tlsutil.Fingerprint(second) {
			t.Error("another certificate on the second start")
		}
		if proxied != "200" {
			t.Errorf("/livekit/ did not reach LiveKit: %s", proxied)
		}
	})
	t.Run("A certificate of the operator", func(t *testing.T) {
		authority := tlstest.NewAuthority(t)
		cert, key := authority.Issue(t, t.TempDir(), "localhost")
		got, _ := served(t, authority.Pool(), "--cert-file", cert, "--key-file", key)
		pem, _ := os.ReadFile(cert) //nolint:gosec // a test file
		if !strings.Contains(string(pem), base64.StdEncoding.EncodeToString(got.Raw)[:60]) {
			t.Error("not the operator's certificate")
		}
	})
}

func TestUpFlagsHelp(t *testing.T) {
	code, out, _ := execute(t, "up", "--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		`--data-dir string`, `(default "./data")`, `--addr string`, `(default ":8080")`, `--public-url string`,
		`(default "http://localhost:8080")`, `--node-ip string`, `default: the IPv4 address of the default route, else 127.0.0.1`,
		`--livekit-tcp-port int`, `(default 7881)`, `--livekit-udp-port int`, `(default 7882)`, `--webrtc-port int`,
		`(default 8189)`, `--self-signed`, `--cert-file string`, `--key-file string`, `--no-migrate`, `(default false)`, `(default none)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("up --help lacks %q", want)
		}
	}
}

// TestCommandsThatNeedNoConfiguration: version, licenses and up --help in a directory with a broken config.toml and
// no secret.
func TestCommandsThatNeedNoConfiguration(t *testing.T) {
	noSecretsInProcess(t)
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.toml", []byte("[[[ not toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	notices := fstest.MapFS{"LICENSE": {Data: []byte("MIT License")}, "third_party/livekit/NOTICE": {Data: []byte("LiveKit")}}
	for _, args := range [][]string{{"version"}, {"licenses"}, {"up", "--help"}} {
		var out, errOut bytes.Buffer
		c := &cli{in: strings.NewReader(""), out: &out, errOut: &errOut, notices: notices}
		if code := runCLI(c, args); code != 0 {
			t.Errorf("%v: exit %d %q", args, code, errOut.String())
		}
		if args[0] == "licenses" && (!strings.Contains(out.String(), "==== LICENSE ====\n\nMIT License") ||
			!strings.Contains(out.String(), "==== third_party/livekit/NOTICE ====")) {
			t.Errorf("licenses printed %q", out.String())
		}
	}
}

// TestVersionDoesNotUnpack is the half of "Commands that do not unpack" a test can see: version writes nothing.
func TestVersionDoesNotUnpack(t *testing.T) {
	began := time.Now()
	code, _, _ := execute(t, "version")
	if entries, _ := os.ReadDir("."); code != 0 || len(entries) != 0 || time.Since(began) > time.Second {
		t.Fatalf("exit %d, wrote %v, took %v", code, entries, time.Since(began))
	}
}

func TestDataDirFlag(t *testing.T) {
	dir := validEnv(t)
	other := t.TempDir()
	t.Setenv("STREAMGO_SERVER_DATA_DIR", dir)
	if code, _, errOut := execute(t, "migrate", "up", "--data-dir", other); code != 0 {
		t.Fatalf("exit %d %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(other, "streamgo.db")); err != nil {
		t.Fatal("--data-dir did not win:", err)
	}
}
