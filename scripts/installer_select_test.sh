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
  /^harness_is_supported\(\) \{/                   { in_fn = 1 }
  /^parse_harness_selection\(\) \{/                { in_fn = 1 }
  /^select_harnesses\(\) \{/                       { in_fn = 1 }
  /^run_harness_setup\(\) \{/                      { in_fn = 1 }
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

# --- parse_harness_selection ---

got="$(parse_harness_selection "1,3,5" 2>/dev/null)"
assert_equal "$got" $'claude-code\nopencode\ngithub-copilot' "numeric comma list"

got="$(parse_harness_selection "1 3 5" 2>/dev/null)"
assert_equal "$got" $'claude-code\nopencode\ngithub-copilot' "numeric space list"

got="$(parse_harness_selection "claude-code,codex" 2>/dev/null)"
assert_equal "$got" $'claude-code\ncodex' "name comma list"

got="$(parse_harness_selection "1 codex" 2>/dev/null)"
assert_equal "$got" $'claude-code\ncodex' "mixed numeric and name"

got="$(parse_harness_selection "" 2>/dev/null)"
assert_equal "$got" "" "empty input emits nothing"

got="$(parse_harness_selection "99" 2>/dev/null)"
assert_equal "$got" "" "out-of-range index emits no harness"
err="$(parse_harness_selection "99" 2>&1 >/dev/null)"
case "$err" in
  *"out of range"*) ;;
  *) fail "out-of-range index did not warn on stderr: $err" ;;
esac

got="$(parse_harness_selection "bogus" 2>/dev/null)"
assert_equal "$got" "" "unknown name emits no harness"
err="$(parse_harness_selection "bogus" 2>&1 >/dev/null)"
case "$err" in
  *"ignoring unknown harness"*) ;;
  *) fail "unknown harness did not warn on stderr: $err" ;;
esac

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

echo "OK: install.sh harness selection helpers behave as expected."
