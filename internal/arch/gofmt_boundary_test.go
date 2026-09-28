package arch

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gofmtBoundaryRoots are the source trees the format gate covers. They are the
// two roots `mise run fmt` actually reformats in this repo — everything else
// under the module root is generated, vendored fixture data, or non-Go.
var gofmtBoundaryRoots = []string{"internal", "cmd"}

// gofmtViolations reports every Go source under gofmtBoundaryRoots whose bytes
// differ from their canonical gofmt rendering, as `path: <hint>` entries.
//
// The check runs through go/format rather than shelling out to the gofmt
// binary, so it is hermetic: it needs nothing on PATH and is pinned to the
// exact toolchain .mise.toml already pins for the build. `mise run fmt:check`
// runs the real `gofmt -l` binary over the same two roots, so the two gates
// cross-check each other inside `mise run check` — if go/format and the gofmt
// binary ever disagreed, one of them would go red rather than both staying
// quiet.
//
// Test files are included deliberately. The drift this gate was written for
// was struct-tag alignment plus doc-comment normalization, and 22 of the 39
// files carrying it were _test.go files; excluding them would have let more
// than half the population drift on unwatched.
func gofmtViolations(repo string) (boundaryScan, error) {
	scan := boundaryScan{}
	for _, root := range gofmtBoundaryRoots {
		err := walkGoSources(repo, root, true, func(string) bool { return true }, func(rel string, data []byte) {
			scan.inspected = append(scan.inspected, rel)
			formatted, err := format.Source(data)
			if err != nil {
				scan.violations = append(scan.violations, rel+": does not parse: "+err.Error())
				return
			}
			if !bytes.Equal(data, formatted) {
				scan.violations = append(scan.violations, rel+": "+gofmtDiffHint(data, formatted))
			}
		})
		if err != nil {
			return scan, err
		}
	}
	sort.Strings(scan.violations)
	sort.Strings(scan.inspected)
	return scan, nil
}

// gofmtDiffHint locates the first line where the file and its canonical
// rendering diverge and renders it as `line N: "got" -> "want"`, so the
// failure message points at the offending spot instead of only naming a file.
func gofmtDiffHint(got, want []byte) string {
	gotLines := strings.Split(string(got), "\n")
	wantLines := strings.Split(string(want), "\n")
	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if gotLines[i] != wantLines[i] {
			return "line " + itoa(i+1) + ": " + quoteLine(gotLines[i]) + " -> " + quoteLine(wantLines[i])
		}
	}
	return "line count " + itoa(len(gotLines)) + " -> " + itoa(len(wantLines))
}

// quoteLine renders a source line for the failure message with tabs made
// visible, because the whole point of a formatting diff is usually invisible
// whitespace.
func quoteLine(line string) string {
	return `"` + strings.ReplaceAll(strings.TrimRight(line, " \t"), "\t", `\t`) + `"`
}

// TestGofmtBoundary enforces that every Go source under internal/ and cmd/ is
// gofmt-clean.
//
// The drift this gate closes had grown to 39 files before anyone noticed,
// because `mise run check` ran tests, lint, govulncheck and docs but never a
// format verification. Nothing failed while the tree drifted, so the cost
// landed on whoever ran `mise run fmt` next: task #2418's Builder got 39
// unrelated files in its diff and had to revert every one by hand.
//
// ENFORCED by default, with no opt-out env var. The rule lands on an already
// swept tree, so a loosening switch could only ever be used to land new drift.
func TestGofmtBoundary(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	scan, err := gofmtViolations(root)
	if err != nil {
		t.Fatalf("walk sources: %v", err)
	}

	// Anti-vacuity: this gate's scope is two directory names. A rename or a
	// package move would silently shrink it to nothing and it would report
	// green while inspecting zero files — the failure mode this family was
	// repaired for. Coverage is asserted per root, not assumed.
	for _, sub := range gofmtBoundaryRoots {
		if covered := scan.covered(sub + "/"); covered == 0 {
			t.Fatalf("gofmt boundary inspected no Go files under %s/; the gate is vacuous", sub)
		}
	}
	t.Logf("gofmt boundary inspected %d Go files (%d under internal/, %d under cmd/)",
		len(scan.inspected), scan.covered("internal/"), scan.covered("cmd/"))

	if len(scan.violations) > 0 {
		t.Fatalf("these files are not gofmt-clean — run `mise run fmt`:\n  - %s", strings.Join(scan.violations, "\n  - "))
	}
}

// TestGofmtBoundaryGateRejectsUnformatted is the paired meta-test: it seeds a
// synthetic tree with the exact drift shapes the gate must reject and the
// clean shapes it must let through, and asserts the scanner's verdict. Without
// it the gate could silently stop detecting anything — a formatting gate that
// cannot fail is worse than no gate, because it advertises a guarantee it is
// not providing.
func TestGofmtBoundaryGateRejectsUnformatted(t *testing.T) {
	cases := []struct {
		name       string
		file       string
		source     string
		wantIssues bool
	}{
		{
			name:       "misaligned struct tags — the exact drift this gate was written for",
			file:       "internal/synthetic/tags.go",
			source:     "package synthetic\n\ntype T struct {\n\tID int `json:\"id\"`\n\tLongerName string `json:\"longer_name\"`\n}\n",
			wantIssues: true,
		},
		{
			name:       "space indentation instead of tabs",
			file:       "internal/synthetic/indent.go",
			source:     "package synthetic\n\nfunc F() int {\n    return 1\n}\n",
			wantIssues: true,
		},
		{
			name:       "stray blank lines inside a block",
			file:       "internal/synthetic/blanks.go",
			source:     "package synthetic\n\nfunc G() int {\n\n\n\treturn 2\n}\n",
			wantIssues: true,
		},
		{
			name:       "unaligned consecutive assignments",
			file:       "internal/synthetic/align.go",
			source:     "package synthetic\n\nvar (\n\ta = 1\n\tbbbb = 2\n)\n",
			wantIssues: true,
		},
		{
			name:       "drift under cmd/ is covered too, not only internal/",
			file:       "cmd/synthetic/main.go",
			source:     "package main\n\nfunc main() {\n    _ = 1\n}\n",
			wantIssues: true,
		},
		{
			name:       "a _test.go file is in scope, not skipped",
			file:       "internal/synthetic/synthetic_test.go",
			source:     "package synthetic\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n    _ = t\n}\n",
			wantIssues: true,
		},
		{
			name:       "a file that does not parse is reported rather than passed over",
			file:       "internal/synthetic/broken.go",
			source:     "package synthetic\n\nfunc H( {\n",
			wantIssues: true,
		},
		{
			name:   "canonically formatted source passes",
			file:   "internal/synthetic/clean.go",
			source: "package synthetic\n\ntype U struct {\n\tID         int    `json:\"id\"`\n\tLongerName string `json:\"longer_name\"`\n}\n\nfunc I() int { return 3 }\n",
		},
		{
			name:   "canonically formatted cmd source passes",
			file:   "cmd/synthetic/clean.go",
			source: "package main\n\nfunc helper() int { return 4 }\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runGofmtBoundaryCase(t, tc.file, tc.source, tc.wantIssues)
		})
	}
}

func runGofmtBoundaryCase(t *testing.T, file, source string, wantIssues bool) {
	t.Helper()
	repo := t.TempDir()
	// Both roots always exist: gofmtViolations walks them strictly, so
	// a vanished root surfaces as a hard error rather than a quietly
	// narrower scope.
	for _, sub := range gofmtBoundaryRoots {
		if err := os.MkdirAll(filepath.Join(repo, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	target := filepath.Join(repo, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte(source), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	scan, err := gofmtViolations(repo)
	if err != nil {
		t.Fatalf("gofmtViolations: %v", err)
	}
	if got := len(scan.violations) > 0; got != wantIssues {
		t.Fatalf("violations=%v, want %v\n  scan: %v", scan.violations, wantIssues, scan.inspected)
	}
	// The report must name the offending file, since that is what a
	// contributor needs in order to act on the failure.
	if wantIssues && !strings.Contains(strings.Join(scan.violations, "\n"), file) {
		t.Fatalf("failure message does not name %q: %v", file, scan.violations)
	}
}
