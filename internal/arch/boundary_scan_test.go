package arch

import (
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

func repoRootOrFail(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	return root
}
