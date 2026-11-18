package mediaservers

import (
	"errors"
	"io"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Made-up secrets: never real ones.
const (
	secretA = "lk-secret-0123456789abcdef0123456789"
	secretB = "mtx-pass-0123456789abcdef"
)

// fragments are the pieces of secret of 6 bytes or more: none may reach a log line.
func leaks(text string, secrets ...string) string {
	for _, s := range secrets {
		for i := 0; i+6 <= len(s); i++ {
			if strings.Contains(text, s[i:i+6]) {
				return s[i : i+6]
			}
		}
	}
	return ""
}

func TestRedactPrefix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text string
		secrets    []string
		limit      int
		want       string
	}{
		{"none", "plain text", []string{secretA}, 100, "plain text"},
		{"one", "key " + secretA + " end", []string{secretA}, 100, "key [redacted] end"},
		{"twice", secretA + secretA, []string{secretA}, 100, "[redacted]"},
		// a secret inside a longer one: replaced in turn, the shorter would leave the rest of the longer
		{"a secret within another", "x passphrase-long y", []string{"pass", "passphrase-long"}, 100, "x [redacted] y"},
		{"overlapping", "x token-AAAA-BBBB-CCCC-tail y", []string{"token-AAAA-BBBB", "BBBB-CCCC-tail"}, 100, "x [redacted] y"},
		// a secret that starts before the cut is replaced whole, and nothing after the cut is kept
		{"across the cut", "abc" + secretA + "zzz", []string{secretA}, 10, "abc[redacted]"},
		{"after the cut", "abcdefghij" + secretA, []string{secretA}, 10, "abcdefghij"},
		{"empty secret", "abc", []string{""}, 100, "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := redactPrefix(tc.text, tc.secrets, tc.limit); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// readAll runs the reader of a Group with the secrets over what write writes, in pieces, and returns the lines it
// gives the logger and keeps in the tail.
func readAll(t *testing.T, secrets []string, write func(w io.Writer)) (logged, tail []string) {
	t.Helper()
	g := &Group{opts: Options{Secrets: secrets}}
	c := &child{Child: Child{Name: "livekit"}}
	r, w := io.Pipe()
	go func() {
		write(w)
		_ = w.Close()
	}()
	lines := make(chan logLine, 1000)
	g.read(c, r, lines)
	close(lines)
	for l := range lines {
		logged = append(logged, l.text)
	}
	return logged, c.lastLines()
}

// pieces writes s a few bytes at a time, as a server's writes may arrive.
func pieces(w io.Writer, s string) {
	for len(s) > 0 {
		n := min(3, len(s))
		_, _ = io.WriteString(w, s[:n])
		s = s[n:]
	}
}

// TestCredentialsStayOutOfSightInPieces is "Credentials stay out of sight" at the edges: a secret across the cut of
// a long line, two secrets that overlap, writes that split them, and a last line without a line break.
func TestCredentialsStayOutOfSightInPieces(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		secrets []string
		text    string
		want    []string // the lines logged
	}{
		{"a secret across the cut", []string{secretA},
			strings.Repeat("a", MaxLine-10) + secretA + strings.Repeat("b", 100) + "\n",
			[]string{strings.Repeat("a", MaxLine-10) + "[redacted] [line cut]"}},
		{"a secret just before the cut", []string{secretA},
			strings.Repeat("a", MaxLine-len(secretA)) + secretA + "b\n",
			[]string{strings.Repeat("a", MaxLine-len(secretA)) + "[redacted] [line cut]"}},
		{"overlapping secrets", []string{"0123456789abcdef0123456789", secretA},
			"keys: " + secretA + "\n",
			[]string{"keys: [redacted]"}},
		{"no line break at the end", []string{secretA, secretB},
			"first\npass: " + secretB + " key: " + secretA,
			[]string{"first", "pass: [redacted] key: [redacted]"}},
		{"a long last line without a line break", []string{secretA},
			strings.Repeat("c", MaxLine-5) + secretA,
			[]string{strings.Repeat("c", MaxLine-5) + "[redacted] [line cut]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logged, tail := readAll(t, tc.secrets, func(w io.Writer) { pieces(w, tc.text) })
			if strings.Join(logged, "\n") != strings.Join(tc.want, "\n") || strings.Join(tail, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("logged %.200q\ntail %.200q", logged, tail)
			}
			if f := leaks(strings.Join(logged, "\n")+strings.Join(tail, "\n"), tc.secrets...); f != "" {
				t.Fatalf("a piece of a secret is visible: %q", f)
			}
		})
	}
}

// TestTheReaderNeverWaits is "A server cannot block by writing" at the reader: with nobody taking the lines, it
// drains everything, keeps the tail, and counts what did not fit.
func TestTheReaderNeverWaits(t *testing.T) {
	t.Parallel()
	g := &Group{}
	c := &child{Child: Child{Name: "livekit"}}
	r, w := io.Pipe()
	go func() {
		for range 10000 {
			_, _ = io.WriteString(w, "a line of output\n")
		}
		_, _ = io.WriteString(w, "the last line\n")
		_ = w.Close()
	}()
	lines := make(chan logLine, 1) // never read
	done := make(chan struct{})
	go func() {
		g.read(c, r, lines)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the reader waits for the logger")
	}
	if tail := c.lastLines(); tail[len(tail)-1] != "the last line" {
		t.Fatalf("tail %q", tail)
	}
}

// TestNoStartOnceShutdownBegan: once restarts are disabled, the launcher refuses every start, however the launch
// and the stop interleave.
func TestNoStartOnceShutdownBegan(t *testing.T) {
	t.Parallel()
	path, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true")
	}
	g := &Group{launches: make(chan launch), launcher: make(chan struct{}), quiet: make(chan struct{})}
	go g.launch()
	g.disableRestarts()
	c := &child{Child: Child{Name: "livekit", Path: path}}
	for range 50 {
		if _, err := g.run(c); !errors.Is(err, errShuttingDown) {
			t.Fatalf("a process started after shutdown began: %v", err)
		}
	}
	close(g.launches)
	<-g.launcher
}

// fakeListener is a listener on a chosen port.
type fakeListener struct {
	net.Listener
	port int
}

func (l fakeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: l.port} }
func (l fakeListener) Close() error   { return nil }

// TestInternalPortsAvoidThePlannedOnes: a port the system offers that this instance will listen on itself (a media
// port, the HTTP port) is never chosen.
func TestInternalPortsAvoidThePlannedOnes(t *testing.T) {
	t.Parallel()
	offered := []int{7881, 40000, 40001, 8080, 40002, 40003}
	listen := func() (net.Listener, error) {
		p := offered[0]
		offered = offered[1:]
		return fakeListener{port: p}, nil
	}
	got, err := freePorts(4, []int{7881, 8080, 8189}, listen)
	if err != nil || len(got) != 4 || got[0] != 40000 || got[3] != 40003 {
		t.Fatalf("ports %v, %v", got, err)
	}
}
