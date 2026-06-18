#!/usr/bin/env bash
set -euo pipefail

# install.sh — fetch latest okt release, drop the binary in INSTALL_DIR,
# and hand off to `okt setup` for the interactive picker (or the env-var
# headless path). Every prompt, the rc-file wrapper writer, and the
# harness/preset selection logic live inside the Go binary now; this
# wrapper is meant to stay short and platform-portable so a `curl|bash`
# invocation that only needs to bootstrap the binary does not have to
# carry the picker UI in two places.

REPO="This-Is-NPC/omakiten"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Base hosts default to GitHub but are overridable so the hermetic
# tamper-abort test (and any release mirror) can point the download +
# checksum fetch at a local HTTPS endpoint without rewriting URL logic.
# Production users never set these; the trust assumption (TLS to the
# release host) is unchanged.
GITHUB_API_BASE="${GITHUB_API_BASE:-https://api.github.com}"
GITHUB_DL_BASE="${GITHUB_DL_BASE:-https://github.com}"

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
# sha256-verify-before-extract gate (internal/cli/update.go:362-383) so the
# very first okt binary a user runs is verified the same way every later
# `okt update` is.
#
# Trust assumption: the canonical hash comes from the release asset
# checksums.txt fetched over HTTPS from the release host (GitHub). We trust
# the TLS connection to that host to deliver an authentic checksums.txt;
# the archive is then pinned to that file. This closes the supply-chain gap
# between bootstrap and the updater but is NOT a substitute for signing /
# notarization / SLSA provenance (out of scope here).
verify_checksum() {
  local archive="$1" asset="$2" tag="$3" tmpdir="$4"
  local sums_url="${GITHUB_DL_BASE}/${REPO}/releases/download/v${tag}/checksums.txt"
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
  local os; os=$(get_os)
  local arch; arch=$(get_arch)
  local asset="okt_${os}_${arch}.tar.gz"
  local url="${GITHUB_DL_BASE}/${REPO}/releases/download/v${tag}/${asset}"
  local tmpdir; tmpdir=$(mktemp -d)

  echo "=> Installing okt v${tag} for ${os} ${arch}..."
  echo "=> Downloading ${url}"

  curl -fsSL "${url}" -o "${tmpdir}/${asset}"

  # Verify BEFORE extracting/executing: a mismatch aborts non-zero here and
  # no binary ever reaches INSTALL_DIR or PATH.
  verify_checksum "${tmpdir}/${asset}" "${asset}" "${tag}" "${tmpdir}"

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

main "$@"
