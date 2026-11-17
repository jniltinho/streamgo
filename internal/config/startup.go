package config

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
