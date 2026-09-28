#!/usr/bin/env bash
set -euo pipefail

# install.sh — fetch an okt release, drop the binary in INSTALL_DIR, and hand
# off to `okt setup` for the interactive picker (or the env-var headless path).
# Every prompt, the rc-file wrapper writer, and the harness/preset selection
# logic live inside the Go binary now; this wrapper is meant to stay short and
# platform-portable so a `curl|bash` invocation that only needs to bootstrap
# the binary does not have to carry the picker UI in two places.
#
# TWO VERIFICATION MODES (OKT_VERIFY_MODE):
#
#   checksum (default) — the convenience path. Downloads the archive and
#     verifies its SHA-256 against the published checksums.txt before
#     extracting. This is a corruption and swap check, NOT an authenticity
#     check: it trusts the installer script you just fetched and it trusts
#     GitHub's TLS. It is not end-to-end verified.
#
#   strict — the verified bootstrap path. Requires a Cosign you installed
#     yourself, authenticates the version-bound manifest, checksums.txt, and
#     SLSA provenance against a pinned OIDC issuer and a pinned release
#     workflow identity, and only then compares the archive digest. It fails
#     closed: there is no unsigned fallback anywhere inside strict mode, and
#     the installer never downloads or implicitly executes a verifier.

REPO="This-Is-NPC/omakiten"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Base hosts default to GitHub but are overridable for release lookup and
# artifact downloads so hermetic tests (and any release mirror) can point those
# requests at a local endpoint without rewriting URL logic. Checksums are a
# separate trust root: by default checksums.txt stays pinned to GitHub, and a
# mirrored checksum host requires OKT_ALLOW_MIRROR_CHECKSUM=1 plus the separate
# OKT_CHECKSUM_BASE override.
GITHUB_API_BASE="${GITHUB_API_BASE:-https://api.github.com}"
GITHUB_DL_BASE="${GITHUB_DL_BASE:-https://github.com}"
CHECKSUM_BASE="https://github.com"

VERIFY_MODE="${OKT_VERIFY_MODE:-checksum}"
case "${VERIFY_MODE}" in
  checksum|strict) ;;
  *)
    echo "error: OKT_VERIFY_MODE must be 'checksum' or 'strict' (got '${VERIFY_MODE}')" >&2
    exit 1
    ;;
esac

# --- pinned release trust anchors ------------------------------------------
#
# These four values are the shell twin of the constants compiled into
# internal/releaseverify, which is the strict verifier `okt update` uses. They
# are deliberately exact strings, never patterns: a regex identity would let
# any repository or ref whose SAN merely *contains* the expected substring
# pass. internal/installscript asserts they stay in step with the Go package.
COSIGN_OIDC_ISSUER="https://token.actions.githubusercontent.com"
COSIGN_CERT_IDENTITY="https://github.com/${REPO}/.github/workflows/release.yml@refs/heads/master"
# Last release published without signed metadata. Anything at or before it can
# never be authenticated after the fact, so strict mode refuses it outright.
SIGNED_RELEASE_CUTOFF="0.30.0"
# Bundles are published in the Sigstore bundle format cosign v3 writes.
COSIGN_MIN_VERSION="3.0.0"
# The complete published archive matrix, mirroring releasemeta.ArchiveNames().
# Used to reject a truncated or padded manifest.
RELEASE_ARCHIVES="okt_Darwin_arm64.tar.gz okt_Darwin_x86_64.tar.gz okt_Linux_arm64.tar.gz okt_Linux_x86_64.tar.gz okt_Windows_arm64.zip okt_Windows_x86_64.zip"

COSIGN_BIN=""

# The mirrored-checksum opt-in only exists for the convenience path, where the
# checksum host *is* the trust root. Strict mode derives trust from the
# signature rather than from the host, so the opt-in is inert there and mirrors
# are covered by the "authenticated metadata or nothing" rule instead.
if [ "${VERIFY_MODE}" = "checksum" ] && [ "${OKT_ALLOW_MIRROR_CHECKSUM:-}" = "1" ]; then
  if [ -z "${OKT_CHECKSUM_BASE:-}" ]; then
    echo "error: OKT_ALLOW_MIRROR_CHECKSUM=1 requires OKT_CHECKSUM_BASE to name the checksum mirror" >&2
    exit 1
  fi
  CHECKSUM_BASE="${OKT_CHECKSUM_BASE%/}"
fi

get_latest_tag() {
  curl -fsSL "${GITHUB_API_BASE}/repos/${REPO}/releases/latest" |
    grep -m1 '"tag_name":' |
    sed -E 's/.*"tag_name" *: *"v?([^"]+)".*/\1/'
}

get_os() {
  case "$(uname -s)" in
    Linux*)  echo Linux ;;
    Darwin*) echo Darwin ;;
    *)       echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

get_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo x86_64 ;;
    arm64|aarch64) echo arm64 ;;
    *)            echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
}

# sha256_of prints the lowercase hex sha256 of a file using whichever of
# the two ubiquitous tools is present. We deliberately depend on nothing
# beyond the base system: `sha256sum` (coreutils, Linux) or `shasum -a 256`
# (BSD/macOS). If neither exists we abort rather than skipping the check —
# a missing hasher must never silently degrade to "install unverified".
sha256_of() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    echo "error: no sha256 tool found (need 'sha256sum' or 'shasum'); refusing to install unverified binary" >&2
    exit 1
  fi
}

# verify_checksum fetches the goreleaser-published checksums.txt for the
# release and verifies ${asset} against it BEFORE the archive is ever
# extracted or executed. This mirrors the in-app updater's
# sha256-verify-before-extract gate so the very first okt binary a user runs is
# verified the same way every later `okt update` is.
#
# Trust assumption: by default the canonical hash comes from checksums.txt
# fetched over HTTPS from GitHub, independent of any artifact mirror override.
# Mirrored checksums are allowed only via the explicit OKT_ALLOW_MIRROR_CHECKSUM
# + OKT_CHECKSUM_BASE opt-in, which means the caller is choosing that checksum
# trust root. This is NOT signature verification: use OKT_VERIFY_MODE=strict
# for an authenticated install.
verify_checksum() {
  local archive="$1" asset="$2" tag="$3" tmpdir="$4"
  local sums_url="${CHECKSUM_BASE}/${REPO}/releases/download/v${tag}/checksums.txt"
  local sums="${tmpdir}/checksums.txt"

  echo "=> Verifying checksum against ${sums_url}"
  if ! curl -fsSL "${sums_url}" -o "${sums}"; then
    echo "error: failed to download checksums.txt from ${sums_url}; aborting" >&2
    exit 1
  fi

  # checksums.txt lines are "<sha256>  <filename>"; pull the row for our asset.
  local expected
  expected="$(awk -v want="${asset}" '$2 == want {print $1; exit}' "${sums}")"
  if [ -z "${expected}" ]; then
    echo "error: ${asset} not listed in checksums.txt; aborting" >&2
    exit 1
  fi

  local actual
  actual="$(sha256_of "${archive}")"

  # Case-insensitive hex compare (defensive; both tools emit lowercase).
  if [ "$(printf '%s' "${expected}" | tr 'A-F' 'a-f')" != "$(printf '%s' "${actual}" | tr 'A-F' 'a-f')" ]; then
    echo "error: checksum mismatch for ${asset}" >&2
    echo "       expected: ${expected}" >&2
    echo "       actual:   ${actual}" >&2
    echo "       refusing to install a tampered or corrupt archive" >&2
    exit 1
  fi
  echo "=> Checksum OK (${actual})"
}

# announce_convenience_trust states plainly what the default path does and does
# not prove, so nobody reads "Checksum OK" as "authenticated".
announce_convenience_trust() {
  echo "=> Convenience mode: this install is NOT signature-verified."
  echo "   It trusts the installer script you fetched and GitHub's TLS; the checksum"
  echo "   check below only proves the archive matches the checksums.txt served"
  echo "   alongside it. For an authenticated install, install Cosign yourself and"
  echo "   re-run with OKT_VERIFY_MODE=strict."
}

# --- strict verification ----------------------------------------------------

# version_compare echoes -1, 0, or 1 for "$1 <=> $2" over MAJOR.MINOR.PATCH
# with SemVer prerelease ordering, matching releaseverify.CompareVersions for
# the tags the release workflow can emit.
version_compare() {
  local LC_ALL=C
  local left="${1#v}" right="${2#v}"
  local left_core="${left%%-*}" right_core="${right%%-*}"
  local left_pre="" right_pre=""
  case "${left}" in *-*) left_pre="${left#*-}" ;; esac
  case "${right}" in *-*) right_pre="${right#*-}" ;; esac

  local i left_part right_part comparison
  local left_parts right_parts
  IFS='.' read -r -a left_parts <<<"${left_core}"
  IFS='.' read -r -a right_parts <<<"${right_core}"
  for i in 0 1 2; do
    left_part="${left_parts[i]:-0}"
    right_part="${right_parts[i]:-0}"
    comparison="$(numeric_identifier_compare "${left_part}" "${right_part}")"
    if [ "${comparison}" != "0" ]; then echo "${comparison}"; return; fi
  done
  if [ "${left_pre}" = "${right_pre}" ]; then echo 0; return; fi
  if [ -z "${left_pre}" ]; then echo 1; return; fi
  if [ -z "${right_pre}" ]; then echo -1; return; fi

  local left_ids right_ids left_id right_id left_numeric right_numeric
  IFS='.' read -r -a left_ids <<<"${left_pre}"
  IFS='.' read -r -a right_ids <<<"${right_pre}"
  for ((i=0; i<${#left_ids[@]} && i<${#right_ids[@]}; i++)); do
    left_id="${left_ids[i]}"; right_id="${right_ids[i]}"
    [ "${left_id}" = "${right_id}" ] && continue
    left_numeric=0; right_numeric=0
    [[ "${left_id}" =~ ^[0-9]+$ ]] && left_numeric=1
    [[ "${right_id}" =~ ^[0-9]+$ ]] && right_numeric=1
    if [ "${left_numeric}" = 1 ] && [ "${right_numeric}" = 0 ]; then echo -1; return; fi
    if [ "${left_numeric}" = 0 ] && [ "${right_numeric}" = 1 ]; then echo 1; return; fi
    if [ "${left_numeric}" = 1 ]; then
      numeric_identifier_compare "${left_id}" "${right_id}"
      return
    fi
    if [[ "${left_id}" < "${right_id}" ]]; then echo -1; else echo 1; fi
    return
  done
  if [ "${#left_ids[@]}" -lt "${#right_ids[@]}" ]; then echo -1
  elif [ "${#left_ids[@]}" -gt "${#right_ids[@]}" ]; then echo 1
  else echo 0
  fi
}

numeric_identifier_compare() {
  local left="$1" right="$2"
  if [ "${#left}" -lt "${#right}" ]; then echo -1
  elif [ "${#left}" -gt "${#right}" ]; then echo 1
  elif [[ "${left}" < "${right}" ]]; then echo -1
  elif [[ "${left}" > "${right}" ]]; then echo 1
  else echo 0
  fi
}

is_release_version() {
  printf '%s' "$1" | grep -qE '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?$'
}

cosign_missing() {
  echo "error: OKT_VERIFY_MODE=strict requires Cosign v${COSIGN_MIN_VERSION} or newer, and none was found." >&2
  echo "       Strict mode never downloads or runs a verifier for you — install Cosign" >&2
  echo "       yourself, from a source you trust, and re-run:" >&2
  echo "         Linux    : download cosign-linux-<arch> from https://github.com/sigstore/cosign/releases" >&2
  echo "         macOS    : brew install cosign" >&2
  echo "       Point OKT_COSIGN at the binary if it is not on PATH." >&2
  exit 1
}

# require_cosign resolves the user-installed verifier and enforces the minimum
# supported version. It runs BEFORE anything is downloaded, so a host without a
# verifier never fetches a release at all.
require_cosign() {
  COSIGN_BIN="${OKT_COSIGN:-}"
  if [ -z "${COSIGN_BIN}" ]; then
    COSIGN_BIN="$(command -v cosign 2>/dev/null || true)"
  fi
  if [ -z "${COSIGN_BIN}" ] || [ ! -x "${COSIGN_BIN}" ]; then
    cosign_missing
  fi
  if ! command -v jq >/dev/null 2>&1; then
    echo "error: OKT_VERIFY_MODE=strict requires jq to decode and validate the authenticated DSSE provenance." >&2
    echo "       Install jq from a source you trust and re-run; strict mode will not parse signed JSON heuristically." >&2
    exit 1
  fi

  local raw version
  raw="$("${COSIGN_BIN}" version 2>/dev/null || true)"
  version="$(printf '%s\n' "${raw}" | sed -n 's/^[[:space:]]*GitVersion:[[:space:]]*v\{0,1\}\([0-9][0-9.]*\).*/\1/p' | head -n1)"
  if [ -z "${version}" ]; then
    echo "error: could not determine the version of ${COSIGN_BIN}; refusing to verify with an unknown verifier" >&2
    exit 1
  fi
  if [ "$(version_compare "${version}" "${COSIGN_MIN_VERSION}")" -lt 0 ]; then
    echo "error: ${COSIGN_BIN} is v${version}; strict mode needs v${COSIGN_MIN_VERSION} or newer" >&2
    echo "       (older Cosign cannot read the Sigstore bundle format these releases publish)" >&2
    exit 1
  fi
  echo "=> Strict verification mode — using ${COSIGN_BIN} v${version}"
}

# require_signed_release refuses any target at or before the signed cutoff.
# There is no override: those releases have no authenticated metadata and never
# will, so strict mode simply does not support them.
require_signed_release() {
  local tag="$1"
  if ! is_release_version "${tag}"; then
    echo "error: strict mode needs a normalized release version like 1.2.3 (got '${tag}')" >&2
    exit 1
  fi
  if [ "$(version_compare "${tag}" "${SIGNED_RELEASE_CUTOFF}")" -le 0 ]; then
    echo "error: okt v${tag} is at or before the signed-release cutoff v${SIGNED_RELEASE_CUTOFF}." >&2
    echo "       Releases up to and including v${SIGNED_RELEASE_CUTOFF} were published without signed" >&2
    echo "       metadata and can never be authenticated after the fact, so strict mode does" >&2
    echo "       not support them. Install a release newer than v${SIGNED_RELEASE_CUTOFF}." >&2
    exit 1
  fi
}

# fetch_required downloads one release file or aborts. Strict mode has no
# unsigned fallback, so a missing metadata file is a hard failure — a stripped
# release must never be able to demote the client to checksum-only trust.
fetch_required() {
  local url="$1" dest="$2" label="$3"
  if ! curl -fsSL "${url}" -o "${dest}"; then
    echo "error: could not download the ${label} from ${url}" >&2
    echo "       strict mode has no unsigned fallback; refusing to install." >&2
    exit 1
  fi
}

# cosign_verify_blob authenticates a detached blob signature. No
# --insecure-ignore-* flag and no regex matcher is ever passed, so cosign's
# default Rekor transparency-log and Fulcio SCT requirements stay in force.
cosign_verify_blob() {
  local bundle="$1" artifact="$2" label="$3"
  if ! "${COSIGN_BIN}" verify-blob \
    --bundle "${bundle}" \
    --certificate-identity "${COSIGN_CERT_IDENTITY}" \
    --certificate-oidc-issuer "${COSIGN_OIDC_ISSUER}" \
    "${artifact}" >/dev/null; then
    echo "error: could not authenticate the ${label}." >&2
    echo "       The signature must come from ${COSIGN_CERT_IDENTITY}" >&2
    echo "       issued by ${COSIGN_OIDC_ISSUER}, with Rekor transparency-log evidence." >&2
    echo "       Refusing to install." >&2
    exit 1
  fi
}

# cosign_verify_attestation authenticates the SLSA provenance and requires the
# archive to appear as a digest-bound subject of the signed statement.
cosign_verify_attestation() {
  local bundle="$1" artifact="$2"
  if ! "${COSIGN_BIN}" verify-blob-attestation \
    --bundle "${bundle}" \
    --certificate-identity "${COSIGN_CERT_IDENTITY}" \
    --certificate-oidc-issuer "${COSIGN_OIDC_ISSUER}" \
    --type slsaprovenance1 \
    "${artifact}" >/dev/null; then
    echo "error: could not authenticate the SLSA provenance for $(basename "${artifact}")." >&2
    echo "       The archive is not a signed subject of this release's provenance statement." >&2
    echo "       Refusing to install." >&2
    exit 1
  fi
}

# manifest_string prints one top-level string field of the manifest. The
# manifest is authenticated before this runs, so the parser only has to be
# correct, not adversarial. awk's index() is a literal search, which keeps
# archive names containing '.' from being read as patterns.
manifest_string() {
  local file="$1" key="$2"
  tr -d ' \t\r\n' <"${file}" | awk -v key="\"${key}\":\"" '
    {
      i = index($0, key)
      if (i == 0) { exit 1 }
      rest = substr($0, i + length(key))
      j = index(rest, "\"")
      if (j == 0) { exit 1 }
      print substr(rest, 1, j - 1)
    }'
}

# manifest_digest prints the recorded sha256 for one artifact name.
manifest_digest() {
  local file="$1" name="$2"
  tr -d ' \t\r\n' <"${file}" | awk -v key="\"name\":\"${name}\",\"sha256\":\"" '
    {
      i = index($0, key)
      if (i == 0) { exit 1 }
      print substr($0, i + length(key), 64)
    }'
}

manifest_schema_version() {
  tr -d ' \t\r\n' <"$1" | awk '
    {
      i = index($0, "\"schema_version\":")
      if (i == 0) { exit 1 }
      rest = substr($0, i + length("\"schema_version\":"))
      j = index(rest, ",")
      if (j == 0) { exit 1 }
      print substr(rest, 1, j - 1)
    }'
}

validate_checksum_set() {
  local checksums="$1"
  awk -v expected="${RELEASE_ARCHIVES}" '
    BEGIN { n = split(expected, names, " "); for (i=1; i<=n; i++) want[names[i]]=1 }
    NF != 2 || length($1) != 64 || $1 !~ /^[0-9a-f]+$/ || !($2 in want) || seen[$2]++ { bad=1 }
    END {
      for (name in want) if (seen[name] != 1) bad=1
      if (bad || NR != n) exit 1
    }' "${checksums}"
}

validate_authenticated_metadata() {
  local manifest="$1" checksums="$2" provenance_bundle="$3" tag="$4" source_commit="$5"
  local expected_count=7 name digest recorded

  if ! jq -e --argjson count "${expected_count}" '.artifacts | type == "array" and length == $count' "${manifest}" >/dev/null; then
    echo "error: signed manifest must contain exactly ${expected_count} release artifacts" >&2
    exit 1
  fi
  for name in ${RELEASE_ARCHIVES} checksums.txt; do
    if ! jq -e --arg name "${name}" '([.artifacts[] | select(.name == $name)]) as $entries | ($entries | length) == 1 and ($entries[0].sha256 | test("^[0-9a-f]{64}$"))' "${manifest}" >/dev/null; then
      echo "error: signed manifest must contain exactly one valid digest for ${name}" >&2
      exit 1
    fi
  done
  if ! validate_checksum_set "${checksums}"; then
    echo "error: signed checksums.txt must contain exactly one entry for each published archive and no others" >&2
    exit 1
  fi
  for name in ${RELEASE_ARCHIVES}; do
    digest="$(jq -r --arg name "${name}" '.artifacts[] | select(.name == $name) | .sha256' "${manifest}")"
    recorded="$(awk -v want="${name}" '$2 == want {print $1}' "${checksums}")"
    if [ "${recorded}" != "${digest}" ]; then
      echo "error: signed checksums.txt digest for ${name} disagrees with the signed manifest" >&2
      exit 1
    fi
  done
  digest="$(jq -r --arg name checksums.txt '.artifacts[] | select(.name == $name) | .sha256' "${manifest}")"
  recorded="$(sha256_of "${checksums}")"
  if [ "${recorded}" != "${digest}" ]; then
    echo "error: signed manifest digest for checksums.txt does not match the authenticated whole file" >&2
    exit 1
  fi

  local statement workflow source_uri
  statement="${provenance_bundle}.statement.json"
  if ! jq -er 'select(.dsseEnvelope.payloadType == "application/vnd.in-toto+json") | .dsseEnvelope.payload | @base64d' "${provenance_bundle}" >"${statement}" || ! jq -e . "${statement}" >/dev/null; then
    echo "error: authenticated provenance bundle does not carry a valid in-toto DSSE JSON payload" >&2
    exit 1
  fi
  workflow="${COSIGN_CERT_IDENTITY}"
  source_uri="git+https://github.com/${REPO}@refs/tags/v${tag}"
  local invocation_prefix="https://github.com/${REPO}/actions/runs/"
  if ! jq -e --arg repo "${REPO}" --arg tag "v${tag}" --arg commit "${source_commit}" --arg workflow "${workflow}" --arg uri "${source_uri}" --arg invocation_prefix "${invocation_prefix}" '
    ._type == "https://in-toto.io/Statement/v1" and
    .predicateType == "https://slsa.dev/provenance/v1" and
    .predicate.buildDefinition.buildType == $workflow and
    .predicate.runDetails.builder.id == $workflow and
    .predicate.buildDefinition.externalParameters.repository == $repo and
    .predicate.buildDefinition.externalParameters.tag == $tag and
    .predicate.buildDefinition.externalParameters.commit == $commit and
    (.predicate.buildDefinition.resolvedDependencies | type == "array" and length == 1) and
    .predicate.buildDefinition.resolvedDependencies[0].uri == $uri and
    (.predicate.buildDefinition.resolvedDependencies[0].digest | keys == ["gitCommit"]) and
    .predicate.buildDefinition.resolvedDependencies[0].digest.gitCommit == $commit and
    (.predicate.runDetails.metadata.invocationId | type == "string") and
    (.predicate.runDetails.metadata.invocationId | startswith($invocation_prefix)) and
    (.predicate.runDetails.metadata.invocationId | ltrimstr($invocation_prefix) | test("^[0-9]+/attempts/[0-9]+$"))' "${statement}" >/dev/null; then
    echo "error: authenticated provenance source identity, build type, builder, or exact resolved dependency is not bound to this release" >&2
    exit 1
  fi
  if ! jq -e --argjson count 6 '.subject | type == "array" and length == $count' "${statement}" >/dev/null; then
    echo "error: authenticated provenance must contain exactly six archive subjects" >&2
    exit 1
  fi
  for name in ${RELEASE_ARCHIVES}; do
    digest="$(jq -r --arg name "${name}" '.artifacts[] | select(.name == $name) | .sha256' "${manifest}")"
    if ! jq -e --arg name "${name}" --arg digest "${digest}" '[.subject[] | select(.name == $name)] | length == 1 and (.[0].digest | keys == ["sha256"]) and .[0].digest.sha256 == $digest' "${statement}" >/dev/null; then
      echo "error: authenticated provenance subjects do not exactly match ${name} and its manifest digest" >&2
      exit 1
    fi
  done
}

# verify_release_strict is the fail-closed bootstrap gate. It follows the same
# order internal/releaseverify uses for `okt update`: authenticate the metadata
# bytes first, only then parse them, then bind the provenance to the archive,
# and only then compare digests. Nothing is extracted until every step passed.
verify_release_strict() {
  local archive="$1" asset="$2" tag="$3" tmpdir="$4"
  local base="${GITHUB_DL_BASE}/${REPO}/releases/download/v${tag}"

  local manifest="${tmpdir}/release-manifest-v${tag}.json"
  local manifest_bundle="${tmpdir}/release-manifest-v${tag}.sigstore.json"
  local checksums="${tmpdir}/checksums.txt"
  local checksums_bundle="${tmpdir}/checksums-v${tag}.sigstore.json"
  local provenance_bundle="${tmpdir}/release-provenance-v${tag}.sigstore.json"

  echo "=> Fetching signed release metadata for v${tag}"
  fetch_required "${base}/release-manifest-v${tag}.json" "${manifest}" "release manifest"
  fetch_required "${base}/release-manifest-v${tag}.sigstore.json" "${manifest_bundle}" "release manifest signature"
  fetch_required "${base}/checksums.txt" "${checksums}" "checksums.txt"
  fetch_required "${base}/checksums-v${tag}.sigstore.json" "${checksums_bundle}" "checksums.txt signature"
  fetch_required "${base}/release-provenance-v${tag}.sigstore.json" "${provenance_bundle}" "release provenance"

  # 1. Authenticate the manifest bytes.
  cosign_verify_blob "${manifest_bundle}" "${manifest}" "release manifest"
  echo "=> Signature OK (release manifest)"

  # 2. Authenticate the checksums.txt bytes.
  cosign_verify_blob "${checksums_bundle}" "${checksums}" "checksums.txt"
  echo "=> Signature OK (checksums.txt)"

  # 3. Only now is it safe to read the manifest, and only for this tag.
  local schema repository manifest_tag source_commit
  schema="$(jq -r '.schema_version | type' "${manifest}" 2>/dev/null || true)"
  local schema_token
  schema_token="$(manifest_schema_version "${manifest}" 2>/dev/null || true)"
  if [ "${schema}" != "number" ] || [ "${schema_token}" != "1" ] || ! jq -e '.schema_version == 1' "${manifest}" >/dev/null; then
    echo "error: unsupported release manifest schema version '${schema}'" >&2
    exit 1
  fi
  if ! jq -e '.repository | type == "string"' "${manifest}" >/dev/null; then
    echo "error: signed manifest repository must be a string" >&2
    exit 1
  fi
  repository="$(jq -r '.repository' "${manifest}")"
  if [ "${repository}" != "${REPO}" ]; then
    echo "error: signed manifest names repository '${repository}', not '${REPO}'" >&2
    exit 1
  fi
  if ! jq -e '.tag | type == "string"' "${manifest}" >/dev/null; then
    echo "error: signed manifest tag must be a string" >&2
    exit 1
  fi
  manifest_tag="$(jq -r '.tag' "${manifest}")"
  if [ "${manifest_tag}" != "v${tag}" ]; then
    echo "error: signed manifest is bound to tag '${manifest_tag}', not the requested tag 'v${tag}'" >&2
    echo "       refusing a replayed release manifest" >&2
    exit 1
  fi
  if ! jq -e '.source_commit | type == "string"' "${manifest}" >/dev/null; then
    echo "error: signed manifest source commit must be a string" >&2
    exit 1
  fi
  source_commit="$(jq -r '.source_commit' "${manifest}")"
  if ! printf '%s' "${source_commit}" | grep -qE '^[0-9a-f]{40}([0-9a-f]{24})?$'; then
    echo "error: signed manifest does not record a valid source commit" >&2
    exit 1
  fi

  # 4. Bind the provenance to the exact archive bytes on disk.
  cosign_verify_attestation "${provenance_bundle}" "${archive}"
  echo "=> Provenance OK (${asset})"

  # Decode the now-authenticated DSSE and bind every provenance and artifact
  # field, including all subjects and the whole checksums.txt file.
  validate_authenticated_metadata "${manifest}" "${checksums}" "${provenance_bundle}" "${tag}" "${source_commit}"

  # 5. Cross-bind the three authenticated views of the archive digest.
  local expected recorded actual
  expected="$(jq -r --arg name "${asset}" '.artifacts[] | select(.name == $name) | .sha256' "${manifest}")"
  if ! printf '%s' "${expected}" | grep -qE '^[0-9a-f]{64}$'; then
    echo "error: signed manifest does not list ${asset}" >&2
    exit 1
  fi
  recorded="$(awk -v want="${asset}" '$2 == want {print $1; exit}' "${checksums}")"
  if [ "${recorded}" != "${expected}" ]; then
    echo "error: signed checksums.txt digest for ${asset} disagrees with the signed manifest" >&2
    echo "       checksums.txt: ${recorded}" >&2
    echo "       manifest:      ${expected}" >&2
    exit 1
  fi
  actual="$(sha256_of "${archive}")"
  if [ "${actual}" != "${expected}" ]; then
    echo "error: ${asset} digest mismatch against the authenticated release metadata" >&2
    echo "       expected: ${expected}" >&2
    echo "       actual:   ${actual}" >&2
    echo "       refusing to install a tampered or corrupt archive" >&2
    exit 1
  fi
  echo "=> Digest OK (${actual})"
  echo "=> Verified okt v${tag} — source commit ${source_commit}"
}

# resolve_tty prints the path to an open tty (/dev/stdin or /dev/tty)
# usable by `okt setup` so curl|bash invocations can still drive the
# bubbletea picker. Empty output means no tty — `okt setup` will fall
# back to the headless contract and only run if every OKT_* env var is
# set.
resolve_tty() {
  if [ -t 0 ]; then
    printf '/dev/stdin\n'
    return
  fi
  if ( exec 3</dev/tty ) 2>/dev/null; then
    printf '/dev/tty\n'
  fi
}

ensure_path() {
  command -v okt >/dev/null 2>&1 && return
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) return ;;
  esac
  echo "=> Adding ${INSTALL_DIR} to PATH"
  case "${SHELL:-}" in
    */zsh)  echo "export PATH=\"${INSTALL_DIR}:\${PATH}\"" >> "${HOME}/.zshrc" ;;
    */bash) echo "export PATH=\"${INSTALL_DIR}:\${PATH}\"" >> "${HOME}/.bashrc" ;;
    *)      echo "=> Please add ${INSTALL_DIR} to your PATH manually" ;;
  esac
}

main() {
  local tag="${VERSION:-$(get_latest_tag)}"

  # Both strict preconditions run before a single byte is downloaded, so a host
  # with no verifier — or a target with no signed metadata — never fetches a
  # release at all.
  if [ "${VERIFY_MODE}" = "strict" ]; then
    require_cosign
    require_signed_release "${tag}"
  else
    announce_convenience_trust
  fi

  local os; os=$(get_os)
  local arch; arch=$(get_arch)
  local asset="okt_${os}_${arch}.tar.gz"
  local url="${GITHUB_DL_BASE}/${REPO}/releases/download/v${tag}/${asset}"
  local tmpdir; tmpdir=$(mktemp -d)

  echo "=> Installing okt v${tag} for ${os} ${arch}..."
  echo "=> Downloading ${url}"

  curl -fsSL "${url}" -o "${tmpdir}/${asset}"

  # Verify BEFORE extracting/executing: any failure aborts non-zero here and
  # no binary ever reaches INSTALL_DIR or PATH.
  if [ "${VERIFY_MODE}" = "strict" ]; then
    verify_release_strict "${tmpdir}/${asset}" "${asset}" "${tag}" "${tmpdir}"
  else
    verify_checksum "${tmpdir}/${asset}" "${asset}" "${tag}" "${tmpdir}"
  fi

  tar -xzf "${tmpdir}/${asset}" -C "${tmpdir}"

  mkdir -p "${INSTALL_DIR}"
  install -m 755 "${tmpdir}/okt" "${INSTALL_DIR}/okt"

  rm -rf "${tmpdir}"

  ensure_path

  echo "=> Installed $("${INSTALL_DIR}/okt" --version)"

  # Hand off to the Go picker. When a TTY is reachable we redirect stdin
  # to it so curl|bash users see the bubbletea screens; when none is
  # available `okt setup` runs headlessly against the OKT_* env vars.
  local tty
  tty="$(resolve_tty)"
  if [ -n "$tty" ]; then
    "${INSTALL_DIR}/okt" setup < "$tty"
  else
    "${INSTALL_DIR}/okt" setup
  fi

  echo "=> Run 'okt --help' to get started"
}

if [ "${OKT_INSTALLER_TEST_LIBRARY:-}" != "1" ]; then
  main "$@"
fi
