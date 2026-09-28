#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

if [[ $# -gt 0 ]]; then
  exec go run "$repo_root/scripts/new-language-pack.go" "$@"
fi
exec go run "$repo_root/scripts/new-language-pack.go" \
  "${usage_code:?missing code}" "${usage_native:?missing native name}" "${usage_name:?missing English name}"
