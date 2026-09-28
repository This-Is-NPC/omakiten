// Package releaseverify authenticates a published Omakiten release before any
// downloaded byte is trusted.
//
// It consumes exactly the artifact set the release workflow publishes (see
// .github/workflows/release.yml and internal/releasemeta): a version-bound
// manifest, keyless Cosign blob signatures over that manifest and over
// checksums.txt, and a DSSE attestation carrying the SLSA v1 provenance
// statement whose subjects are the six release archives.
//
// The package is deliberately fail-closed. There is no unsigned mode, no
// "warn and continue", and no way for a caller to relax the certificate
// policy: the pinned OIDC issuer and workflow identity are compiled in, the
// matchers are exact-string (never regex), and a bundle that lacks a Rekor
// inclusion proof or a Fulcio SCT is refused. Callers that cannot obtain a
// trusted Sigstore root must abort rather than proceed.
//
// Both strict consumers share this package: the in-binary updater and the
// bootstrap installer verifier.
package releaseverify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"

	"omakiten/internal/releasemeta"
)

const (
	// OIDCIssuer is the only accepted certificate issuer. GitHub Actions'
	// OIDC provider signs the ephemeral Fulcio identity used by the
	// keyless `sign-and-attest` job; anything else is a different signer.
	OIDCIssuer = "https://token.actions.githubusercontent.com"

	// LegacyChecksumCutoff is the last release published without signed
	// metadata. Releases at or before it are checksum-only and can never be
	// authenticated after the fact, so `okt update` refuses them outright:
	// the documented transition is a verified reinstall of the first signed
	// release, not a silent checksum-over-TLS fallback.
	LegacyChecksumCutoff = "0.30.0"

	// releaseWorkflowRef is the workflow file and git ref baked into the
	// Fulcio SAN by GitHub's OIDC claims.
	releaseWorkflowRef = "/.github/workflows/release.yml@refs/heads/master"
)

var (
	// ErrMissingArtifact reports a release that did not publish one of the
	// signed metadata files at all. Treated as a hard failure so a stripped
	// release cannot degrade the client to checksum-only trust.
	ErrMissingArtifact = errors.New("signed release artifact is missing")

	// ErrUnsignedTarget reports a target release at or before the signed
	// cutoff.
	ErrUnsignedTarget = errors.New("target release predates signed releases")

	// ErrNotAnUpgrade reports a replayed or downgraded target version.
	ErrNotAnUpgrade = errors.New("target release is not an upgrade")

	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	versionPattern    = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)
)

// CertificateIdentity is the exact Fulcio SAN the release workflow presents.
func CertificateIdentity(repository string) string {
	return "https://github.com/" + repository + releaseWorkflowRef
}

// Tag renders the git tag for a normalized version ("0.31.0" -> "v0.31.0").
func Tag(version string) string { return "v" + strings.TrimPrefix(version, "v") }

// ManifestName is the published version-bound manifest asset.
func ManifestName(version string) string { return "release-manifest-" + Tag(version) + ".json" }

// ManifestBundleName is the Sigstore bundle covering the manifest bytes.
func ManifestBundleName(version string) string {
	return "release-manifest-" + Tag(version) + ".sigstore.json"
}

// ChecksumsBundleName is the Sigstore bundle covering checksums.txt.
func ChecksumsBundleName(version string) string {
	return "checksums-" + Tag(version) + ".sigstore.json"
}

// ProvenanceBundleName is the DSSE attestation carrying SLSA provenance.
func ProvenanceBundleName(version string) string {
	return "release-provenance-" + Tag(version) + ".sigstore.json"
}

// ChecksumsName is the GoReleaser checksum file.
const ChecksumsName = releasemeta.ChecksumsName

// Release is the complete byte set a strict consumer must hold before it can
// trust a downloaded archive.
type Release struct {
	Repository       string
	Version          string
	ArchiveName      string
	Archive          []byte
	Checksums        []byte
	Manifest         []byte
	ManifestBundle   []byte
	ChecksumsBundle  []byte
	ProvenanceBundle []byte
}

// Result carries the facts the caller may act on once verification passed.
type Result struct {
	Tag           string
	SourceCommit  string
	ArchiveSHA256 string
}

// Verifier authenticates releases of one repository against one trusted
// Sigstore root.
type Verifier struct {
	inner    *verify.Verifier
	identity verify.CertificateIdentity
	repo     string
}

// PinnedIdentity builds the exact-match certificate policy. Both matchers are
// literal strings and both regex fields stay zero — a regex identity would let
// any repository or ref whose SAN merely contains the expected substring pass.
func PinnedIdentity(repository string) (verify.CertificateIdentity, error) {
	if !repositoryPattern.MatchString(repository) {
		return verify.CertificateIdentity{}, fmt.Errorf("invalid repository %q", repository)
	}
	return verify.NewShortCertificateIdentity(OIDCIssuer, "", CertificateIdentity(repository), "")
}

// New builds a verifier that requires, for every bundle it inspects, a Fulcio
// certificate with an embedded SCT, a Rekor transparency-log entry, and an
// observer timestamp. None of these thresholds is caller-configurable: there
// is deliberately no option struct, so no call site can dial the policy down.
func New(trust root.TrustedMaterial, repository string) (*Verifier, error) {
	return newVerifier(trust, repository,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
}

func newVerifier(trust root.TrustedMaterial, repository string, options ...verify.VerifierOption) (*Verifier, error) {
	if trust == nil {
		return nil, errors.New("trusted Sigstore root is unavailable; refusing to verify")
	}
	identity, err := PinnedIdentity(repository)
	if err != nil {
		return nil, err
	}
	inner, err := verify.NewVerifier(trust, options...)
	if err != nil {
		return nil, fmt.Errorf("build sigstore verifier: %w", err)
	}
	return &Verifier{inner: inner, identity: identity, repo: repository}, nil
}

// Verify authenticates the release and returns the digest the caller must see
// on the archive it downloaded. Every failure path returns an error and no
// caller-usable value, so there is no shape in which a partially verified
// release can be extracted or installed.
//
// Order matters: signatures are checked before any manifest or checksum text
// is parsed, and the archive digest is compared only against values that came
// out of an authenticated document.
func (v *Verifier) Verify(rel Release) (Result, error) {
	if err := v.validate(rel); err != nil {
		return Result{}, err
	}

	// 1. Authenticate the manifest bytes.
	if err := v.verifyBlob(rel.ManifestBundle, rel.Manifest); err != nil {
		return Result{}, fmt.Errorf("verify signed manifest: %w", err)
	}
	// 2. Authenticate the checksums.txt bytes.
	if err := v.verifyBlob(rel.ChecksumsBundle, rel.Checksums); err != nil {
		return Result{}, fmt.Errorf("verify signed checksums.txt: %w", err)
	}

	// 3. Only now is it safe to read the manifest, and only for this tag.
	manifest, err := releasemeta.ParseManifest(rel.Manifest)
	if err != nil {
		return Result{}, err
	}
	identity := releasemeta.SourceIdentity{
		Repository: v.repo,
		Tag:        Tag(rel.Version),
		Commit:     manifest.SourceCommit,
	}
	if err := releasemeta.ValidateIdentity(identity); err != nil {
		return Result{}, err
	}
	if err := releasemeta.BindManifestIdentity(manifest, identity); err != nil {
		return Result{}, err
	}
	if err := releasemeta.ValidateManifestArtifacts(manifest); err != nil {
		return Result{}, err
	}

	// 4. Authenticate the provenance over the exact archive bytes: the DSSE
	//    statement must carry this archive's digest as a subject.
	archiveDigest := sha256.Sum256(rel.Archive)
	statementBytes, err := v.verifyAttestation(rel.ProvenanceBundle, archiveDigest[:])
	if err != nil {
		return Result{}, fmt.Errorf("verify release provenance: %w", err)
	}
	statement, err := releasemeta.ParseStatement(statementBytes)
	if err != nil {
		return Result{}, fmt.Errorf("verify release provenance: %w", err)
	}
	if err := releasemeta.VerifyStatement(statement, manifest, identity); err != nil {
		return Result{}, fmt.Errorf("verify release provenance: %w", err)
	}

	// 5. Cross-bind the three authenticated views of the archive digest.
	wantDigest, ok := manifest.DigestFor(rel.ArchiveName)
	if !ok {
		return Result{}, fmt.Errorf("signed manifest does not list archive %q", rel.ArchiveName)
	}
	checksums, err := releasemeta.ParseChecksums(bytes.NewReader(rel.Checksums))
	if err != nil {
		return Result{}, err
	}
	if checksums[rel.ArchiveName] != wantDigest {
		return Result{}, fmt.Errorf("signed checksums.txt digest %q for %s disagrees with the signed manifest digest %q",
			checksums[rel.ArchiveName], rel.ArchiveName, wantDigest)
	}
	got := hex.EncodeToString(archiveDigest[:])
	if got != wantDigest {
		return Result{}, fmt.Errorf("archive %s digest %s does not match the authenticated digest %s", rel.ArchiveName, got, wantDigest)
	}

	return Result{Tag: identity.Tag, SourceCommit: manifest.SourceCommit, ArchiveSHA256: got}, nil
}

func (v *Verifier) validate(rel Release) error {
	if rel.Repository != v.repo {
		return fmt.Errorf("release repository %q does not match the pinned %q", rel.Repository, v.repo)
	}
	if !versionPattern.MatchString(rel.Version) {
		return fmt.Errorf("invalid release version %q", rel.Version)
	}
	if strings.TrimSpace(rel.ArchiveName) == "" {
		return errors.New("release archive name is required")
	}
	for _, missing := range []struct {
		data  []byte
		label string
	}{
		{rel.Archive, "release archive"},
		{rel.Checksums, "checksums.txt"},
		{rel.Manifest, "release manifest"},
		{rel.ManifestBundle, "manifest signature bundle"},
		{rel.ChecksumsBundle, "checksums signature bundle"},
		{rel.ProvenanceBundle, "provenance bundle"},
	} {
		if len(missing.data) == 0 {
			return fmt.Errorf("%w: %s is missing", ErrMissingArtifact, missing.label)
		}
	}
	return nil
}

// verifyBlob authenticates a detached blob signature over artifact.
func (v *Verifier) verifyBlob(bundleJSON, artifact []byte) error {
	entity, err := parseBundle(bundleJSON)
	if err != nil {
		return err
	}
	_, err = v.inner.Verify(entity, verify.NewPolicy(
		verify.WithArtifact(bytes.NewReader(artifact)),
		verify.WithCertificateIdentity(v.identity),
	))
	return err
}

// verifyAttestation authenticates a DSSE attestation and requires the supplied
// digest to appear among the statement's subjects, then returns the raw
// statement payload.
func (v *Verifier) verifyAttestation(bundleJSON, digest []byte) ([]byte, error) {
	entity, err := parseBundle(bundleJSON)
	if err != nil {
		return nil, err
	}
	if _, err := v.inner.Verify(entity, verify.NewPolicy(
		verify.WithArtifactDigest("sha256", digest),
		verify.WithCertificateIdentity(v.identity),
	)); err != nil {
		return nil, err
	}
	content, err := entity.SignatureContent()
	if err != nil {
		return nil, err
	}
	envelope := content.EnvelopeContent()
	if envelope == nil {
		return nil, errors.New("provenance bundle does not carry a DSSE envelope")
	}
	raw := envelope.RawEnvelope()
	if raw == nil || raw.PayloadType != "application/vnd.in-toto+json" {
		return nil, errors.New("provenance payload is not in-toto JSON")
	}
	payload, err := base64.StdEncoding.DecodeString(raw.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode provenance payload: %w", err)
	}
	return payload, nil
}

// parseBundle decodes bundle JSON and enforces the Rekor inclusion-proof
// requirement. sigstore-go accepts an inclusion *promise* alone for older
// bundle versions; a promise is only the log's word that it will include the
// entry, so strict consumers hold out for the proof.
func parseBundle(data []byte) (*bundle.Bundle, error) {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(data); err != nil {
		return nil, fmt.Errorf("decode sigstore bundle: %w", err)
	}
	if !b.HasInclusionProof() {
		return nil, errors.New("sigstore bundle has no Rekor inclusion proof")
	}
	return &b, nil
}

// TrustedRoot loads the Sigstore public-good trust anchors through TUF,
// staging the refreshed metadata under cacheDir.
//
// The directory is created 0700 before the TUF client touches it so a
// world-writable cache cannot be pre-seeded with attacker metadata. A refresh
// failure — offline host, unreachable TUF mirror, expired local metadata — is
// returned as an error and never degrades into "verify with whatever is on
// disk" or "skip verification": strict consumers must abort instead.
func TrustedRoot(ctx context.Context, cacheDir string) (root.TrustedMaterial, error) {
	if strings.TrimSpace(cacheDir) == "" {
		return nil, errors.New("sigstore trusted-root cache directory is required")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("prepare sigstore root cache: %w", err)
	}
	if err := os.Chmod(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure sigstore root cache: %w", err)
	}
	trusted, err := root.FetchTrustedRootWithOptions(tuf.DefaultOptions().WithCachePath(cacheDir).WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("refresh trusted sigstore root: %w", err)
	}
	return trusted, nil
}

// RequireSignedTarget refuses any target release at or before the signed
// cutoff. There is no override: an old release has no authenticated metadata
// and never will, so the only safe transition is a verified reinstall.
func RequireSignedTarget(version string) error {
	cmp, err := CompareVersions(version, LegacyChecksumCutoff)
	if err != nil {
		return err
	}
	if cmp <= 0 {
		return fmt.Errorf("%w: %s is at or before the signed-release cutoff %s", ErrUnsignedTarget, version, LegacyChecksumCutoff)
	}
	return nil
}

// RequireUpgrade refuses a target that is not strictly newer than the running
// version, which covers both a replayed identical version and a rollback to a
// known-vulnerable build.
func RequireUpgrade(current, target string) error {
	cmp, err := CompareVersions(target, current)
	if err != nil {
		return err
	}
	if cmp <= 0 {
		return fmt.Errorf("%w: target %s is not newer than the installed %s", ErrNotAnUpgrade, target, current)
	}
	return nil
}

// CompareVersions orders two normalized release versions. Prereleases are
// ordered before their release (SemVer §11), which is enough for the tags the
// release workflow can emit; build metadata is not published and is rejected
// by versionPattern.
func CompareVersions(a, b string) (int, error) {
	left, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	right, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	if cmp := compareVersionCore(left, right); cmp != 0 {
		return cmp, nil
	}
	return compareVersionPrerelease(left.pre, right.pre), nil
}

func compareVersionCore(left, right parsedVersion) int {
	for i := range left.core {
		cmp := compareNumericIdentifier(left.core[i], right.core[i])
		if cmp != 0 {
			return cmp
		}
	}
	return 0
}

func compareVersionPrerelease(left, right []string) int {
	switch {
	case len(left) == 0 && len(right) == 0:
		return 0
	case len(left) == 0:
		return 1
	case len(right) == 0:
		return -1
	}
	for i := 0; i < len(left) && i < len(right); i++ {
		leftNumeric := isNumericIdentifier(left[i])
		rightNumeric := isNumericIdentifier(right[i])
		if cmp := comparePrereleaseIdentifier(left[i], right[i], leftNumeric, rightNumeric); cmp != 0 {
			return cmp
		}
	}
	switch {
	case len(left) < len(right):
		return -1
	case len(left) > len(right):
		return 1
	default:
		return 0
	}
}

func comparePrereleaseIdentifier(left, right string, leftNumeric, rightNumeric bool) int {
	if leftNumeric && !rightNumeric {
		return -1
	}
	if !leftNumeric && rightNumeric {
		return 1
	}
	if leftNumeric {
		return compareNumericIdentifier(left, right)
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

type parsedVersion struct {
	core [3]string
	pre  []string
}

func parseVersion(value string) (parsedVersion, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "v")
	if !versionPattern.MatchString(trimmed) {
		return parsedVersion{}, fmt.Errorf("invalid release version %q", value)
	}
	core, pre, _ := strings.Cut(trimmed, "-")
	parts := strings.SplitN(core, ".", 3)
	var out parsedVersion
	copy(out.core[:], parts)
	if pre != "" {
		out.pre = strings.Split(pre, ".")
	}
	return out, nil
}

func isNumericIdentifier(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func compareNumericIdentifier(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}
