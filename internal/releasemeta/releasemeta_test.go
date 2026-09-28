package releasemeta

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testRepository = "This-Is-NPC/omakiten"
	testTag        = "v0.31.0"
	testCommit     = "0123456789abcdef0123456789abcdef01234567"
)

func TestGenerateAndVerifyReleaseMetadata(t *testing.T) {
	t.Parallel()

	dist := writeReleaseFixture(t)
	manifest, statement, err := Generate(GenerateOptions{
		Dist:         dist,
		Repository:   testRepository,
		Tag:          testTag,
		Commit:       testCommit,
		InvocationID: "https://github.com/This-Is-NPC/omakiten/actions/runs/123/attempts/1",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if manifest.SchemaVersion != 1 {
		t.Fatalf("manifest schema version = %d, want 1", manifest.SchemaVersion)
	}
	if manifest.Repository != testRepository || manifest.Tag != testTag || manifest.SourceCommit != testCommit {
		t.Fatalf("manifest identity = %s %s %s", manifest.Repository, manifest.Tag, manifest.SourceCommit)
	}
	if len(manifest.Artifacts) != 7 {
		t.Fatalf("manifest artifact count = %d, want 7", len(manifest.Artifacts))
	}
	if statement.Type != inTotoStatementType || statement.PredicateType != slsaProvenanceType {
		t.Fatalf("statement types = %q %q", statement.Type, statement.PredicateType)
	}
	if len(statement.Subject) != 6 {
		t.Fatalf("provenance subject count = %d, want 6", len(statement.Subject))
	}
	if statement.Predicate.BuildDefinition.ExternalParameters.Tag != testTag ||
		statement.Predicate.BuildDefinition.ExternalParameters.Commit != testCommit {
		t.Fatalf("provenance source identity = %#v", statement.Predicate.BuildDefinition.ExternalParameters)
	}

	manifestPath := filepath.Join(dist, "release-manifest-v0.31.0.json")
	writeJSON(t, manifestPath, manifest)
	bundlePath := filepath.Join(dist, "release-provenance-v0.31.0.sigstore.json")
	writeProvenanceBundle(t, bundlePath, statement)

	if err := Verify(VerifyOptions{
		Dist:             dist,
		ManifestPath:     manifestPath,
		ProvenanceBundle: bundlePath,
		Repository:       testRepository,
		Tag:              testTag,
		Commit:           testCommit,
	}); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsChangedArchive(t *testing.T) {
	t.Parallel()

	dist := writeReleaseFixture(t)
	manifest, statement, err := Generate(GenerateOptions{
		Dist:         dist,
		Repository:   testRepository,
		Tag:          testTag,
		Commit:       testCommit,
		InvocationID: "urn:omakiten:test",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	manifestPath := filepath.Join(dist, "manifest.json")
	writeJSON(t, manifestPath, manifest)
	bundlePath := filepath.Join(dist, "provenance.sigstore.json")
	writeProvenanceBundle(t, bundlePath, statement)

	changed := filepath.Join(dist, expectedArchiveNames[0])
	if err := os.WriteFile(changed, []byte("changed after metadata generation"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = Verify(VerifyOptions{
		Dist:             dist,
		ManifestPath:     manifestPath,
		ProvenanceBundle: bundlePath,
		Repository:       testRepository,
		Tag:              testTag,
		Commit:           testCommit,
	})
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("Verify() error = %v, want digest mismatch", err)
	}
}

func TestGenerateRejectsIncompleteArchiveMatrix(t *testing.T) {
	t.Parallel()

	dist := writeReleaseFixture(t)
	if err := os.Remove(filepath.Join(dist, expectedArchiveNames[0])); err != nil {
		t.Fatal(err)
	}

	_, _, err := Generate(GenerateOptions{
		Dist:         dist,
		Repository:   testRepository,
		Tag:          testTag,
		Commit:       testCommit,
		InvocationID: "urn:omakiten:test",
	})
	if err == nil || !strings.Contains(err.Error(), expectedArchiveNames[0]) {
		t.Fatalf("Generate() error = %v, want missing archive", err)
	}
}

func TestVerifyRejectsReplayIdentity(t *testing.T) {
	t.Parallel()

	dist := writeReleaseFixture(t)
	manifest, statement, err := Generate(GenerateOptions{
		Dist:         dist,
		Repository:   testRepository,
		Tag:          testTag,
		Commit:       testCommit,
		InvocationID: "urn:omakiten:test",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	manifestPath := filepath.Join(dist, "manifest.json")
	writeJSON(t, manifestPath, manifest)
	bundlePath := filepath.Join(dist, "provenance.sigstore.json")
	writeProvenanceBundle(t, bundlePath, statement)

	err = Verify(VerifyOptions{
		Dist:             dist,
		ManifestPath:     manifestPath,
		ProvenanceBundle: bundlePath,
		Repository:       testRepository,
		Tag:              "v0.31.1",
		Commit:           testCommit,
	})
	if err == nil || !strings.Contains(err.Error(), "tag") {
		t.Fatalf("Verify() error = %v, want tag mismatch", err)
	}
}

func TestVerifyRejectsProvenanceFromAnotherTag(t *testing.T) {
	t.Parallel()

	dist := writeReleaseFixture(t)
	manifest, statement, err := Generate(GenerateOptions{
		Dist:         dist,
		Repository:   testRepository,
		Tag:          testTag,
		Commit:       testCommit,
		InvocationID: "urn:omakiten:test",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	manifestPath := filepath.Join(dist, "manifest.json")
	writeJSON(t, manifestPath, manifest)
	statement.Predicate.BuildDefinition.ExternalParameters.Tag = "v0.30.0"
	bundlePath := filepath.Join(dist, "provenance.sigstore.json")
	writeProvenanceBundle(t, bundlePath, statement)

	err = Verify(VerifyOptions{
		Dist:             dist,
		ManifestPath:     manifestPath,
		ProvenanceBundle: bundlePath,
		Repository:       testRepository,
		Tag:              testTag,
		Commit:           testCommit,
	})
	if err == nil || !strings.Contains(err.Error(), "provenance source identity") {
		t.Fatalf("Verify() error = %v, want provenance identity mismatch", err)
	}
}

func writeReleaseFixture(t *testing.T) string {
	t.Helper()

	dist := t.TempDir()
	var checksums strings.Builder
	for _, name := range expectedArchiveNames {
		content := []byte("fixture archive: " + name + "\n")
		if err := os.WriteFile(filepath.Join(dist, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		fmt.Fprintf(&checksums, "%s  %s\n", hex.EncodeToString(digest[:]), name)
	}
	if err := os.WriteFile(filepath.Join(dist, ChecksumsName), []byte(checksums.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dist
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeProvenanceBundle(t *testing.T, path string, statement Statement) {
	t.Helper()

	payload, err := json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	bundle := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"dsseEnvelope": map[string]any{
			"payloadType": "application/vnd.in-toto+json",
			"payload":     base64.StdEncoding.EncodeToString(payload),
			"signatures":  []map[string]string{{"sig": "fixture-only"}},
		},
	}
	writeJSON(t, path, bundle)
}
