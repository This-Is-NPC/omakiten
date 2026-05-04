#!/usr/bin/env bash
set -euo pipefail

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

  if ! command -v okt >/dev/null 2>&1; then
    case ":${PATH}:" in
      *":${INSTALL_DIR}:"*) ;;
      *)
        echo "=> Adding ${INSTALL_DIR} to PATH"
        case "${SHELL:-}" in
          */zsh)  echo "export PATH=\"${INSTALL_DIR}:\${PATH}\"" >> "${HOME}/.zshrc" ;;
          */bash) echo "export PATH=\"${INSTALL_DIR}:\${PATH}\"" >> "${HOME}/.bashrc" ;;
          *)      echo "=> Please add ${INSTALL_DIR} to your PATH manually" ;;
        esac
        ;;
    esac
  fi

  echo "=> Installed $("${INSTALL_DIR}/okt" --version)"
  echo "=> Run 'okt --help' to get started"
}

main "$@"
