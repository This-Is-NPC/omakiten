// Package releasemeta creates and validates the version-bound manifest and
// SLSA provenance statement used by Omakiten's release workflow.
package releasemeta

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// ChecksumsName is the GoReleaser checksum file published with every
	// release and signed by the release workflow.
	ChecksumsName = "checksums.txt"

	inTotoStatementType = "https://in-toto.io/Statement/v1"
	slsaProvenanceType  = "https://slsa.dev/provenance/v1"
)

var (
	expectedArchiveNames = []string{
		"okt_Darwin_arm64.tar.gz",
		"okt_Darwin_x86_64.tar.gz",
		"okt_Linux_arm64.tar.gz",
		"okt_Linux_x86_64.tar.gz",
		"okt_Windows_arm64.zip",
		"okt_Windows_x86_64.zip",
	}
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	tagPattern        = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)
	commitPattern     = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

// Artifact identifies one release file by its basename and SHA-256 digest.
type Artifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Manifest binds every archive and checksums.txt to one repository, tag, and
// source commit.
type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	Repository    string     `json:"repository"`
	Tag           string     `json:"tag"`
	SourceCommit  string     `json:"source_commit"`
	Artifacts     []Artifact `json:"artifacts"`
}

// Subject is an in-toto subject descriptor.
type Subject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// SourceIdentity is the release source recorded in SLSA external parameters.
type SourceIdentity struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
}

// ResourceDescriptor identifies the exact tagged source dependency.
type ResourceDescriptor struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest"`
}

// BuildDefinition records how and from which source the release was built.
type BuildDefinition struct {
	BuildType            string               `json:"buildType"`
	ExternalParameters   SourceIdentity       `json:"externalParameters"`
	InternalParameters   map[string]string    `json:"internalParameters"`
	ResolvedDependencies []ResourceDescriptor `json:"resolvedDependencies"`
}

// Builder identifies the trusted workflow that produced the release.
type Builder struct {
	ID string `json:"id"`
}

// RunMetadata identifies one workflow invocation.
type RunMetadata struct {
	InvocationID string `json:"invocationId"`
}

// RunDetails records the authenticated builder and invocation.
type RunDetails struct {
	Builder  Builder     `json:"builder"`
	Metadata RunMetadata `json:"metadata"`
}

// ProvenancePredicate is a SLSA build provenance v1 predicate.
type ProvenancePredicate struct {
	BuildDefinition BuildDefinition `json:"buildDefinition"`
	RunDetails      RunDetails      `json:"runDetails"`
}

// Statement is the in-toto statement signed into the provenance bundle.
type Statement struct {
	Type          string              `json:"_type"`
	Subject       []Subject           `json:"subject"`
	PredicateType string              `json:"predicateType"`
	Predicate     ProvenancePredicate `json:"predicate"`
}

// GenerateOptions identifies the release files and source invocation.
type GenerateOptions struct {
	Dist         string
	Repository   string
	Tag          string
	Commit       string
	InvocationID string
}

// VerifyOptions identifies the signed metadata and expected source identity.
type VerifyOptions struct {
	Dist             string
	ManifestPath     string
	ProvenanceBundle string
	Repository       string
	Tag              string
	Commit           string
}

// Generate creates deterministic manifest and provenance values for a complete
// GoReleaser archive matrix.
func Generate(opts GenerateOptions) (Manifest, Statement, error) {
	identity := SourceIdentity{Repository: opts.Repository, Tag: opts.Tag, Commit: opts.Commit}
	if err := validateIdentity(identity); err != nil {
		return Manifest{}, Statement{}, err
	}
	if opts.Dist == "" {
		return Manifest{}, Statement{}, errors.New("dist directory is required")
	}
	if _, err := url.ParseRequestURI(opts.InvocationID); err != nil {
		return Manifest{}, Statement{}, fmt.Errorf("invalid invocation id: %w", err)
	}

	checksums, err := readChecksums(filepath.Join(opts.Dist, ChecksumsName))
	if err != nil {
		return Manifest{}, Statement{}, err
	}
	if err := validateArchiveSet(checksums); err != nil {
		return Manifest{}, Statement{}, err
	}

	artifacts := make([]Artifact, 0, len(expectedArchiveNames)+1)
	subjects := make([]Subject, 0, len(expectedArchiveNames))
	for _, name := range expectedArchiveNames {
		digest, err := fileSHA256(filepath.Join(opts.Dist, name))
		if err != nil {
			return Manifest{}, Statement{}, fmt.Errorf("digest %s: %w", name, err)
		}
		if digest != checksums[name] {
			return Manifest{}, Statement{}, fmt.Errorf("digest mismatch for %s: archive=%s checksums.txt=%s", name, digest, checksums[name])
		}
		artifacts = append(artifacts, Artifact{Name: name, SHA256: digest})
		subjects = append(subjects, Subject{Name: name, Digest: map[string]string{"sha256": digest}})
	}
	checksumsDigest, err := fileSHA256(filepath.Join(opts.Dist, ChecksumsName))
	if err != nil {
		return Manifest{}, Statement{}, fmt.Errorf("digest %s: %w", ChecksumsName, err)
	}
	artifacts = append(artifacts, Artifact{Name: ChecksumsName, SHA256: checksumsDigest})
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })

	workflowIdentity := workflowIdentity(opts.Repository)
	manifest := Manifest{
		SchemaVersion: 1,
		Repository:    opts.Repository,
		Tag:           opts.Tag,
		SourceCommit:  opts.Commit,
		Artifacts:     artifacts,
	}
	statement := Statement{
		Type:          inTotoStatementType,
		Subject:       subjects,
		PredicateType: slsaProvenanceType,
		Predicate: ProvenancePredicate{
			BuildDefinition: BuildDefinition{
				BuildType:          workflowIdentity,
				ExternalParameters: identity,
				InternalParameters: map[string]string{},
				ResolvedDependencies: []ResourceDescriptor{{
					URI:    sourceURI(identity),
					Digest: map[string]string{"gitCommit": opts.Commit},
				}},
			},
			RunDetails: RunDetails{
				Builder:  Builder{ID: workflowIdentity},
				Metadata: RunMetadata{InvocationID: opts.InvocationID},
			},
		},
	}
	return manifest, statement, nil
}

// Verify checks the manifest, checksums, archive bytes, and embedded
// provenance statement against one expected release identity.
func Verify(opts VerifyOptions) error {
	identity := SourceIdentity{Repository: opts.Repository, Tag: opts.Tag, Commit: opts.Commit}
	if err := validateIdentity(identity); err != nil {
		return err
	}

	manifestData, err := os.ReadFile(filepath.Clean(opts.ManifestPath))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return err
	}
	if err := verifyManifest(opts.Dist, manifest, identity); err != nil {
		return err
	}

	statement, err := readProvenanceStatement(opts.ProvenanceBundle)
	if err != nil {
		return err
	}
	return verifyStatement(statement, manifest, identity)
}

// ParseManifest decodes raw manifest bytes. It is the byte-level twin of the
// file read inside Verify so consumers that fetch the manifest over the
// network (the updater, the bootstrap installer) never have to re-implement
// the decode or re-declare the schema check.
func ParseManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return Manifest{}, fmt.Errorf("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	return manifest, nil
}

// ValidateIdentity rejects a repository, tag, or commit that does not match the
// shapes the release workflow can emit.
func ValidateIdentity(identity SourceIdentity) error { return validateIdentity(identity) }

// BindManifestIdentity checks that a decoded manifest names exactly the
// expected repository, tag, and source commit. Consumers that learn the commit
// *from* the (already authenticated) manifest pass manifest.SourceCommit here
// so the same comparison still guards repository and tag.
func BindManifestIdentity(manifest Manifest, identity SourceIdentity) error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	if manifest.Repository != identity.Repository {
		return fmt.Errorf("manifest repository %q does not match %q", manifest.Repository, identity.Repository)
	}
	if manifest.Tag != identity.Tag {
		return fmt.Errorf("manifest tag %q does not match %q", manifest.Tag, identity.Tag)
	}
	if manifest.SourceCommit != identity.Commit {
		return fmt.Errorf("manifest source commit %q does not match %q", manifest.SourceCommit, identity.Commit)
	}
	return nil
}

// DigestFor returns the manifest's SHA-256 for one artifact name.
func (m Manifest) DigestFor(name string) (string, bool) {
	for _, artifact := range m.Artifacts {
		if artifact.Name == name {
			return artifact.SHA256, true
		}
	}
	return "", false
}

// ArchiveNames returns the complete published archive matrix.
func ArchiveNames() []string {
	out := make([]string, len(expectedArchiveNames))
	copy(out, expectedArchiveNames)
	return out
}

// ValidateManifestArtifacts checks that a manifest lists exactly the published
// archive matrix plus checksums.txt, with no duplicates and no extras. It is
// the disk-free half of verifyManifest so a network consumer can reject a
// truncated or padded manifest without holding every archive.
func ValidateManifestArtifacts(manifest Manifest) error {
	wantNames := make(map[string]struct{}, len(expectedArchiveNames)+1)
	for _, name := range expectedArchiveNames {
		wantNames[name] = struct{}{}
	}
	wantNames[ChecksumsName] = struct{}{}
	if len(manifest.Artifacts) != len(wantNames) {
		return fmt.Errorf("manifest has %d artifacts, want %d", len(manifest.Artifacts), len(wantNames))
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		if _, ok := wantNames[artifact.Name]; !ok {
			return fmt.Errorf("manifest contains unexpected artifact %q", artifact.Name)
		}
		if _, ok := seen[artifact.Name]; ok {
			return fmt.Errorf("manifest contains duplicate artifact %q", artifact.Name)
		}
		seen[artifact.Name] = struct{}{}
		if !commitDigestPattern(artifact.SHA256) {
			return fmt.Errorf("manifest digest for %q is not a lowercase SHA-256 hex digest", artifact.Name)
		}
	}
	return nil
}

// VerifyStatement checks a decoded in-toto statement against the manifest it
// must agree with and the release identity it must name.
func VerifyStatement(statement Statement, manifest Manifest, identity SourceIdentity) error {
	return verifyStatement(statement, manifest, identity)
}

// ParseStatement decodes an in-toto statement from a DSSE payload.
func ParseStatement(payload []byte) (Statement, error) {
	var statement Statement
	if err := json.Unmarshal(payload, &statement); err != nil {
		return Statement{}, fmt.Errorf("decode provenance statement: %w", err)
	}
	return statement, nil
}

func verifyManifest(dist string, manifest Manifest, identity SourceIdentity) error {
	if err := BindManifestIdentity(manifest, identity); err != nil {
		return err
	}

	checksums, err := readChecksums(filepath.Join(dist, ChecksumsName))
	if err != nil {
		return err
	}
	if err := validateArchiveSet(checksums); err != nil {
		return err
	}
	if err := ValidateManifestArtifacts(manifest); err != nil {
		return err
	}
	for _, artifact := range manifest.Artifacts {
		digest, err := fileSHA256(filepath.Join(dist, artifact.Name))
		if err != nil {
			return fmt.Errorf("digest %s: %w", artifact.Name, err)
		}
		if digest != artifact.SHA256 {
			return fmt.Errorf("manifest digest mismatch for %s: file=%s manifest=%s", artifact.Name, digest, artifact.SHA256)
		}
		if artifact.Name != ChecksumsName && digest != checksums[artifact.Name] {
			return fmt.Errorf("checksums digest mismatch for %s: file=%s checksums.txt=%s", artifact.Name, digest, checksums[artifact.Name])
		}
	}
	return nil
}

func verifyStatement(statement Statement, manifest Manifest, identity SourceIdentity) error {
	if err := verifyStatementIdentity(statement, identity); err != nil {
		return err
	}
	if err := verifyStatementSource(statement, identity); err != nil {
		return err
	}
	return verifyStatementSubjects(statement, manifest)
}

func verifyStatementIdentity(statement Statement, identity SourceIdentity) error {
	if statement.Type != inTotoStatementType {
		return fmt.Errorf("provenance statement type %q is not in-toto v1", statement.Type)
	}
	if statement.PredicateType != slsaProvenanceType {
		return fmt.Errorf("provenance predicate type %q is not SLSA v1", statement.PredicateType)
	}
	params := statement.Predicate.BuildDefinition.ExternalParameters
	if params != identity {
		return fmt.Errorf("provenance source identity %#v does not match %#v", params, identity)
	}
	wantWorkflow := workflowIdentity(identity.Repository)
	if statement.Predicate.BuildDefinition.BuildType != wantWorkflow {
		return fmt.Errorf("provenance build type %q does not match %q", statement.Predicate.BuildDefinition.BuildType, wantWorkflow)
	}
	if statement.Predicate.RunDetails.Builder.ID != wantWorkflow {
		return fmt.Errorf("provenance builder %q does not match %q", statement.Predicate.RunDetails.Builder.ID, wantWorkflow)
	}
	return nil
}

func verifyStatementSource(statement Statement, identity SourceIdentity) error {
	if _, err := url.ParseRequestURI(statement.Predicate.RunDetails.Metadata.InvocationID); err != nil {
		return fmt.Errorf("invalid provenance invocation id: %w", err)
	}
	dependencies := statement.Predicate.BuildDefinition.ResolvedDependencies
	if len(dependencies) != 1 || dependencies[0].URI != sourceURI(identity) || len(dependencies[0].Digest) != 1 || dependencies[0].Digest["gitCommit"] != identity.Commit {
		return fmt.Errorf("provenance does not resolve exact tagged source %s at %s", sourceURI(identity), identity.Commit)
	}
	return nil
}

func verifyStatementSubjects(statement Statement, manifest Manifest) error {
	archiveDigests := make(map[string]string, len(expectedArchiveNames))
	for _, artifact := range manifest.Artifacts {
		if artifact.Name != ChecksumsName {
			archiveDigests[artifact.Name] = artifact.SHA256
		}
	}
	if len(statement.Subject) != len(archiveDigests) {
		return fmt.Errorf("provenance has %d subjects, want %d", len(statement.Subject), len(archiveDigests))
	}
	seen := make(map[string]struct{}, len(statement.Subject))
	for _, subject := range statement.Subject {
		wantDigest, ok := archiveDigests[subject.Name]
		if !ok {
			return fmt.Errorf("provenance contains unexpected subject %q", subject.Name)
		}
		if _, ok := seen[subject.Name]; ok {
			return fmt.Errorf("provenance contains duplicate subject %q", subject.Name)
		}
		seen[subject.Name] = struct{}{}
		if len(subject.Digest) != 1 || subject.Digest["sha256"] != wantDigest {
			return fmt.Errorf("provenance digest mismatch for %s: provenance=%s manifest=%s", subject.Name, subject.Digest["sha256"], wantDigest)
		}
	}
	return nil
}

func readProvenanceStatement(path string) (Statement, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Statement{}, fmt.Errorf("read provenance bundle: %w", err)
	}
	var bundle struct {
		DSSEEnvelope struct {
			PayloadType string `json:"payloadType"`
			Payload     string `json:"payload"`
		} `json:"dsseEnvelope"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		return Statement{}, fmt.Errorf("decode provenance bundle: %w", err)
	}
	if bundle.DSSEEnvelope.PayloadType != "application/vnd.in-toto+json" {
		return Statement{}, fmt.Errorf("provenance payload type %q is not in-toto JSON", bundle.DSSEEnvelope.PayloadType)
	}
	payload, err := base64.StdEncoding.DecodeString(bundle.DSSEEnvelope.Payload)
	if err != nil {
		return Statement{}, fmt.Errorf("decode provenance payload: %w", err)
	}
	return ParseStatement(payload)
}

func readChecksums(path string) (map[string]string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ChecksumsName, err)
	}
	defer file.Close()
	return ParseChecksums(file)
}

// ParseChecksums decodes the `<sha256>  <name>` lines GoReleaser writes into
// checksums.txt. Exported so consumers holding the *signed bytes* (rather than
// a file on disk) parse them through exactly the same strict reader.
func ParseChecksums(r io.Reader) (map[string]string, error) {
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || !commitDigestPattern(fields[0]) {
			return nil, fmt.Errorf("invalid %s line %q", ChecksumsName, scanner.Text())
		}
		name := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(name) != name {
			return nil, fmt.Errorf("invalid archive name %q in %s", name, ChecksumsName)
		}
		if _, ok := checksums[name]; ok {
			return nil, fmt.Errorf("duplicate archive %q in %s", name, ChecksumsName)
		}
		checksums[name] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", ChecksumsName, err)
	}
	return checksums, nil
}

func validateArchiveSet(checksums map[string]string) error {
	want := make(map[string]struct{}, len(expectedArchiveNames))
	for _, name := range expectedArchiveNames {
		want[name] = struct{}{}
		if _, ok := checksums[name]; !ok {
			return fmt.Errorf("required release archive %s is missing from %s", name, ChecksumsName)
		}
	}
	for name := range checksums {
		if _, ok := want[name]; !ok {
			return fmt.Errorf("unexpected release archive %s in %s", name, ChecksumsName)
		}
	}
	return nil
}

func validateIdentity(identity SourceIdentity) error {
	if !repositoryPattern.MatchString(identity.Repository) {
		return fmt.Errorf("invalid repository %q", identity.Repository)
	}
	if !tagPattern.MatchString(identity.Tag) {
		return fmt.Errorf("invalid release tag %q", identity.Tag)
	}
	if !commitPattern.MatchString(identity.Commit) {
		return fmt.Errorf("invalid source commit %q", identity.Commit)
	}
	return nil
}

func workflowIdentity(repository string) string {
	return "https://github.com/" + repository + "/.github/workflows/release.yml@refs/heads/master"
}

func sourceURI(identity SourceIdentity) string {
	return "git+https://github.com/" + identity.Repository + "@refs/tags/" + identity.Tag
}

func commitDigestPattern(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
