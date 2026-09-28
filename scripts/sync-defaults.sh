#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

if [ "$#" -ne 1 ]; then
  printf 'usage: %s <target-root>\n' "$0" >&2
  exit 1
fi

target_root="$1"
if [[ -L "$target_root" ]]; then
  printf 'sync target must not be a symlink: %s\n' "$target_root" >&2
  exit 1
fi
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

  if [[ -L "$dst_dir" ]]; then
    printf 'managed directory must not be a symlink: %s\n' "$dst_dir" >&2
    return 1
  fi
  mkdir -p "$dst_dir"
  if [[ "$preserve_custom" == true && ! -e "$dst_dir/custom" && ! -L "$dst_dir/custom" ]]; then
    mkdir -p "$dst_dir/custom"
  fi

  # Publish replacements before pruning stale entries. A failed copy can leave
  # an older managed file in place, but it cannot empty the install first.
  for src in "$src_dir"/*; do
    [ -e "$src" ] || continue
    dst="$dst_dir/${src##*/}"
    if [[ -L "$dst" ]]; then
      printf 'managed entry must not be a symlink: %s\n' "$dst" >&2
      return 1
    fi
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
    [[ -e "$existing" || -L "$existing" ]] || continue
    name="${existing##*/}"
    if [ "$preserve_custom" = true ] && [ "$name" = custom ]; then
      continue
    fi
    [ -e "$src_dir/$name" ] || rm -rf "$existing"
  done
}

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
  sync_managed_dir "$src_dir" "$dst_dir" true
done

printf 'Synced defaults into %s\n' "$target_root"
