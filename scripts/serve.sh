#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

config="$repo_root/dev_env/config.yaml"
if [[ ! -f "$config" || -L "$config" ]]; then
  printf 'dev preset selection is missing or unsafe: %s; run mise run tui once\n' "$config" >&2
  exit 1
fi
exec go run ./cmd/okt --config "$config" --project omakiten serve "$@"
