// okt-docs-refresh validates the hand-authored documentation tree and removes
// legacy generated-doc artifacts. Run via `mise run docs:refresh`. See
// .docs/internal/authoring.md.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var legacyMarkerRe = regexp.MustCompile(`<!--\s*(BEGIN\s+(?:include:[^>]+|auto:[^>]+)|END\s+(?:include|auto)|SECTION:[^>]+|END\s+SECTION)\s*-->`)

func main() {
	root := flag.String("root", ".", "project root containing .docs/")
	check := flag.Bool("check", false, "exit non-zero if legacy docs-refresh artifacts are present")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fail("resolve root: %v", err)
	}
	if err := run(abs, *check); err != nil {
		fail("%v", err)
	}
}

func run(root string, check bool) error {
	docsDir := filepath.Join(root, ".docs")
	info, err := os.Stat(docsDir)
	if err != nil {
		return fmt.Errorf("stat .docs: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf(".docs is not a directory")
	}

	problems := []string{}
	generatedDir := filepath.Join(docsDir, "_generated")
	if _, err := os.Stat(generatedDir); err == nil {
		if check {
			problems = append(problems, ".docs/_generated exists")
		} else if err := os.RemoveAll(generatedDir); err != nil {
			return fmt.Errorf("remove .docs/_generated: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat .docs/_generated: %w", err)
	}

	markers, err := findLegacyMarkers(docsDir)
	if err != nil {
		return err
	}
	problems = append(problems, markers...)

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("legacy docs-refresh artifacts detected:\n  - %s\nremove them or run `mise run docs:refresh`. See .docs/internal/authoring.md", strings.Join(problems, "\n  - "))
	}
	return nil
}

func findLegacyMarkers(docsDir string) ([]string, error) {
	problems := []string{}
	err := filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "_generated" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fences := codeFenceSpans(raw)
		matches := legacyMarkerRe.FindAllIndex(raw, -1)
		for _, match := range matches {
			if insideAny(match[0], fences) {
				continue
			}
			rel, err := filepath.Rel(filepath.Dir(docsDir), path)
			if err != nil {
				return err
			}
			line := lineNumber(raw, match[0])
			marker := strings.TrimSpace(string(raw[match[0]:match[1]]))
			problems = append(problems, fmt.Sprintf("%s:%d contains legacy marker %s", rel, line, marker))
		}
		return nil
	})
	return problems, err
}

type span struct{ start, end int }

// codeFenceSpans returns byte ranges (start inclusive, end exclusive) covered
// by triple-backtick fenced code blocks. Legacy marker examples are allowed
// inside fenced snippets, but not in live markdown.
func codeFenceSpans(b []byte) []span {
	var out []span
	lines := bytes.Split(b, []byte("\n"))
	offset := 0
	openStart := -1
	for _, line := range lines {
		trimmed := bytes.TrimLeft(line, " \t")
		if bytes.HasPrefix(trimmed, []byte("```")) {
			if openStart < 0 {
				openStart = offset
			} else {
				out = append(out, span{openStart, offset + len(line) + 1})
				openStart = -1
			}
		}
		offset += len(line) + 1
	}
	return out
}

func insideAny(pos int, spans []span) bool {
	for _, s := range spans {
		if pos >= s.start && pos < s.end {
			return true
		}
	}
	return false
}

func lineNumber(b []byte, pos int) int {
	if pos > len(b) {
		pos = len(b)
	}
	return bytes.Count(b[:pos], []byte("\n")) + 1
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "okt-docs-refresh: "+format+"\n", args...)
	os.Exit(1)
}
