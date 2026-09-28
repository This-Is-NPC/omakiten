package installscript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type localCheckCase struct {
	args               []string
	checkRC, lookupRC  string
	dirty, previousSHA bool
	wantRC             int
	wantCheck          bool
	wantPost           string
}

func TestLocalCheckNeverSkipsValidation(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Bash is not installed")
	}
	cases := map[string]localCheckCase{
		"no authentication":         {args: []string{"--pre-push"}, lookupRC: "1", wantCheck: true},
		"failure without auth":      {args: []string{"--pre-push"}, lookupRC: "1", checkRC: "17", wantRC: 17, wantCheck: true},
		"success status":            {wantCheck: true, wantPost: "state=success"},
		"failure status":            {checkRC: "17", wantRC: 17, wantCheck: true, wantPost: "state=failure"},
		"dirty checkout":            {dirty: true, wantRC: 1},
		"another commit":            {previousSHA: true, wantRC: 1},
		"no unverified status mode": {args: []string{"--post-only"}, wantRC: 2},
		"dry run":                   {args: []string{"--dry-run"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { runLocalCheckCase(t, tc) })
	}
}

func runLocalCheckCase(t *testing.T, tc localCheckCase) {
	t.Helper()
	root := t.TempDir()
	repository := localCheckRepository(t, root)
	if tc.dirty {
		writeCheckFixture(t, filepath.Join(repository, "untracked"), "dirty")
	}
	if tc.previousSHA {
		tc.args = []string{"--sha=HEAD~1"}
	}
	bin := localCheckTools(t, root)
	marker, posted := filepath.Join(root, "checked"), filepath.Join(root, "posted")
	args := append([]string{filepath.Join(repository, "scripts", "local-check.sh")}, tc.args...)
	cmd := exec.Command("bash", args...)
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"MISE_PROJECT_ROOT="+repository, "usage_sha=", "usage_dry_run=false", "OKT_LOCAL_CHECK_SHA=HEAD", "OKT_SKIP_LOCAL_CHECK=0",
		"OKT_TEST_MARKER="+marker, "OKT_TEST_POSTED="+posted, "OKT_TEST_CHECK_RC="+tc.checkRC, "OKT_TEST_LOOKUP_RC="+tc.lookupRC)
	out, err := cmd.CombinedOutput()
	rc := localCheckExit(t, err)
	if rc != tc.wantRC {
		t.Fatalf("exit = %d, want %d: %s", rc, tc.wantRC, out)
	}
	_, markerErr := os.Stat(marker)
	if (markerErr == nil) != tc.wantCheck {
		t.Fatalf("check executed = %v, want %v: %s", markerErr == nil, tc.wantCheck, out)
	}
	assertCheckStatus(t, posted, tc.wantPost)
}

func localCheckExit(t *testing.T, err error) int {
	t.Helper()
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0
}

func assertCheckStatus(t *testing.T, path, want string) {
	t.Helper()
	post, err := os.ReadFile(path)
	if want == "" {
		if err == nil {
			t.Fatalf("unexpected status publication: %s", post)
		}
	} else if err != nil || !strings.Contains(string(post), want) {
		t.Fatalf("status = %s (%v), want %s", post, err, want)
	}
}

func localCheckRepository(t *testing.T, root string) string {
	t.Helper()
	repository := filepath.Join(root, "repo")
	for _, relative := range []string{"scripts/local-check.sh", "scripts/post-check-status.sh", "scripts/lib/workspace.sh"} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), relative))
		if err != nil {
			t.Fatal(err)
		}
		writeCheckFixture(t, filepath.Join(repository, relative), string(data))
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repository
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	for range 2 {
		git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "fixture")
	}
	return repository
}

func localCheckTools(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(root, "bin")
	writeCheckFixture(t, filepath.Join(bin, "mise"), "#!/usr/bin/env bash\nset -eu\n[[ $1 == run && $2 == check ]]\nprintf checked > \"$OKT_TEST_MARKER\"\nexit \"${OKT_TEST_CHECK_RC:-0}\"\n")
	writeCheckFixture(t, filepath.Join(bin, "gh"), "#!/usr/bin/env bash\nset -eu\nif [[ $1 == repo ]]; then\n  [[ ${OKT_TEST_LOOKUP_RC:-0} == 0 ]] || exit 1\n  printf 'fixture/repo\\n'\nelse\n  printf '%s\\n' \"$@\" > \"$OKT_TEST_POSTED\"\nfi\n")
	return bin
}

func writeCheckFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}
