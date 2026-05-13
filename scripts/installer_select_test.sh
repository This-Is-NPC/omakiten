#!/usr/bin/env bash
# Validates install.sh's harness selection helpers:
#   - parse_harness_selection accepts numbers, names, and mixed input
#   - parse_harness_selection rejects out-of-range indices and unknown names
#   - select_harnesses honors OKT_HARNESSES without prompting
#   - select_harnesses is silent when no TTY and no env override
#
# Run with: bash scripts/installer_select_test.sh
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Source only the helper functions and the SUPPORTED_HARNESSES global out of
# install.sh. We avoid the awk-extraction approach used by
# wrapper_idempotency_test.sh because the helpers reference each other; a
# guard env var lets install.sh's main() bail out at the very end.
helpers="$(awk '
  /^SUPPORTED_HARNESSES=/                          { print; next }
  /^SUPPORTED_PRESETS=/                            { print; next }
  /^SUPPORTED_PRESETS_DESC=/                       { in_arr = 1 }
  in_arr                                           { print }
  in_arr && /^\)$/                                 { in_arr = 0; next }
  /^DEFAULT_PRESET=/                               { print; next }
  /^harness_is_supported\(\) \{/                   { in_fn = 1 }
  /^parse_harness_selection\(\) \{/                { in_fn = 1 }
  /^select_harnesses\(\) \{/                       { in_fn = 1 }
  /^run_harness_setup\(\) \{/                      { in_fn = 1 }
  /^preset_is_supported\(\) \{/                    { in_fn = 1 }
  /^resolve_config_dir\(\) \{/                     { in_fn = 1 }
  /^select_preset\(\) \{/                          { in_fn = 1 }
  /^write_active_preset\(\) \{/                    { in_fn = 1 }
  in_fn                                            { print }
  in_fn && /^\}$/                                  { in_fn = 0 }
' "$repo/install.sh")"

eval "$helpers"

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

assert_equal() {
  local got="$1" want="$2" label="$3"
  if [ "$got" != "$want" ]; then
    printf 'FAIL: %s\n  got:  %q\n  want: %q\n' "$label" "$got" "$want" >&2
    exit 1
  fi
}

# --- parse_harness_selection: stdout shape ---

got="$(parse_harness_selection "1,3,5" 2>/dev/null)"
assert_equal "$got" $'claude-code\nopencode\ngithub-copilot' "numeric comma list"

got="$(parse_harness_selection "1 3 5" 2>/dev/null)"
assert_equal "$got" $'claude-code\nopencode\ngithub-copilot' "numeric space list"

got="$(parse_harness_selection "claude-code,codex" 2>/dev/null)"
assert_equal "$got" $'claude-code\ncodex' "name comma list"

got="$(parse_harness_selection "1 codex" 2>/dev/null)"
assert_equal "$got" $'claude-code\ncodex' "mixed numeric and name"

got="$(parse_harness_selection "1,bogus" 2>/dev/null)"
assert_equal "$got" "claude-code" "partial-valid input emits the valid harness"

# `|| true` is needed for the cases below: parse_harness_selection now exits
# non-zero on empty / all-invalid input to drive the retry loop in
# select_harnesses, and `var=$(cmd)` propagates that under `set -e`.
got="$(parse_harness_selection "" 2>/dev/null || true)"
assert_equal "$got" "" "empty input emits nothing"

got="$(parse_harness_selection "99" 2>/dev/null || true)"
assert_equal "$got" "" "out-of-range index emits no harness"
err="$(parse_harness_selection "99" 2>&1 >/dev/null || true)"
case "$err" in
  *"out of range"*) ;;
  *) fail "out-of-range index did not warn on stderr: $err" ;;
esac

got="$(parse_harness_selection "bogus" 2>/dev/null || true)"
assert_equal "$got" "" "unknown name emits no harness"
err="$(parse_harness_selection "bogus" 2>&1 >/dev/null || true)"
case "$err" in
  *"ignoring unknown harness"*) ;;
  *) fail "unknown harness did not warn on stderr: $err" ;;
esac

# --- parse_harness_selection: exit codes drive the retry loop ---

assert_rc() {
  local raw="$1" want_rc="$2" label="$3"
  local got_rc=0
  parse_harness_selection "$raw" >/dev/null 2>&1 || got_rc=$?
  if [ "$got_rc" -ne "$want_rc" ]; then
    fail "$label — exit $got_rc, want $want_rc"
  fi
}

assert_rc "1,3,5"          0 "rc 0 on full-valid input"
assert_rc "1,bogus"        0 "rc 0 on partial-valid input"
assert_rc ""               3 "rc 3 on empty input"
assert_rc "   "            3 "rc 3 on whitespace-only input"
assert_rc "bogus"          1 "rc 1 on tokens with zero matches"
assert_rc "99"             1 "rc 1 on out-of-range numeric"
assert_rc "0"              2 "rc 2 on '0' (skip sentinel)"
assert_rc "skip"           2 "rc 2 on 'skip'"
assert_rc "Skip"           2 "rc 2 on 'Skip' (case-insensitive)"
assert_rc "NONE"           2 "rc 2 on 'NONE' (case-insensitive)"
assert_rc "1,0"            2 "rc 2 — skip wins over a valid token in the same input"
assert_rc "bogus,skip"     2 "rc 2 — skip wins over invalid tokens"

# Skip sentinel must not leak any harness on stdout.
got="$(parse_harness_selection "1,0" 2>/dev/null || true)"
assert_equal "$got" "" "'1,0' emits nothing on stdout (skip wins)"
got="$(parse_harness_selection "skip" 2>/dev/null || true)"
assert_equal "$got" "" "'skip' emits nothing on stdout"

# --- select_harnesses honors OKT_HARNESSES ---

OKT_HARNESSES="claude-code,opencode" got="$(select_harnesses 2>/dev/null)"
assert_equal "$got" $'claude-code\nopencode' "OKT_HARNESSES env override"

# OKT_HARNESSES with junk separators
OKT_HARNESSES=$'crush\tcodex,, ,' got="$(select_harnesses 2>/dev/null)"
assert_equal "$got" $'crush\ncodex' "OKT_HARNESSES tolerates junk separators"

# --- select_harnesses silent when no TTY and no env override ---

got="$(unset OKT_HARNESSES; select_harnesses </dev/null 2>/dev/null)"
assert_equal "$got" "" "no TTY + no env → no output"

# --- SUPPORTED_HARNESSES contains every harness Setup recognises ---

want_count=6
got_count="${#SUPPORTED_HARNESSES[@]}"
if [ "$got_count" -ne "$want_count" ]; then
  fail "SUPPORTED_HARNESSES has $got_count entries, want $want_count (sync with internal/agentsetup.SupportedHarnesses)"
fi

# --- preset_is_supported ---

if ! preset_is_supported "omakase"; then fail "omakase must be supported"; fi
if ! preset_is_supported "shokunin"; then fail "shokunin must be supported"; fi
if preset_is_supported "bogus" 2>/dev/null; then fail "bogus must not be supported"; fi

# --- select_preset honors OKT_PRESET ---

got="$(OKT_PRESET=izakaya select_preset 2>/dev/null)"
assert_equal "$got" "izakaya" "OKT_PRESET env override"

got="$(OKT_PRESET=shokunin select_preset 2>/dev/null)"
assert_equal "$got" "shokunin" "OKT_PRESET=shokunin honored"

# Unknown OKT_PRESET falls back to the default with a stderr warning.
got="$(OKT_PRESET=bogus select_preset 2>/dev/null)"
assert_equal "$got" "omakase" "OKT_PRESET=bogus falls back to omakase"
err="$(OKT_PRESET=bogus select_preset 2>&1 >/dev/null)"
case "$err" in
  *"is not a supported preset"*) ;;
  *) fail "OKT_PRESET=bogus did not warn on stderr: $err" ;;
esac

# --- select_preset returns default when no TTY and no env override ---

got="$(unset OKT_PRESET; select_preset </dev/null 2>/dev/null)"
assert_equal "$got" "omakase" "no TTY + no env → default preset"

# --- resolve_config_dir precedence ---

got="$(OMAKITEN_HOME=/tmp/oh resolve_config_dir)"
assert_equal "$got" "/tmp/oh/config" "OMAKITEN_HOME wins"

got="$(unset OMAKITEN_HOME; XDG_CONFIG_HOME=/tmp/xdg resolve_config_dir)"
assert_equal "$got" "/tmp/xdg/omakiten/config" "XDG_CONFIG_HOME wins over default"

got="$(unset OMAKITEN_HOME; unset XDG_CONFIG_HOME; HOME=/tmp/h resolve_config_dir)"
assert_equal "$got" "/tmp/h/.config/omakiten/config" "default path uses ~/.config/omakiten"

# --- write_active_preset writes .active and creates the dir ---

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
OMAKITEN_HOME="$tmpdir" write_active_preset "kaiseki" >/dev/null
if [ ! -f "$tmpdir/config/.active" ]; then
  fail "write_active_preset did not create .active"
fi
got="$(cat "$tmpdir/config/.active")"
assert_equal "$got" "kaiseki.yaml" ".active content"

# --- SUPPORTED_PRESETS contains every official preset ---

want_count=4
got_count="${#SUPPORTED_PRESETS[@]}"
if [ "$got_count" -ne "$want_count" ]; then
  fail "SUPPORTED_PRESETS has $got_count entries, want $want_count (sync with defaults/config/<preset>.yaml)"
fi

echo "OK: install.sh harness + preset selection helpers behave as expected."
