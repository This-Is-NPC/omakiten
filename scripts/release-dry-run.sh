#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

dist="$tmpdir/dist"
config="$tmpdir/goreleaser.yml"
sed '/^dist:/d' "$repo_root/.goreleaser.yml" > "$config"
printf '\ndist: %s\n' "$dist" >>"$config"

cd "$repo_root"
goreleaser release --snapshot --clean --config "$config"

tag="v0.0.0-dry-run"
commit="$(git rev-parse HEAD)"
manifest="$dist/release-manifest-${tag}.json"
statement="$tmpdir/release-provenance-${tag}.intoto.json"
manifest_bundle="$dist/release-manifest-${tag}.sigstore.json"
checksums_bundle="$dist/checksums-${tag}.sigstore.json"
provenance_bundle="$dist/release-provenance-${tag}.sigstore.json"
key_prefix="$tmpdir/dry-run"

go run ./cmd/okt-release-metadata create \
  --dist "$dist" \
  --repository This-Is-NPC/omakiten \
  --tag "$tag" \
  --commit "$commit" \
  --invocation-id urn:omakiten:release-dry-run \
  --manifest "$manifest" \
  --provenance "$statement"

# Local fixture signatures use an ephemeral key and never contact Fulcio or
# Rekor. Production releases use GitHub OIDC keyless signing in release.yml.
COSIGN_PASSWORD="" cosign generate-key-pair --output-key-prefix "$key_prefix"
COSIGN_PASSWORD="" cosign sign-blob \
  --key "$key_prefix.key" \
  --bundle "$manifest_bundle" \
  --use-signing-config=false \
  --yes "$manifest"
COSIGN_PASSWORD="" cosign sign-blob \
  --key "$key_prefix.key" \
  --bundle "$checksums_bundle" \
  --use-signing-config=false \
  --yes "$dist/checksums.txt"
COSIGN_PASSWORD="" cosign attest-blob \
  --key "$key_prefix.key" \
  --statement "$statement" \
  --bundle "$provenance_bundle" \
  --use-signing-config=false \
  --yes

cosign verify-blob --key "$key_prefix.pub" --bundle "$manifest_bundle" "$manifest"
cosign verify-blob --key "$key_prefix.pub" --bundle "$checksums_bundle" "$dist/checksums.txt"
for archive in "$dist"/*.tar.gz "$dist"/*.zip; do
  cosign verify-blob-attestation \
    --key "$key_prefix.pub" \
    --bundle "$provenance_bundle" \
    --type slsaprovenance1 \
    "$archive"
done

go run ./cmd/okt-release-metadata verify \
  --dist "$dist" \
  --repository This-Is-NPC/omakiten \
  --tag "$tag" \
  --commit "$commit" \
  --manifest "$manifest" \
  --provenance-bundle "$provenance_bundle"

printf 'release dry run passed: 6 archives, manifest, 3 signature bundles, and SLSA provenance are digest-consistent\n'
