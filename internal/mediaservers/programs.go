package mediaservers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The two programs, by the name of their executable.
const (
	LiveKit  = "livekit-server"
	MediaMTX = "mediamtx"
)

// Where a program came from.
const (
	FromPayload = "payload"
	FromBeside  = "beside the executable"
	FromPATH    = "PATH"
)

// Program is a media server chosen to run.
type Program struct {
	Name    string // LiveKit or MediaMTX
	Path    string
	Version string
	From    string // FromPayload, FromBeside or FromPATH
}

// Finder chooses each program once, in this order: the payload of Executable, the file beside it, PATH. It never
// tries the next source when the chosen one fails.
type Finder struct {
	Executable string            // the running executable
	DataDir    string            // the payload is unpacked under <DataDir>/media
	Pinned     map[string]string // the SHA-256 compiled into the program, by name; empty: the trailer's
	Download   string            // the file to download when a program is nowhere, for the message
	LookPath   func(string) (string, error)
}

// Find returns the two programs, unpacking the payload when there is one.
func (f Finder) Find(ctx context.Context) ([]Program, error) {
	p, err := ReadPayload(f.Executable)
	if err != nil {
		return nil, err
	}
	if p != nil {
		return f.unpack(p)
	}
	look := f.LookPath
	if look == nil {
		look = exec.LookPath
	}
	var out []Program
	for _, name := range []string{LiveKit, MediaMTX} {
		prog := Program{Name: name, Path: filepath.Join(filepath.Dir(f.Executable), name), From: FromBeside}
		if _, err := os.Stat(prog.Path); err == nil {
			if err := runnable(prog.Path); err != nil {
				return nil, fmt.Errorf("%s cannot be run: %w", prog.Path, err)
			}
		} else if path, lerr := look(name); lerr == nil {
			prog.Path, prog.From = path, FromPATH
		} else {
			return nil, fmt.Errorf("%s was not found beside %s nor in PATH: install it, or download %s, which carries both media servers",
				name, f.Executable, f.Download)
		}
		prog.Version = version(ctx, prog.Path)
		out = append(out, prog)
	}
	return out, nil
}

// runnable checks that path is a regular file this user may execute.
func runnable(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	if err := unix.Access(path, unix.X_OK); err != nil {
		return fmt.Errorf("not executable: %w", err)
	}
	return nil
}

// version asks a program of the operator for its version: the last word of the first line of --version.
func version(ctx context.Context, path string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version") //nolint:gosec // the chosen media server
	cmd.Env = BaseEnv(os.Environ())
	out, err := cmd.Output()
	first, _, _ := strings.Cut(string(out), "\n")
	if fields := strings.Fields(first); err == nil && len(fields) > 0 {
		return fields[len(fields)-1]
	}
	return "unknown"
}

// MediaDir prepares <dataDir>/media, the directory up owns: it creates it with mode 0700, and refuses it when it
// is a symbolic link, not a directory or another user's.
func MediaDir(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "media")
	st, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return "", fmt.Errorf("media directory: %w", err)
		}
		return dir, nil
	}
	if err != nil {
		return "", fmt.Errorf("media directory: %w", err)
	}
	switch {
	case st.Mode()&fs.ModeSymlink != 0:
		return "", fmt.Errorf("%s is a symbolic link: up uses only a directory of its own there", dir)
	case !st.IsDir():
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Getuid() {
		return "", fmt.Errorf("%s belongs to another user (uid %d): up uses only a directory of its own there", dir, sys.Uid)
	}
	if st.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // a directory: 0700 is owner-only
			return "", fmt.Errorf("media directory: %w", err)
		}
	}
	return dir, nil
}

// unpack writes each program of the payload to <data_dir>/media/<name>-<12 hex digits of its digest>, unless the
// file there already has that digest, and removes the files of other releases.
func (f Finder) unpack(p *Payload) ([]Program, error) {
	dir, err := MediaDir(f.DataDir)
	if err != nil {
		return nil, err
	}
	if noexec(dir) { // before anything is unpacked there
		return nil, fmt.Errorf("cannot run the media servers from %s: its file system is mounted noexec; choose a data directory on another file system with --data-dir", dir)
	}
	var out []Program
	keep := map[string]bool{}
	for _, name := range []string{LiveKit, MediaMTX} {
		e, ok := p.Entries[name]
		if !ok {
			return nil, damaged(p.path, "%s is missing", name)
		}
		want := e.SHA256
		if pinned := f.Pinned[name]; pinned != "" {
			if !strings.EqualFold(pinned, e.SHA256) {
				return nil, damaged(p.path, "%s is not the one this release pins", name)
			}
			want = strings.ToLower(pinned)
		}
		path := filepath.Join(dir, name+"-"+want[:12])
		if err := install(p, name, path, want); err != nil {
			return nil, err
		}
		keep[filepath.Base(path)] = true
		out = append(out, Program{Name: name, Path: path, Version: e.Version, From: FromPayload})
	}
	if err := cleanup(dir, keep); err != nil {
		return nil, err
	}
	return out, nil
}

// install leaves a correct file alone (giving it back mode 0700) and otherwise writes it again from the payload:
// a temporary file in the same directory, verified, then renamed.
func install(p *Payload, name, path, want string) error {
	if st, err := os.Lstat(path); err == nil && st.Mode().IsRegular() {
		if sum, err := fileSHA256(path); err == nil && sum == want {
			if st.Mode().Perm() != 0o700 {
				return os.Chmod(path, 0o700) //nolint:gosec // a program: 0700 is owner-only
			}
			return nil
		}
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	w, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700) //nolint:gosec // up's own directory
	if err != nil {
		return fmt.Errorf("unpack %s: %w", name, err)
	}
	sum, err := p.Extract(name, w)
	if err == nil {
		err = w.Sync()
	}
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err == nil && sum != want {
		err = damaged(p.path, "%s does not have its digest", name)
	}
	if err == nil {
		if st, lerr := os.Lstat(path); lerr == nil && st.IsDir() {
			err = os.RemoveAll(path)
		}
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // up's own directory
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var unpacked = regexp.MustCompile(`^(livekit-server|mediamtx)-[0-9a-f]{12}$`)

// cleanup removes the media servers of other releases: only files named like one of the two programs.
func cleanup(dir string, keep map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("media directory: %w", err)
	}
	for _, e := range entries {
		if n := e.Name(); unpacked.MatchString(n) && !keep[n] && e.Type().IsRegular() {
			if err := os.Remove(filepath.Join(dir, n)); err != nil {
				return fmt.Errorf("remove an earlier media server: %w", err)
			}
		}
	}
	return nil
}

// noexec reports whether dir is on a file system mounted noexec.
func noexec(dir string) bool {
	var st unix.Statfs_t
	return unix.Statfs(dir, &st) == nil && st.Flags&unix.ST_NOEXEC != 0
}
