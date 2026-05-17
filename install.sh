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

get_latest_tag() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
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
  local url="https://github.com/${REPO}/releases/download/v${tag}/${asset}"
  local tmpdir; tmpdir=$(mktemp -d)

  echo "=> Installing okt v${tag} for ${os} ${arch}..."
  echo "=> Downloading ${url}"

  curl -fsSL "${url}" -o "${tmpdir}/${asset}"
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
