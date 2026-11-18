// Package mediatest is a fake LiveKit and MediaMTX for the tests of `streamgo up`: a test binary run under the name
// of a media server (a symbolic link, or an unpacked copy) listens on the ports of the configuration it is given and
// answers the readiness checks of internal/livekit and internal/mediamtx.
//
// A child of up gets only PATH, HOME, TZ and the locale from up's environment, so the fake is steered through files
// in $HOME: <component>.mode holds one behaviour per line for its successive starts (the last line repeats), and
// each start appends a Record to <component>.jsonl. Behaviours: "ok" (serve until SIGTERM), "exit" (exit 1 at once),
// "run=<duration>" (serve, then exit 1), "hang" (print 25 lines, never listen, ignore SIGTERM), "secret" (print its
// credentials, exit 1), "big" (print a line and one of 1 MiB without a line break, then serve).
package mediatest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Component names, as the supervisor logs them.
const (
	LiveKit  = "livekit"
	MediaMTX = "mediamtx"
)

// Record is what one start of the fake saw.
type Record struct {
	Pid  int      `json:"pid"`
	Args []string `json:"args"`
	Env  []string `json:"env"`
	At   int64    `json:"at"` // UnixNano
}

// Main runs the fake and exits when the program's name is the one of a media server; call it first in TestMain.
func Main() {
	name := filepath.Base(os.Args[0])
	switch {
	case strings.HasPrefix(name, "livekit-server"):
		os.Exit(fake(LiveKit))
	case strings.HasPrefix(name, "mediamtx"):
		os.Exit(fake(MediaMTX))
	}
}

// Install links livekit-server and mediamtx in dir to the running test binary.
func Install(t testing.TB, dir string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"livekit-server", "mediamtx"} {
		if err := os.Symlink(exe, filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
}

// SetModes writes the behaviours of component's successive starts to home.
func SetModes(t testing.TB, home, component string, modes ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, component+".mode"), []byte(strings.Join(modes, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Records are the starts of component so far.
func Records(home, component string) []Record {
	f, err := os.Open(filepath.Join(home, component+".jsonl")) //nolint:gosec // a test file
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

func fake(component string) int {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("fake " + component + " version 0.0.0-test")
		return 0
	}
	home := os.Getenv("HOME")
	starts := len(Records(home, component))
	rec, _ := json.Marshal(Record{Pid: os.Getpid(), Args: os.Args, Env: os.Environ(), At: time.Now().UnixNano()})
	if f, err := os.OpenFile(filepath.Join(home, component+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil { //nolint:gosec // a test file
		_, _ = f.Write(append(rec, '\n'))
		_ = f.Close()
	}
	mode := "ok"
	if b, err := os.ReadFile(filepath.Join(home, component+".mode")); err == nil { //nolint:gosec // a test file
		lines := strings.Fields(string(b))
		if len(lines) > 0 {
			mode = lines[min(starts, len(lines)-1)]
		}
	}
	fmt.Println("fake", component, "starting:", mode)
	switch mode {
	case "exit":
		return 1
	case "hang":
		signal.Ignore(syscall.SIGTERM)
		for i := 1; i <= 25; i++ {
			fmt.Printf("hang line %d\n", i)
		}
		select {}
	case "secret":
		fmt.Println("keys:", os.Getenv("LIVEKIT_KEYS"), "pass:", os.Getenv("MTX_AUTHINTERNALUSERS_0_PASS"))
		return 1
	case "big":
		fmt.Println("before the long line")
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 1<<20) + "\n")
	}
	if err := serve(component); err != nil {
		fmt.Println("fake:", err)
		return 1
	}
	if d, ok := strings.CutPrefix(mode, "run="); ok {
		wait, _ := time.ParseDuration(d)
		time.Sleep(wait)
		return 1
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	return 0
}

// serve opens the listeners of the configuration in the arguments.
func serve(component string) error {
	var conf string
	for i, a := range os.Args {
		if component == LiveKit && a == "--config" && i+1 < len(os.Args) {
			conf = os.Args[i+1]
		}
	}
	if component == MediaMTX && len(os.Args) > 1 {
		conf = os.Args[1]
	}
	b, err := os.ReadFile(conf) //nolint:gosec // the generated configuration
	if err != nil {
		return err
	}
	values := map[string]string{}
	for line := range strings.Lines(string(b)) {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), ": "); ok {
			values[k] = v
		}
	}
	srv := func(addr string, h http.HandlerFunc) error {
		l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", addr)
		if err != nil {
			return err
		}
		go func() { _ = http.Serve(l, h) }() //nolint:gosec // a test server
		return nil
	}
	tcp := func(addr string) error {
		l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", addr)
		if err == nil {
			go func() {
				for {
					c, err := l.Accept()
					if err != nil {
						return
					}
					_ = c.Close()
				}
			}()
		}
		return err
	}
	udp := func(addr string) error {
		c, err := new(net.ListenConfig).ListenPacket(context.Background(), "udp", addr)
		if err == nil {
			go func() { // a goroutine keeps it referenced: the garbage collector would close it
				buf := make([]byte, 1500)
				for {
					if _, _, err := c.ReadFrom(buf); err != nil {
						return
					}
				}
			}()
		}
		return err
	}
	if component == LiveKit {
		return firstErr(
			srv("127.0.0.1:"+values["port"], func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/protobuf") // an empty body is an empty message
			}),
			tcp(":"+values["tcp_port"]), udp(":"+values["udp_port"]))
	}
	api := func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v3/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"itemCount":0,"pageCount":0,"items":[]}`))
	}
	return firstErr(srv(values["apiAddress"], api), srv(values["webrtcAddress"], http.NotFound), tcp(values["rtspAddress"]),
		tcp(values["webrtcLocalTCPAddress"]), udp(values["webrtcLocalUDPAddress"]))
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
