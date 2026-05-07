#!/usr/bin/env bash
# Install the okt() shell wrapper into the user's bash and zsh init files.
# Wrapped in sentinel comments so uninstall-wrapper.sh can remove it cleanly.
# Idempotent: re-running replaces the existing block in place.
#
# Used by both the curl-based install.sh (canonical end-user install) and
# the local dev `mise run install` task. The two paths share this script
# so a developer's local environment matches what shipped users get.
set -euo pipefail

WRAPPER_BEGIN="# >>> okt wrapper >>>"
WRAPPER_END="# <<< okt wrapper <<<"

block="$(printf '%s\n' \
  "${WRAPPER_BEGIN}" \
  "# Auto-installed by omakiten. Lets \`okt tui\` cd the parent shell" \
  "# into the project chosen on the Home screen when the TUI exits." \
  "# Remove with the bundled uninstall.sh (or scripts/uninstall-wrapper.sh)." \
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

install_into() {
  local rc="$1"
  [ -e "$rc" ] || touch "$rc"
  if grep -qF "${WRAPPER_BEGIN}" "$rc"; then
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

main() {
  local installed_into=()
  for rc in "${HOME}/.bashrc" "${HOME}/.zshrc"; do
    if [ -e "$rc" ]; then
      install_into "$rc"
      installed_into+=("$rc")
    fi
  done
  if [ ${#installed_into[@]} -gt 0 ]; then
    echo "=> Installed okt() shell wrapper into: ${installed_into[*]}"
    echo "   Open a new shell (or 'source' your rc file) to enable cd-on-exit."
  else
    echo "=> No ~/.bashrc or ~/.zshrc found; skipping shell wrapper install."
  fi
}

main "$@"
