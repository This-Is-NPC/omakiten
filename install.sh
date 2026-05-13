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

# Workflow presets the preset prompt offers. Index 1 is the default selected
# on empty input; SUPPORTED_PRESETS_DESC carries a one-line description per
# entry shown in the prompt.
SUPPORTED_PRESETS=("omakase" "izakaya" "kaiseki" "shokunin")
SUPPORTED_PRESETS_DESC=(
  "balanced — trunk-based + DORA + small batches"
  "minimal — lean spike / tracer-bullet"
  "formal — staged delivery + decision records + peer review"
  "max rigor — SRE + dual peer-review + postmortems"
)
DEFAULT_PRESET="omakase"

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

preset_is_supported() {
  local candidate="$1" p
  for p in "${SUPPORTED_PRESETS[@]}"; do
    if [ "$p" = "$candidate" ]; then
      return 0
    fi
  done
  return 1
}

# resolve_config_dir prints the absolute path of the directory that holds
# `<active>.yaml` and `.active`, following the same precedence as
# internal/paths/paths.go (OMAKITEN_HOME → XDG_CONFIG_HOME → ~/.config).
resolve_config_dir() {
  if [ -n "${OMAKITEN_HOME:-}" ]; then
    printf '%s/config' "$OMAKITEN_HOME"
    return
  fi
  if [ -n "${XDG_CONFIG_HOME:-}" ]; then
    printf '%s/omakiten/config' "$XDG_CONFIG_HOME"
    return
  fi
  printf '%s/.config/omakiten/config' "${HOME}"
}

# select_preset prints the chosen preset name (single-line) by reading
# OKT_PRESET when set, otherwise prompting on /dev/tty. Defaults to omakase
# on empty input. Silent in non-interactive shells (returns the default).
select_preset() {
  if [ -n "${OKT_PRESET:-}" ]; then
    if preset_is_supported "$OKT_PRESET"; then
      printf '%s\n' "$OKT_PRESET"
      return 0
    fi
    printf 'warn: OKT_PRESET=%s is not a supported preset; falling back to %s\n' \
      "$OKT_PRESET" "$DEFAULT_PRESET" >&2
    printf '%s\n' "$DEFAULT_PRESET"
    return 0
  fi

  local input_src=""
  if [ -t 0 ]; then
    input_src="/dev/stdin"
  elif ( exec 3</dev/tty ) 2>/dev/null; then
    input_src="/dev/tty"
  else
    printf '%s\n' "$DEFAULT_PRESET"
    return 0
  fi

  printf '\n=> Pick a workflow preset (process discipline level)\n' >&2
  local i=1
  for p in "${SUPPORTED_PRESETS[@]}"; do
    local idx=$((i - 1))
    local default_marker=""
    [ "$p" = "$DEFAULT_PRESET" ] && default_marker="   [default]"
    printf '   %d) %-10s — %s%s\n' "$i" "$p" "${SUPPORTED_PRESETS_DESC[$idx]}" "$default_marker" >&2
    i=$((i + 1))
  done

  local raw token idx
  while true; do
    printf '\n   Enter a number or name (Enter for default): ' >&2
    if ! IFS= read -r raw < "$input_src"; then
      printf '%s\n' "$DEFAULT_PRESET"
      return 0
    fi
    raw="${raw#"${raw%%[![:space:]]*}"}"
    raw="${raw%"${raw##*[![:space:]]}"}"
    if [ -z "$raw" ]; then
      printf '%s\n' "$DEFAULT_PRESET"
      return 0
    fi
    token="$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')"
    case "$token" in
      ''|*[!0-9]*)
        if preset_is_supported "$token"; then
          printf '%s\n' "$token"
          return 0
        fi
        printf '   "%s" is not a supported preset — try again\n' "$raw" >&2
        ;;
      *)
        idx=$((token - 1))
        if [ "$idx" -ge 0 ] && [ "$idx" -lt ${#SUPPORTED_PRESETS[@]} ]; then
          printf '%s\n' "${SUPPORTED_PRESETS[$idx]}"
          return 0
        fi
        printf '   index %s out of range — try again\n' "$token" >&2
        ;;
    esac
  done
}

# write_active_preset writes <preset>.yaml to `<config-dir>/.active`, creating
# the directory if needed. The yaml file itself is materialized by `okt init`
# on first invocation from the embedded defaults; this just primes the
# resolver to pick the right one when okt runs.
write_active_preset() {
  local preset="$1"
  local config_dir
  config_dir="$(resolve_config_dir)"
  mkdir -p "$config_dir"
  printf '%s.yaml\n' "$preset" > "$config_dir/.active"
  printf '=> Active workflow preset: %s (%s/.active)\n' "$preset" "$config_dir"
}

# parse_harness_selection reads a free-form selection string (numbers, names,
# any of "," " " "\t" "\n" as separators) and emits one valid harness per line
# on stdout. Unknown tokens go to stderr but do not fail the function.
#
# Exit codes drive the retry loop in select_harnesses:
#   0 — at least one valid harness was emitted
#   1 — input had tokens but none matched a harness (typo / garbage)
#   2 — input contained "0" / "skip" / "none" → user chose to skip explicitly
#   3 — input was empty (only whitespace, no tokens)
parse_harness_selection() {
  local raw="$1" token idx had_token=0
  local results=()
  local oldifs="$IFS"
  IFS=$',\t \n'
  set -- $raw
  IFS="$oldifs"
  for token in "$@"; do
    [ -z "$token" ] && continue
    had_token=1
    # Skip sentinel — wins over anything else in the same input.
    case "$(printf '%s' "$token" | tr '[:upper:]' '[:lower:]')" in
      0|skip|none) return 2 ;;
    esac
    case "$token" in
      ''|*[!0-9]*)
        if harness_is_supported "$token"; then
          results+=("$token")
        else
          printf 'warn: ignoring unknown harness "%s"\n' "$token" >&2
        fi
        ;;
      *)
        idx=$((token - 1))
        if [ "$idx" -ge 0 ] && [ "$idx" -lt ${#SUPPORTED_HARNESSES[@]} ]; then
          results+=("${SUPPORTED_HARNESSES[$idx]}")
        else
          printf 'warn: index %s out of range\n' "$token" >&2
        fi
        ;;
    esac
  done
  if [ ${#results[@]} -gt 0 ]; then
    printf '%s\n' "${results[@]}"
    return 0
  fi
  if [ "$had_token" -eq 0 ]; then
    return 3
  fi
  return 1
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
  printf '   0) skip\n' >&2

  # Loop until the user picks at least one valid harness or asks to skip.
  # Empty input or all-invalid input just re-prompts — only "0" / "skip" /
  # "none" exits without configuring anything.
  local raw out rc
  while true; do
    printf '\n   Enter numbers (e.g. 1,3,5) or names, or 0 to skip: ' >&2
    if ! IFS= read -r raw < "$input_src"; then
      return 0
    fi
    raw="${raw#"${raw%%[![:space:]]*}"}"
    raw="${raw%"${raw##*[![:space:]]}"}"

    rc=0
    out="$(parse_harness_selection "$raw")" || rc=$?
    case "$rc" in
      0)
        printf '%s\n' "$out"
        return 0
        ;;
      2)
        return 0
        ;;
      3)
        printf '   please enter at least one number/name, or 0 to skip\n' >&2
        ;;
      *)
        printf '   none of those matched a harness — try again, or 0 to skip\n' >&2
        ;;
    esac
  done
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

  local preset
  preset="$(select_preset)"
  write_active_preset "$preset"

  local selections
  selections="$(select_harnesses)"
  if [ -n "$selections" ]; then
    printf '%s\n' "$selections" | run_harness_setup "${INSTALL_DIR}/okt"
  fi

  echo "=> Run 'okt --help' to get started"
}

main "$@"
