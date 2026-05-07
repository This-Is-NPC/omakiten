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
    $0 == begin { skipping = 1; next }
    skipping && $0 == end { skipping = 0; next }
    !skipping
  ' "$rc" > "$tmp"
  mv "$tmp" "$rc"
  echo "=> Removed okt() wrapper from ${rc}"
}

main() {
  for rc in "${HOME}/.bashrc" "${HOME}/.zshrc"; do
    remove_wrapper_from "$rc"
  done

  if [ -x "${INSTALL_DIR}/okt" ]; then
    rm -f "${INSTALL_DIR}/okt"
    echo "=> Removed ${INSTALL_DIR}/okt"
  fi

  echo "=> Done. Open a new shell so the wrapper change takes effect."
}

main "$@"
