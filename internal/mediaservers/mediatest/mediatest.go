// Package mediatest is a fake LiveKit and MediaMTX for the tests of `streamgo up`: a test binary run under the name
// of a media server (a symbolic link, or an unpacked copy) listens on the ports of the configuration it is given and
// answers the readiness checks of internal/livekit and internal/mediamtx. It reads its configuration as YAML and
// LIVEKIT_KEYS as LiveKit does (a YAML map whose string values are the secrets), so a file or a variable the real
// servers would not read fails the fake too.
//
// A child of up gets only PATH, HOME, TZ and the locale from up's environment, so the fake is steered through files
// in $HOME: <component>.mode holds one behaviour per line for its successive starts (the last line repeats), and
// each start appends a Record to <component>.jsonl and, once it listens, writes its addresses to
// <component>-<pid>.listen. Behaviours: "ok" (serve until SIGTERM), "exit" (exit 1 at once), "run=<duration>"
// (serve, then exit 1), "hang" (print 25 lines, never listen, ignore SIGTERM), "secret" (print its credentials, exit
// 1), "big" (print a line and 1 MiB without a line break, then serve with the line still open), "fork" (start a
// sleep in its process group, then serve), "flood" (print 20000 lines, then create <component>.flooded and serve).
package mediatest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
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

// Listeners are the addresses ("tcp :1234", "udp :1235") the start pid of component listens on; nil until it does.
func Listeners(home, component string, pid int) []string {
	b, err := os.ReadFile(filepath.Join(home, fmt.Sprintf("%s-%d.listen", component, pid))) //nolint:gosec // a test file
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// Ports handed out by FreePorts: below the kernel's ephemeral range (32768 and up), so neither a listener on port 0
// nor an outgoing connection (the readiness checks make many) ever takes one between the choice and the bind.
const (
	portsFrom  = 20000
	portsBlock = 500 // each test binary counts through one block, chosen at random: two packages rarely share one
	portBlocks = 24
)

// nextPort is the last port handed out by this test binary. shortcut: process-wide state of a test helper, so
// parallel tests never get the same port; a test that needs more than a block of ports would need a bigger block.
var nextPort atomic.Int64

// FreePorts returns n distinct ports free at this moment on every address, over TCP and over UDP both (the media
// ports need both), none of them given to another test of this binary.
func FreePorts(t testing.TB, n int) []int {
	t.Helper()
	nextPort.CompareAndSwap(0, int64(portsFrom+rand.IntN(portBlocks)*portsBlock)) //nolint:gosec // not a secret
	var ports []int
	for tries := 0; len(ports) < n; tries++ {
		if tries > portsBlock {
			t.Fatal("no port free over TCP and UDP")
		}
		p := int(nextPort.Add(1))
		if p >= portsFrom+portBlocks*portsBlock {
			nextPort.CompareAndSwap(int64(p), portsFrom)
			continue
		}
		if free(p) {
			ports = append(ports, p)
		}
	}
	return ports
}

// free reports whether port p can be bound on every address over TCP and over UDP.
func free(p int) bool {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(p)) //nolint:noctx,gosec // a media port: on every address, as the servers bind it
	if err != nil {
		return false
	}
	defer func() { _ = l.Close() }()
	u, err := net.ListenPacket("udp", ":"+strconv.Itoa(p)) //nolint:noctx,gosec // the same
	if err != nil {
		return false
	}
	_ = u.Close()
	return true
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
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 1<<20)) // no line break: the line stays open while it serves
	case "fork":
		sleep := exec.Command("sleep", "60") //nolint:noctx // a grandchild in the fake's process group
		if err := sleep.Start(); err != nil {
			fmt.Println("fake:", err)
			return 1
		}
		fmt.Println("fake: grandchild", sleep.Process.Pid)
	case "flood":
		w := bufio.NewWriter(os.Stdout)
		for i := range 20000 {
			_, _ = fmt.Fprintf(w, "flood line %d\n", i)
		}
		_ = w.Flush()
		_ = os.WriteFile(filepath.Join(home, component+".flooded"), nil, 0o600) //nolint:gosec // the test's home
	}
	listen, err := serve(component)
	if err != nil {
		fmt.Println("fake:", err)
		return 1
	}
	_ = os.WriteFile(filepath.Join(home, fmt.Sprintf("%s-%d.listen", component, os.Getpid())), []byte(strings.Join(listen, "\n")+"\n"), 0o600) //nolint:gosec // the test's home
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

// liveKitKeys reads LIVEKIT_KEYS as LiveKit does: a YAML map whose string values are kept.
func liveKitKeys() (map[string]string, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(os.Getenv("LIVEKIT_KEYS")), &raw); err != nil {
		return nil, fmt.Errorf("LIVEKIT_KEYS: %w", err)
	}
	keys := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			keys[k] = s
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("LIVEKIT_KEYS holds no key")
	}
	return keys, nil
}

// serve opens the listeners of the configuration in the arguments and returns them.
func serve(component string) ([]string, error) {
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
		return nil, err
	}
	var values map[string]any
	if err := yaml.Unmarshal(b, &values); err != nil {
		return nil, fmt.Errorf("%s: %w", conf, err)
	}
	str := func(m map[string]any, k string) string {
		switch v := m[k].(type) {
		case string:
			return v
		case int:
			return strconv.Itoa(v)
		}
		return ""
	}
	var listening []string
	srv := func(addr string, h http.HandlerFunc) error {
		l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", addr)
		if err != nil {
			return err
		}
		listening = append(listening, "tcp "+addr)
		go func() { _ = http.Serve(l, h) }() //nolint:gosec // a test server
		return nil
	}
	tcp := func(addr string) error {
		l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", addr)
		if err == nil {
			listening = append(listening, "tcp "+addr)
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
			listening = append(listening, "udp "+addr)
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
		if _, err := liveKitKeys(); err != nil {
			return nil, err
		}
		rtc, _ := values["rtc"].(map[string]any)
		err := firstErr(
			srv("127.0.0.1:"+str(values, "port"), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/protobuf") // an empty body is an empty message
			}),
			tcp(":"+str(rtc, "tcp_port")), udp(":"+str(rtc, "udp_port")))
		return listening, err
	}
	api := func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v3/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"itemCount":0,"pageCount":0,"items":[]}`))
	}
	err = firstErr(srv(str(values, "apiAddress"), api), srv(str(values, "webrtcAddress"), http.NotFound),
		tcp(str(values, "rtspAddress")), tcp(str(values, "webrtcLocalTCPAddress")), udp(str(values, "webrtcLocalUDPAddress")))
	return listening, err
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
