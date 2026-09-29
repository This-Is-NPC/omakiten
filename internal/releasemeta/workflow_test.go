package releasemeta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflowFile struct {
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Needs       any               `yaml:"needs"`
	Permissions map[string]string `yaml:"permissions"`
	Outputs     map[string]string `yaml:"outputs"`
	Steps       []workflowStep    `yaml:"steps"`
}

type workflowStep struct {
	Name  string         `yaml:"name"`
	ID    string         `yaml:"id"`
	Shell string         `yaml:"shell"`
	Uses  string         `yaml:"uses"`
	Run   string         `yaml:"run"`
	With  map[string]any `yaml:"with"`
}

func TestReleaseWorkflowFailsClosedAroundSigning(t *testing.T) {
	t.Parallel()
	workflow := readWorkflow(t, "release.yml")
	assertReleaseWorkflowJobs(t, workflow)
	assertReleaseWorkflowPermissions(t, workflow)
	assertReleaseWorkflowActionsPinned(t, workflow)
	assertReleaseBuildJob(t, workflow)
	assertReleaseSigningJob(t, workflow)
	assertReleasePublishJob(t, workflow)
}

func assertReleaseWorkflowJobs(t *testing.T, workflow workflowFile) {
	if len(workflow.Permissions) != 0 {
		t.Fatalf("top-level permissions = %#v, want none", workflow.Permissions)
	}
	wantJobs := []string{"release-please", "build", "sign-and-attest", "publish"}
	for _, name := range wantJobs {
		if _, ok := workflow.Jobs[name]; !ok {
			t.Fatalf("release workflow is missing %q job", name)
		}
	}
	assertNeeds(t, workflow.Jobs["build"], "release-please")
	assertNeeds(t, workflow.Jobs["sign-and-attest"], "build", "release-please")
	assertNeeds(t, workflow.Jobs["publish"], "release-please", "sign-and-attest")
}

func assertReleaseWorkflowPermissions(t *testing.T, workflow workflowFile) {
	for name, job := range workflow.Jobs {
		if got := job.Permissions["id-token"]; got != "" && (name != "sign-and-attest" || got != "write") {
			t.Errorf("job %q has id-token permission %q", name, got)
		}
		if name == "sign-and-attest" && job.Permissions["id-token"] != "write" {
			t.Errorf("sign-and-attest id-token permission = %q, want write", job.Permissions["id-token"])
		}
	}
}

func assertReleaseWorkflowActionsPinned(t *testing.T, workflow workflowFile) {
	usesPattern := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Uses != "" && !usesPattern.MatchString(step.Uses) {
				t.Errorf("job %q action is not commit-pinned: %s", jobName, step.Uses)
			}
		}
	}
}

func assertReleaseBuildJob(t *testing.T, workflow workflowFile) {
	build := workflow.Jobs["build"]
	assertCheckoutRef(t, build, "${{ needs.release-please.outputs.tag_name }}")
	if got := build.Outputs["commit"]; got != "${{ steps.source.outputs.commit }}" {
		t.Errorf("build commit output = %q", got)
	}
	source := requireStep(t, build, "Record build source commit")
	if source.ID != "source" || strings.TrimSpace(source.Run) != `printf 'commit=%s\n' "$(git rev-parse HEAD)" >>"$GITHUB_OUTPUT"` {
		t.Errorf("build source step does not export git HEAD structurally: id=%q run=%q", source.ID, source.Run)
	}
	goreleaser := requireStep(t, build, "Build release archives without publishing")
	if goreleaser.With["version"] != "v2.17.0" || goreleaser.With["args"] != "release --clean --skip=publish" {
		t.Errorf("GoReleaser configuration = %#v", goreleaser.With)
	}
	assertPathSet(t, requireStep(t, build, "Stage unsigned release files"), unsignedAssets())
}

func assertReleaseSigningJob(t *testing.T, workflow workflowFile) {
	signing := workflow.Jobs["sign-and-attest"]
	assertCheckoutRef(t, signing, "${{ needs.release-please.outputs.tag_name }}")
	metadata := requireStep(t, signing, "Bind metadata to the tag and source commit")
	assertExactLine(t, metadata.Run, `test "$(git rev-list -n 1 "$TAG")" = "$commit"`)
	assertExactLine(t, metadata.Run, `test "${{ needs.build.outputs.commit }}" = "$commit"`)
	for _, name := range []string{"Sign manifest, checksums, and provenance keylessly", "Verify certificate policy and all signed bytes"} {
		if strings.TrimSpace(requireStep(t, signing, name).Run) == "" {
			t.Errorf("signing step %q has no command", name)
		}
	}
	verifyIndex := stepIndex(t, signing, "Verify certificate policy and all signed bytes")
	installerGateIndex := stepIndex(t, signing, "Verify real keyless release through both production installers")
	stageIndex := stepIndex(t, signing, "Stage verified release files")
	if verifyIndex >= installerGateIndex || installerGateIndex >= stageIndex {
		t.Errorf("installer keyless gate order = verify:%d gate:%d stage:%d", verifyIndex, installerGateIndex, stageIndex)
	}
	installerGate := signing.Steps[installerGateIndex]
	if installerGate.Shell != "bash" || strings.TrimSpace(installerGate.Run) != `scripts/release-installer-gate.sh "$TAG" .tmp/release` {
		t.Errorf("installer keyless gate = %#v", installerGate)
	}
	assertInstallerGateContract(t)
	cosign := requireStep(t, signing, "Install Cosign")
	if cosign.With["cosign-release"] != "v3.1.1" {
		t.Errorf("release Cosign version = %#v", cosign.With["cosign-release"])
	}
	assertPathSet(t, requireStep(t, signing, "Stage verified release files"), signedAssets("${{ needs.release-please.outputs.tag_name }}"))
}

func assertReleasePublishJob(t *testing.T, workflow workflowFile) {
	publish := workflow.Jobs["publish"]
	upload := requireStep(t, publish, "Upload assets and publish the draft")
	if got := parseShellArray(t, upload.Run, "assets"); !sameStrings(got, publishAssets("${TAG}")) {
		t.Errorf("publish allowlist = %#v, want %#v", got, publishAssets("${TAG}"))
	}
	assertExactLine(t, upload.Run, `gh release upload "$TAG" "${assets[@]}" --repo "$GITHUB_REPOSITORY"`)
	assertExactLine(t, upload.Run, `test "$(find .tmp/release -maxdepth 1 -type f | wc -l)" -eq "${#assets[@]}"`)
}

func TestReleasePleaseCreatesDraftUntilSigningSucceeds(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "release-please-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Packages map[string]struct {
			Draft bool `json:"draft"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if !config.Packages["."].Draft {
		t.Error("release-please must create a draft release until signed assets are ready")
	}
}

func readWorkflow(t *testing.T, name string) workflowFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func requireStep(t *testing.T, job workflowJob, name string) workflowStep {
	t.Helper()
	for _, step := range job.Steps {
		if step.Name == name {
			return step
		}
	}
	t.Fatalf("job is missing step %q", name)
	return workflowStep{}
}

func stepIndex(t *testing.T, job workflowJob, name string) int {
	t.Helper()
	for i, step := range job.Steps {
		if step.Name == name {
			return i
		}
	}
	t.Fatalf("job is missing step %q", name)
	return -1
}

func assertInstallerGateContract(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "release-installer-gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`GITHUB_DL_BASE="http://127.0.0.1:`,
		`OKT_INSTALLER_TEST_LIBRARY=1`,
		`source "$repo_root/install.sh"`,
		`verify_release_strict "$dist/$asset" "$asset" "$version"`,
		`. (Join-Path $env:OKT_GATE_ROOT "install.ps1")`,
		`Invoke-StrictVerification -Cosign $cosign`,
		`COSIGN_BIN="$(command -v cosign)"`,
	} {
		if !strings.Contains(script, required) {
			t.Errorf("installer gate harness is missing %q", required)
		}
	}
	for _, forbidden := range []string{"--insecure-ignore-tlog", "--insecure-ignore-sct", "--key"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("installer gate harness weakens production verification with %q", forbidden)
		}
	}
}

func assertCheckoutRef(t *testing.T, job workflowJob, want string) {
	t.Helper()
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/checkout@") {
			if step.With["ref"] != want || step.With["fetch-depth"] != 0 {
				t.Errorf("checkout config = %#v", step.With)
			}
			return
		}
	}
	t.Error("job has no checkout step")
}

func assertNeeds(t *testing.T, job workflowJob, want ...string) {
	t.Helper()
	var got []string
	switch needs := job.Needs.(type) {
	case string:
		got = []string{needs}
	case []any:
		for _, item := range needs {
			got = append(got, item.(string))
		}
	}
	if !sameStrings(got, want) {
		t.Errorf("job dependencies = %v, want %v", got, want)
	}
}

func assertPathSet(t *testing.T, step workflowStep, want []string) {
	t.Helper()
	if step.With["include-hidden-files"] != true {
		t.Errorf("step %q must include its explicit .tmp/release allowlist", step.Name)
	}
	raw, ok := step.With["path"].(string)
	if !ok {
		t.Fatalf("step %q path is not a string: %#v", step.Name, step.With["path"])
	}
	got := nonemptyLines(raw)
	if !sameStrings(got, want) {
		t.Errorf("step %q paths = %#v, want %#v", step.Name, got, want)
	}
}

func assertExactLine(t *testing.T, script, want string) {
	t.Helper()
	for _, line := range strings.Split(script, "\n") {
		if strings.TrimSpace(line) == want {
			return
		}
	}
	t.Errorf("script is missing exact line %q", want)
}

func parseShellArray(t *testing.T, script, name string) []string {
	t.Helper()
	lines := strings.Split(script, "\n")
	inside := false
	var out []string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == name+"=(" {
			inside = true
			continue
		}
		if inside && line == ")" {
			return out
		}
		if inside {
			out = append(out, strings.Trim(line, `"`))
		}
	}
	t.Fatalf("script has no closed %s array", name)
	return nil
}

func nonemptyLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func sameStrings(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}

func unsignedAssets() []string {
	return []string{
		".tmp/release/okt_Darwin_arm64.tar.gz", ".tmp/release/okt_Darwin_x86_64.tar.gz", ".tmp/release/okt_Linux_arm64.tar.gz",
		".tmp/release/okt_Linux_x86_64.tar.gz", ".tmp/release/okt_Windows_arm64.zip", ".tmp/release/okt_Windows_x86_64.zip", ".tmp/release/checksums.txt",
	}
}

func signedAssets(tag string) []string {
	return append(unsignedAssets(),
		".tmp/release/release-manifest-"+tag+".json", ".tmp/release/release-manifest-"+tag+".sigstore.json",
		".tmp/release/checksums-"+tag+".sigstore.json", ".tmp/release/release-provenance-"+tag+".sigstore.json")
}

func publishAssets(tag string) []string { return signedAssets(tag) }

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
