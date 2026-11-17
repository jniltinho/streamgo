package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// The documentation scenarios: "Index is complete", "No dead links" and "Every diagram has both formats".

var (
	mdLink   = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	fence    = regexp.MustCompile("(?ms)^```.*?^```")
	heading  = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)
	inDiagrm = regexp.MustCompile(`images/(diagram-[a-z0-9-]+)\.(?:svg|png)`)
)

// docFiles is the root README and every Markdown file under docs/.
func docFiles(t *testing.T) []string {
	t.Helper()
	files := []string{"README.md"}
	err := filepath.WalkDir("docs", func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// links returns the relative link targets of a Markdown file, outside fenced code blocks.
func links(text string) []string {
	var out []string
	for _, m := range mdLink.FindAllStringSubmatch(fence.ReplaceAllString(text, ""), -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		out = append(out, target)
	}
	return out
}

// slug is GitHub's anchor for a heading: lower case, punctuation dropped, spaces as hyphens.
func slug(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func anchors(text string) map[string]bool {
	set := map[string]bool{}
	for _, m := range heading.FindAllStringSubmatch(fence.ReplaceAllString(text, ""), -1) {
		set[slug(m[1])] = true
	}
	return set
}

func TestDocsIndexIsComplete(t *testing.T) {
	index := links(readDoc(t, "docs/README.md"))
	for _, want := range []string{"architecture.md", "install-linux.md", "install-binary.md", "install-docker.md",
		"standalone-https.md", "reverse-proxy.md", "deployment.md", "security.md"} {
		found := false
		for _, l := range index {
			found = found || strings.SplitN(l, "#", 2)[0] == want
		}
		if !found {
			t.Errorf("docs/README.md does not link %s", want)
		}
		if _, err := os.Stat(filepath.Join("docs", want)); err != nil {
			t.Errorf("docs/%s: %v", want, err)
		}
	}
	found := false
	for _, l := range links(readDoc(t, "README.md")) {
		found = found || l == "docs/README.md"
	}
	if !found {
		t.Error("the root README.md does not link docs/README.md")
	}
}

func TestDocsNoDeadLinks(t *testing.T) {
	for _, file := range docFiles(t) {
		text := readDoc(t, file)
		for _, target := range links(text) {
			path, frag, _ := strings.Cut(target, "#")
			resolved := file
			if path != "" {
				resolved = filepath.Join(filepath.Dir(file), path)
				if _, err := os.Stat(resolved); err != nil {
					t.Errorf("%s: dead link %q", file, target)
					continue
				}
			}
			if frag != "" && strings.HasSuffix(resolved, ".md") && !anchors(readDoc(t, resolved))[frag] {
				t.Errorf("%s: link %q: no heading #%s in %s", file, target, frag, resolved)
			}
		}
	}
}

func TestDocsDiagrams(t *testing.T) {
	used := map[string]bool{}
	for _, file := range docFiles(t) {
		for _, m := range inDiagrm.FindAllStringSubmatch(readDoc(t, file), -1) {
			used[m[1]] = true
		}
	}
	svgs, err := filepath.Glob("docs/images/diagram-*.svg")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, svg := range svgs {
		name := strings.TrimSuffix(filepath.Base(svg), ".svg")
		have[name] = true
		if _, err := os.Stat(strings.TrimSuffix(svg, ".svg") + ".png"); err != nil {
			t.Errorf("%s has no PNG of the same name", svg)
		}
		if !used[name] {
			t.Errorf("%s is not used by any document", svg)
		}
	}
	// The diagrams the documentation spec names.
	for _, want := range []string{"diagram-overview", "diagram-media-path", "diagram-go-live", "diagram-output-states",
		"diagram-deploy-packages", "diagram-deploy-docker", "diagram-deploy-reverse-proxy",
		"diagram-deploy-standalone-https", "diagram-ports"} {
		if !have[want] {
			t.Errorf("docs/images/%s.svg is missing", want)
		}
	}
}
