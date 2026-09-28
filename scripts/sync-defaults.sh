#!/usr/bin/env bash
# Mirror defaults/ into a target config root using the v2 layout:
#
#   <root>/config/                 (official profiles and imported fragments,
#                                  fully mirrored every run)
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

sync_managed_dir() {
  local src_dir="$1"
  local dst_dir="$2"
  local preserve_custom="$3"
  local src dst existing name

  mkdir -p "$dst_dir"

  # Publish replacements before pruning stale entries. A failed copy can leave
  # an older managed file in place, but it cannot empty the install first.
  for src in "$src_dir"/*; do
    [ -e "$src" ] || continue
    dst="$dst_dir/$(basename "$src")"
    if [ -d "$src" ]; then
      if [ -e "$dst" ] && [ ! -d "$dst" ]; then
        rm -f "$dst"
      fi
      sync_managed_dir "$src" "$dst" false
    else
      if [ -d "$dst" ]; then
        rm -rf "$dst"
      fi
      install -m644 "$src" "$dst"
    fi
  done

  for existing in "$dst_dir"/*; do
    [ -e "$existing" ] || continue
    name="$(basename "$existing")"
    if [ "$preserve_custom" = true ] && [ "$name" = custom ]; then
      continue
    fi
    [ -e "$src_dir/$name" ] || rm -rf "$existing"
  done
}

mkdir -p "$target_root/config/custom"

if [ -d "$defaults_dir/config" ]; then
  # Presets import config/modules and config/themes, so replacing only the
  # top-level YAML files can combine a current preset with stale fragments.
  # Mirror every managed entry recursively while preserving custom/ and the
  # hidden .active marker (shell globs deliberately exclude it).
  sync_managed_dir "$defaults_dir/config" "$target_root/config" true
fi

for sub in skills laws personas templates themes notifications languages; do
  src_dir="$defaults_dir/$sub"
  dst_dir="$target_root/$sub"
  mkdir -p "$dst_dir/custom"
  sync_managed_dir "$src_dir" "$dst_dir" true
done

printf 'Synced defaults into %s\n' "$target_root"
