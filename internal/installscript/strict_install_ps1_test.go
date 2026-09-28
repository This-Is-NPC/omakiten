package installscript

// Executable coverage of install.ps1's strict (Cosign-verified) bootstrap path.
//
// These are real `pwsh -File install.ps1` runs against the same hermetic
// fixtures the shell tests use, not source-parity assertions: the PowerShell
// installer downloads from a local httptest server, shells out to the cosign
// double, and writes into a temp install dir. PowerShell 7 is cross-platform,
// so the whole path executes on a Unix host; the tests skip with an explicit
// message when no pwsh is reachable.

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/releasemeta"
	"omakiten/internal/releaseverify"
)

// resolvePwsh finds a usable PowerShell 7 and returns the path of the *real*
// executable, never a version-manager shim. OKT_PWSH lets a developer or CI
// point at a specific build.
//
// The shim indirection matters for correctness, not just tidiness: a shim
// re-execs through its version manager, which prepends the managed toolchain to
// PATH for the child process. That would leak the host's own cosign into every
// run and let "no verifier installed" pass for the wrong reason, since the
// installer would find a real cosign the test never put there. Asking the
// interpreter for its own module path collapses the shim to the binary behind
// it, so the PATH this test builds stays authoritative.
func resolvePwsh(t *testing.T) string {
	t.Helper()
	candidate := os.Getenv("OKT_PWSH")
	if candidate == "" {
		found, err := exec.LookPath("pwsh")
		if err != nil {
			t.Skip("pwsh not available; set OKT_PWSH to run the executable PowerShell installer tests")
		}
		candidate = found
	}
	probe := exec.Command(candidate, "-NoProfile", "-NonInteractive", "-Command",
		"[Diagnostics.Process]::GetCurrentProcess().MainModule.FileName")
	out, err := probe.Output()
	if err != nil {
		t.Skipf("pwsh at %s is not runnable (%v)", candidate, err)
	}
	real := strings.TrimSpace(string(out))
	if real == "" {
		t.Skipf("pwsh at %s did not report its own executable path", candidate)
	}
	return real
}

// assertNoRealCosign fails when the PATH handed to a child still resolves a
// cosign the test did not install. Without it, a "verifier missing" fixture can
// silently become a "verifier present and correctly refusing" fixture.
func assertNoRealCosign(t *testing.T, out string) {
	t.Helper()
	if strings.Contains(out, "Strict verification mode — using") {
		t.Fatalf("the child resolved a cosign the test did not install; PATH isolation leaked:\n%s", out)
	}
}

// runStrictPs1 executes install.ps1 in strict mode against a fake release host.
func runStrictPs1(t *testing.T, srv *httptest.Server, cosignDir string, cosignEnv []string, logPath string, extraEnv ...string) strictRun {
	t.Helper()
	pwsh := resolvePwsh(t)
	script := filepath.Join(repoRoot(t), "install.ps1")
	home := t.TempDir()
	installDir := filepath.Join(t.TempDir(), "bin")

	pathValue := pathWithout(t, "cosign")
	if cosignDir != "" {
		pathValue = cosignDir + string(os.PathListSeparator) + pathValue
	}

	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-File", script)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"INSTALL_DIR="+installDir,
		"GITHUB_API_BASE="+srv.URL,
		"GITHUB_DL_BASE="+srv.URL,
		"PROCESSOR_ARCHITECTURE=AMD64",
		"PATH="+pathValue,
		"OKT_VERIFY_MODE=strict",
	)
	cmd.Env = append(cmd.Env, cosignEnv...)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()

	log := ""
	if logPath != "" {
		log = readLog(t, logPath)
	}
	return strictRun{Output: string(out), InstallDir: installDir, CosignLog: log, Err: err}
}

// windowsFixture builds the Windows amd64 release install.ps1 selects.
func windowsFixture(t *testing.T, tag string) *strictFixture {
	t.Helper()
	return buildStrictFixture(t, tag, "okt_Windows_x86_64.zip", buildZipArchive(t, tag))
}

func TestInstallPs1Strict_ValidRelease_VerifiesThenInstalls(t *testing.T) {
	requireBash(t) // the cosign double is a POSIX sh script
	fixture := windowsFixture(t, fakeTag)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err != nil {
		t.Fatalf("strict install of a valid release failed: %v\noutput:\n%s", run.Err, run.Output)
	}
	assertOrderedMarkers(t, "install.ps1 strict output", run.Output, []string{
		"Strict verification mode",
		"Signature OK (release manifest)",
		"Signature OK (checksums.txt)",
		"Provenance OK",
		"Digest OK",
	})
	lines := strings.Split(strings.TrimSpace(run.CosignLog), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected exactly 3 cosign verifications, got %d:\n%s", len(lines), run.CosignLog)
	}
	wantIdentity := "identity=" + releaseverify.CertificateIdentity(fixtureRepo)
	wantIssuer := "issuer=" + releaseverify.OIDCIssuer
	for _, line := range lines {
		if !strings.Contains(line, wantIdentity) || !strings.Contains(line, wantIssuer) {
			t.Errorf("cosign call did not pin the release identity/issuer:\n%s", line)
		}
	}
	if _, err := os.Stat(filepath.Join(run.InstallDir, "okt.exe")); err != nil {
		t.Fatalf("verified release must install okt.exe: %v", err)
	}
}

func TestInstallPs1Strict_MissingCosign_AbortsNoBinary(t *testing.T) {
	fixture := windowsFixture(t, fakeTag)
	srv := strictServer(t, fixture)

	run := runStrictPs1(t, srv, "", nil, "")
	if run.Err == nil {
		t.Fatalf("strict mode must fail when Cosign is not installed\noutput:\n%s", run.Output)
	}
	if !strings.Contains(strings.ToLower(run.Output), "cosign") {
		t.Errorf("error must name the missing verifier:\n%s", run.Output)
	}
	if strings.Contains(run.Output, "Checksum OK") {
		t.Errorf("strict mode fell back to the unsigned checksum path:\n%s", run.Output)
	}
	assertNoRealCosign(t, run.Output)
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_MissingBundle_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := windowsFixture(t, fakeTag)
	fixture.Missing[releaseverify.ChecksumsBundleName(fakeTag)] = true
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a release missing the checksums signature must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not download the checksums.txt signature")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_InvalidBundle_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := windowsFixture(t, fakeTag)
	fixture.ManifestBundle = []byte("not a sigstore bundle at all\n")
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("an unparseable bundle must abort the install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not authenticate the release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_WrongIdentity_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := windowsFixture(t, fakeTag)
	fixture.resign(t, releaseverify.CertificateIdentity("attacker/omakiten"), releaseverify.OIDCIssuer)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a signature from another repository's workflow must be refused\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not authenticate the release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_ModifiedManifest_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := windowsFixture(t, fakeTag)
	fixture.Manifest = append(fixture.Manifest, []byte("\n")...)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a manifest whose bytes drifted from its signature must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not authenticate the release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_ChecksumMismatch_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := windowsFixture(t, fakeTag)
	fixture.setManifestDigest(t, fixture.Asset, strings.Repeat("0", 64))
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("an archive whose digest disagrees with the signed manifest must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "disagrees with the signed manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_RejectsAuthenticatedWrongBuilder(t *testing.T) {
	requireBash(t)
	fixture := windowsFixture(t, fakeTag)
	fixture.mutateProvenance(t, func(statement *releasemeta.Statement) {
		statement.Predicate.RunDetails.Builder.ID = releaseverify.CertificateIdentity("attacker/omakiten")
	})
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("authenticated provenance from another builder must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "provenance source identity")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_RejectsExtraManifestArtifact(t *testing.T) {
	requireBash(t)
	fixture := windowsFixture(t, fakeTag)
	var manifest releasemeta.Manifest
	if err := json.Unmarshal(fixture.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Artifacts = append(manifest.Artifacts, releasemeta.Artifact{Name: "unexpected.txt", SHA256: strings.Repeat("a", 64)})
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Manifest = data
	fixture.resign(t, releaseverify.CertificateIdentity(fixtureRepo), releaseverify.OIDCIssuer)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("extra manifest artifact must not install\n%s", run.Output)
	}
	assertReason(t, run.Output, "exactly")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_RejectsCoerciveJSONAndInvalidInvocation(t *testing.T) {
	requireBash(t)
	cases := map[string]struct {
		mutate func(t *testing.T, fixture *strictFixture)
		want   string
	}{
		"string schema version": {
			mutate: func(t *testing.T, fixture *strictFixture) {
				fixture.Manifest = bytes.Replace(fixture.Manifest, []byte(`"schema_version": 1`), []byte(`"schema_version": "1"`), 1)
				fixture.ManifestBundle = testBundle{Kind: "blob", Identity: releaseverify.CertificateIdentity(fixtureRepo), Issuer: releaseverify.OIDCIssuer, SHA256: hexDigest(fixture.Manifest)}.bytes(t)
			},
			want: "schema version",
		},
		"non-integer schema number": {
			mutate: func(t *testing.T, fixture *strictFixture) {
				fixture.Manifest = bytes.Replace(fixture.Manifest, []byte(`"schema_version": 1`), []byte(`"schema_version": 1.0`), 1)
				fixture.ManifestBundle = testBundle{Kind: "blob", Identity: releaseverify.CertificateIdentity(fixtureRepo), Issuer: releaseverify.OIDCIssuer, SHA256: hexDigest(fixture.Manifest)}.bytes(t)
			},
			want: "schema version",
		},
		"uppercase source commit": {
			mutate: func(t *testing.T, fixture *strictFixture) {
				var manifest releasemeta.Manifest
				if err := json.Unmarshal(fixture.Manifest, &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.SourceCommit = strings.ToUpper(manifest.SourceCommit)
				data, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				fixture.Manifest = data
				fixture.resign(t, releaseverify.CertificateIdentity(fixtureRepo), releaseverify.OIDCIssuer)
			},
			want: "valid source commit",
		},
		"case shifted repository": {
			mutate: func(t *testing.T, fixture *strictFixture) {
				var manifest releasemeta.Manifest
				if err := json.Unmarshal(fixture.Manifest, &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.Repository = strings.ToUpper(manifest.Repository)
				data, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				fixture.Manifest = data
				fixture.resign(t, releaseverify.CertificateIdentity(fixtureRepo), releaseverify.OIDCIssuer)
			},
			want: "not 'This-Is-NPC/omakiten'",
		},
		"malformed invocation id": {
			mutate: func(t *testing.T, fixture *strictFixture) {
				fixture.mutateProvenance(t, func(statement *releasemeta.Statement) {
					statement.Predicate.RunDetails.Metadata.InvocationID = "not a URI"
				})
			},
			want: "source identity",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := windowsFixture(t, fakeTag)
			tc.mutate(t, fixture)
			srv := strictServer(t, fixture)
			cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})
			run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
			if run.Err == nil {
				t.Fatalf("invalid authenticated metadata must not install\n%s", run.Output)
			}
			assertReason(t, run.Output, tc.want)
			assertNoBinary(t, run.InstallDir, run.Output)
		})
	}
}

func TestInstallPs1Strict_ReplayedManifest_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := windowsFixture(t, fakeTag)
	fixture.setManifestTag(t, "8.8.8")
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a signed manifest bound to another tag must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "refusing a replayed release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallPs1Strict_LegacyVersion_AbortsBeforeDownload(t *testing.T) {
	requireBash(t)
	legacy := releaseverify.LegacyChecksumCutoff
	fixture := windowsFixture(t, legacy)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrictPs1(t, srv, cosignDir, cosignEnv, logPath, "VERSION="+legacy)
	if run.Err == nil {
		t.Fatalf("strict mode must refuse a release at or before the signed cutoff\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "signed-release cutoff")
	if run.CosignLog != "" {
		t.Errorf("the cutoff gate must run before anything is verified; cosign log:\n%s", run.CosignLog)
	}
	assertNoBinary(t, run.InstallDir, run.Output)
}

// TestInstallPs1Strict_UnsupportedArm64Verifier pins the AC6 guidance path:
// Windows arm64 has no native Cosign build, so strict mode must fail with the
// explicit supported arrangement rather than a bare "not found".
func TestInstallPs1Strict_UnsupportedArm64Verifier(t *testing.T) {
	fixture := buildStrictFixture(t, fakeTag, "okt_Windows_arm64.zip", buildZipArchive(t, fakeTag))
	srv := strictServer(t, fixture)

	run := runStrictPs1(t, srv, "", nil, "", "PROCESSOR_ARCHITECTURE=ARM64")
	if run.Err == nil {
		t.Fatalf("strict mode on Windows arm64 without Cosign must fail\noutput:\n%s", run.Output)
	}
	lower := strings.ToLower(run.Output)
	if !strings.Contains(lower, "arm64") || !strings.Contains(lower, "amd64") {
		t.Errorf("Windows arm64 guidance must name the supported amd64 Cosign arrangement:\n%s", run.Output)
	}
	assertNoRealCosign(t, run.Output)
	assertNoBinary(t, run.InstallDir, run.Output)
}
