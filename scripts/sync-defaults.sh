#!/usr/bin/env bash
# Mirror defaults/ into a target config root using the v2 layout:
#
#   <root>/config/<profile>.yaml  (official config profiles, overwritten every run)
#   <root>/<entity>/<file>        (default scope — fully mirrored: stale
#                                  files at the default scope are removed,
#                                  fresh ones from defaults/ are copied in)
#   <root>/<entity>/custom/       (user scope — created if missing,
#                                  contents NEVER touched)
#
# The default scope is treated like a published kit: it MUST equal what
# defaults/ ships, no more, no less. Users keep their tweaks under
# custom/, which this script ignores.
#
# Usage: sync-defaults.sh <target-root>

set -euo pipefail

if [ "$#" -ne 1 ]; then
  printf 'usage: %s <target-root>\n' "$0" >&2
  exit 1
fi

target_root="$1"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
defaults_dir="$repo_root/defaults"

if [ ! -d "$defaults_dir" ]; then
  printf 'defaults directory missing: %s\n' "$defaults_dir" >&2
  exit 1
fi

mkdir -p "$target_root/config/custom"

if [ -d "$defaults_dir/config" ]; then
  for src in "$defaults_dir/config"/*.yaml; do
    [ -f "$src" ] || continue
    install -m644 "$src" "$target_root/config/$(basename "$src")"
  done
fi

for sub in skills laws personas templates themes notifications; do
  src_dir="$defaults_dir/$sub"
  dst_dir="$target_root/$sub"
  mkdir -p "$dst_dir/custom"

  # Purge stale files at the default scope (top-level only — never
  # descend into custom/) so removed defaults disappear on re-sync.
  if [ -d "$dst_dir" ]; then
    for existing in "$dst_dir"/*; do
      [ -f "$existing" ] || continue
      rm -f "$existing"
    done
  fi

  [ -d "$src_dir" ] || continue
  for src in "$src_dir"/*; do
    [ -f "$src" ] || continue
    install -m644 "$src" "$dst_dir/$(basename "$src")"
  done
done

printf 'Synced defaults into %s\n' "$target_root"
