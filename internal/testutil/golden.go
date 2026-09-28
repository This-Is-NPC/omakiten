package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateGolden is THE switch that rewrites a golden fixture. There is exactly
// one, on purpose.
//
// The tree used to carry two: internal/tui gated its writer on this flag while
// six screen packages gated theirs on `os.Getenv("UPDATE_GOLDEN") == "1"`. The
// flag survived the merge and the environment variable was deleted, because
// the two switches fail differently under pressure:
//
//   - A flag has to be typed on the command line of the run that is meant to
//     rewrite something. It cannot be left behind.
//   - An exported environment variable is inherited by every child process of
//     the shell that set it, for the rest of that shell's life. A single stale
//     `export UPDATE_GOLDEN=1` turns every later `go test ./...` into a silent
//     mass-regeneration — and a regenerated fixture is a green test, so
//     nothing reports it.
//
// Registering the flag here rather than per package has a second effect worth
// keeping: `go test ./... -update` does not work. Packages that hold no
// fixtures never link this helper, so they reject the unknown flag and the run
// fails before anything is written. Refreshing is therefore a per-package act:
//
//	go test ./internal/tui/screens/project -update
//
// A plain `go test ./...` leaves every fixture byte-identical; that property
// is what lets a refactor's golden diff be read as evidence.
var updateGolden = flag.Bool("update", false, "rewrite the golden fixtures under testdata/ instead of asserting against them")

// GoldenRefreshRequested reports whether this run was started with -update and
// is therefore allowed to rewrite fixtures. Tests that build an expensive
// fixture only to discard it during a refresh run can branch on it; assertions
// should just call Golden.
func GoldenRefreshRequested() bool { return *updateGolden }

// Golden asserts that got matches the fixture stored at `testdata/<name>`, and
// is the only supported way to read or write a golden file in this repo.
//
// name is slash-separated and relative to the calling package's testdata
// directory, extension included — "home.80x24.view.golden",
// "screens/stats_general.view.golden". Keeping the testdata/ prefix inside the
// helper is what makes the fixture layout uniform across packages.
//
// Comparison is byte-exact. A golden file is a characterization record: the
// refactor waves that assert "this change rendered nothing differently" are
// only as strong as the equality they assert, so a trailing-newline tolerance
// would quietly widen every one of them.
//
// A mismatch is reported with Errorf, not Fatalf, so a loop over a screen's
// fixtures reports every drift in one run instead of stopping at the first.
// A missing fixture is reported the same way, with the rendered output
// attached so the intended content is reviewable before it is written.
//
// With -update the fixture is rewritten from got and no assertion runs. See
// updateGolden for why that switch is a flag and never an environment
// variable.
func Golden(tb testing.TB, name, got string) {
	tb.Helper()
	path := filepath.Join("testdata", filepath.FromSlash(name))
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			tb.Errorf("golden %s: create fixture directory: %v", name, err)
			return
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			tb.Errorf("golden %s: write fixture: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		tb.Errorf("golden %s: %v — create it with `go test <package> -update` once the output below is correct\n--- got ---\n%s", name, err, got)
		return
	}
	if got != string(want) {
		tb.Errorf("golden %s mismatch — refresh with `go test <package> -update` only when the change is intended\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

// GoldenNewlineTerminated is Golden for a fixture that predates this harness
// and was saved with the customary final newline of a POSIX text file — a byte
// its subject does not emit. It appends that newline to got and defers to
// Golden, so the fixture asserts byte-for-byte like every other one and a
// refresh rewrites it unchanged.
//
// It exists so the tolerance stays confined to the fixtures that need it.
// Folding a trailing-newline tolerance into Golden itself would blind every
// fixture in the tree to a drift in its final byte — and a layout refactor
// gaining or losing a trailing blank line is exactly the regression the
// characterization suites are there to catch.
//
// # Remaining consumer
//
// Exactly one, named here rather than left to be discovered:
//
//   - internal/sqlite/search_integrity_test.go, for
//     internal/sqlite/testdata/search_integrity_mixed.golden.
//
// It started with three. The other two were internal/tui/screens/home and
// internal/tui/screens/plannetwork, both re-recorded at three geometries
// through screentest.Record and moved to the strict Golden; their legacy
// fixtures are gone. The sqlite one survives because nothing in that work
// regenerates it — it is a search-index integrity report, not a screen render,
// and rewriting a fixture no change touches would spend the characterization
// baseline it exists to be.
//
// Retiring it is a two-line change whenever that fixture is next legitimately
// regenerated: call Golden with the encoded report verbatim, drop the
// fixture's final newline, and delete this function along with the
// GoldenNewlineTerminated mention in internal/arch's goldenRefreshAllowlist
// reason for that file.
func GoldenNewlineTerminated(tb testing.TB, name, got string) {
	tb.Helper()
	Golden(tb, name, got+"\n")
}
