// Package arch hosts the hexagonal-boundary enforcement test that ships with
// the repo. The test walks every non-test Go file under internal/ and asserts
// that the import graph respects the directions documented in
// internal/app/doc.go: domain has no adapter dependencies, app does not pull
// in concrete adapters, and adapters do not depend on each other in ways that
// would re-introduce coupling.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const modulePath = "omakiten/"

// rule expresses one forbidden import edge: every package whose import path
// matches `from` (prefix-match) must not import any path starting with one
// of the prefixes in `forbidden`.
type rule struct {
	from      string
	forbidden []string
	reason    string
}

var hexagonalRules = []rule{
	{
		from: "internal/domain",
		forbidden: []string{
			"internal/sqlite", "internal/config", "internal/configstore",
			"internal/tui", "internal/cli", "internal/mcp", "internal/agent",
			"internal/app", "internal/agentruntime",
		},
		reason: "domain is the inner core; it cannot depend on adapters or application services",
	},
	{
		from: "internal/app",
		forbidden: []string{
			"internal/sqlite", "internal/configstore",
			"internal/tui", "internal/cli", "internal/mcp", "internal/agent",
			"internal/agentruntime",
		},
		reason: "app talks to adapters via ports declared in app/ports.go; concrete adapter imports invert the hex direction",
	},
	{
		from: "internal/sqlite",
		forbidden: []string{
			"internal/app", "internal/configstore", "internal/tui", "internal/cli",
			"internal/mcp", "internal/agent", "internal/agentruntime",
		},
		reason: "sqlite is a leaf adapter; depending on app or sibling adapters would cycle the dependency graph",
	},
	{
		from: "internal/configstore",
		forbidden: []string{
			"internal/app", "internal/sqlite", "internal/tui", "internal/cli",
			"internal/mcp", "internal/agent", "internal/agentruntime",
		},
		reason: "configstore is a leaf adapter for config I/O; depending on app or sibling adapters cycles the graph",
	},
	{
		from: "internal/agentruntime",
		forbidden: []string{
			"internal/tui",
		},
		reason: "agentruntime is the headless agent/MCP composition root; TUI delivery must stay behind neutral hook actions and sender ports",
	},
}

// TestHexagonalBoundaries scans every non-test Go file in internal/ and
// reports any forbidden cross-package import. Failures point at the file
// and the offending import so the fix is mechanical.
func TestHexagonalBoundaries(t *testing.T) {
	repoRoot, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	internalDir := filepath.Join(repoRoot, "internal")

	violations, err := scanViolations(internalDir, hexagonalRules)
	if err != nil {
		t.Fatalf("scanViolations: %v", err)
	}
	if len(violations) == 0 {
		return
	}
	sort.Strings(violations)
	t.Fatalf("hexagonal boundary violations:\n  - %s", strings.Join(violations, "\n  - "))
}

// repoRoot walks up from this file's location looking for go.mod so the
// scanner is independent of the test's working directory.
func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func scanViolations(internalDir string, rules []rule) ([]string, error) {
	var violations []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(internalDir, path)
		if err != nil {
			return err
		}
		// "internal/<pkg>/..." form for matching against the rule "from" prefix.
		pkgRel := "internal/" + filepath.ToSlash(filepath.Dir(rel))

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			raw := strings.Trim(imp.Path.Value, "\"")
			if !strings.HasPrefix(raw, modulePath) {
				continue
			}
			importedRel := strings.TrimPrefix(raw, modulePath)
			for _, r := range rules {
				if !strings.HasPrefix(pkgRel, r.from) {
					continue
				}
				for _, forbidden := range r.forbidden {
					if strings.HasPrefix(importedRel, forbidden) {
						violations = append(violations, formatViolation(rel, raw, r))
					}
				}
			}
		}
		return nil
	})
	return violations, err
}

func formatViolation(filePath, importPath string, r rule) string {
	return filePath + " imports " + importPath + " — forbidden by `" + r.from + "`: " + r.reason
}
