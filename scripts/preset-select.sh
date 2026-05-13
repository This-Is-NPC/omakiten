#!/usr/bin/env bash
# Mirrors the install.sh workflow-preset prompt for the local
# `mise run install` flow, so a developer's dev install matches what a
# curl|bash user gets.
#
# The selection logic lives in install.sh; this script awk-extracts the
# helpers (SUPPORTED_PRESETS, SUPPORTED_PRESETS_DESC, DEFAULT_PRESET,
# preset_is_supported, resolve_config_dir, select_preset, write_active_preset)
# and evaluates them in-process. Single source of truth → no duplication,
# no drift.
#
# Usage: scripts/preset-select.sh
#   OKT_PRESET=<name> bypasses the prompt with the named preset.
#   Non-interactive shells skip the prompt silently and pick the default.
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

helpers="$(awk '
  /^SUPPORTED_PRESETS=/                            { print; next }
  /^SUPPORTED_PRESETS_DESC=/                       { in_arr = 1 }
  in_arr                                           { print }
  in_arr && /^\)$/                                 { in_arr = 0; next }
  /^DEFAULT_PRESET=/                               { print; next }
  /^preset_is_supported\(\) \{/                    { in_fn = 1 }
  /^resolve_config_dir\(\) \{/                     { in_fn = 1 }
  /^select_preset\(\) \{/                          { in_fn = 1 }
  /^write_active_preset\(\) \{/                    { in_fn = 1 }
  in_fn                                            { print }
  in_fn && /^\}$/                                  { in_fn = 0 }
' "$repo/install.sh")"

if [ -z "$helpers" ]; then
  printf 'preset-select: failed to extract helpers from install.sh\n' >&2
  exit 1
fi

eval "$helpers"

preset="$(select_preset)"
write_active_preset "$preset"
