#!/usr/bin/env bash
set -euo pipefail

REPO="This-Is-NPC/omakiten"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Sentinels delimit the okt() shell-wrapper block in user rc files. The
# uninstaller (uninstall.sh) removes the lines between these markers, so the
# strings must match byte-for-byte across both scripts.
WRAPPER_BEGIN="# >>> okt wrapper >>>"
WRAPPER_END="# <<< okt wrapper <<<"

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

install_wrapper_into() {
  local rc="$1"
  [ -e "$rc" ] || touch "$rc"
  local block
  block="$(printf '%s\n' \
    "${WRAPPER_BEGIN}" \
    "# Auto-installed by omakiten. Lets \`okt tui\` cd the parent shell" \
    "# into the project chosen on the Home screen when the TUI exits." \
    "# Remove with the bundled uninstall.sh; do not edit by hand." \
    "okt() {" \
    "  local cd_file=\"\${OKT_CD_FILE:-\${XDG_RUNTIME_DIR:-\${TMPDIR:-/tmp}}/okt-cd}\"" \
    "  if [ -z \"\${XDG_RUNTIME_DIR:-}\" ] && [ -z \"\${OKT_CD_FILE:-}\" ]; then" \
    "    cd_file=\"\${TMPDIR:-/tmp}/okt-cd-\$(id -u)\"" \
    "  fi" \
    "  rm -f \"\$cd_file\" 2>/dev/null || true" \
    "  command okt \"\$@\"" \
    "  local rc=\$?" \
    "  if [ -f \"\$cd_file\" ]; then" \
    "    local target" \
    "    target=\"\$(head -n 1 \"\$cd_file\")\"" \
    "    rm -f \"\$cd_file\" 2>/dev/null || true" \
    "    if [ -n \"\$target\" ] && [ -d \"\$target\" ]; then" \
    "      cd \"\$target\" || return \$rc" \
    "    fi" \
    "  fi" \
    "  return \$rc" \
    "}" \
    "${WRAPPER_END}")"

  if grep -qF "${WRAPPER_BEGIN}" "$rc"; then
    # Idempotent replace: drop the existing block in place. awk variant
    # avoids a second `sed` pass and survives in BSD sed environments.
    local tmp; tmp=$(mktemp)
    awk -v begin="${WRAPPER_BEGIN}" -v end="${WRAPPER_END}" -v block="$block" '
      $0 == begin { skipping = 1; print block; next }
      skipping && $0 == end { skipping = 0; next }
      !skipping
    ' "$rc" > "$tmp"
    mv "$tmp" "$rc"
  else
    printf '\n%s\n' "$block" >> "$rc"
  fi
}

install_wrapper() {
  local installed_into=()
  for rc in "${HOME}/.bashrc" "${HOME}/.zshrc"; do
    if [ -e "$rc" ] || { [ "$rc" = "${HOME}/.bashrc" ] && [ -n "${BASH_VERSION:-}" ]; } || { [ "$rc" = "${HOME}/.zshrc" ] && [ -n "${ZSH_VERSION:-}" ]; }; then
      install_wrapper_into "$rc"
      installed_into+=("$rc")
    fi
  done
  if [ ${#installed_into[@]} -gt 0 ]; then
    echo "=> Installed okt() shell wrapper into: ${installed_into[*]}"
    echo "   Open a new shell (or 'source' your rc file) to enable cd-on-exit."
  else
    echo "=> Skipping shell-wrapper install: no ~/.bashrc or ~/.zshrc found"
  fi
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

  install_wrapper

  echo "=> Installed $("${INSTALL_DIR}/okt" --version)"
  echo "=> Run 'okt --help' to get started"
}

main "$@"
