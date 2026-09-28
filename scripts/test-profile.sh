#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

package="${usage_package:?missing package}"
name="$(go list -f '{{.ImportPath}}' "$package")"
if [[ "$name" == *$'\n'* ]]; then
  printf 'Profile exactly one package per invocation.\n' >&2
  exit 2
fi
output="$repo_root/.tmp/profiles/$name"
mkdir -p "$output"
exec go test "$package" -run '^$' -bench "${usage_bench:-.}" \
  -o "$output/package.test" -outputdir "$output" \
  -cpuprofile cpu.pprof -memprofile memory.pprof
