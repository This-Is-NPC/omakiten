package installscript

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type releasePrepareCase struct {
	draft, tagCommit, count, createRC, lookupRC string
	wantRC                                      int
	wantCreate, wantRelease                     bool
}

func TestPrepareReleaseDraft(t *testing.T) {
	const commit = "b689883d81763b6e115dc5450bb730a34a757e3e"
	cases := map[string]releasePrepareCase{
		"published":            {count: "1"},
		"missing tag":          {draft: commit, tagCommit: commit, count: "0", wantCreate: true, wantRelease: true},
		"existing tag":         {draft: commit, tagCommit: commit, count: "1", wantRelease: true},
		"wrong tag commit":     {draft: commit, tagCommit: strings.Repeat("a", 40), count: "1", wantRC: 1},
		"mutable draft target": {draft: "master", wantRC: 1},
		"tag creation fails":   {draft: commit, count: "0", createRC: "1", wantCreate: true, wantRC: 1},
		"API access fails":     {lookupRC: "1", wantRC: 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runReleasePrepareCase(t, tc, commit)
		})
	}
}

func runReleasePrepareCase(t *testing.T, tc releasePrepareCase, commit string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	marker, output := filepath.Join(root, "created"), filepath.Join(root, "output")
	writeCheckFixture(t, filepath.Join(bin, "gh"), `#!/usr/bin/env bash
set -eu
case "$*" in
  *contents/.release-please-manifest.json*)
    exit_code="${OKT_TEST_LOOKUP_RC:-0}"
    [ "$exit_code" = 0 ] || exit "$exit_code"
    printf '0.31.0\n' ;;
  *repos/test/omakiten/releases*) printf '%s\n' "$OKT_TEST_DRAFT" ;;
  *matching-refs/tags*) printf '%s\n' "$OKT_TEST_COUNT" ;;
  *'--method POST'*)
    printf '%s\n' "$*" >"$OKT_TEST_CREATED"
    exit "${OKT_TEST_CREATE_RC:-0}" ;;
  *commits/v0.31.0*) printf '%s\n' "$OKT_TEST_TAG_COMMIT" ;;
  *) printf 'unexpected gh invocation: %s\n' "$*" >&2; exit 2 ;;
esac
`)
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "scripts", "prepare-release.sh"))
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"MISE_PROJECT_ROOT="+root, "GITHUB_REPOSITORY=test/omakiten", "GITHUB_OUTPUT="+output,
		"OKT_TEST_CREATED="+marker, "OKT_TEST_DRAFT="+tc.draft, "OKT_TEST_COUNT="+tc.count,
		"OKT_TEST_TAG_COMMIT="+tc.tagCommit, "OKT_TEST_CREATE_RC="+tc.createRC,
		"OKT_TEST_LOOKUP_RC="+tc.lookupRC)
	out, err := cmd.CombinedOutput()
	if rc := localCheckExit(t, err); rc != tc.wantRC {
		t.Fatalf("exit = %d, want %d: %s", rc, tc.wantRC, out)
	}
	created, createErr := os.ReadFile(marker)
	if (createErr == nil) != tc.wantCreate {
		t.Fatalf("tag created = %v, want %v: %s", createErr == nil, tc.wantCreate, out)
	}
	if tc.wantCreate && (!strings.Contains(string(created), "ref=refs/tags/v0.31.0") || !strings.Contains(string(created), "sha="+commit)) {
		t.Fatalf("incorrect tag creation: %s", created)
	}
	prepared, _ := os.ReadFile(output)
	if strings.Contains(string(prepared), "release_created=true") != tc.wantRelease {
		t.Fatalf("release output = %s, want release %v: %s", prepared, tc.wantRelease, out)
	}
	if tc.wantRelease && !strings.Contains(string(prepared), "tag_name=v0.31.0") {
		t.Fatalf("release tag missing: %s", prepared)
	}
}
