package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLayeringRule is the build-and-deploy "Forbidden import in the core" scenario: golangci-lint,
// with this repository's configuration, fails on a file in internal/core that imports GORM,
// net/http or os/exec, naming the file and the import. It runs where golangci-lint is installed
// (the tools image of make test).
func TestLayeringRule(t *testing.T) {
	lint, err := exec.LookPath("golangci-lint")
	if errors.Is(err, exec.ErrNotFound) {
		t.Skip("golangci-lint not installed (run make test)")
	}
	dir := t.TempDir()
	for _, f := range []string{"go.mod", "go.sum", ".golangci.yml"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bad := filepath.Join(dir, "internal", "core", "bad")
	if err := os.MkdirAll(bad, 0o750); err != nil {
		t.Fatal(err)
	}
	src := `// Package bad breaks the layering rule.
package bad

import (
	"net/http"
	"os/exec"

	"gorm.io/gorm"
)

// Uses keeps the imports.
var Uses = []any{gorm.ErrRecordNotFound, http.MethodGet, exec.ErrNotFound}
`
	if err := os.WriteFile(filepath.Join(bad, "bad.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, lint, "run", "--enable-only", "depguard", "./...")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("golangci-lint passed or did not run (%v):\n%s", err, out.String())
	}
	for _, want := range []string{
		"internal/core/bad/bad.go:8:2: import 'gorm.io/gorm' is not allowed",
		"internal/core/bad/bad.go:5:2: import 'net/http' is not allowed",
		"internal/core/bad/bad.go:6:2: import 'os/exec' is not allowed",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the lint output does not name %s:\n%s", want, out.String())
		}
	}
}
