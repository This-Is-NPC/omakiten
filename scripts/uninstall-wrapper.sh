#!/usr/bin/env bash
# Remove the okt() shell wrapper block from the user's bash and zsh init
# files. Idempotent: a no-op if the sentinels are not present. Surgical:
# only the lines between the sentinels are removed; everything else in
# the rc file is preserved byte-for-byte.
set -euo pipefail

WRAPPER_BEGIN="# >>> okt wrapper >>>"
WRAPPER_END="# <<< okt wrapper <<<"

remove_from() {
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

for rc in "${HOME}/.bashrc" "${HOME}/.zshrc"; do
  remove_from "$rc"
done
