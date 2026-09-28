#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

if [ -z "${HOME:-}" ]; then
  printf 'HOME is not set\n' >&2
  exit 1
fi
.tmp/build/okt mcp setup --harness "${1:?missing harness}" --force
