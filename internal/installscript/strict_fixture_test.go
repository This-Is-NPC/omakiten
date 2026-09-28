package installscript

// Hermetic fixtures for the strict (Cosign-verified) bootstrap path.
//
// The fixtures are generated from the *real* release types — release archive
// names come from releasemeta.ArchiveNames(), the manifest is the marshalled
// releasemeta.Manifest the release workflow publishes, and the asset names,
// pinned OIDC issuer, pinned workflow SAN, and signed-release cutoff all come
// from internal/releaseverify. Nothing about the release contract is restated
// here as a literal, so an installer that pins the wrong issuer, the wrong SAN,
// or the wrong artifact names fails these tests by execution rather than by a
// source grep.
//
// What is NOT reproduced: a real keyless Sigstore bundle. Minting one requires
// Fulcio and Rekor, and cosign v3 deliberately refuses to sign without a
// transparency service, so no valid bundle can be produced offline. The double
// below therefore models cosign's *contract* — a bundle binds a byte range to
// exactly one (issuer, SAN) pair — and enforces it with a real SHA-256 over
// whatever file the installer hands it. Real cosign is additionally exercised
// against forged bundles in TestInstallShStrict_RealCosignRejectsForgedBundle.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"omakiten/internal/releasemeta"
	"omakiten/internal/releaseverify"
)

const (
	fixtureRepo   = "This-Is-NPC/omakiten"
	fixtureCommit = "1f2e3d4c5b6a798877665544332211000ffeeddc"
)

// strictFixture is one complete published release: the six archives, the
// checksums file, the version-bound manifest, and the three bundles.
//
// Fields are public so a test can tamper with one after the fixture is built
// (that is the whole point — a mutated field is an unsigned or mis-signed
// artifact, exactly like a compromised mirror would serve).
type strictFixture struct {
	Tag   string // normalized, no leading "v"
	Asset string // the archive install.sh/install.ps1 will select

	Archive          []byte
	Checksums        []byte
	Manifest         []byte
	ManifestBundle   []byte
	ChecksumsBundle  []byte
	ProvenanceBundle []byte

	// Missing names are served as 404 so a stripped release can be tested.
	Missing map[string]bool
}

// testBundle is the test double's stand-in for a Sigstore bundle. It records
// the two things a real bundle proves — which certificate identity signed, and
// which bytes were covered — so the double can enforce both for real.
type testBundle struct {
	Kind         string            `json:"kind"` // "blob" or "attestation"
	Identity     string            `json:"identity"`
	Issuer       string            `json:"issuer"`
	SHA256       string            `json:"sha256,omitempty"`   // blob: digest of the covered bytes
	Subjects     []string          `json:"subjects,omitempty"` // attestation: digests of the subject archives
	DSSEEnvelope *testDSSEEnvelope `json:"dsseEnvelope,omitempty"`
}

type testDSSEEnvelope struct {
	PayloadType string `json:"payloadType"`
	Payload     string `json:"payload"`
}

func (b testBundle) bytes(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal test bundle: %v", err)
	}
	return data
}

func hexDigest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// buildStrictFixture assembles a self-consistent signed release around the
// caller's archive bytes. The five archives the running platform will not
// download are synthesized so the manifest and checksums.txt carry the full
// published matrix, matching releasemeta.ValidateManifestArtifacts.
func buildStrictFixture(t *testing.T, tag, asset string, archive []byte) *strictFixture {
	t.Helper()

	identity := releaseverify.CertificateIdentity(fixtureRepo)
	issuer := releaseverify.OIDCIssuer

	digests := map[string]string{}
	for _, name := range releasemeta.ArchiveNames() {
		if name == asset {
			digests[name] = hexDigest(archive)
			continue
		}
		digests[name] = hexDigest([]byte("synthetic release archive " + name + " " + tag))
	}

	names := releasemeta.ArchiveNames()
	sort.Strings(names)
	var sums bytes.Buffer
	for _, name := range names {
		fmt.Fprintf(&sums, "%s  %s\n", digests[name], name)
	}
	checksums := sums.Bytes()

	fixture := &strictFixture{
		Tag:       tag,
		Asset:     asset,
		Archive:   archive,
		Checksums: checksums,
		Missing:   map[string]bool{},
	}
	fixture.Manifest = buildManifest(t, tag, digests, checksums, fixtureCommit)
	fixture.resign(t, identity, issuer)
	return fixture
}

// buildManifest marshals the same releasemeta.Manifest value the release
// workflow publishes, with the same indentation cmd/okt-release-metadata uses,
// so the installer's jq-free parser is exercised against the real byte layout.
func buildManifest(t *testing.T, tag string, digests map[string]string, checksums []byte, commit string) []byte {
	t.Helper()
	artifacts := make([]releasemeta.Artifact, 0, len(digests)+1)
	for name, digest := range digests {
		artifacts = append(artifacts, releasemeta.Artifact{Name: name, SHA256: digest})
	}
	artifacts = append(artifacts, releasemeta.Artifact{
		Name:   releasemeta.ChecksumsName,
		SHA256: hexDigest(checksums),
	})
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })

	manifest := releasemeta.Manifest{
		SchemaVersion: 1,
		Repository:    fixtureRepo,
		Tag:           releaseverify.Tag(tag),
		SourceCommit:  commit,
		Artifacts:     artifacts,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return append(data, '\n')
}

// resign re-issues all three bundles over the fixture's current bytes under the
// given certificate identity. Tests call it after deliberately editing the
// manifest when they want a *validly signed* but semantically wrong release.
func (f *strictFixture) resign(t *testing.T, identity, issuer string) {
	t.Helper()
	manifest, err := releasemeta.ParseManifest(f.Manifest)
	if err != nil {
		t.Fatalf("fixture manifest is not parseable: %v", err)
	}
	subjects := make([]string, 0, len(manifest.Artifacts))
	statementSubjects := make([]releasemeta.Subject, 0, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		if artifact.Name != releasemeta.ChecksumsName {
			subjects = append(subjects, artifact.SHA256)
			statementSubjects = append(statementSubjects, releasemeta.Subject{Name: artifact.Name, Digest: map[string]string{"sha256": artifact.SHA256}})
		}
	}
	sort.Strings(subjects)
	workflow := releaseverify.CertificateIdentity(fixtureRepo)
	statement := releasemeta.Statement{
		Type: "https://in-toto.io/Statement/v1", Subject: statementSubjects,
		PredicateType: "https://slsa.dev/provenance/v1",
		Predicate: releasemeta.ProvenancePredicate{
			BuildDefinition: releasemeta.BuildDefinition{
				BuildType:            workflow,
				ExternalParameters:   releasemeta.SourceIdentity{Repository: fixtureRepo, Tag: releaseverify.Tag(f.Tag), Commit: manifest.SourceCommit},
				InternalParameters:   map[string]string{},
				ResolvedDependencies: []releasemeta.ResourceDescriptor{{URI: "git+https://github.com/" + fixtureRepo + "@refs/tags/" + releaseverify.Tag(f.Tag), Digest: map[string]string{"gitCommit": manifest.SourceCommit}}},
			},
			RunDetails: releasemeta.RunDetails{Builder: releasemeta.Builder{ID: workflow}, Metadata: releasemeta.RunMetadata{InvocationID: "https://github.com/" + fixtureRepo + "/actions/runs/1/attempts/1"}},
		},
	}
	payload, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal provenance statement: %v", err)
	}

	f.ManifestBundle = testBundle{Kind: "blob", Identity: identity, Issuer: issuer, SHA256: hexDigest(f.Manifest)}.bytes(t)
	f.ChecksumsBundle = testBundle{Kind: "blob", Identity: identity, Issuer: issuer, SHA256: hexDigest(f.Checksums)}.bytes(t)
	f.ProvenanceBundle = testBundle{Kind: "attestation", Identity: identity, Issuer: issuer, Subjects: subjects, DSSEEnvelope: &testDSSEEnvelope{PayloadType: "application/vnd.in-toto+json", Payload: base64.StdEncoding.EncodeToString(payload)}}.bytes(t)
}

func (f *strictFixture) mutateProvenance(t *testing.T, mutate func(*releasemeta.Statement)) {
	t.Helper()
	var bundle testBundle
	if err := json.Unmarshal(f.ProvenanceBundle, &bundle); err != nil {
		t.Fatal(err)
	}
	payload, err := base64.StdEncoding.DecodeString(bundle.DSSEEnvelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var statement releasemeta.Statement
	if err := json.Unmarshal(payload, &statement); err != nil {
		t.Fatal(err)
	}
	mutate(&statement)
	payload, err = json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	bundle.DSSEEnvelope.Payload = base64.StdEncoding.EncodeToString(payload)
	f.ProvenanceBundle = bundle.bytes(t)
}

// setManifestDigest rewrites one artifact digest inside the manifest and
// re-signs, producing an authentic signature over a manifest that disagrees
// with the bytes actually published — the checksum-mismatch fixture.
//
// The provenance attestation is deliberately left covering the *real* archive
// digests, so this fixture isolates the manifest/checksums disagreement rather
// than tripping the earlier provenance subject check.
func (f *strictFixture) setManifestDigest(t *testing.T, name, digest string) {
	t.Helper()
	provenance := f.ProvenanceBundle
	defer func() { f.ProvenanceBundle = provenance }()
	manifest, err := releasemeta.ParseManifest(f.Manifest)
	if err != nil {
		t.Fatalf("parse fixture manifest: %v", err)
	}
	found := false
	for i := range manifest.Artifacts {
		if manifest.Artifacts[i].Name == name {
			manifest.Artifacts[i].SHA256 = digest
			found = true
		}
	}
	if !found {
		t.Fatalf("fixture manifest has no artifact %q", name)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	f.Manifest = append(data, '\n')
	f.resign(t, releaseverify.CertificateIdentity(fixtureRepo), releaseverify.OIDCIssuer)
}

// setManifestTag rebinds the manifest to a different release tag and re-signs:
// a genuinely signed manifest replayed onto another version.
func (f *strictFixture) setManifestTag(t *testing.T, tag string) {
	t.Helper()
	manifest, err := releasemeta.ParseManifest(f.Manifest)
	if err != nil {
		t.Fatalf("parse fixture manifest: %v", err)
	}
	manifest.Tag = releaseverify.Tag(tag)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	f.Manifest = append(data, '\n')
	f.resign(t, releaseverify.CertificateIdentity(fixtureRepo), releaseverify.OIDCIssuer)
}

// assetNames maps every published filename to the bytes the fake release host
// should serve for it.
func (f *strictFixture) assetNames() map[string][]byte {
	return map[string][]byte{
		f.Asset:                                   f.Archive,
		releasemeta.ChecksumsName:                 f.Checksums,
		releaseverify.ManifestName(f.Tag):         f.Manifest,
		releaseverify.ManifestBundleName(f.Tag):   f.ManifestBundle,
		releaseverify.ChecksumsBundleName(f.Tag):  f.ChecksumsBundle,
		releaseverify.ProvenanceBundleName(f.Tag): f.ProvenanceBundle,
	}
}

// strictServer stands in for the GitHub release host for one fixture.
func strictServer(t *testing.T, f *strictFixture) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+fixtureRepo+"/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v%s"}`, f.Tag)
	})
	base := fmt.Sprintf("/%s/releases/download/v%s/", fixtureRepo, f.Tag)
	for name, body := range f.assetNames() {
		name, body := name, body
		mux.HandleFunc(base+name, func(w http.ResponseWriter, _ *http.Request) {
			if f.Missing[name] {
				http.NotFound(w, nil)
				return
			}
			_, _ = w.Write(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// fakeCosignOptions tunes the double so a test can stage a verifier that is
// absent, too old, or issued for a different repository.
type fakeCosignOptions struct {
	Version string // reported by `cosign version`; defaults to 3.1.1
}

// writeFakeCosign drops the cosign double into a fresh directory and returns
// the directory to prepend to PATH, the environment it needs, and the path of
// its invocation log.
//
// The double is deliberately strict about the flags it is handed: any attempt
// to relax the policy (an --insecure-ignore-* flag, a regexp identity matcher,
// or a raw --key) aborts with exit 90 and a FATAL line, so an installer that
// quietly weakens verification fails loudly instead of passing.
func writeFakeCosign(t *testing.T, opts fakeCosignOptions) (string, []string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "cosign-invocations.log")
	version := opts.Version
	if version == "" {
		version = "3.1.1"
	}

	script := `#!/bin/sh
# cosign test double — see internal/installscript/strict_fixture_test.go.
set -eu

log() { [ -n "${FAKE_COSIGN_LOG:-}" ] && printf '%s\n' "$*" >>"$FAKE_COSIGN_LOG" || true; }
die() { printf 'cosign: %s\n' "$1" >&2; exit 1; }

[ "$#" -gt 0 ] || die "no subcommand"
sub="$1"; shift

if [ "$sub" = "version" ]; then
  printf 'GitVersion:    v%s\n' "$FAKE_COSIGN_VERSION"
  exit 0
fi

bundle=""; identity=""; issuer=""; atype=""; artifact=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bundle) bundle="$2"; shift 2 ;;
    --certificate-identity) identity="$2"; shift 2 ;;
    --certificate-oidc-issuer) issuer="$2"; shift 2 ;;
    --type) atype="$2"; shift 2 ;;
    --insecure-ignore-*|--certificate-identity-regexp*|--certificate-oidc-issuer-regexp*|--key|--key=*)
      printf 'FATAL: installer weakened the cosign policy with %s\n' "$1" >&2
      exit 90
      ;;
    --*) shift ;;
    *) artifact="$1"; shift ;;
  esac
done

log "$sub bundle=$(basename "$bundle" 2>/dev/null || echo none) artifact=$(basename "$artifact" 2>/dev/null || echo none) identity=$identity issuer=$issuer type=$atype"

[ -n "$bundle" ] || die "no --bundle supplied"
[ -f "$bundle" ] || die "bundle not found: $bundle"
[ -n "$artifact" ] || die "no artifact supplied"
[ -f "$artifact" ] || die "artifact not found: $artifact"

field() { sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p" "$bundle"; }

kind="$(field kind)"
[ -n "$kind" ] || die "bundle is not a Sigstore bundle"
[ "$(field issuer)" = "$issuer" ] || die "certificate OIDC issuer does not match the bundle"
[ "$(field identity)" = "$identity" ] || die "certificate identity does not match the bundle"

if command -v sha256sum >/dev/null 2>&1; then
  digest="$(sha256sum "$artifact" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  digest="$(shasum -a 256 "$artifact" | awk '{print $1}')"
else
  die "no SHA-256 tool available"
fi

case "$sub" in
  verify-blob)
    [ "$kind" = "blob" ] || die "bundle is not a blob signature"
    [ "$(field sha256)" = "$digest" ] || die "signature does not cover $artifact"
    ;;
  verify-blob-attestation)
    [ "$kind" = "attestation" ] || die "bundle is not an attestation"
    [ "$atype" = "slsaprovenance1" ] || die "unexpected attestation type: $atype"
    grep -q "$digest" "$bundle" || die "attestation has no subject matching $artifact"
    ;;
  *) die "unsupported subcommand: $sub" ;;
esac

echo "Verified OK"
`
	path := filepath.Join(dir, "cosign")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write cosign double: %v", err)
	}
	env := []string{"FAKE_COSIGN_VERSION=" + version, "FAKE_COSIGN_LOG=" + logPath}
	return dir, env, logPath
}

// pathWithout returns the host PATH with every directory that contains a
// `cosign` executable removed, so "no verifier installed" can be staged
// without touching the host.
func pathWithout(t *testing.T, tool string) string {
	t.Helper()
	kept := make([]string, 0)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, tool)); err == nil {
			continue
		}
		kept = append(kept, dir)
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// buildTarArchive returns a .tar.gz holding an executable `okt` stub.
func buildTarArchive(t *testing.T, marker string) []byte {
	t.Helper()
	stub := oktStub(marker)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "okt", Mode: 0o755, Size: int64(len(stub))}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write([]byte(stub)); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// buildZipArchive returns a .zip holding an executable `okt.exe` stub. The
// entry carries mode 0755 so PowerShell's Expand-Archive restores the exec bit
// when these fixtures run under pwsh on a Unix host.
func buildZipArchive(t *testing.T, marker string) []byte {
	t.Helper()
	stub := oktStub(marker)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: "okt.exe", Method: zip.Deflate}
	header.SetMode(0o755)
	w, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatalf("zip header: %v", err)
	}
	if _, err := w.Write([]byte(stub)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func oktStub(marker string) string {
	return "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  --version) echo 'okt " + marker + " (test stub)' ;;\n" +
		"  setup) echo 'okt setup (test stub) ok' ;;\n" +
		"  *) echo \"okt stub: $*\" ;;\n" +
		"esac\n"
}

// assertNoBinary is the load-bearing assertion of every negative fixture: the
// install directory must be empty of an okt binary, so nothing executable ever
// reached the user's PATH.
func assertNoBinary(t *testing.T, installDir string, out string) {
	t.Helper()
	for _, name := range []string{"okt", "okt.exe"} {
		if _, err := os.Stat(filepath.Join(installDir, name)); !os.IsNotExist(err) {
			t.Fatalf("strict mode placed %s in the install dir despite a failed verification (stat err = %v)\noutput:\n%s", name, err, out)
		}
	}
}

// assertReason pins *why* a negative fixture failed. Without it a test could
// pass on an unrelated crash and still look like the trust gate worked.
func assertReason(t *testing.T, out, reason string) {
	t.Helper()
	if !strings.Contains(out, reason) {
		t.Fatalf("expected the installer to fail with %q, got:\n%s", reason, out)
	}
}

// readLog returns the cosign double's invocation log (empty when never called).
func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read cosign log: %v", err)
	}
	return string(data)
}

func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		if _, shasumErr := exec.LookPath("shasum"); shasumErr != nil {
			t.Skip("neither sha256sum nor shasum is available")
		}
	}
}
