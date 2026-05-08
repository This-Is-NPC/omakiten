#!/usr/bin/env bash
set -euo pipefail

REPO="This-Is-NPC/omakiten"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Sentinels delimit the okt() shell-wrapper block in user rc files. The
# uninstaller (uninstall.sh) removes the lines between these markers, so the
# strings must match byte-for-byte across both scripts.
WRAPPER_BEGIN="# >>> okt wrapper >>>"
WRAPPER_END="# <<< okt wrapper <<<"

# Harnesses the multi-select prompt offers. Order is significant: the prompt
# numbers options 1..N in this order, and the same index → name mapping is
# used by the test in scripts/installer_select_test.sh.
SUPPORTED_HARNESSES=("claude-code" "claude-desktop" "opencode" "crush" "github-copilot" "codex")

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

harness_is_supported() {
  local candidate="$1" h
  for h in "${SUPPORTED_HARNESSES[@]}"; do
    if [ "$h" = "$candidate" ]; then
      return 0
    fi
  done
  return 1
}

# parse_harness_selection reads a free-form selection string (numbers, names,
# any of "," " " "\t" "\n" as separators) and emits one valid harness per line
# on stdout. Unknown tokens go to stderr but do not fail the function.
parse_harness_selection() {
  local raw="$1" token idx
  local oldifs="$IFS"
  IFS=$',\t \n'
  set -- $raw
  IFS="$oldifs"
  for token in "$@"; do
    [ -z "$token" ] && continue
    case "$token" in
      ''|*[!0-9]*)
        if harness_is_supported "$token"; then
          printf '%s\n' "$token"
        else
          printf 'warn: ignoring unknown harness "%s"\n' "$token" >&2
        fi
        ;;
      *)
        idx=$((token - 1))
        if [ "$idx" -ge 0 ] && [ "$idx" -lt ${#SUPPORTED_HARNESSES[@]} ]; then
          printf '%s\n' "${SUPPORTED_HARNESSES[$idx]}"
        else
          printf 'warn: index %s out of range\n' "$token" >&2
        fi
        ;;
    esac
  done
}

# select_harnesses prints the chosen harness list (one per line) by reading
# OKT_HARNESSES (env override) when set, otherwise prompting on /dev/tty when
# one is available. Silent when neither is present, so curl|bash with a
# non-interactive parent (CI) finishes without hanging.
select_harnesses() {
  if [ -n "${OKT_HARNESSES:-}" ]; then
    parse_harness_selection "$OKT_HARNESSES"
    return
  fi

  # Pick the readable input source. `[ -r /dev/tty ]` checks the device
  # node but not whether open(2) succeeds — Docker without --tty and CI
  # runners without a controlling terminal leave the node in place but
  # return ENXIO on open. Probe with an exec redirect to be sure.
  local input_src=""
  if [ -t 0 ]; then
    input_src="/dev/stdin"
  elif ( exec 3</dev/tty ) 2>/dev/null; then
    input_src="/dev/tty"
  else
    return 0
  fi

  printf '\n=> Wire up MCP for your AI agents (optional)\n' >&2
  local i=1
  for h in "${SUPPORTED_HARNESSES[@]}"; do
    printf '   %d) %s\n' "$i" "$h" >&2
    i=$((i + 1))
  done
  printf '\n   Enter numbers (e.g. 1,3,5), names, or press Enter to skip: ' >&2

  local raw
  if ! IFS= read -r raw < "$input_src"; then
    return 0
  fi
  raw="${raw#"${raw%%[![:space:]]*}"}"
  raw="${raw%"${raw##*[![:space:]]}"}"
  if [ -z "$raw" ]; then
    return 0
  fi
  parse_harness_selection "$raw"
}

# run_harness_setup runs `okt mcp setup --harness X --force` for each harness
# read from stdin. Per-harness failures are reported but do not abort the
# loop, so one missing config dir doesn't block the others.
run_harness_setup() {
  local okt_bin="$1" harness rc
  while IFS= read -r harness; do
    [ -z "$harness" ] && continue
    printf '=> Configuring MCP for %s\n' "$harness"
    if "$okt_bin" mcp setup --harness "$harness" --force >/dev/null 2>&1; then
      continue
    fi
    rc=$?
    printf '   (skipped %s, exit %d — re-run manually: %s mcp setup --harness %s --force)\n' \
      "$harness" "$rc" "$okt_bin" "$harness"
  done
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

  local selections
  selections="$(select_harnesses)"
  if [ -n "$selections" ]; then
    printf '%s\n' "$selections" | run_harness_setup "${INSTALL_DIR}/okt"
  fi

  echo "=> Run 'okt --help' to get started"
}

main "$@"
