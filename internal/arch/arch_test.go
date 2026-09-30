// Package arch hosts the hexagonal-boundary enforcement test that ships with
// the repo. The test walks every non-test Go file under internal/ and asserts
// that the import graph respects the directions documented in
// internal/app/doc.go: domain has no adapter dependencies, app does not pull
// in concrete adapters, delivery adapters (cli/tui) do not import app, screens
// do not import the operation facade, and adapters do not depend on each
// other in ways that would re-introduce coupling.
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
		from:      "internal/domain",
		forbidden: []string{"internal/"},
		reason:    "domain is the inner core; it cannot depend on adapters or application services",
	},
	{
		from: "internal/app",
		forbidden: []string{
			"internal/sqlite", "internal/configstore",
			"internal/tui", "internal/cli", "internal/operation",
			"internal/agentruntime",
		},
		reason: "app talks to adapters via ports declared in app/ports.go; concrete adapter imports invert the hex direction",
	},
	{
		from: "internal/sqlite",
		forbidden: []string{
			"internal/app", "internal/configstore", "internal/tui", "internal/cli",
			"internal/operation", "internal/agentruntime",
		},
		reason: "sqlite is a leaf adapter; depending on app or sibling adapters would cycle the dependency graph",
	},
	{
		from: "internal/configstore",
		forbidden: []string{
			"internal/app", "internal/sqlite", "internal/tui", "internal/cli",
			"internal/operation", "internal/agentruntime",
		},
		reason: "configstore is a leaf adapter for config I/O; depending on app or sibling adapters cycles the graph",
	},
	{
		from: "internal/agentruntime",
		forbidden: []string{
			"internal/tui", "internal/cli", "internal/terminal", "internal/httpapi", "internal/daemon",
		},
		reason: "agentruntime is the headless composition root; TUI delivery must stay behind neutral hook actions and sender ports",
	},
	{
		from:      "internal/tui/components",
		forbidden: []string{"internal/config", "internal/domain", "internal/app", "internal/sqlite"},
		reason:    "components are presentation primitives; they must stay independent of configuration, domain, application, and storage layers",
	},
	ruleCLINoApp,
	ruleTUINoApp,
	ruleScreensNoOperation,
	{from: "internal/tui", forbidden: []string{"internal/cli", "internal/operation", "internal/agentruntime", "internal/configstore", "internal/sqlite", "internal/recovery", "internal/httpapi", "internal/daemon"}, reason: "tui receives neutral contracts and ports"},
	{from: "internal/cli", forbidden: []string{"internal/tui", "internal/terminal", "internal/httpapi", "internal/daemon"}, reason: "cli receives injected delivery runners"},
	ruleHTTPAPIPorts,
	{from: "internal/filelock", forbidden: []string{"internal/"}, reason: "filelock is a leaf primitive"},
	{from: "internal/contract", forbidden: []string{"internal/app", "internal/operation", "internal/agentruntime", "internal/sqlite", "internal/configstore", "internal/recovery", "internal/tui", "internal/cli", "internal/terminal", "internal/updater", "internal/httpapi", "internal/daemon"}, reason: "contracts contain no implementations"},
	{from: "internal/config", forbidden: []string{"internal/tui", "internal/cli", "internal/terminal", "internal/httpapi", "internal/daemon"}, reason: "configuration is independent of delivery"},
	{from: "internal/app", forbidden: []string{"internal/recovery", "internal/terminal", "internal/hooks"}, reason: "application services use adapter ports"},
	{from: "internal/operation", forbidden: []string{"internal/tui", "internal/cli", "internal/terminal", "internal/httpapi", "internal/daemon"}, reason: "operations are independent of their consumers"},
}

// The HTTP adapter reaches runtimes and storage only through its ports.
var ruleHTTPAPIPorts = rule{
	from:      "internal/httpapi",
	forbidden: []string{"internal/app", "internal/agentruntime", "internal/sqlite", "internal/configstore", "internal/recovery", "internal/cli", "internal/tui", "internal/terminal", "internal/daemon"},
	reason:    "httpapi receives operation and runtime ports; the daemon binds them",
}

// Delivery adapters cannot bypass their operation contracts to call application services.
var ruleCLINoApp = rule{
	from:      "internal/cli",
	forbidden: []string{"internal/app"},
	reason:    "cli consumes operation.Service; importing application services bypasses the facade",
}

var ruleTUINoApp = rule{
	from:      "internal/tui",
	forbidden: []string{"internal/app"},
	reason:    "tui consumes neutral contracts; importing application services bypasses its ports",
}

// Screens depend on the TUI host and neutral contracts.
var ruleScreensNoOperation = rule{
	from:      "internal/tui/screens",
	forbidden: []string{"internal/operation"},
	reason:    "screens talk to the TUI host and cannot import operation implementations",
}

// deliveryTestRules are A1+A2 scanned again with _test.go included. The
// production hex walk skips tests (matching depguard's global _test.go
// exclusion); D1/A2 greps included tests, so a dedicated pass has to as
// well. These rules are scoped to cli/tui/screens — enabling the full
// hexagonal set on tests would fail unrelated packages (sqlite tests
// import app, app tests import sqlite).
var deliveryTestRules = []rule{
	ruleCLINoApp,
	ruleTUINoApp,
	ruleScreensNoOperation,
	ruleHTTPAPIPorts,
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

	scan, err := scanHex(internalDir, hexagonalRules, false)
	if err != nil {
		t.Fatalf("scanViolations: %v", err)
	}
	assertHexScanNotVacuous(t, scan, 200)
	components := scan.covered("tui/components/")
	if components < 60 {
		t.Fatalf("hexagonal boundary inspected components=%d files, want at least 60 — the components rule is looking at the wrong tree", components)
	}
	t.Logf("hexagonal boundary inspected components=%d files", components)
	if len(scan.violations) == 0 {
		return
	}
	sort.Strings(scan.violations)
	t.Fatalf("hexagonal boundary violations:\n  - %s", strings.Join(scan.violations, "\n  - "))
}

// TestDeliveryBoundariesIncludeTests is A1+A2 over _test.go as well as
// production files. D1 grepped cli/tui (including tests) for internal/app;
// A2 grepped screens (including tests) for internal/operation. The main hex
// walk skips tests, matching depguard; this pass closes that hole without
// turning depguard on for every package's tests.
func TestDeliveryBoundariesIncludeTests(t *testing.T) {
	repoRoot, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	internalDir := filepath.Join(repoRoot, "internal")

	scan, err := scanHex(internalDir, deliveryTestRules, true)
	if err != nil {
		t.Fatalf("scanHex: %v", err)
	}
	cli := scan.covered("cli/")
	tui := scan.covered("tui/")
	screens := scan.covered("tui/screens/")
	if cli < 40 || tui < 100 || screens < 50 {
		t.Fatalf("delivery boundary inspected cli=%d tui=%d screens=%d; want at least 40/100/50 — the walker is looking at the wrong tree",
			cli, tui, screens)
	}
	t.Logf("delivery boundary inspected cli=%d tui=%d screens=%d (including tests)", cli, tui, screens)
	if len(scan.violations) == 0 {
		return
	}
	sort.Strings(scan.violations)
	t.Fatalf("delivery boundary violations in tests:\n  - %s", strings.Join(scan.violations, "\n  - "))
}

// TestHexagonalRulesIncludeDeliveryGates fails if A1/A2 are dropped from
// hexagonalRules while a local copy of the rules keeps the other tests green.
func TestHexagonalRulesIncludeDeliveryGates(t *testing.T) {
	want := []struct{ from, forbidden string }{
		{"internal/cli", "internal/app"},
		{"internal/tui", "internal/app"},
		{"internal/tui/screens", "internal/operation"},
		{"internal/tui/components", "internal/domain"},
		{"internal/tui/components", "internal/config"},
		{"internal/tui/components", "internal/app"},
		{"internal/tui/components", "internal/sqlite"},
	}
	for _, w := range want {
		if !hexRuleForbids(hexagonalRules, w.from, w.forbidden) {
			t.Errorf("hexagonalRules missing %s ↛ %s", w.from, w.forbidden)
		}
	}
}

func hexRuleForbids(rules []rule, from, forbidden string) bool {
	for _, r := range rules {
		if r.from != from {
			continue
		}
		for _, f := range r.forbidden {
			if f == forbidden {
				return true
			}
		}
	}
	return false
}

func assertHexScanNotVacuous(t *testing.T, scan boundaryScan, floor int) {
	t.Helper()
	if len(scan.inspected) < floor {
		t.Fatalf("hexagonal scan inspected %d files, want at least %d — the walker is looking at the wrong tree",
			len(scan.inspected), floor)
	}
}

// TestHexagonalScanRejectsSeededImports is the paired meta-test: plant the
// imports A1/A2 must reject (and the host-facade import they must let
// through) in a temp tree and assert the scanner's verdict. Without it the
// live-tree tests can pass forever while matching nothing.
func TestHexagonalScanRejectsSeededImports(t *testing.T) {
	cases := []struct {
		name         string
		file         string
		source       string
		includeTests bool
		wantIssues   bool
	}{
		{
			name:       "cli production imports app",
			file:       "cli/bad.go",
			source:     "package cli\nimport \"omakiten/internal/app\"\n",
			wantIssues: true,
		},
		{
			name:       "tui host production imports app",
			file:       "tui/bad.go",
			source:     "package tui\nimport \"omakiten/internal/app\"\n",
			wantIssues: true,
		},
		{
			name:       "screens production imports app",
			file:       "tui/screens/bad.go",
			source:     "package screens\nimport \"omakiten/internal/app\"\n",
			wantIssues: true,
		},
		{
			name:       "screens production imports operation",
			file:       "tui/screens/bad.go",
			source:     "package screens\nimport \"omakiten/internal/operation\"\n",
			wantIssues: true,
		},
		{
			name:       "screens subpackage production imports operation",
			file:       "tui/screens/home/bad.go",
			source:     "package home\nimport \"omakiten/internal/operation\"\n",
			wantIssues: true,
		},
		{
			name:       "components import domain",
			file:       "tui/components/bad.go",
			source:     "package components\nimport \"omakiten/internal/domain\"\n",
			wantIssues: true,
		},
		{
			name:       "components import config",
			file:       "tui/components/bad.go",
			source:     "package components\nimport \"omakiten/internal/config\"\n",
			wantIssues: true,
		},
		{
			name:       "components import app",
			file:       "tui/components/bad.go",
			source:     "package components\nimport \"omakiten/internal/app\"\n",
			wantIssues: true,
		},
		{
			name:       "components import sqlite",
			file:       "tui/components/bad.go",
			source:     "package components\nimport \"omakiten/internal/sqlite\"\n",
			wantIssues: true,
		},
		{
			name:       "tui host production rejects operation",
			wantIssues: true,
			file:       "tui/host.go",
			source:     "package tui\nimport \"omakiten/internal/operation\"\n",
		},
		{
			name:   "cli production may import operation",
			file:   "cli/ok.go",
			source: "package cli\nimport \"omakiten/internal/operation\"\n",
		},
		{
			name:   "cli test imports app are skipped without includeTests",
			file:   "cli/bad_test.go",
			source: "package cli\nimport \"omakiten/internal/app\"\n",
		},
		{
			name:         "cli test imports app are caught with includeTests",
			file:         "cli/bad_test.go",
			source:       "package cli\nimport \"omakiten/internal/app\"\n",
			includeTests: true,
			wantIssues:   true,
		},
		{
			name:         "screens test imports operation are caught with includeTests",
			file:         "tui/screens/bad_test.go",
			source:       "package screens\nimport \"omakiten/internal/operation\"\n",
			includeTests: true,
			wantIssues:   true,
		},
		{
			name:         "tui host test rejects operation",
			wantIssues:   true,
			file:         "tui/host_test.go",
			source:       "package tui\nimport \"omakiten/internal/operation\"\n",
			includeTests: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			internalDir := t.TempDir()
			target := filepath.Join(internalDir, filepath.FromSlash(tc.file))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatalf("mkdir fixture: %v", err)
			}
			if err := os.WriteFile(target, []byte(tc.source), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			scan, err := scanHex(internalDir, hexagonalRules, tc.includeTests)
			if err != nil {
				t.Fatalf("scan fixture: %v", err)
			}
			if got := len(scan.violations) > 0; got != tc.wantIssues {
				t.Fatalf("violations = %v, want issues %v", scan.violations, tc.wantIssues)
			}
		})
	}
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

func scanHex(internalDir string, rules []rule, includeTests bool) (boundaryScan, error) {
	scan := boundaryScan{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		return scanHexFile(internalDir, path, fset, rules, includeTests, &scan)
	})
	return scan, err
}

func scanHexFile(internalDir, path string, fset *token.FileSet, rules []rule, includeTests bool, scan *boundaryScan) error {
	if !strings.HasSuffix(path, ".go") || (!includeTests && strings.HasSuffix(path, "_test.go")) {
		return nil
	}
	rel, err := filepath.Rel(internalDir, path)
	if err != nil {
		return err
	}
	relSlash := filepath.ToSlash(rel)
	scan.inspected = append(scan.inspected, relSlash)
	// "internal/<pkg>/..." form for matching against the rule "from" prefix.
	pkgRel := "internal/" + filepath.ToSlash(filepath.Dir(rel))

	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return err
	}
	for _, imp := range f.Imports {
		scanHexImport(relSlash, pkgRel, strings.Trim(imp.Path.Value, "\""), rules, scan)
	}
	return nil
}

func scanHexImport(relSlash, pkgRel, raw string, rules []rule, scan *boundaryScan) {
	if !strings.HasPrefix(raw, modulePath) {
		return
	}
	importedRel := strings.TrimPrefix(raw, modulePath)
	for _, r := range rules {
		if !strings.HasPrefix(pkgRel, r.from) {
			continue
		}
		for _, forbidden := range r.forbidden {
			if strings.HasPrefix(importedRel, forbidden) {
				scan.violations = append(scan.violations, formatViolation(relSlash, raw, r))
			}
		}
	}
}

func formatViolation(filePath, importPath string, r rule) string {
	return filePath + " imports " + importPath + " — forbidden by `" + r.from + "`: " + r.reason
}
