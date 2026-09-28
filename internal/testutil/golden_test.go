package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingTB stands in for *testing.T so a helper that is supposed to fail
// can be asserted on instead of failing the run. testing.TB carries an
// unexported method, so the real interface is embedded; only the methods
// Golden actually calls are overridden, and the embedded value is a live *T so
// an unexpected call panics loudly rather than nil-dereferencing silently.
type recordingTB struct {
	testing.TB
	errors []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, format)
	_ = args
}

// goldenSandbox moves the test into a scratch directory with a testdata/ tree,
// so Golden resolves its relative path there rather than against the real
// fixtures of this package.
func goldenSandbox(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("seed testdata dir: %v", err)
	}
}

// withRefresh turns the -update switch on for the duration of the test through
// the real flag plumbing, so the test exercises the same path a contributor
// gets from `go test <package> -update`.
func withRefresh(t *testing.T) {
	t.Helper()
	if err := flag.Set("update", "true"); err != nil {
		t.Fatalf("set -update: %v", err)
	}
	t.Cleanup(func() {
		if err := flag.Set("update", "false"); err != nil {
			t.Fatalf("reset -update: %v", err)
		}
	})
}

// TestGoldenLeavesFixturesUntouchedWithoutTheRefreshSwitch is the acceptance
// test for the whole convention: a run that did not ask for a refresh must
// report the drift and leave the bytes on disk exactly as it found them. If
// this ever inverts, the characterization fixtures the layout-standardization
// waves lean on stop being evidence — a refactor would rewrite the record of
// what it changed.
func TestGoldenLeavesFixturesUntouchedWithoutTheRefreshSwitch(t *testing.T) {
	goldenSandbox(t)
	path := filepath.Join("testdata", "view.golden")
	if err := os.WriteFile(path, []byte("recorded output\n"), 0o644); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	stub := &recordingTB{TB: t}
	Golden(stub, "view.golden", "drifted output\n")

	if len(stub.errors) != 1 {
		t.Fatalf("drift against a fixture reported %d errors, want exactly 1", len(stub.errors))
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture back: %v", err)
	}
	if string(after) != "recorded output\n" {
		t.Fatalf("fixture was rewritten without -update: %q", string(after))
	}
}

// TestGoldenComparesBytesExactly pins the equality Golden asserts. A trailing
// newline is the drift a tolerant comparison hides, and the one an editor
// introduces by accident.
func TestGoldenComparesBytesExactly(t *testing.T) {
	goldenSandbox(t)
	if err := os.WriteFile(filepath.Join("testdata", "view.golden"), []byte("body\n"), 0o644); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}

	tolerant := &recordingTB{TB: t}
	Golden(tolerant, "view.golden", "body")
	if len(tolerant.errors) != 1 {
		t.Fatalf("a missing trailing newline reported %d errors, want exactly 1", len(tolerant.errors))
	}

	exact := &recordingTB{TB: t}
	Golden(exact, "view.golden", "body\n")
	if len(exact.errors) != 0 {
		t.Fatalf("byte-identical output reported errors: %v", exact.errors)
	}
}

// TestGoldenReportsAMissingFixtureWithoutCreatingIt keeps a first run honest:
// an absent fixture is a failure that shows the rendered output for review, not
// a silent creation that turns whatever the code does today into the contract.
func TestGoldenReportsAMissingFixtureWithoutCreatingIt(t *testing.T) {
	goldenSandbox(t)

	stub := &recordingTB{TB: t}
	Golden(stub, "absent.golden", "rendered")

	if len(stub.errors) != 1 {
		t.Fatalf("missing fixture reported %d errors, want exactly 1", len(stub.errors))
	}
	if _, err := os.Stat(filepath.Join("testdata", "absent.golden")); !os.IsNotExist(err) {
		t.Fatalf("missing fixture was created without -update (stat err = %v)", err)
	}
}

// TestGoldenRewritesFixturesWithTheRefreshSwitch proves the deliberate switch
// still works, including the nested fixture directories the observability
// snapshots use, and that it writes got byte for byte rather than a
// normalisation of it.
func TestGoldenRewritesFixturesWithTheRefreshSwitch(t *testing.T) {
	goldenSandbox(t)
	withRefresh(t)

	stub := &recordingTB{TB: t}
	Golden(stub, "screens/nested.view.golden", "fresh output")
	if len(stub.errors) != 0 {
		t.Fatalf("refresh reported errors: %v", stub.errors)
	}

	written, err := os.ReadFile(filepath.Join("testdata", "screens", "nested.view.golden"))
	if err != nil {
		t.Fatalf("read refreshed fixture: %v", err)
	}
	if string(written) != "fresh output" {
		t.Fatalf("refreshed fixture = %q, want the rendered bytes verbatim", string(written))
	}
	if !GoldenRefreshRequested() {
		t.Fatal("GoldenRefreshRequested() = false during a -update run")
	}
}

// TestGoldenRefreshIsOffByDefault guards the default value of the switch: the
// whole convention rests on it, and a copy-paste that flips the flag default
// would be invisible in every other test.
func TestGoldenRefreshIsOffByDefault(t *testing.T) {
	entry := flag.Lookup("update")
	if entry == nil {
		t.Fatal("the -update flag is not registered")
	}
	if entry.DefValue != "false" {
		t.Fatalf("-update default = %q, want \"false\"", entry.DefValue)
	}
	if !strings.Contains(entry.Usage, "testdata") {
		t.Fatalf("-update usage does not say what it rewrites: %q", entry.Usage)
	}
}
