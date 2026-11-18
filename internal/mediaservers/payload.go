// Package mediaservers runs LiveKit and MediaMTX for `streamgo up` (design B2 to B7 of add-single-binary): it finds
// each program (the payload of the own executable, the file beside it, PATH), unpacks and verifies the embedded ones
// under <data_dir>/media, writes their configuration, builds their environment and supervises them as child
// processes. It is an adapter: it uses os/exec and the file system, and internal/core never imports it.
package mediaservers

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// The payload of the single file, appended to the executable (packaging/single-payload.sh writes it):
//
//	<executable> <zstd stream of each program> <trailer> <footer>
//
// The trailer is one line per program: "<name> <version> <sha256 of the program> <offset> <size>\n", the offset
// and size of its zstd stream counted in bytes from the start of the file. The footer is the trailer's length in
// 16 decimal digits followed by Mark. An executable that does not end with Mark has no payload.
const (
	Mark       = "STREAMGO-PAYLOAD-1\n"
	footerLen  = 16 + len(Mark)
	maxTrailer = 64 << 10
	elfSlack   = 4 << 10 // what UPX leaves after the last segment, or the section table of a plain executable
)

// ErrDamaged is returned when the executable's payload was altered or cut short.
var ErrDamaged = errors.New("the file is damaged: download it again")

// Entry is one program of the payload.
type Entry struct {
	Name    string
	Version string
	SHA256  string // of the program, hex
	Offset  int64  // of its zstd stream
	Size    int64
}

// Payload is the set of programs appended to an executable.
type Payload struct {
	path    string
	Entries map[string]Entry
}

func damaged(path, format string, args ...any) error {
	return fmt.Errorf("%s: %w (%s)", path, ErrDamaged, fmt.Sprintf(format, args...))
}

// ReadPayload reads the trailer of the executable at path. It returns nil, nil when the file has no payload, and
// an error wrapping ErrDamaged when it has one that was altered or cut short. Only the trailer is read.
func ReadPayload(path string) (*Payload, error) {
	f, err := os.Open(path) //nolint:gosec // the own executable
	if err != nil {
		return nil, fmt.Errorf("open the executable: %w", err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("open the executable: %w", err)
	}
	size := st.Size()
	footer := make([]byte, footerLen)
	if size < int64(footerLen) {
		return nil, nil
	}
	if _, err := f.ReadAt(footer, size-int64(footerLen)); err != nil {
		return nil, fmt.Errorf("read the executable: %w", err)
	}
	if string(footer[16:]) != Mark {
		return nil, noMark(path, f, size)
	}
	n, err := strconv.ParseInt(string(footer[:16]), 10, 64)
	end := size - int64(footerLen) // where the trailer ends
	if err != nil || n <= 0 || n > maxTrailer || n > end {
		return nil, damaged(path, "trailer length")
	}
	body := make([]byte, n)
	if _, err := f.ReadAt(body, end-n); err != nil {
		return nil, fmt.Errorf("read the executable: %w", err)
	}
	p := &Payload{path: path, Entries: map[string]Entry{}}
	for line := range strings.Lines(string(body)) {
		fields := strings.Fields(line)
		if len(fields) != 5 || !strings.HasSuffix(line, "\n") {
			return nil, damaged(path, "trailer line %q", strings.TrimSpace(line))
		}
		e := Entry{Name: fields[0], Version: fields[1], SHA256: fields[2]}
		e.Offset, err = strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return nil, damaged(path, "offset of %s", e.Name)
		}
		e.Size, err = strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, damaged(path, "size of %s", e.Name)
		}
		if b, err := hex.DecodeString(e.SHA256); err != nil || len(b) != sha256.Size {
			return nil, damaged(path, "digest of %s", e.Name)
		}
		if e.Offset <= 0 || e.Size <= 0 || e.Offset > end-n || e.Size > end-n-e.Offset {
			return nil, damaged(path, "%s lies outside the file", e.Name)
		}
		if _, dup := p.Entries[e.Name]; dup {
			return nil, damaged(path, "%s twice", e.Name)
		}
		p.Entries[e.Name] = e
	}
	return p, nil
}

// noMark tells an executable without a payload from a single file cut short: an ELF executable ends with its last
// segment or section (UPX leaves a few hundred bytes after it, the section table is under 4 KiB), so more than that after it is a payload without its footer.
func noMark(path string, f *os.File, size int64) error {
	ef, err := elf.NewFile(f)
	if err != nil {
		return nil //nolint:nilerr // not an ELF file: nothing to tell
	}
	var last uint64
	for _, p := range ef.Progs {
		last = max(last, p.Off+p.Filesz)
	}
	for _, s := range ef.Sections {
		if s.Type != elf.SHT_NOBITS {
			last = max(last, s.Offset+s.FileSize)
		}
	}
	if size > int64(last)+elfSlack { //nolint:gosec // an offset in the file fits an int64
		return damaged(path, "the payload was cut short")
	}
	return nil
}

// Extract writes the program name to w, decompressed, and returns its SHA-256 in hex. A stream that does not
// decompress is ErrDamaged.
func (p *Payload) Extract(name string, w io.Writer) (string, error) {
	e, ok := p.Entries[name]
	if !ok {
		return "", damaged(p.path, "%s is missing", name)
	}
	f, err := os.Open(p.path)
	if err != nil {
		return "", fmt.Errorf("open the executable: %w", err)
	}
	defer func() { _ = f.Close() }()
	dec, err := zstd.NewReader(io.NewSectionReader(f, e.Offset, e.Size), zstd.WithDecoderConcurrency(1))
	if err != nil {
		return "", fmt.Errorf("zstd: %w", err)
	}
	defer dec.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), dec); err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			return "", fmt.Errorf("unpack %s: %w", name, err)
		}
		return "", damaged(p.path, "%s does not decompress: %v", name, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
