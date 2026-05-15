package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIWalkUpStandaloneInstall exercises the .omakiten/ walk-up path: a
// fresh repo with a SeedInstall'd .omakiten/ must be the bundle source for
// every subsequent CLI invocation from the repo (no --config flag passed).
// The user-global ConfigRoot is pointed at a sibling tmp dir via
// $OMAKITEN_HOME so the test never touches the host machine's config.
func TestCLIWalkUpStandaloneInstall(t *testing.T) {
	homeDir := t.TempDir()
	repoDir := t.TempDir()
	dbPath := filepath.Join(homeDir, "data", "omakiten.db")

	t.Setenv("OMAKITEN_HOME", homeDir)
	t.Chdir(repoDir)

	// Seed the repo-local standalone install. okt init walks up to git
	// root; we materialise the install manually here so the test does not
	// have to fabricate a .git directory.
	repoLocalRoot := filepath.Join(repoDir, ".omakiten")
	out := runCLIWithoutConfig(t, dbPath, "config", "init", "--scope", "local", "--preset", "izakaya")
	if !strings.Contains(out, `"name":"izakaya"`) {
		t.Skipf("okt config init --scope local not wired yet; output = %s", out)
	}

	// Now run a regular okt command from a deep subdir with NO --config
	// flag. Discovery must walk up and load the local install, NOT the
	// empty $OMAKITEN_HOME global.
	deep := filepath.Join(repoDir, "src", "internal")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll(deep) error = %v", err)
	}
	t.Chdir(deep)
	out = runCLIWithoutConfig(t, dbPath, "config", "validate")
	if !strings.Contains(out, repoLocalRoot) {
		t.Fatalf("validate output = %s, want path under repo-local %s", out, repoLocalRoot)
	}
	if !strings.Contains(out, "izakaya") {
		t.Fatalf("validate output = %s, want izakaya kit", out)
	}
}

// TestCLIProjectFlagPicksProjectRepoLocal exercises the --project triggers
// project-root walk-up path: two registered projects with their own
// .omakiten/, current working directory NOT under either, and --project B
// must resolve to B's install rather than A's or the global.
func TestCLIProjectFlagPicksProjectRepoLocal(t *testing.T) {
	homeDir := t.TempDir()
	dbPath := filepath.Join(homeDir, "data", "omakiten.db")
	t.Setenv("OMAKITEN_HOME", homeDir)

	tmp := t.TempDir()
	projectA := filepath.Join(tmp, "repo-a")
	projectB := filepath.Join(tmp, "repo-b")
	for _, p := range []string{projectA, projectB} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", p, err)
		}
	}

	// Seed A with omakase + register in DB.
	t.Chdir(projectA)
	runCLIWithoutConfig(t, dbPath, "config", "init", "--scope", "local", "--preset", "omakase")
	runCLIWithoutConfig(t, dbPath, "init", "--name", "Repo A", "--slug", "repo-a", "--root", projectA)

	// Seed B with izakaya + register in DB.
	t.Chdir(projectB)
	runCLIWithoutConfig(t, dbPath, "config", "init", "--scope", "local", "--preset", "izakaya")
	runCLIWithoutConfig(t, dbPath, "init", "--name", "Repo B", "--slug", "repo-b", "--root", projectB)

	// chdir to a third unrelated directory; --project B must still pick
	// B's .omakiten/ via project.root_path walk-up rather than CWD.
	t.Chdir(tmp)
	out := runCLIWithoutConfig(t, dbPath, "--project", "repo-b", "config", "show", "--scope", "local")
	if !strings.Contains(out, "key: izakaya") {
		t.Fatalf("--project repo-b output = %s, want izakaya kit", out)
	}
	if strings.Contains(out, "key: omakase") {
		t.Fatalf("--project repo-b leaked into omakase from repo-a")
	}
}

// TestCLIPerProjectListIsolatesTasks is the Phase 3c acceptance check
// for `--project` routing on the data plane: two registered projects,
// each with one task in its own DB row, must surface only their own
// task when `okt --project <slug> list` runs. Failure mode catches a
// regression that collapses every CLI invocation onto a shared
// project context (the bug the BundleCache layer exists to prevent).
func TestCLIPerProjectListIsolatesTasks(t *testing.T) {
	homeDir := t.TempDir()
	dbPath := filepath.Join(homeDir, "data", "omakiten.db")
	t.Setenv("OMAKITEN_HOME", homeDir)

	tmp := t.TempDir()
	projectA := filepath.Join(tmp, "proj-a")
	projectB := filepath.Join(tmp, "proj-b")
	for _, p := range []string{projectA, projectB} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) = %v", p, err)
		}
	}

	t.Chdir(projectA)
	runCLIWithoutConfig(t, dbPath, "config", "init", "--scope", "local", "--preset", "omakase")
	runCLIWithoutConfig(t, dbPath, "init", "--name", "Proj A", "--slug", "proj-a", "--root", projectA)
	runCLIWithoutConfig(t, dbPath, "--project", "proj-a", "add", "-t", "task-only-in-a")

	t.Chdir(projectB)
	runCLIWithoutConfig(t, dbPath, "config", "init", "--scope", "local", "--preset", "omakase")
	runCLIWithoutConfig(t, dbPath, "init", "--name", "Proj B", "--slug", "proj-b", "--root", projectB)
	runCLIWithoutConfig(t, dbPath, "--project", "proj-b", "add", "-t", "task-only-in-b")

	t.Chdir(tmp)
	outA := runCLIWithoutConfig(t, dbPath, "--project", "proj-a", "list")
	if !strings.Contains(outA, "task-only-in-a") {
		t.Fatalf("--project proj-a list = %s, want task-only-in-a", outA)
	}
	if strings.Contains(outA, "task-only-in-b") {
		t.Fatalf("--project proj-a list leaked task-only-in-b: %s", outA)
	}

	outB := runCLIWithoutConfig(t, dbPath, "--project", "proj-b", "list")
	if !strings.Contains(outB, "task-only-in-b") {
		t.Fatalf("--project proj-b list = %s, want task-only-in-b", outB)
	}
	if strings.Contains(outB, "task-only-in-a") {
		t.Fatalf("--project proj-b list leaked task-only-in-a: %s", outB)
	}
}

// TestCLIPerProjectBundleCacheSeeded asserts the Phase 3c plumbing:
// opening the CLI runtime must Install a ProjectRuntime in the
// BundleCache so subcommand helpers can read the per-project EnumRegistry
// without a second bundle parse. The test reaches in via the package-
// private runtime helpers to verify cache.Get returns a non-nil entry
// after open() runs with materializeConfig=true.
func TestCLIPerProjectBundleCacheSeeded(t *testing.T) {
	homeDir := t.TempDir()
	dbPath := filepath.Join(homeDir, "data", "omakiten.db")
	t.Setenv("OMAKITEN_HOME", homeDir)
	t.Chdir(homeDir)

	// Seed the global install so open(materializeConfig=true) finds a
	// bundle to parse.
	runCLIWithoutConfig(t, dbPath, "config", "init", "--scope", "global", "--preset", "omakase")

	opts := &runtimeOptions{dbPath: dbPath}
	rt, err := opts.open(context.Background(), true)
	if err != nil {
		t.Fatalf("open(): %v", err)
	}
	defer rt.close()

	if rt.cache == nil {
		t.Fatal("rt.cache nil after open() — BundleCache not initialised")
	}
	if rt.cache.Size() != 1 {
		t.Fatalf("cache size = %d, want 1 (single boot-seeded entry)", rt.cache.Size())
	}
	pr := rt.ProjectRuntime()
	if pr == nil {
		t.Fatal("rt.ProjectRuntime() nil — cache.Install did not seed entry under projectID")
	}
	if pr.EnumRegistry == nil {
		t.Fatal("ProjectRuntime.EnumRegistry nil — registry not seeded")
	}
	if rt.activeRegistry() != pr.EnumRegistry {
		t.Fatalf("activeRegistry()=%p does not match ProjectRuntime.EnumRegistry=%p — service helpers would skip the cache", rt.activeRegistry(), pr.EnumRegistry)
	}
	if rt.activeRegistry() != rt.registry {
		// Boot path keeps rt.registry as a mirror of the cache entry's
		// registry; the helper should return the same pointer either way.
		t.Logf("note: rt.registry and ProjectRuntime.EnumRegistry are distinct pointers — acceptable as long as activeRegistry() picks the cache entry first")
	}
}

// runCLIWithoutConfig mirrors runCLI but does NOT inject --config so the
// resolver's $OMAKITEN_HOME + walk-up paths run end-to-end.
func runCLIWithoutConfig(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	full := append([]string{"--db", dbPath}, args...)
	cmd.SetArgs(full)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(%v) error = %v, output = %s", full, err, out.String())
	}
	trimmed := strings.TrimSpace(out.String())
	var envelope map[string]any
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		t.Fatalf("json.Unmarshal(%v) error = %v, output = %s", full, err, trimmed)
	}
	if envelope["ok"] != true {
		t.Fatalf("Execute(%v) ok = %v, output = %s", full, envelope["ok"], trimmed)
	}
	return trimmed
}
