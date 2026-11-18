package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// EnvFileName is the file of the data directory that holds the secrets `streamgo up` created: NAME=value lines,
// read as data (never by a shell) for the variables the environment lacks.
const EnvFileName = "streamgo.env"

// EnvFilePath is <dataDir>/streamgo.env.
func EnvFilePath(dataDir string) string { return filepath.Join(dataDir, EnvFileName) }

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseEnv reads NAME=value lines (the format of packaging/streamgo.env): blank lines and lines starting with # are
// skipped, a value in single quotes is taken literally without them, and one in double quotes too, but for \\ and
// \" that stand for \ and " (as systemd reads them). Any other line fails, naming file and its line number; the
// error never holds a value.
func ParseEnv(file string, data []byte) (map[string]string, error) {
	vars := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || !envName.MatchString(name) {
			return nil, fmt.Errorf("%s line %d: not a NAME=value assignment", file, i+1)
		}
		switch {
		case len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'':
			value = value[1 : len(value)-1]
		case len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"':
			value = doubleQuoted.Replace(value[1 : len(value)-1])
		}
		vars[name] = value
	}
	return vars, nil
}

var doubleQuoted = strings.NewReplacer(`\\`, `\`, `\"`, `"`)

// ReadEnvFile parses the env file at path; a missing file has no variables. readFile nil reads it as
// OpenEnvFile does.
func ReadEnvFile(path string, readFile func(string) ([]byte, error)) (map[string]string, error) {
	if readFile == nil {
		readFile = OpenEnvFile
	}
	data, err := readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseEnv(path, data)
}

// OpenEnvFile reads the env file at path, which holds secrets, only when it is safe: opened without following a
// symbolic link, a regular file, owned by this user (root reads the file of any user). Permissions wider than 0600
// on a file of this user are taken back to 0600; on another's file they refuse it. No error holds a value.
func OpenEnvFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0) //nolint:gosec // the data directory's own file, checked below
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s is a symbolic link: it holds secrets, so it must be a file of its own", path)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file (%v): it holds secrets, so it must be a file of its own", path, st.Mode().Type())
	}
	uid := -1
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		uid = int(sys.Uid)
	}
	mine := uid == os.Geteuid()
	if !mine && os.Geteuid() != 0 {
		return nil, fmt.Errorf("%s belongs to another user (uid %d): it holds secrets, so it must be yours", path, uid)
	}
	if st.Mode().Perm()&0o077 != 0 {
		if !mine {
			return nil, fmt.Errorf("%s can be read by other users (mode %v): make it 0600", path, st.Mode().Perm())
		}
		if err := f.Chmod(0o600); err != nil {
			return nil, fmt.Errorf("%s can be read by other users (mode %v) and cannot be made 0600: %w", path, st.Mode().Perm(), err)
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxEnvFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxEnvFile {
		return nil, fmt.Errorf("%s is larger than %d bytes: it is not the env file up writes", path, MaxEnvFile)
	}
	return data, nil
}

// MaxEnvFile is the size of the largest env file read.
const MaxEnvFile = 1 << 20

// The variables EnsureEnvFile stores, with how each is generated: the same values as packaging/init-env.sh.
var firstStartVars = []struct {
	name     string
	generate func(io.Reader) (string, error)
}{
	{EnvPrefix + "SECRET_KEY", func(r io.Reader) (string, error) { return random(r, SecretKeyBytes, base64.StdEncoding.EncodeToString) }},
	{EnvPrefix + "LIVEKIT_API_KEY", func(io.Reader) (string, error) { return "streamgo", nil }},
	{EnvPrefix + "LIVEKIT_API_SECRET", func(r io.Reader) (string, error) { return random(r, 32, hex.EncodeToString) }},
	{EnvPrefix + "MEDIAMTX_USER", func(io.Reader) (string, error) { return "streamgo", nil }},
	{EnvPrefix + "MEDIAMTX_SECRET", func(r io.Reader) (string, error) { return random(r, 24, hex.EncodeToString) }},
}

func random(r io.Reader, n int, encode func([]byte) string) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("random bytes: %w", err)
	}
	return encode(b), nil
}

// EnsureEnvFile creates or completes <dataDir>/streamgo.env with the secret key and the credentials the server and
// the two media servers share. A variable the file lacks takes the environment's value when there is one and is
// generated otherwise; a value already in the file is never replaced. The file is written to a temporary file of
// mode 0600 and renamed, and not touched at all when it is complete. It returns the names it added.
func EnsureEnvFile(dataDir string, lookup func(string) (string, bool), rnd io.Reader) ([]string, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	path := EnvFilePath(dataDir)
	old, err := OpenEnvFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	have, err := ParseEnv(path, old)
	if err != nil {
		return nil, err
	}
	var added []string
	var buf bytes.Buffer
	buf.Write(old)
	if len(old) > 0 && !bytes.HasSuffix(old, []byte("\n")) {
		buf.WriteByte('\n')
	}
	for _, v := range firstStartVars {
		if _, ok := have[v.name]; ok {
			continue
		}
		value, ok := lookup(v.name)
		if !ok || value == "" {
			if value, err = v.generate(rnd); err != nil {
				return nil, err
			}
		}
		quoted, err := envValue(v.name, value)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, "%s=%s\n", v.name, quoted)
		added = append(added, v.name)
	}
	if len(added) == 0 {
		return nil, nil
	}
	return added, writeFileAtomic(path, buf.Bytes())
}

// envValue writes value so ParseEnv and systemd read it back as it is: bare when it is plain, else in single quotes,
// else (it has a single quote) in double quotes with \ and " escaped.
func envValue(name, value string) (string, error) {
	switch {
	case strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-_./:@=") == "":
		return value, nil
	case strings.ContainsAny(value, "\n\r"):
		return "", fmt.Errorf("%s: the value has a line break and cannot be stored in %s", name, EnvFileName)
	case !strings.Contains(value, "'"):
		return "'" + value + "'", nil
	default:
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`, nil
	}
}

// writeFileAtomic writes data to a temporary file of mode 0600 beside path and renames it over path.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
