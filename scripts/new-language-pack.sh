#!/usr/bin/env bash
# new-language-pack.sh — scaffold a new bundled language pack from en.yaml.
#
# Usage: scripts/new-language-pack.sh <code> <native> <name>
#
#   <code>    BCP-47-style lowercase subset, hyphen-separated (e.g. it, pt-br, zh-tw).
#   <native>  Endonym used in the picker row label (e.g. "Italiano", "हिन्दी").
#   <name>    English name (e.g. "Italian", "Hindi").
#
# Writes defaults/languages/<code>.yaml as a verbatim copy of en.yaml with:
#   - code/name/native header rewritten,
#   - a `# TODO(translate): <key>` line comment prepended to every translated value.
#
# The English value is preserved as fallback so TestBundledLanguagePacksHaveIdenticalKeySets
# stays green on the very first commit. Contributors translate values incrementally,
# removing each `# TODO(translate)` marker as they go. The guide is at
# .docs/languages-guide.md.
#
# Exit codes:
#   0 — pack written.
#   1 — usage error or destination already exists.
#   2 — en.yaml baseline missing (run from repo root).

set -euo pipefail

if [[ $# -ne 3 ]]; then
  printf 'usage: %s <code> <native> <name>\n' "$0" >&2
  printf 'example: %s it Italiano Italian\n' "$0" >&2
  exit 1
fi

code=$1
native=$2
name=$3

if [[ ! "$code" =~ ^[a-z]+(-[a-z0-9]+)?$ ]]; then
  printf 'error: code %q must be lowercase BCP-47 subset (letters with optional -suffix)\n' "$code" >&2
  exit 1
fi

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
src="$repo_root/defaults/languages/en.yaml"
dst="$repo_root/defaults/languages/$code.yaml"

if [[ ! -f "$src" ]]; then
  printf 'error: baseline %s not found (run from the repo)\n' "$src" >&2
  exit 2
fi

if [[ -e "$dst" ]]; then
  printf 'error: %s already exists; refusing to overwrite\n' "$dst" >&2
  exit 1
fi

awk -v code="$code" -v native="$native" -v name="$name" '
  NR == 1 && /^code:/   { printf "code: %s\n",   code;   next }
  NR == 2 && /^name:/   { printf "name: %s\n",   name;   next }
  NR == 3 && /^native:/ { printf "native: %s\n", native; next }
  /^keys:/              { print; in_keys = 1; next }
  in_keys && match($0, /^  ([A-Za-z0-9_.-]+):/, m) {
    indent = "  "
    printf "%s# TODO(translate): %s\n%s\n", indent, m[1], $0
    next
  }
  { print }
' "$src" > "$dst"

printf 'wrote %s\n' "$dst"
printf 'next: translate values, drop each `# TODO(translate)` line as you go.\n'
printf 'verify: mise run check  (or: go test ./internal/config -run TestBundledLanguagePacksHaveIdenticalKeySets)\n'
