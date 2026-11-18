package config

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// CheckDataDir creates server.data_dir when missing and verifies the server can write to it.
func CheckDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("server.data_dir: %w", err)
	}
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("server.data_dir: %s is not writable: %w", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// CheckPrivateDir refuses dir when a user other than this one (and root) could replace it or what it holds. The path
// is resolved one name at a time, the way the kernel does, and every directory met on the way is checked, those a
// link leads through included: each must belong to this user or to root, and none may be written by everyone unless
// it has the sticky bit (as /tmp), which forbids replacing another's entry. dir itself, once resolved, may be written
// by its owner only. A directory above it that its group can write is accepted: distributions with a private group
// per user create every directory that way (umask 002); the members of that group could replace dir.
func CheckPrivateDir(dir string) error {
	abs := dir // not cleaned: a .. after a link leads where the kernel takes it
	if !filepath.IsAbs(dir) {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("--data-dir: %w", err)
		}
		abs = wd + "/" + dir
	}
	isSlash := func(r rune) bool { return r == '/' }
	cur, rest, links := "/", strings.FieldsFunc(abs, isSlash), 0
	if err := privateDir(cur, false); err != nil {
		return err
	}
	for len(rest) > 0 {
		name := rest[0]
		rest = rest[1:]
		switch name {
		case ".":
			continue
		case "..":
			cur = filepath.Dir(cur) // cur has no link in it: its parent was checked on the way
			continue
		}
		next := filepath.Join(cur, name)
		st, err := os.Lstat(next)
		if err != nil {
			return fmt.Errorf("--data-dir: %w", err)
		}
		if st.Mode()&fs.ModeSymlink != 0 { // replaceable only by who can write cur, checked
			if links++; links > 40 {
				return fmt.Errorf("--data-dir: %s: too many links", dir)
			}
			target, err := os.Readlink(next)
			if err != nil {
				return fmt.Errorf("--data-dir: %w", err)
			}
			if filepath.IsAbs(target) {
				cur = "/"
			}
			rest = append(strings.FieldsFunc(target, isSlash), rest...)
			continue
		}
		if len(rest) > 0 { // the last is checked below, as the data directory
			if err := privateDir(next, false); err != nil {
				return err
			}
		}
		cur = next
	}
	return privateDir(cur, true)
}

func privateDir(d string, last bool) error {
	st, err := os.Lstat(d)
	if err != nil {
		return fmt.Errorf("--data-dir: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("--data-dir: %s is not a directory", d)
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && sys.Uid != 0 && int(sys.Uid) != os.Geteuid() {
		return fmt.Errorf("--data-dir: %s belongs to another user (uid %d), who could replace the data directory: choose one of your own", d, sys.Uid)
	}
	perm := st.Mode().Perm()
	if last && perm&0o022 != 0 {
		return fmt.Errorf("--data-dir: other users can write to %s (mode %v) and replace what up runs from it: chmod go-w it, or choose another", d, perm)
	}
	if !last && perm&0o002 != 0 && st.Mode()&fs.ModeSticky == 0 {
		return fmt.Errorf("--data-dir: everyone can write to %s (mode %v) and replace the data directory in it: chmod o-w it, or choose another", d, perm)
	}
	return nil
}

// ffmpegNeeds is what the live output's two command lines need from ffmpeg (the list the
// spike derived, docs/spike-media-path.md): the listing flag, what the listing calls the
// item, and the names that must be in it. RTSP is a format in ffmpeg, not a protocol.
var ffmpegNeeds = []struct {
	flag, kind string
	names      []string
}{
	{"-encoders", "encoder", []string{"libx264", "aac"}},
	{"-decoders", "decoder", []string{"h264", "vp8", "opus"}},
	{"-demuxers", "demuxer", []string{"rtsp"}},
	{"-muxers", "muxer", []string{"rtsp", "rtp", "flv"}},
	{"-protocols", "output protocol", []string{"tcp", "tls", "rtmp", "rtmps"}},
	{"-filters", "filter", []string{"scale", "pad", "format"}},
}

// CheckFFmpeg runs the ffmpeg at path and verifies it offers every encoder, decoder,
// format and protocol the live output uses; the error names what is missing.
func CheckFFmpeg(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var missing []string
	for _, need := range ffmpegNeeds {
		out, err := exec.CommandContext(ctx, path, "-hide_banner", need.flag).Output() //nolint:gosec // the operator's own setting
		if err != nil {
			return fmt.Errorf("output.ffmpeg_path: cannot run %q: %w", path, err)
		}
		have := listed(out, need.flag)
		for _, n := range need.names {
			if !have[n] {
				missing = append(missing, fmt.Sprintf("the %s %s", n, need.kind))
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("output.ffmpeg_path: %q lacks %s", path, strings.Join(missing, ", "))
	}
	return nil
}

// listed parses one of ffmpeg's listings. Codec and format listings have a header ending in
// a dashed line, then one entry per line: flags, name(s), description. The protocol listing
// has an "Output:" section with one name per line. The filter listing has no dashed line; an
// entry is flags, name, its pads ("V->V") and a description.
func listed(out []byte, flag string) map[string]bool {
	have := map[string]bool{}
	protocols := flag == "-protocols"
	inside := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case flag == "-filters":
			if len(fields) >= 3 && strings.Contains(fields[2], "->") {
				have[fields[1]] = true
			}
		case protocols && strings.HasSuffix(line, ":"):
			inside = line == "Output:"
		case protocols:
			if inside {
				have[fields[0]] = true
			}
		case strings.HasPrefix(line, "--"):
			inside = true
		case inside && len(fields) >= 2:
			for n := range strings.SplitSeq(fields[1], ",") {
				have[n] = true
			}
		}
	}
	return have
}
