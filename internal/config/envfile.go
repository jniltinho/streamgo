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
)

// EnvFileName is the file of the data directory that holds the secrets `streamgo up` created: NAME=value lines,
// read as data (never by a shell) for the variables the environment lacks.
const EnvFileName = "streamgo.env"

// EnvFilePath is <dataDir>/streamgo.env.
func EnvFilePath(dataDir string) string { return filepath.Join(dataDir, EnvFileName) }

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseEnv reads NAME=value lines (the format of packaging/streamgo.env): blank lines and lines starting with # are
// skipped, a value in single or double quotes is taken literally without them. Any other line fails, naming file
// and its line number; the error never holds a value.
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
		if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		vars[name] = value
	}
	return vars, nil
}

// ReadEnvFile parses the env file at path; a missing file has no variables.
func ReadEnvFile(path string, readFile func(string) ([]byte, error)) (map[string]string, error) {
	data, err := readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseEnv(path, data)
}

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
	old, err := os.ReadFile(path) //nolint:gosec // the operator's own data directory
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

// envValue writes value so ParseEnv, systemd and the shell all read it literally.
func envValue(name, value string) (string, error) {
	if strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-_./:@=") == "" {
		return value, nil
	}
	if strings.ContainsAny(value, "'\n\r") {
		return "", fmt.Errorf("%s: the value has a quote or a line break and cannot be stored in %s", name, EnvFileName)
	}
	return "'" + value + "'", nil
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
