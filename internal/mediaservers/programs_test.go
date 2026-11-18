package mediaservers_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/mediaservers"
	"github.com/jniltinho/streamgo/internal/mediaservers/mediatest"
)

// The scenarios of "Where the media servers come from" and "Embedded media servers".

// Two small programs stand for the media servers where they need not answer.
var (
	scriptLK  = []byte("#!/bin/sh\necho livekit-server version 1.0.0\n")
	scriptMTX = []byte("#!/bin/sh\necho v1.0.0\n")
)

// lookNowhere is a PATH without either program.
func lookNowhere(string) (string, error) { return "", exec.ErrNotFound }

// lookIn finds the programs in dir only.
func lookIn(dir string) func(string) (string, error) {
	return func(name string) (string, error) {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			return "", exec.ErrNotFound
		}
		return p, nil
	}
}

// plainExecutable writes a file standing for an executable without a payload.
func plainExecutable(t *testing.T, dir string) string {
	t.Helper()
	exe := filepath.Join(dir, "streamgo")
	if err := os.WriteFile(exe, []byte("not a payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestPlainBuild(t *testing.T) {
	t.Parallel()
	beside, inPath := t.TempDir(), t.TempDir()
	mediatest.Install(t, beside)
	mediatest.Install(t, inPath)
	exe := plainExecutable(t, beside)
	got, err := mediaservers.Finder{Executable: exe, DataDir: t.TempDir(), LookPath: lookIn(inPath)}.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"livekit-server", "mediamtx"} {
		if got[i].Path != filepath.Join(beside, name) || got[i].From != mediaservers.FromBeside || got[i].Version != "0.0.0-test" {
			t.Errorf("%s: %+v", name, got[i])
		}
	}
	// PATH when nothing is beside
	got, err = mediaservers.Finder{Executable: plainExecutable(t, t.TempDir()), LookPath: lookIn(inPath)}.Find(context.Background())
	if err != nil || got[0].From != mediaservers.FromPATH || got[1].Path != filepath.Join(inPath, "mediamtx") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestACandidateThatCannotRun(t *testing.T) { // not parallel: it runs a file it wrote (text file busy)
	beside, inPath := t.TempDir(), t.TempDir()
	mediatest.Install(t, inPath)
	if err := os.WriteFile(filepath.Join(beside, "livekit-server"), scriptLK, 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beside, "mediamtx"), scriptMTX, 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home) // the version probe of a fake records itself there
	_, err := mediaservers.Finder{Executable: plainExecutable(t, beside), LookPath: lookIn(inPath)}.Find(context.Background())
	if err == nil || !strings.Contains(err.Error(), filepath.Join(beside, "mediamtx")) {
		t.Fatalf("error %v", err)
	}
	if len(mediatest.Records(home, mediatest.MediaMTX)) != 0 {
		t.Fatal("the one in PATH ran")
	}
}

func TestNoMediaServersAnywhere(t *testing.T) {
	t.Parallel()
	_, err := mediaservers.Finder{Executable: plainExecutable(t, t.TempDir()), LookPath: lookNowhere,
		Download: "streamgo_X.Y.Z_linux_amd64"}.Find(context.Background())
	if err == nil || !strings.Contains(err.Error(), "livekit-server") || !strings.Contains(err.Error(), "streamgo_X.Y.Z_linux_amd64") {
		t.Fatalf("error %v", err)
	}
}

// single writes a single file of the two scripts and returns its path.
func single(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "streamgo")
	payload(t, exe, []byte("an executable"), scriptLK, scriptMTX)
	return exe
}

func unpack(t *testing.T, exe, dataDir string, pinned map[string]string) []mediaservers.Program {
	t.Helper()
	got, err := mediaservers.Finder{Executable: exe, DataDir: dataDir, Pinned: pinned, LookPath: lookNowhere}.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPayloadUnpacked(t *testing.T) { // not parallel: it runs a file it wrote (text file busy)
	dataDir := t.TempDir()
	got := unpack(t, single(t), dataDir, scriptDigests())
	want := filepath.Join(dataDir, "media", "livekit-server-"+digest(scriptLK)[:12])
	if got[0].Path != want || got[0].From != mediaservers.FromPayload || got[0].Version != "v1.0.0-livekit-server" {
		t.Fatalf("%+v", got[0])
	}
	out, err := exec.Command(got[1].Path).Output() //nolint:gosec,noctx // the unpacked test script
	if err != nil || string(out) != "v1.0.0\n" {
		t.Fatalf("the unpacked program: %q %v", out, err)
	}
	if st, _ := os.Stat(filepath.Join(dataDir, "media")); st.Mode().Perm() != 0o700 {
		t.Fatalf("media directory mode %v", st.Mode())
	}
}

func TestADamagedPayload(t *testing.T) {
	t.Parallel()
	exe := single(t)
	data, _ := os.ReadFile(exe) //nolint:gosec // a test file
	// a byte of the compressed LiveKit changed
	altered := append([]byte(nil), data...)
	altered[len("an executable")+6] ^= 0xff
	// the base is an ELF executable (this test binary), as a single file's is: cut short, it has no footer left
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base, _ := os.ReadFile(self) //nolint:gosec // this test binary
	full := filepath.Join(t.TempDir(), "full")
	big := make([]byte, 64<<10) // incompressible: a cut leaves more than the slack an executable may have
	_, _ = rand.Read(big)
	payload(t, full, base, big, big)
	whole, _ := os.ReadFile(full) //nolint:gosec // a test file
	for name, content := range map[string][]byte{
		"altered":           altered,
		"cut short":         whole[:len(whole)-40],
		"cut in the middle": whole[:len(base)+16<<10],
		"pinned digest":     data,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "streamgo")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			pinned := scriptDigests()
			if name == "pinned digest" { // a payload of another release: checked against the program, not itself
				pinned = pinnedOf(scriptLK, []byte("another"))
			}
			dataDir := t.TempDir()
			_, err := mediaservers.Finder{Executable: path, DataDir: dataDir, Pinned: pinned, LookPath: lookNowhere}.Find(context.Background())
			if !errors.Is(err, mediaservers.ErrDamaged) || !strings.Contains(err.Error(), "damaged") {
				t.Fatalf("error %v", err)
			}
			entries, _ := os.ReadDir(filepath.Join(dataDir, "media"))
			for _, e := range entries {
				if !strings.HasPrefix(e.Name(), ".") && e.Name() != "livekit-server-"+digest(scriptLK)[:12] {
					t.Errorf("%s was unpacked", e.Name())
				}
			}
		})
	}
	// the test binary itself has no payload
	if p, err := mediaservers.ReadPayload(self); p != nil || err != nil {
		t.Fatalf("the plain executable: %v %v", p, err)
	}
}

func TestUnpackedOnce(t *testing.T) {
	t.Parallel()
	exe, dataDir := single(t), t.TempDir()
	first := unpack(t, exe, dataDir, scriptDigests())
	st1, _ := os.Stat(first[0].Path)
	st2a, _ := os.Stat(first[1].Path)
	second := unpack(t, exe, dataDir, scriptDigests())
	st2, _ := os.Stat(second[0].Path)
	st2b, _ := os.Stat(second[1].Path)
	ino := func(fi os.FileInfo) uint64 { return fi.Sys().(*syscall.Stat_t).Ino }
	if ino(st1) != ino(st2) || !st1.ModTime().Equal(st2.ModTime()) || ino(st2a) != ino(st2b) || !st2a.ModTime().Equal(st2b.ModTime()) {
		t.Fatal("a file was written again")
	}
}

func TestAChangedFileIsReplaced(t *testing.T) {
	t.Parallel()
	exe, dataDir := single(t), t.TempDir()
	got := unpack(t, exe, dataDir, scriptDigests())
	if err := os.WriteFile(got[0].Path, []byte("#!/bin/sh\necho changed\n"), 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}
	got = unpack(t, exe, dataDir, scriptDigests())
	if b, _ := os.ReadFile(got[0].Path); digest(b) != digest(scriptLK) {
		t.Fatal("the changed file was kept")
	}
	// not a regular file
	if err := os.Remove(got[1].Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(got[1].Path, 0o700); err != nil {
		t.Fatal(err)
	}
	got = unpack(t, exe, dataDir, scriptDigests())
	if b, _ := os.ReadFile(got[1].Path); digest(b) != digest(scriptMTX) {
		t.Fatal("the directory was kept")
	}
}

func TestALostPermissionIsRepaired(t *testing.T) { // not parallel: it runs a file it wrote (text file busy)
	exe, dataDir := single(t), t.TempDir()
	got := unpack(t, exe, dataDir, scriptDigests())
	if err := os.Chmod(got[0].Path, 0o600); err != nil {
		t.Fatal(err)
	}
	got = unpack(t, exe, dataDir, scriptDigests())
	if st, _ := os.Stat(got[0].Path); st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", st.Mode())
	}
	if out, err := exec.Command(got[0].Path).Output(); err != nil || !strings.Contains(string(out), "livekit") { //nolint:gosec,noctx // the unpacked test script
		t.Fatalf("it does not run: %v", err)
	}
}

func TestAnOlderVersionIsRemoved(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	media := filepath.Join(dataDir, "media")
	if err := os.Mkdir(media, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"livekit-server-0123456789ab", "mediamtx-0123456789ab", "notes.txt", "mediamtx-old"} {
		if err := os.WriteFile(filepath.Join(media, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := unpack(t, single(t), dataDir, scriptDigests())
	entries, _ := os.ReadDir(media)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	want := []string{filepath.Base(got[0].Path), filepath.Base(got[1].Path), "mediamtx-old", "notes.txt"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("media holds %v, want %v", names, want)
	}
}

func TestADirectoryThatIsNotItsOwn(t *testing.T) {
	t.Parallel()
	dataDir, target := t.TempDir(), t.TempDir()
	if err := os.Symlink(target, filepath.Join(dataDir, "media")); err != nil {
		t.Fatal(err)
	}
	_, err := mediaservers.Finder{Executable: single(t), DataDir: dataDir, Pinned: scriptDigests(), LookPath: lookNowhere}.Find(context.Background())
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dataDir, "media")) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error %v", err)
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Fatalf("written in it: %v", entries)
	}
}

// TestSinglePayloadScript runs packaging/single-payload.sh and checks the reader takes its output.
func TestSinglePayloadScript(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("zstd"); err != nil {
		t.Skip("zstd is not installed")
	}
	dir := t.TempDir()
	// a name sha256sum would escape (a backslash) does not reach the digest
	files := map[string][]byte{"streamgo": []byte("an executable"), `l\k`: scriptLK, "mtx": scriptMTX}
	for n, b := range files {
		if err := os.WriteFile(filepath.Join(dir, n), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "single")
	cmd := exec.Command("sh", "../../packaging/single-payload.sh", filepath.Join(dir, "streamgo"), //nolint:noctx // a test script
		filepath.Join(dir, `l\k`), "1.13.10", filepath.Join(dir, "mtx"), "v1.21.4", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	p, err := mediaservers.ReadPayload(out)
	if err != nil || p == nil {
		t.Fatalf("payload %v, %v", p, err)
	}
	if e := p.Entries[mediaservers.LiveKit]; e.Version != "1.13.10" || e.SHA256 != digest(scriptLK) {
		t.Fatalf("livekit entry %+v", e)
	}
	got := unpack(t, out, t.TempDir(), map[string]string{mediaservers.LiveKit: digest(scriptLK), mediaservers.MediaMTX: digest(scriptMTX)})
	if b, _ := os.ReadFile(got[1].Path); string(b) != string(scriptMTX) || got[1].Version != "v1.21.4" {
		t.Fatalf("mediamtx unpacked as %q, version %s", b, got[1].Version)
	}
	if st, _ := os.Stat(out); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode())
	}
	// a run that fails leaves the output as it was and nothing beside it
	before, _ := os.ReadFile(out) //nolint:gosec // a test file

	cmd = exec.Command("sh", "../../packaging/single-payload.sh", filepath.Join(dir, "streamgo"), //nolint:noctx // a test script
		filepath.Join(dir, `l\k`), "1.13.10", filepath.Join(dir, "absent"), "v1.21.4", out)
	if b, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("it succeeded: %s", b)
	}
	entries, _ := os.ReadDir(dir)
	after, _ := os.ReadFile(out) //nolint:gosec // a test file
	if len(entries) != 4 || !bytes.Equal(before, after) {
		t.Fatalf("after a failure: %d entries, output changed %v", len(entries), !bytes.Equal(before, after))
	}
}

// scriptDigests are the digests a release of the two scripts pins.
func scriptDigests() map[string]string { return pinnedOf(scriptLK, scriptMTX) }

// TestAPayloadNeedsPinnedDigests is "Embedded media servers" without the digests: an executable built without them
// (or with a malformed one) refuses its payload instead of trusting the payload's own trailer, and writes nothing.
func TestAPayloadNeedsPinnedDigests(t *testing.T) {
	t.Parallel()
	for name, pinned := range map[string]map[string]string{
		"none":         nil,
		"only LiveKit": {mediaservers.LiveKit: digest(scriptLK)},
		"not a digest": {mediaservers.LiveKit: digest(scriptLK), mediaservers.MediaMTX: "unknown"},
		"short":        {mediaservers.LiveKit: digest(scriptLK), mediaservers.MediaMTX: digest(scriptMTX)[:40]},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dataDir := t.TempDir()
			_, err := mediaservers.Finder{Executable: single(t), DataDir: dataDir, Pinned: pinned, LookPath: lookNowhere}.Find(context.Background())
			if err == nil || !strings.Contains(err.Error(), "no SHA-256") || !strings.Contains(err.Error(), "buildinfo.") {
				t.Fatalf("error %v", err)
			}
			if entries, _ := os.ReadDir(dataDir); len(entries) != 0 {
				t.Fatalf("written: %v", entries)
			}
		})
	}
}

// TestTemporaryFilesAreNew: a link planted where a temporary file used to be (the unpacked programs and the
// generated configuration) is never written through.
func TestTemporaryFilesAreNew(t *testing.T) {
	t.Parallel()
	dataDir, outside := t.TempDir(), t.TempDir()
	media := filepath.Join(dataDir, "media")
	if err := os.Mkdir(media, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "streamgo.db")
	if err := os.WriteFile(victim, []byte("the database"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"." + "livekit-server-" + digest(scriptLK)[:12] + ".tmp", "." + "mediamtx-" + digest(scriptMTX)[:12] + ".tmp",
		"livekit.yaml.tmp", "mediamtx.yml.tmp"} {
		if err := os.Symlink(victim, filepath.Join(media, n)); err != nil {
			t.Fatal(err)
		}
	}
	programs := unpack(t, single(t), dataDir, scriptDigests())
	s := newStack(t)
	s.setup.Dir = media
	if _, err := mediaservers.Children(s.setup, programs[0], programs[1]); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "the database" { //nolint:gosec // a test file
		t.Fatalf("the file behind the link was written: %q", b)
	}
	for _, n := range []string{filepath.Base(programs[0].Path), "livekit.yaml", "mediamtx.yml"} {
		if st, err := os.Lstat(filepath.Join(media, n)); err != nil || !st.Mode().IsRegular() {
			t.Errorf("%s: %v %v", n, st, err)
		}
	}
	if st, _ := os.Stat(filepath.Join(media, "livekit.yaml")); st.Mode().Perm() != 0o600 {
		t.Errorf("livekit.yaml mode %v", st.Mode())
	}
}

// TestAPayloadThatDecompressesTooMuch: a stream that decodes to more than the limit fails as damaged at the limit,
// and nothing is left of it.
func TestAPayloadThatDecompressesTooMuch(t *testing.T) {
	t.Parallel()
	exe := filepath.Join(t.TempDir(), "streamgo")
	big := make([]byte, 1<<20) // a megabyte of zeros: a few bytes compressed
	payload(t, exe, []byte("an executable"), big, scriptMTX)
	dataDir := t.TempDir()
	_, err := mediaservers.Finder{Executable: exe, DataDir: dataDir, Pinned: pinnedOf(big, scriptMTX), LookPath: lookNowhere,
		MaxProgramSize: 64 << 10}.Find(context.Background())
	if !errors.Is(err, mediaservers.ErrDamaged) || !strings.Contains(err.Error(), "more than 65536 bytes") {
		t.Fatalf("error %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dataDir, "media")); len(entries) != 0 {
		t.Fatalf("left: %v", entries)
	}
	// the default limit is generous: the same file unpacks
	f := mediaservers.Finder{Executable: exe, DataDir: dataDir, Pinned: pinnedOf(big, scriptMTX), LookPath: lookNowhere}
	if _, err := f.Find(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestUnpackingStopsWithTheContext: a start interrupted while it unpacks stops unpacking and leaves nothing.
func TestUnpackingStopsWithTheContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dataDir := t.TempDir()
	_, err := mediaservers.Finder{Executable: single(t), DataDir: dataDir, Pinned: scriptDigests(), LookPath: lookNowhere}.Find(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dataDir, "media")); len(entries) != 0 {
		t.Fatalf("left: %v", entries)
	}
}

// TestADirectoryInTheWayIsKept: a directory where a program goes is removed only when empty, never with what it
// holds.
func TestADirectoryInTheWayIsKept(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "media", "livekit-server-"+digest(scriptLK)[:12])
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := mediaservers.Finder{Executable: single(t), DataDir: dataDir, Pinned: scriptDigests(), LookPath: lookNowhere}.Find(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("error %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(path, "notes.txt")); string(b) != "mine" { //nolint:gosec // a test file
		t.Fatal("what the directory held was deleted")
	}
}

// TestADanglingLinkBeside is "A candidate that cannot run" for a link beside the executable whose target is gone:
// it fails naming it, and the program in PATH does not run instead.
func TestADanglingLinkBeside(t *testing.T) {
	beside, inPath, home := t.TempDir(), t.TempDir(), t.TempDir()
	mediatest.Install(t, inPath)
	t.Setenv("HOME", home) // the version probe of a fake records itself there
	if err := os.Symlink(filepath.Join(beside, "gone"), filepath.Join(beside, "mediamtx")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(inPath, "livekit-server"), filepath.Join(beside, "livekit-server")); err != nil {
		t.Fatal(err)
	}
	_, err := mediaservers.Finder{Executable: plainExecutable(t, beside), LookPath: lookIn(inPath)}.Find(context.Background())
	if err == nil || !strings.Contains(err.Error(), filepath.Join(beside, "mediamtx")+" cannot be run") {
		t.Fatalf("error %v", err)
	}
}

// TestAVersionProbeThatHangs: a candidate whose --version prints without end and leaves a process holding its
// output costs a bounded time, not the life of that process.
func TestAVersionProbeThatHangs(t *testing.T) { // not parallel: it runs files it wrote (text file busy)
	beside := t.TempDir()
	script := []byte("#!/bin/sh\nsleep 60 &\nhead -c 3000000 /dev/zero | tr '\\0' x\nexec sleep 60\n")
	for _, n := range []string{"livekit-server", "mediamtx"} {
		if err := os.WriteFile(filepath.Join(beside, n), script, 0o700); err != nil { //nolint:gosec // a test script
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	began := time.Now()
	got, err := mediaservers.Finder{Executable: plainExecutable(t, beside), LookPath: lookNowhere}.Find(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(began); d > 8*time.Second || got[0].Version != "unknown" {
		t.Fatalf("took %v, version %q", d, got[0].Version)
	}
}

// TestAVersionProbeThatLeavesAProcess: a candidate whose --version answers at once and leaves a process in the
// background; that process is gone when the probe returned.
func TestAVersionProbeThatLeavesAProcess(t *testing.T) { // not parallel: it runs files it wrote (text file busy)
	beside, pids := t.TempDir(), t.TempDir()
	script := []byte("#!/bin/sh\nsleep 60 >/dev/null 2>&1 &\necho $! > " + pids + "/$(basename $0)\necho v1.2.3\n")
	for _, n := range []string{"livekit-server", "mediamtx"} {
		if err := os.WriteFile(filepath.Join(beside, n), script, 0o700); err != nil { //nolint:gosec // a test script
			t.Fatal(err)
		}
	}
	got, err := mediaservers.Finder{Executable: plainExecutable(t, beside), LookPath: lookNowhere}.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range []string{"livekit-server", "mediamtx"} {
		if got[i].Version != "v1.2.3" {
			t.Errorf("%s: version %q", n, got[i].Version)
		}
		b, err := os.ReadFile(filepath.Join(pids, n)) //nolint:gosec // a test file
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, 2*time.Second, n+"'s background sleep gone", func() bool { return gone(pid) })
	}
}

// TestADamagedPayloadWithAGoodCache is "A damaged payload" after a first start: the file unpacked then still
// matches, but the payload is read again and fails.
func TestADamagedPayloadWithAGoodCache(t *testing.T) {
	t.Parallel()
	exe, dataDir := single(t), t.TempDir()
	unpack(t, exe, dataDir, scriptDigests())
	data, _ := os.ReadFile(exe) //nolint:gosec // a test file
	data[len("an executable")+6] ^= 0xff
	if err := os.WriteFile(exe, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := mediaservers.Finder{Executable: exe, DataDir: dataDir, Pinned: scriptDigests(), LookPath: lookNowhere}.Find(context.Background())
	if !errors.Is(err, mediaservers.ErrDamaged) {
		t.Fatalf("error %v", err)
	}
}
