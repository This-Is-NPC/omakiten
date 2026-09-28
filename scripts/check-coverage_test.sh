#!/usr/bin/env bash
# Deterministic focused tests for the portable ignored Go coverage checker.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/check-coverage" "$repo_root/scripts/check-coverage.go"
checker=("$tmp/check-coverage")
source="$tmp/seed.go"
profile="$tmp/coverage.out"
summary="$tmp/coverage.func"
printf 'package seed\n' >"$source"
printf 'package seed\n' >"$tmp/second.go"

run_case() {
  local name=$1 covered=$2 total=$3 summary_ratio=$4 want=$5
  local uncovered=$((total - covered))
  printf 'mode: set\n%s-covered:1.1,1.2 %d 1\n%s-uncovered:2.1,2.2 %d 0\n' "$name" "$covered" "$name" "$uncovered" >"$profile"
  printf 'seed %s\n\ttotal: (statements) %s%%\n' "$name" "$summary_ratio" >"$summary"
  [[ $(sed -n '2p' "$profile") == "$name-covered:1.1,1.2 $covered 1" ]]
  [[ $(sed -n '2p' "$summary") == $'\ttotal: (statements) '"$summary_ratio%" ]]
  touch "$source" "$tmp/second.go" "$profile" "$summary"
  output=$({ "${checker[@]}" "$profile" "$summary" "$tmp"; } 2>&1) && rc=0 || rc=$?
  if [[ $name == exact-boundary ]]; then [[ $output == *"78.000%"* ]]; fi
  [[ $rc -eq $want ]] || { echo "FAIL: $name expected rc=$want got rc=$rc" >&2; exit 1; }
}
run_case below-floor 779 1000 78.0 1
run_case exact-boundary 780 1000 78.0 0
run_case above-floor 781 1000 78.1 0
run_case rounded-below-floor 779 1000 78.0 1

expect_fail() { if "${checker[@]}" "$profile" "$summary" "$tmp" >/dev/null 2>&1; then echo "FAIL: $1 unexpectedly passed" >&2; exit 1; fi; }
printf 'mode: set\nseed-zero:1.1,1.2 0 1\nseed-covered:2.1,2.2 10 1\n' >"$profile"
printf 'seed\ntotal: (statements) 100.0%%\n' >"$summary"; touch "$profile" "$summary"; output=$("${checker[@]}" "$profile" "$summary" "$tmp" 2>&1); [[ $output == *"100.000%"* ]]
printf 'mode: set\nseed-zero:1.1,1.2 0 1\n' >"$profile"; touch "$profile"; expect_fail all-zero-total
printf 'mode: set\nseed:1.1,1.2 1 1\n' >"$profile"; printf 'seed\ntotal: (statements) 100.0%%\n' >"$summary"; touch "$profile" "$summary"
rm "$profile"; expect_fail missing-profile
printf 'mode: set\nseed:1.1,1.2 1 1\n' >"$profile"; rm "$summary"; expect_fail missing-summary
: >"$profile"; printf 'x\n' >"$summary"; expect_fail empty-profile
printf 'mode: set extra\nseed:1.1,1.2 1 1\n' >"$profile"; printf 'seed\ntotal: (statements) 99.0%%\n' >"$summary"; touch "$profile" "$summary"; expect_fail extra-header
printf 'mode: set\nseed:1.1,1.2 garbage 1\n' >"$profile"; expect_fail garbage-range
printf 'mode: set\nseed:1.1,1.2 1 1\n' >"$profile"; : >"$summary"; expect_fail empty-summary
printf 'mode: set\nseed:1.1,1.2 1 1\n' >"$profile"; printf 'seed\nfoo: (statements) 1.0%%\n' >"$summary"; expect_fail missing-total
printf 'seed\ntotal: (statements) 99.0%%\ntotal: (statements) 99.0%%\n' >"$summary"; expect_fail duplicate-total
printf 'seed\ntotal: statements 99.0%%\n' >"$summary"; expect_fail malformed-total
printf 'seed\ntotal: (statements) 99.0%% extra\n' >"$summary"; expect_fail total-extra-field
printf 'mode: set\nseed:2.1,1.2 1 1\n' >"$profile"; printf 'seed\ntotal: (statements) 99.0%%\n' >"$summary"; expect_fail backwards-range
printf 'mode: set\nseed:18446744073709551616.1,18446744073709551616.2 1 1\n' >"$profile"; expect_fail coordinate-overflow

printf 'mode: set\nseed-stale:1.1,1.2 10 1\n' >"$profile"
printf 'seed-stale\n\ttotal: (statements) 100.0%%\n' >"$summary"
[[ $(sed -n '2p' "$profile") == 'seed-stale:1.1,1.2 10 1' ]]
[[ $(sed -n '2p' "$summary") == $'\ttotal: (statements) 100.0%' ]]
touch "$profile" "$summary"; sleep 0.1; touch "$source"; expect_fail subsecond-stale

# Newer generated Go files must not invalidate evidence for the source tree.
touch "$profile" "$summary"
sleep 0.1
for directory in .tmp .opencode _scratch bin build dist dev_env vendor node_modules; do
  mkdir -p "$tmp/$directory"
  printf 'package generated\n' >"$tmp/$directory/generated.go"
done
"${checker[@]}" "$profile" "$summary" "$tmp" >/dev/null

# A newer package source must still invalidate that same evidence.
mkdir -p "$tmp/internal/example"
printf 'package example\n' >"$tmp/internal/example/source.go"
expect_fail nested-source-stale
echo 'OK: coverage checker grammar, freshness, boundary, and failure cases'
