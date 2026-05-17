#!/usr/bin/env bash
# Smoke-tests `okt setup` headless env-var path. The interactive picker
# is exercised by internal/cli/setup_picker_test.go (model-level Update);
# this script covers the cobra wiring that turns OKT_* / --flag values
# into the JSON envelope downstream tooling depends on.
#
# Builds okt once into a tmpdir, then invokes `okt setup` with various
# env-var combinations under `--skip-wrapper --skip-harnesses` so the
# process never mutates the host's rc files or runs `okt mcp setup`.
#
# Run with: bash scripts/installer_select_test.sh
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

tmproot="$(mktemp -d)"
trap 'rm -rf "$tmproot"' EXIT

bin="$tmproot/okt"
( cd "$repo" && go build -o "$bin" ./cmd/okt )

fail() { echo "FAIL: $1" >&2; exit 1; }

# run_setup invokes `okt setup --skip-wrapper --skip-harnesses` with the
# caller's env, pinning OMAKITEN_HOME + HOME under a per-call tmpdir so
# each assertion lands in an isolated config root.
run_setup() {
  local home; home="$(mktemp -d "$tmproot/case.XXXXXX")"
  HOME="$home" OMAKITEN_HOME="$home/oh" XDG_CONFIG_HOME="" \
    "$bin" setup --skip-wrapper --skip-harnesses "$@" 2>"$home/stderr"
}

json_get() {
  # Tiny jq-free key extractor. Echoes the JSON value (numbers, strings,
  # bare arrays) for the dotted path under `.data`. Good enough for the
  # smoke shapes this test asserts on.
  local path="$1" json="$2"
  python3 -c "import json,sys; d=json.loads(sys.argv[1]).get('data',{}); cur=d
for p in sys.argv[2].split('.'):
    cur = cur.get(p) if isinstance(cur, dict) else None
print(json.dumps(cur))" "$json" "$path"
}

assert_envelope_ok() {
  local label="$1" json="$2"
  local ok
  ok="$(python3 -c "import json,sys; print(json.loads(sys.argv[1]).get('ok'))" "$json")"
  if [ "$ok" != "True" ]; then
    printf 'FAIL: %s — envelope.ok = %s\n  json: %s\n' "$label" "$ok" "$json" >&2
    exit 1
  fi
}

assert_equal() {
  local got="$1" want="$2" label="$3"
  if [ "$got" != "$want" ]; then
    printf 'FAIL: %s\n  got:  %s\n  want: %s\n' "$label" "$got" "$want" >&2
    exit 1
  fi
}

# --- env-var driven harness selection ---

out="$(OKT_CLI_LANG=en OKT_TUI_LANG=en OKT_AGENT_LANG=en OKT_PRESET=omakase \
       OKT_HARNESSES="claude-code,opencode" run_setup)"
assert_envelope_ok "harnesses csv (names)" "$out"
assert_equal "$(json_get harnesses_planned "$out")" '["claude-code", "opencode"]' "harnesses_planned echoes selection"

out="$(OKT_CLI_LANG=en OKT_TUI_LANG=en OKT_AGENT_LANG=en OKT_PRESET=omakase \
       OKT_HARNESSES="1,3" run_setup)"
assert_envelope_ok "harnesses csv (indexes)" "$out"
assert_equal "$(json_get harnesses_planned "$out")" '["claude-code", "opencode"]' "indexes resolve to canonical names"

out="$(OKT_CLI_LANG=en OKT_TUI_LANG=en OKT_AGENT_LANG=en OKT_PRESET=omakase \
       OKT_HARNESSES="0" run_setup)"
assert_envelope_ok "harnesses skip sentinel" "$out"
assert_equal "$(json_get harnesses_planned "$out")" 'null' "skip sentinel clears harness list"

# --- preset resolution ---

out="$(OKT_CLI_LANG=en OKT_TUI_LANG=en OKT_AGENT_LANG=en OKT_PRESET=izakaya OKT_HARNESSES=0 run_setup)"
assert_envelope_ok "preset izakaya" "$out"
got="$(json_get preset.name "$out")"
assert_equal "$got" '"izakaya"' "OKT_PRESET=izakaya wins"

# Unknown preset falls back to omakase with a stderr warn line (the
# warning lands in the per-call stderr file inside the case tmpdir; we
# only assert on the envelope here — install.sh's bash contract was the
# same: warn, fall back, exit 0).
out="$(OKT_CLI_LANG=en OKT_TUI_LANG=en OKT_AGENT_LANG=en OKT_PRESET=bogus OKT_HARNESSES=0 run_setup)"
assert_envelope_ok "preset fallback" "$out"
assert_equal "$(json_get preset.name "$out")" '"omakase"' "unknown OKT_PRESET falls back to omakase"

# --- language resolution ---

out="$(OKT_CLI_LANG=pt-br OKT_TUI_LANG=pt-br OKT_AGENT_LANG="Português (Brasil)" \
       OKT_PRESET=omakase OKT_HARNESSES=0 run_setup)"
assert_envelope_ok "pt-br languages" "$out"
assert_equal "$(json_get languages.cli "$out")" '"pt-br"' "languages.cli = pt-br"
assert_equal "$(json_get languages.tui "$out")" '"pt-br"' "languages.tui = pt-br"

# CLI supplied, TUI omitted → TUI mirrors CLI (headless contract).
out="$(unset OKT_TUI_LANG; OKT_CLI_LANG=en OKT_AGENT_LANG=en OKT_PRESET=omakase OKT_HARNESSES=0 run_setup)"
assert_envelope_ok "TUI mirrors CLI when only OKT_CLI_LANG set" "$out"
assert_equal "$(json_get languages.tui "$out")" '"en"' "TUI defaults to CLI choice"

echo "OK: okt setup headless env-var path behaves as expected."
