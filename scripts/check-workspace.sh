#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

artifacts="$(find . \
  -type d \( -name .git -o -name .tmp -o -name dev_env -o -name testdata \
    -o -name .agents -o -name .codex -o -name .claude -o -name .opencode -o -name .cursor \) -prune \
  -o -type f \( -name '*.test' -o -name 'coverage*.out' -o -name 'coverage.html' \
    -o -name 'cov_*.out' -o -name '*.prof' -o -name '*.pprof' -o -name '*.trace' \
    -o -name '*.db' -o -name '*.db-shm' -o -name '*.db-wal' -o -name '*.tmp' \
    -o -name '*.pyc' -o -name '*.pyo' \) -print)"
for path in okt bin dist build tmp .temp; do
  if [[ -e "$path" ]]; then
    artifacts+=$'\n'"./$path"
  fi
done
if [[ -n "$artifacts" ]]; then
  printf 'Generated files belong under .tmp/:\n%s\n' "$artifacts" >&2
  exit 1
fi
