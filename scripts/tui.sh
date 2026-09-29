#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

case "${1:-}" in
  "")
    mise run dev:install
    config="$repo_root/dev_env/config.yaml"
    go run ./cmd/okt --config "$config" init --name Omakiten --slug omakiten --root "$repo_root" >/dev/null
    ;;
  --bare)
    config="$repo_root/dev_env/config.yaml"
    if [[ ! -f "$config" || -L "$config" ]]; then
      printf 'dev preset selection is missing or unsafe: %s\n' "$config" >&2
      exit 1
    fi
    ;;
  *) printf 'usage: %s [--bare]\n' "$0" >&2; exit 2 ;;
esac
exec go run ./cmd/okt --config "$config" --project omakiten tui
