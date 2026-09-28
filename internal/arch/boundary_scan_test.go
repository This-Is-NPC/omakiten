package arch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// boundaryScan is the result of one arch-boundary sweep: the violations
// found, and the files actually inspected. The inspected set exists
// because the defect this gate was repaired for was not a wrong verdict
// but an empty one — the walk covered zero files and reported green.
// Callers assert on coverage, not only on emptiness.
type boundaryScan struct {
	violations []string
	inspected  []string
}

// covered reports how many inspected files live under prefix.
func (s boundaryScan) covered(prefix string) int {
	count := 0
	for _, rel := range s.inspected {
		if strings.HasPrefix(rel, prefix) {
			count++
		}
	}
	return count
}

func itoa(n int) string { return strconv.Itoa(n) }
func walkGoSources(repo, subdir string, includeTests bool, include func(rel string) bool, inspect func(rel string, data []byte)) error {
	root := filepath.Join(repo, subdir)
	return filepath.WalkDir(root, func(p string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if !includeTests && strings.HasSuffix(name, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(repo, p)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relative)
		if !include(rel) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		inspect(rel, data)
		return nil
	})
}

func repoRootOrFail(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	return root
}
