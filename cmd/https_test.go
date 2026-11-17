package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/tlsutil"
	"github.com/jniltinho/streamgo/internal/tlsutil/tlstest"
)

// fakeLiveKit answers LiveKit's ListRooms (readiness, reconciler) with no rooms.
func fakeLiveKit(t *testing.T) {
	t.Helper()
	lk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/protobuf") // an empty body is an empty message
	}))
	t.Cleanup(lk.Close)
	t.Setenv("STREAMGO_LIVEKIT_API_URL", lk.URL)
	t.Setenv("STREAMGO_LIVEKIT_API_KEY", "test-key")
	t.Setenv("STREAMGO_LIVEKIT_API_SECRET", "test-secret-test-secret-test-secret")
}

// runHTTPS starts the server, waits until it answers HTTPS as host (trusting only roots), and returns the
// certificate it serves and its log; the server stops when the test ends.
func runHTTPS(t *testing.T, addr, host string, roots func() *x509.CertPool) (*x509.Certificate, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	c := &cli{in: strings.NewReader(""), out: &bytes.Buffer{}, errOut: &logs}
	done := make(chan error, 1)
	go func() { done <- c.runServer(ctx, false, nil) }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("the server did not stop within 15 s")
		}
	}
	defer stop()
	deadline := time.Now().Add(30 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("server exited: %v\n%s", err, logs.String())
		default:
		}
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not listening on %s", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots(), ServerName: host, MinVersion: tls.VersionTLS12,
	}}}
	defer client.CloseIdleConnections()
	resp, err := client.Get("https://" + addr + "/healthz") //nolint:noctx // a local test request
	if err != nil {
		t.Fatalf("https as %s: %v\n%s", host, err, logs.String())
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz over https: %d", resp.StatusCode)
	}
	// the healthcheck command reaches it on the loopback address
	var out, errOut bytes.Buffer
	if code := run([]string{"healthcheck"}, strings.NewReader(""), &out, &errOut); code != 0 || out.String() != "ready\n" {
		t.Errorf("healthcheck over https: exit %d %q %q", code, out.String(), errOut.String())
	}
	served := resp.TLS.PeerCertificates[0]
	stop()
	return served, logs.String()
}

// loggedFingerprint is the sha256 the "https certificate" log line names; it also checks no key is logged.
func loggedFingerprint(t *testing.T, logs string) string {
	t.Helper()
	if strings.Contains(logs, "PRIVATE KEY") {
		t.Fatal("the log holds a private key")
	}
	for line := range strings.Lines(logs) {
		var entry struct{ Msg, Subject, Sha256 string }
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Msg == "https certificate" && entry.Subject != "" {
			return entry.Sha256
		}
	}
	t.Fatalf("no https certificate line in\n%s", logs)
	return ""
}

func filePool(t *testing.T, file string) func() *x509.CertPool {
	return func() *x509.CertPool {
		b, err := os.ReadFile(file) //nolint:gosec // a test file
		if err != nil {
			t.Fatal(err)
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(b)
		return pool
	}
}

// TestSelfSignedServer is the standalone-https "First start", "Reused" and "Host changed" scenarios, through
// the server command, with the healthcheck command over HTTPS.
func TestSelfSignedServer(t *testing.T) {
	dataDir := validEnv(t)
	fakeLiveKit(t)
	t.Chdir(t.TempDir())
	addr := freeAddr(t)
	t.Setenv("STREAMGO_SERVER_ADDR", addr)
	t.Setenv("STREAMGO_SERVER_SELF_SIGNED", "true")
	t.Setenv("STREAMGO_SERVER_PUBLIC_URL", "https://studio.example.com:8443")
	cert := filepath.Join(dataDir, "tls", "selfsigned.crt")

	first, logs := runHTTPS(t, addr, "studio.example.com", filePool(t, cert))
	if got := loggedFingerprint(t, logs); got != tlsutil.Fingerprint(first) {
		t.Errorf("logged fingerprint %s, served %s", got, tlsutil.Fingerprint(first))
	}
	if info, err := os.Stat(filepath.Join(dataDir, "tls", "selfsigned.key")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("key mode: %v %v", info, err)
	}
	for _, name := range []string{"localhost", "127.0.0.1", "::1"} {
		if err := first.VerifyHostname(name); err != nil {
			t.Errorf("the certificate does not cover %s: %v", name, err)
		}
	}

	again, _ := runHTTPS(t, addr, "studio.example.com", filePool(t, cert))
	if !again.Equal(first) {
		t.Error("restart: another certificate is served")
	}

	t.Setenv("STREAMGO_SERVER_PUBLIC_URL", "https://other.example.org")
	moved, logs := runHTTPS(t, addr, "other.example.org", filePool(t, cert))
	if moved.Equal(first) || loggedFingerprint(t, logs) != tlsutil.Fingerprint(moved) {
		t.Error("host changed: the certificate was not replaced, or the log names another one")
	}
}

// TestCertificateFromFiles is "Certificate from files" (winning over self_signed) and "Unusable certificate".
func TestCertificateFromFiles(t *testing.T) {
	dataDir := validEnv(t)
	fakeLiveKit(t)
	t.Chdir(t.TempDir())
	addr := freeAddr(t)
	authority := tlstest.NewAuthority(t)
	certFile, keyFile := authority.Issue(t, t.TempDir(), "studio.example.com")
	t.Setenv("STREAMGO_SERVER_ADDR", addr)
	t.Setenv("STREAMGO_SERVER_SELF_SIGNED", "true")
	t.Setenv("STREAMGO_SERVER_PUBLIC_URL", "https://studio.example.com")
	t.Setenv("STREAMGO_SERVER_CERT_FILE", certFile)
	t.Setenv("STREAMGO_SERVER_KEY_FILE", keyFile)

	served, logs := runHTTPS(t, addr, "studio.example.com", authority.Pool)
	if got := loggedFingerprint(t, logs); got != tlsutil.Fingerprint(served) {
		t.Errorf("logged fingerprint %s, served %s", got, tlsutil.Fingerprint(served))
	}
	if _, err := os.Stat(filepath.Join(dataDir, "tls")); !os.IsNotExist(err) {
		t.Errorf("files win over self_signed, yet <data_dir>/tls exists: %v", err)
	}

	otherCert, otherKey := authority.Issue(t, t.TempDir(), "studio.example.com")
	for name, env := range map[string][2]string{
		"mismatch":     {certFile, otherKey},
		"missing key":  {otherCert, filepath.Join(t.TempDir(), "absent.pem")},
		"missing cert": {filepath.Join(t.TempDir(), "absent.crt"), keyFile},
	} {
		t.Setenv("STREAMGO_SERVER_CERT_FILE", env[0])
		t.Setenv("STREAMGO_SERVER_KEY_FILE", env[1])
		code, _, errOut := execute(t, "server")
		named := strings.Contains(errOut, env[0]) || strings.Contains(errOut, env[1])
		if code != 1 || !named || strings.Contains(errOut, "PRIVATE") {
			t.Errorf("%s: exit %d, stderr %q", name, code, errOut)
		}
		if conn, err := net.Dial("tcp", addr); err == nil {
			_ = conn.Close()
			t.Errorf("%s: something listens", name)
		}
	}
}

// TestHTTPSNeedsHTTPSPublicURL is "Public URL not HTTPS".
func TestHTTPSNeedsHTTPSPublicURL(t *testing.T) {
	validEnv(t)
	t.Setenv("STREAMGO_SERVER_SELF_SIGNED", "true")
	t.Setenv("STREAMGO_SERVER_PUBLIC_URL", "http://localhost:8080")
	code, _, errOut := execute(t, "server")
	if code != 1 || !strings.Contains(errOut, "server.public_url") || !strings.Contains(errOut, "must be https://") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}
