#!/usr/bin/env bash
# Mirrors the install.sh harness multi-select prompt for the local
# `mise run install` flow, so a developer's dev install matches what a
# curl|bash user gets.
#
# The selection logic lives in install.sh; this script awk-extracts the
# helpers (SUPPORTED_HARNESSES, parse_harness_selection, select_harnesses,
# run_harness_setup, harness_is_supported) and evaluates them in-process.
# Single source of truth → no duplication, no drift.
#
# Usage: scripts/harness-select.sh [okt-binary-path]
#   Defaults to whichever `okt` is on PATH.
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

helpers="$(awk '
  /^SUPPORTED_HARNESSES=/                          { print; next }
  /^harness_is_supported\(\) \{/                   { in_fn = 1 }
  /^parse_harness_selection\(\) \{/                { in_fn = 1 }
  /^select_harnesses\(\) \{/                       { in_fn = 1 }
  /^run_harness_setup\(\) \{/                      { in_fn = 1 }
  in_fn                                            { print }
  in_fn && /^\}$/                                  { in_fn = 0 }
' "$repo/install.sh")"

if [ -z "$helpers" ]; then
  printf 'harness-select: failed to extract helpers from install.sh\n' >&2
  exit 1
fi

eval "$helpers"

okt_bin="${1:-okt}"
if ! command -v "$okt_bin" >/dev/null 2>&1 && [ ! -x "$okt_bin" ]; then
  printf 'harness-select: okt binary not found at %q\n' "$okt_bin" >&2
  exit 1
fi

selections="$(select_harnesses)"
if [ -n "$selections" ]; then
  printf '%s\n' "$selections" | run_harness_setup "$okt_bin"
fi
