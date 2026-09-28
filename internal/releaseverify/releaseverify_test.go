package releaseverify_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"google.golang.org/protobuf/encoding/protojson"

	"omakiten/internal/releasemeta"
	"omakiten/internal/releaseverify"
)

const (
	testRepo    = "This-Is-NPC/omakiten"
	testVersion = "0.31.0"
	testTag     = "v" + testVersion
	testCommit  = "0123456789abcdef0123456789abcdef01234567"
	testArchive = "okt_Linux_x86_64.tar.gz"
	bundleMedia = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

func testIdentity() string { return releaseverify.CertificateIdentity(testRepo) }

// release is the full set of bytes `okt update` downloads for one tag,
// assembled so a test can tamper with exactly one of them.
type release struct {
	archive          []byte
	checksums        []byte
	manifest         []byte
	manifestBundle   []byte
	checksumsBundle  []byte
	provenanceBundle []byte
}

func (r release) input() releaseverify.Release {
	return releaseverify.Release{
		Repository:       testRepo,
		Version:          testVersion,
		ArchiveName:      testArchive,
		Archive:          r.archive,
		Checksums:        r.checksums,
		Manifest:         r.manifest,
		ManifestBundle:   r.manifestBundle,
		ChecksumsBundle:  r.checksumsBundle,
		ProvenanceBundle: r.provenanceBundle,
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// buildRelease produces the exact artifact shape the release workflow
// publishes: six archives, a checksums.txt covering them, a version-bound
// manifest, keyless blob signatures over the manifest and checksums.txt, and
// a DSSE attestation carrying the SLSA v1 statement.
func buildRelease(t *testing.T, sigstore *ca.VirtualSigstore, identity, issuer string) release {
	t.Helper()

	names := releasemeta.ArchiveNames()
	archives := make(map[string][]byte, len(names))
	var checksums strings.Builder
	for _, name := range names {
		archives[name] = []byte("archive-payload-" + name)
		fmt.Fprintf(&checksums, "%s  %s\n", sha256Hex(archives[name]), name)
	}
	checksumsBytes := []byte(checksums.String())

	artifacts := make([]releasemeta.Artifact, 0, len(names)+1)
	subjects := make([]releasemeta.Subject, 0, len(names))
	for _, name := range names {
		artifacts = append(artifacts, releasemeta.Artifact{Name: name, SHA256: sha256Hex(archives[name])})
		subjects = append(subjects, releasemeta.Subject{Name: name, Digest: map[string]string{"sha256": sha256Hex(archives[name])}})
	}
	artifacts = append(artifacts, releasemeta.Artifact{Name: releasemeta.ChecksumsName, SHA256: sha256Hex(checksumsBytes)})

	manifest := releasemeta.Manifest{
		SchemaVersion: 1,
		Repository:    testRepo,
		Tag:           testTag,
		SourceCommit:  testCommit,
		Artifacts:     artifacts,
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	workflow := releaseverify.CertificateIdentity(testRepo)
	statement := releasemeta.Statement{
		Type:          "https://in-toto.io/Statement/v1",
		Subject:       subjects,
		PredicateType: "https://slsa.dev/provenance/v1",
		Predicate: releasemeta.ProvenancePredicate{
			BuildDefinition: releasemeta.BuildDefinition{
				BuildType: workflow,
				ExternalParameters: releasemeta.SourceIdentity{
					Repository: testRepo,
					Tag:        testTag,
					Commit:     testCommit,
				},
				InternalParameters: map[string]string{},
				ResolvedDependencies: []releasemeta.ResourceDescriptor{{
					URI:    "git+https://github.com/" + testRepo + "@refs/tags/" + testTag,
					Digest: map[string]string{"gitCommit": testCommit},
				}},
			},
			RunDetails: releasemeta.RunDetails{
				Builder:  releasemeta.Builder{ID: workflow},
				Metadata: releasemeta.RunMetadata{InvocationID: "https://github.com/" + testRepo + "/actions/runs/1/attempts/1"},
			},
		},
	}
	statementBytes, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal statement: %v", err)
	}

	return release{
		archive:          archives[testArchive],
		checksums:        checksumsBytes,
		manifest:         manifestBytes,
		manifestBundle:   signBlob(t, sigstore, identity, issuer, manifestBytes, nil),
		checksumsBundle:  signBlob(t, sigstore, identity, issuer, checksumsBytes, nil),
		provenanceBundle: attest(t, sigstore, identity, issuer, statementBytes, nil),
	}
}

func signBlob(t *testing.T, sigstore *ca.VirtualSigstore, identity, issuer string, artifact []byte, mutate func(*protobundle.Bundle)) []byte {
	t.Helper()
	entity, err := sigstore.Sign(identity, issuer, artifact)
	if err != nil {
		t.Fatalf("sign blob: %v", err)
	}
	return marshalEntity(t, sigstore, entity, "hashedrekord", "0.0.1", mutate)
}

func attest(t *testing.T, sigstore *ca.VirtualSigstore, identity, issuer string, statement []byte, mutate func(*protobundle.Bundle)) []byte {
	t.Helper()
	entity, err := sigstore.Attest(identity, issuer, statement)
	if err != nil {
		t.Fatalf("attest: %v", err)
	}
	return marshalEntity(t, sigstore, entity, "intoto", "0.0.2", mutate)
}

// marshalEntity serializes a virtual-Sigstore entity into the same v0.3
// protobuf bundle JSON `cosign sign-blob --bundle` writes, so the verifier
// under test parses real bytes rather than an in-memory stub.
func marshalEntity(t *testing.T, sigstore *ca.VirtualSigstore, entity *ca.TestEntity, kind, version string, mutate func(*protobundle.Bundle)) []byte {
	t.Helper()
	content, sig, entries, timestamps := entityContents(t, entity)

	pb := &protobundle.Bundle{
		MediaType: bundleMedia,
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content: &protobundle.VerificationMaterial_Certificate{
				Certificate: &protocommon.X509Certificate{RawBytes: content.Certificate().Raw},
			},
		},
	}
	addTlogEntries(t, sigstore, pb.VerificationMaterial, entries, kind, version)
	if len(timestamps) > 0 {
		pb.VerificationMaterial.TimestampVerificationData = timestampData(timestamps)
	}
	setSignatureContent(t, pb, sig)

	if mutate != nil {
		mutate(pb)
	}
	data, err := protojson.Marshal(pb)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	return data
}

func entityContents(t *testing.T, entity *ca.TestEntity) (verify.VerificationContent, verify.SignatureContent, []*tlog.Entry, [][]byte) {
	t.Helper()
	content, err := entity.VerificationContent()
	if err != nil {
		t.Fatalf("verification content: %v", err)
	}
	sig, err := entity.SignatureContent()
	if err != nil {
		t.Fatalf("signature content: %v", err)
	}
	entries, err := entity.TlogEntries()
	if err != nil {
		t.Fatalf("tlog entries: %v", err)
	}
	timestamps, err := entity.Timestamps()
	if err != nil {
		t.Fatalf("timestamps: %v", err)
	}
	return content, sig, entries, timestamps
}

func addTlogEntries(t *testing.T, sigstore *ca.VirtualSigstore, material *protobundle.VerificationMaterial, entries []*tlog.Entry, kind, version string) {
	for _, entry := range entries {
		tle := entry.TransparencyLogEntry()
		tle.KindVersion = &protorekor.KindVersion{Kind: kind, Version: version}
		if tle.InclusionProof == nil {
			tle.InclusionProof = inclusionProof(t, sigstore, tle.CanonicalizedBody)
			tle.LogIndex = tle.InclusionProof.LogIndex
		}
		material.TlogEntries = append(material.TlogEntries, tle)
	}
}

func timestampData(timestamps [][]byte) *protobundle.TimestampVerificationData {
	data := &protobundle.TimestampVerificationData{}
	for _, raw := range timestamps {
		data.Rfc3161Timestamps = append(data.Rfc3161Timestamps, &protocommon.RFC3161SignedTimestamp{SignedTimestamp: raw})
	}
	return data
}

func setSignatureContent(t *testing.T, pb *protobundle.Bundle, sig verify.SignatureContent) {
	if envelope := sig.EnvelopeContent(); envelope != nil {
		raw := envelope.RawEnvelope()
		payload, err := base64.StdEncoding.DecodeString(raw.Payload)
		if err != nil {
			t.Fatalf("decode dsse payload: %v", err)
		}
		out := &protodsse.Envelope{PayloadType: raw.PayloadType, Payload: payload}
		for _, s := range raw.Signatures {
			decoded, err := base64.StdEncoding.DecodeString(s.Sig)
			if err != nil {
				t.Fatalf("decode dsse signature: %v", err)
			}
			out.Signatures = append(out.Signatures, &protodsse.Signature{Sig: decoded, Keyid: s.KeyID})
		}
		pb.Content = &protobundle.Bundle_DsseEnvelope{DsseEnvelope: out}
		return
	}
	message := sig.MessageSignatureContent()
	pb.Content = &protobundle.Bundle_MessageSignature{MessageSignature: &protocommon.MessageSignature{
		MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: message.Digest()},
		Signature:     message.Signature(),
	}}
}

// inclusionProof builds the Rekor inclusion proof the virtual CA can sign for
// a canonicalized entry body.
func inclusionProof(t *testing.T, sigstore *ca.VirtualSigstore, body []byte) *protorekor.InclusionProof {
	t.Helper()
	proof, err := sigstore.GetInclusionProof(body)
	if err != nil {
		t.Fatalf("inclusion proof: %v", err)
	}
	rootHash, err := hex.DecodeString(*proof.RootHash)
	if err != nil {
		t.Fatalf("decode inclusion root hash: %v", err)
	}
	hashes := make([][]byte, 0, len(proof.Hashes))
	for _, h := range proof.Hashes {
		decoded, err := hex.DecodeString(h)
		if err != nil {
			t.Fatalf("decode inclusion hash: %v", err)
		}
		hashes = append(hashes, decoded)
	}
	return &protorekor.InclusionProof{
		LogIndex:   *proof.LogIndex,
		RootHash:   rootHash,
		TreeSize:   *proof.TreeSize,
		Hashes:     hashes,
		Checkpoint: &protorekor.Checkpoint{Envelope: *proof.Checkpoint},
	}
}

// stripInclusionProof deletes the Rekor inclusion proof so the bundle carries
// only an inclusion promise.
func stripInclusionProof(pb *protobundle.Bundle) {
	for _, entry := range pb.VerificationMaterial.TlogEntries {
		entry.InclusionProof = nil
	}
}

func newVerifier(t *testing.T, sigstore *ca.VirtualSigstore) *releaseverify.Verifier {
	t.Helper()
	v, err := releaseverify.NewWithoutSCTPolicy(sigstore, testRepo)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	return v
}

// TestProductionVerifierRequiresFulcioSCT proves the shipped constructor keeps
// the SCT threshold on: the virtual CA issues Fulcio certificates with no
// embedded Signed Certificate Timestamp, and a release signed that way must be
// refused even though every other binding is genuine.
func TestProductionVerifierRequiresFulcioSCT(t *testing.T) {
	t.Parallel()
	sigstore, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("virtual sigstore: %v", err)
	}
	rel := buildRelease(t, sigstore, testIdentity(), releaseverify.OIDCIssuer)

	verifier, err := releaseverify.New(sigstore, testRepo)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	result, err := verifier.Verify(rel.input())
	if err == nil {
		t.Fatalf("expected SCT rejection, got result %+v", result)
	}
	if !strings.Contains(err.Error(), "certificate timestamp") {
		t.Errorf("error %q does not report the missing SCT", err.Error())
	}
}

func TestVerifyAcceptsGenuineSignedRelease(t *testing.T) {
	t.Parallel()
	sigstore, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("virtual sigstore: %v", err)
	}
	rel := buildRelease(t, sigstore, testIdentity(), releaseverify.OIDCIssuer)

	result, err := newVerifier(t, sigstore).Verify(rel.input())
	if err != nil {
		t.Fatalf("verify genuine release: %v", err)
	}
	if result.Tag != testTag {
		t.Errorf("tag: got %q want %q", result.Tag, testTag)
	}
	if result.SourceCommit != testCommit {
		t.Errorf("source commit: got %q want %q", result.SourceCommit, testCommit)
	}
	if want := sha256Hex(rel.archive); result.ArchiveSHA256 != want {
		t.Errorf("archive digest: got %q want %q", result.ArchiveSHA256, want)
	}
}

func TestVerifyRejectsTamperedAndUnsignedInputs(t *testing.T) {
	t.Parallel()
	sigstore, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("virtual sigstore: %v", err)
	}
	forged, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatalf("forged sigstore: %v", err)
	}
	verifyTamperedCases(t, sigstore, forged)
}

func verifyTamperedCases(t *testing.T, sigstore, forged *ca.VirtualSigstore) {
	cases := []struct {
		name    string
		mutate  func(t *testing.T, rel *release)
		wantSub string
	}{
		{
			name:    "missing manifest bundle",
			mutate:  func(_ *testing.T, rel *release) { rel.manifestBundle = nil },
			wantSub: "manifest signature bundle is missing",
		},
		{
			name:    "missing checksums bundle",
			mutate:  func(_ *testing.T, rel *release) { rel.checksumsBundle = nil },
			wantSub: "checksums signature bundle is missing",
		},
		{
			name:    "missing provenance bundle",
			mutate:  func(_ *testing.T, rel *release) { rel.provenanceBundle = nil },
			wantSub: "provenance bundle is missing",
		},
		{
			name:    "modified manifest",
			mutate:  func(_ *testing.T, rel *release) { rel.manifest = append(rel.manifest, ' ') },
			wantSub: "manifest",
		},
		{
			name:    "modified checksums",
			mutate:  func(_ *testing.T, rel *release) { rel.checksums = append(rel.checksums, '\n') },
			wantSub: "checksums.txt",
		},
		{
			name:    "tampered archive bytes",
			mutate:  func(_ *testing.T, rel *release) { rel.archive = append(rel.archive, 'x') },
			wantSub: "provenance",
		},
		{
			name: "signed by a different workflow identity",
			mutate: func(t *testing.T, rel *release) {
				replacement := buildRelease(t, sigstore, releaseverify.CertificateIdentity("attacker/omakiten"), releaseverify.OIDCIssuer)
				rel.manifestBundle = replacement.manifestBundle
			},
			wantSub: "manifest",
		},
		{
			name: "signed under a different OIDC issuer",
			mutate: func(t *testing.T, rel *release) {
				replacement := buildRelease(t, sigstore, testIdentity(), "https://accounts.google.com")
				rel.manifestBundle = replacement.manifestBundle
			},
			wantSub: "manifest",
		},
		{
			name: "signed by an untrusted Fulcio root",
			mutate: func(t *testing.T, rel *release) {
				replacement := buildRelease(t, forged, testIdentity(), releaseverify.OIDCIssuer)
				rel.manifestBundle = replacement.manifestBundle
			},
			wantSub: "manifest",
		},
		{
			name: "manifest bundle without a Rekor inclusion proof",
			mutate: func(t *testing.T, rel *release) {
				rel.manifestBundle = signBlob(t, sigstore, testIdentity(), releaseverify.OIDCIssuer, rel.manifest, stripInclusionProof)
			},
			wantSub: "inclusion proof",
		},
		{
			name: "manifest bound to a different tag",
			mutate: func(t *testing.T, rel *release) {
				var manifest releasemeta.Manifest
				if err := json.Unmarshal(rel.manifest, &manifest); err != nil {
					t.Fatalf("decode manifest: %v", err)
				}
				manifest.Tag = "v0.30.9"
				replaced, err := json.Marshal(manifest)
				if err != nil {
					t.Fatalf("marshal manifest: %v", err)
				}
				rel.manifest = replaced
				rel.manifestBundle = signBlob(t, sigstore, testIdentity(), releaseverify.OIDCIssuer, replaced, nil)
			},
			wantSub: "tag",
		},
		{
			name: "manifest bound to a different repository",
			mutate: func(t *testing.T, rel *release) {
				var manifest releasemeta.Manifest
				if err := json.Unmarshal(rel.manifest, &manifest); err != nil {
					t.Fatalf("decode manifest: %v", err)
				}
				manifest.Repository = "attacker/omakiten"
				replaced, err := json.Marshal(manifest)
				if err != nil {
					t.Fatalf("marshal manifest: %v", err)
				}
				rel.manifest = replaced
				rel.manifestBundle = signBlob(t, sigstore, testIdentity(), releaseverify.OIDCIssuer, replaced, nil)
			},
			wantSub: "repository",
		},
		{
			name: "checksums line disagrees with the manifest digest",
			mutate: func(t *testing.T, rel *release) {
				swapped := strings.Replace(string(rel.checksums), sha256Hex(rel.archive), strings.Repeat("a", 64), 1)
				rel.checksums = []byte(swapped)
				rel.checksumsBundle = signBlob(t, sigstore, testIdentity(), releaseverify.OIDCIssuer, rel.checksums, nil)
			},
			wantSub: "checksums.txt",
		},
		{
			name: "provenance signed by a foreign root",
			mutate: func(t *testing.T, rel *release) {
				replacement := buildRelease(t, forged, testIdentity(), releaseverify.OIDCIssuer)
				rel.provenanceBundle = replacement.provenanceBundle
			},
			wantSub: "provenance",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rel := buildRelease(t, sigstore, testIdentity(), releaseverify.OIDCIssuer)
			tc.mutate(t, &rel)
			result, err := newVerifier(t, sigstore).Verify(rel.input())
			if err == nil {
				t.Fatalf("expected rejection, got result %+v", result)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestCertificateIdentityIsPinnedWithoutRegexes(t *testing.T) {
	t.Parallel()
	id, err := releaseverify.PinnedIdentity(testRepo)
	if err != nil {
		t.Fatalf("pinned identity: %v", err)
	}
	if got := id.SubjectAlternativeName.SubjectAlternativeName; got != testIdentity() {
		t.Errorf("SAN: got %q want %q", got, testIdentity())
	}
	if got := id.Issuer.Issuer; got != releaseverify.OIDCIssuer {
		t.Errorf("issuer: got %q want %q", got, releaseverify.OIDCIssuer)
	}
	if got := id.SubjectAlternativeName.Regexp.String(); got != "" {
		t.Errorf("SAN regexp must stay empty, got %q", got)
	}
	if got := id.Issuer.Regexp.String(); got != "" {
		t.Errorf("issuer regexp must stay empty, got %q", got)
	}
}

func TestRequireSignedTargetRejectsCutoffAndOlder(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"0.1.0", "0.29.9", "0.30.0"} {
		if err := releaseverify.RequireSignedTarget(version); err == nil {
			t.Errorf("version %s at or before the cutoff must be refused", version)
		}
	}
	for _, version := range []string{"0.30.1", "0.31.0", "1.0.0"} {
		if err := releaseverify.RequireSignedTarget(version); err != nil {
			t.Errorf("version %s after the cutoff must be accepted: %v", version, err)
		}
	}
}

func TestRequireUpgradeRejectsReplayAndDowngrade(t *testing.T) {
	t.Parallel()
	cases := []struct {
		current string
		target  string
		wantErr bool
	}{
		{current: "0.31.0", target: "0.32.0", wantErr: false},
		{current: "0.31.0", target: "0.31.0", wantErr: true},
		{current: "0.31.0", target: "0.30.9", wantErr: true},
		{current: "1.0.0", target: "0.99.0", wantErr: true},
		{current: "0.9.0", target: "0.10.0", wantErr: false},
	}
	for _, tc := range cases {
		err := releaseverify.RequireUpgrade(tc.current, tc.target)
		if tc.wantErr && err == nil {
			t.Errorf("current=%s target=%s: expected rejection", tc.current, tc.target)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("current=%s target=%s: unexpected rejection: %v", tc.current, tc.target, err)
		}
	}
}

func TestCompareVersionsUsesSemVerPrereleasePrecedence(t *testing.T) {
	t.Parallel()
	ordered := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}
	for i := 0; i < len(ordered)-1; i++ {
		got, err := releaseverify.CompareVersions(ordered[i], ordered[i+1])
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q): %v", ordered[i], ordered[i+1], err)
		}
		if got != -1 {
			t.Errorf("CompareVersions(%q, %q) = %d, want -1", ordered[i], ordered[i+1], got)
		}
	}

	for _, invalid := range []string{"1.0.0-01", "1.0.0-alpha..1", "01.0.0", "1.0.0-"} {
		if _, err := releaseverify.CompareVersions(invalid, "1.0.0"); err == nil {
			t.Errorf("CompareVersions accepted invalid SemVer %q", invalid)
		}
	}
}

func TestAssetNamesMatchPublishedRelease(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		releaseverify.ManifestName(testVersion):         "release-manifest-v0.31.0.json",
		releaseverify.ManifestBundleName(testVersion):   "release-manifest-v0.31.0.sigstore.json",
		releaseverify.ChecksumsBundleName(testVersion):  "checksums-v0.31.0.sigstore.json",
		releaseverify.ProvenanceBundleName(testVersion): "release-provenance-v0.31.0.sigstore.json",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("asset name: got %q want %q", got, want)
		}
	}
}
