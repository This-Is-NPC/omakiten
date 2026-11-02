#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Sentinels delimit the okt() shell-wrapper block in user rc files. Must
# match the strings written by install.sh byte-for-byte.
WRAPPER_BEGIN="# >>> okt wrapper >>>"
WRAPPER_END="# <<< okt wrapper <<<"

remove_wrapper_from() {
  local rc="$1"
  [ -f "$rc" ] || return 0
  if ! grep -qF "${WRAPPER_BEGIN}" "$rc"; then
    return 0
  fi
  local tmp; tmp=$(mktemp)
  awk -v begin="${WRAPPER_BEGIN}" -v end="${WRAPPER_END}" '
    { line = $0; sub(/\r$/, "", line) }
    line == begin { skipping = 1; next }
    skipping && line == end { skipping = 0; next }
    !skipping
  ' "$rc" > "$tmp"
  mv "$tmp" "$rc"
  echo "=> Removed okt() wrapper from ${rc}"
}

main() {
  for rc in "${HOME}/.bashrc" "${HOME}/.zshrc"; do
    remove_wrapper_from "$rc"
  done

  for bin in okt okt-server; do
    if [ -x "${INSTALL_DIR}/${bin}" ]; then
      rm -f "${INSTALL_DIR}/${bin}"
      echo "=> Removed ${INSTALL_DIR}/${bin}"
    fi
  done

  echo "=> Done. Open a new shell so the wrapper change takes effect."
}

main "$@"
