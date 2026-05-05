#!/usr/bin/env bash
# Mirror defaults/ into a target config root using the v2 layout:
#
#   <root>/config/omakiten.yaml         (overwritten on every run)
#   <root>/<entity>/<file>              (defaults — overwritten on every run)
#   <root>/<entity>/custom/             (created if missing, never touched)
#
# Default files that the user customized at the root are intentionally
# overwritten. Customization belongs in <entity>/custom/, which this script
# never touches.
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

mkdir -p "$target_root/config"
install -m644 "$defaults_dir/omakiten.yaml" "$target_root/config/omakiten.yaml"

for sub in skills laws personas templates themes; do
  src_dir="$defaults_dir/$sub"
  dst_dir="$target_root/$sub"
  mkdir -p "$dst_dir/custom"
  [ -d "$src_dir" ] || continue
  for src in "$src_dir"/*; do
    [ -f "$src" ] || continue
    install -m644 "$src" "$dst_dir/$(basename "$src")"
  done
done

printf 'Synced defaults into %s\n' "$target_root"
