package installscript

// Executable, hermetic coverage of install.sh's strict (Cosign-verified)
// bootstrap path. Every negative fixture asserts the same load-bearing fact:
// the installer exits non-zero and NO binary reaches the install directory.

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

// strictRun is the outcome of one install.sh invocation.
type strictRun struct {
	Output     string
	InstallDir string
	CosignLog  string
	Err        error
}

// runStrict executes install.sh in strict mode against a fake release host,
// with the cosign double first on PATH. HOME and INSTALL_DIR are per-test temp
// dirs, so the host's real installation is never touched.
func runStrict(t *testing.T, srv *httptest.Server, cosignDir string, cosignEnv []string, logPath string, extraEnv ...string) strictRun {
	t.Helper()
	script := filepath.Join(repoRoot(t), "install.sh")
	home := t.TempDir()
	installDir := filepath.Join(t.TempDir(), "bin")

	pathValue := pathWithout(t, "cosign")
	if cosignDir != "" {
		pathValue = cosignDir + string(os.PathListSeparator) + pathValue
	}

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"INSTALL_DIR="+installDir,
		"GITHUB_API_BASE="+srv.URL,
		"GITHUB_DL_BASE="+srv.URL,
		"SHELL=/bin/bash",
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

// strictFixtureForHost builds a fixture for the archive install.sh will pick on
// this machine.
func strictFixtureForHost(t *testing.T, tag string) *strictFixture {
	t.Helper()
	asset := scriptAsset(t)
	return buildStrictFixture(t, tag, asset, buildTarArchive(t, tag))
}

func TestInstallShStrict_ValidRelease_VerifiesThenInstalls(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err != nil {
		t.Fatalf("strict install of a valid release failed: %v\noutput:\n%s", run.Err, run.Output)
	}

	// Verification must happen in the same order the in-binary verifier uses:
	// manifest signature, then checksums signature, then provenance over the
	// archive, then the cross-bound digest — and only then extraction.
	assertOrderedMarkers(t, "install.sh strict output", run.Output, []string{
		"Strict verification mode",
		"Signature OK (release manifest)",
		"Signature OK (checksums.txt)",
		"Provenance OK",
		"Digest OK",
		"Installed okt",
	})

	// The double logs the exact identity/issuer it was handed; both must be
	// the pinned values, on all three calls.
	wantIdentity := "identity=" + releaseverify.CertificateIdentity(fixtureRepo)
	wantIssuer := "issuer=" + releaseverify.OIDCIssuer
	lines := strings.Split(strings.TrimSpace(run.CosignLog), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected exactly 3 cosign verifications, got %d:\n%s", len(lines), run.CosignLog)
	}
	for _, line := range lines {
		if !strings.Contains(line, wantIdentity) {
			t.Errorf("cosign call did not pin the release workflow SAN:\n%s", line)
		}
		if !strings.Contains(line, wantIssuer) {
			t.Errorf("cosign call did not pin the OIDC issuer:\n%s", line)
		}
	}
	if !strings.Contains(lines[0], "verify-blob ") || !strings.Contains(lines[0], releaseverify.ManifestName(fakeTag)) {
		t.Errorf("first cosign call must authenticate the manifest, got:\n%s", lines[0])
	}
	if !strings.Contains(lines[2], "verify-blob-attestation") || !strings.Contains(lines[2], "type=slsaprovenance1") {
		t.Errorf("third cosign call must be the SLSA provenance attestation, got:\n%s", lines[2])
	}

	if _, err := os.Stat(filepath.Join(run.InstallDir, "okt")); err != nil {
		t.Fatalf("verified release must install the binary: %v", err)
	}
	// The authenticated source commit is surfaced so an operator can pin it.
	if !strings.Contains(run.Output, fixtureCommit) {
		t.Errorf("expected the authenticated source commit %s in the output:\n%s", fixtureCommit, run.Output)
	}
}

func TestInstallShStrict_MissingCosign_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	srv := strictServer(t, fixture)

	// No cosign anywhere on PATH: strict mode must refuse rather than fetch a
	// verifier or fall back to the checksum path.
	run := runStrict(t, srv, "", nil, "")
	if run.Err == nil {
		t.Fatalf("strict mode must fail when Cosign is not installed\noutput:\n%s", run.Output)
	}
	if !strings.Contains(run.Output, "cosign") {
		t.Errorf("error must name the missing verifier:\n%s", run.Output)
	}
	if strings.Contains(run.Output, "Checksum OK") {
		t.Errorf("strict mode fell back to the unsigned checksum path:\n%s", run.Output)
	}
	assertNoRealCosign(t, run.Output)
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_CosignTooOld_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{Version: "2.4.1"})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("strict mode must reject a Cosign below the minimum supported version\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "strict mode needs v3.0.0 or newer")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_MissingBundle_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	fixture.Missing[releaseverify.ManifestBundleName(fakeTag)] = true
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a release with no manifest signature must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not download the release manifest signature")
	if strings.Contains(run.Output, "Checksum OK") {
		t.Errorf("a stripped release must not degrade to checksum-only trust:\n%s", run.Output)
	}
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_InvalidBundle_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	fixture.ManifestBundle = []byte("not a sigstore bundle at all\n")
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("an unparseable bundle must abort the install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not authenticate the release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_WrongIdentity_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	// A perfectly valid signature — issued to a different repository's release
	// workflow. The exact-string SAN pin is the only thing that catches it.
	fixture.resign(t, releaseverify.CertificateIdentity("attacker/omakiten"), releaseverify.OIDCIssuer)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a signature from another repository's workflow must be refused\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not authenticate the release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_WrongIssuer_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	fixture.resign(t, releaseverify.CertificateIdentity(fixtureRepo), "https://accounts.google.com")
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a signature from another OIDC issuer must be refused\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not authenticate the release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

// TestInstallShStrict_LegacyVersion_AbortsBeforeDownload covers the replay
// direction that exists at bootstrap time: there is no "installed version" to
// downgrade from, so the replay a fresh install must refuse is a pre-cutoff
// release, which has no authenticated metadata and never will.
func TestInstallShStrict_LegacyVersion_AbortsBeforeDownload(t *testing.T) {
	requireBash(t)
	legacy := releaseverify.LegacyChecksumCutoff
	fixture := strictFixtureForHost(t, legacy)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath, "VERSION="+legacy)
	if run.Err == nil {
		t.Fatalf("strict mode must refuse a release at or before the signed cutoff\noutput:\n%s", run.Output)
	}
	if !strings.Contains(run.Output, legacy) {
		t.Errorf("error must name the signed-release cutoff %s:\n%s", legacy, run.Output)
	}
	if run.CosignLog != "" {
		t.Errorf("the cutoff gate must run before anything is verified or downloaded; cosign log:\n%s", run.CosignLog)
	}
	assertNoBinary(t, run.InstallDir, run.Output)
}

// TestInstallShStrict_ReplayedManifest_AbortsNoBinary is the other replay
// shape: an authentically signed manifest from a *different* release, served
// for the tag the user asked for.
func TestInstallShStrict_ReplayedManifest_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	fixture.setManifestTag(t, "8.8.8")
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a signed manifest bound to another tag must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "refusing a replayed release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_ModifiedManifest_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	// Edit the manifest *after* signing: the bytes no longer match the bundle.
	fixture.Manifest = append(fixture.Manifest, []byte("\n")...)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a manifest whose bytes drifted from its signature must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not authenticate the release manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

// TestInstallShStrict_ChecksumMismatch_AbortsNoBinary serves an authentically
// signed manifest that records a different digest for the selected archive, so
// every signature verifies and only the cross-bound digest comparison catches
// the swap.
func TestInstallShStrict_ChecksumMismatch_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	fixture.setManifestDigest(t, fixture.Asset, strings.Repeat("0", 64))
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("an archive whose digest disagrees with the signed manifest must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "disagrees with the signed manifest")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_RejectsAuthenticatedWrongProvenanceIdentity(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	fixture.mutateProvenance(t, func(statement *releasemeta.Statement) {
		statement.Predicate.BuildDefinition.ExternalParameters.Repository = "attacker/omakiten"
	})
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("authenticated provenance for another repository must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "provenance source identity")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_RejectsDuplicateManifestArtifact(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	var manifest releasemeta.Manifest
	if err := json.Unmarshal(fixture.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Artifacts = append(manifest.Artifacts, manifest.Artifacts[0])
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.Manifest = data
	fixture.resign(t, releaseverify.CertificateIdentity(fixtureRepo), releaseverify.OIDCIssuer)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("duplicate manifest artifact must not install\n%s", run.Output)
	}
	assertReason(t, run.Output, "exactly")
	assertNoBinary(t, run.InstallDir, run.Output)
}

func TestInstallShStrict_RejectsCoerciveJSONAndInvalidInvocation(t *testing.T) {
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
			fixture := strictFixtureForHost(t, fakeTag)
			tc.mutate(t, fixture)
			srv := strictServer(t, fixture)
			cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})
			run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
			if run.Err == nil {
				t.Fatalf("invalid authenticated metadata must not install\n%s", run.Output)
			}
			assertReason(t, run.Output, tc.want)
			assertNoBinary(t, run.InstallDir, run.Output)
		})
	}
}

func TestInstallShStrict_BindsWholeChecksumsFileDigest(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	lines := strings.Split(strings.TrimSpace(string(fixture.Checksums)), "\n")
	for left, right := 0, len(lines)-1; left < right; left, right = left+1, right-1 {
		lines[left], lines[right] = lines[right], lines[left]
	}
	fixture.Checksums = []byte(strings.Join(lines, "\n") + "\n")
	fixture.resign(t, releaseverify.CertificateIdentity(fixtureRepo), releaseverify.OIDCIssuer)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("reordered authenticated checksums bytes must not install\n%s", run.Output)
	}
	assertReason(t, run.Output, "authenticated whole file")
	assertNoBinary(t, run.InstallDir, run.Output)
}

// TestInstallShStrict_TamperedArchive_AbortsNoBinary serves an archive whose
// bytes were replaced after the release was signed.
func TestInstallShStrict_TamperedArchive_AbortsNoBinary(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	fixture.Archive = buildTarArchive(t, "malicious")
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath)
	if run.Err == nil {
		t.Fatalf("a swapped archive must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "could not authenticate the SLSA provenance")
	assertNoBinary(t, run.InstallDir, run.Output)
}

// TestInstallShStrict_MirrorHasNoUnsignedFallback pins AC4: a mirrored install
// whose metadata is absent fails closed. The legacy OKT_ALLOW_MIRROR_CHECKSUM
// opt-in — which does relax the *checksum* path's trust root — must not open a
// door inside strict mode.
func TestInstallShStrict_MirrorHasNoUnsignedFallback(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	for _, name := range []string{
		releaseverify.ManifestName(fakeTag),
		releaseverify.ManifestBundleName(fakeTag),
		releaseverify.ChecksumsBundleName(fakeTag),
		releaseverify.ProvenanceBundleName(fakeTag),
	} {
		fixture.Missing[name] = true
	}
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath,
		"OKT_ALLOW_MIRROR_CHECKSUM=1",
		"OKT_CHECKSUM_BASE="+srv.URL,
	)
	if run.Err == nil {
		t.Fatalf("a mirror without authenticated metadata must not install\noutput:\n%s", run.Output)
	}
	assertReason(t, run.Output, "strict mode has no unsigned fallback")
	if strings.Contains(run.Output, "Checksum OK") {
		t.Errorf("strict mode degraded to the mirrored checksum path:\n%s", run.Output)
	}
	assertNoBinary(t, run.InstallDir, run.Output)
}

// TestInstallShStrict_RealCosignRejectsForgedBundle runs the *real* installed
// cosign against the real install.sh command line. It cannot assert the happy
// path — a valid keyless bundle needs Fulcio and Rekor and cannot be minted
// offline — but it does prove the flags install.sh passes are the ones cosign
// accepts, and that a forged bundle is refused by the real verifier with no
// binary installed.
func TestInstallShStrict_RealCosignRejectsForgedBundle(t *testing.T) {
	requireBash(t)
	realCosign, err := exec.LookPath("cosign")
	if err != nil {
		t.Skip("cosign not installed; skipping the real-verifier contract test")
	}
	fixture := strictFixtureForHost(t, fakeTag)
	srv := strictServer(t, fixture)

	run := runStrict(t, srv, filepath.Dir(realCosign), nil, "")
	if run.Err == nil {
		t.Fatalf("real cosign must refuse the forged bundle fixture\noutput:\n%s", run.Output)
	}
	if strings.Contains(run.Output, "Signature OK (release manifest)") {
		t.Fatalf("real cosign reported a forged bundle as verified:\n%s", run.Output)
	}
	assertNoBinary(t, run.InstallDir, run.Output)
	t.Logf("real cosign (%s) failed closed as required:\n%s", realCosign, run.Output)
}

// TestInstallShStrict_RejectsUnknownMode guards the mode switch itself: an
// unrecognized value must not silently mean "checksum".
func TestInstallShStrict_RejectsUnknownMode(t *testing.T) {
	requireBash(t)
	fixture := strictFixtureForHost(t, fakeTag)
	srv := strictServer(t, fixture)
	cosignDir, cosignEnv, logPath := writeFakeCosign(t, fakeCosignOptions{})

	run := runStrict(t, srv, cosignDir, cosignEnv, logPath, "OKT_VERIFY_MODE=lenient")
	if run.Err == nil {
		t.Fatalf("an unknown OKT_VERIFY_MODE must abort\noutput:\n%s", run.Output)
	}
	assertNoBinary(t, run.InstallDir, run.Output)
}

// TestInstallSh_ConvenienceModeLabelsItsTrust pins AC5: the default path stays
// available and must describe what it actually trusts, without claiming to be
// end-to-end verified.
func TestInstallSh_ConvenienceModeLabelsItsTrust(t *testing.T) {
	requireBash(t)
	asset := scriptAsset(t)
	archive := buildTarArchive(t, fakeTag)
	sum := hexDigest(archive)

	srv := installServer(t, asset, archive, sum)
	defer srv.Close()

	out, installDir, err := runInstall(t, srv)
	if err != nil {
		t.Fatalf("convenience install must still work: %v\noutput:\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(installDir, "okt")); statErr != nil {
		t.Fatalf("convenience install must place the binary: %v", statErr)
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "not signature-verified") {
		t.Errorf("convenience mode must say it is not signature-verified:\n%s", out)
	}
	if !strings.Contains(lower, "tls") {
		t.Errorf("convenience mode must name the TLS trust it relies on:\n%s", out)
	}
	if !strings.Contains(out, "OKT_VERIFY_MODE=strict") {
		t.Errorf("convenience mode must point at the strict path:\n%s", out)
	}
}

// TestInstallScriptsPinTheReleaseContract is the supplementary source-parity
// check: it keeps the literals the shell and PowerShell paths must hard-code in
// step with internal/releaseverify and internal/releasemeta. It is a drift
// alarm, not a gate — the executable fixtures above are the gate.
func TestInstallScriptsPinTheReleaseContract(t *testing.T) {
	sh := readRootFile(t, "install.sh")
	ps1 := readRootFile(t, "install.ps1")

	pinned := []string{
		releaseverify.OIDCIssuer,
		"/.github/workflows/release.yml@refs/heads/master",
		releaseverify.LegacyChecksumCutoff,
	}
	pinned = append(pinned, releasemeta.ArchiveNames()...)

	for _, body := range []struct {
		label string
		text  string
	}{{"install.sh", sh}, {"install.ps1", ps1}} {
		for _, needle := range pinned {
			if !strings.Contains(body.text, needle) {
				t.Errorf("%s does not pin the release contract value %q", body.label, needle)
			}
		}
		// Neither installer may ever weaken cosign's default policy.
		for _, forbidden := range []string{
			"--insecure-ignore-tlog",
			"--insecure-ignore-sct",
			"--certificate-identity-regexp",
			"--certificate-oidc-issuer-regexp",
		} {
			if strings.Contains(body.text, forbidden) {
				t.Errorf("%s weakens the Cosign policy with %s", body.label, forbidden)
			}
		}
	}
}
