#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

case "${1:-}" in
  "")
    mise run dev:install
    config="$repo_root/dev_env/config/omakase.yaml"
    go run ./cmd/okt --config "$config" init --name Omakiten --slug omakiten --root "$repo_root" >/dev/null
    ;;
  --bare)
    active="$(tr -d '\r\n' < dev_env/config/.active)"
    case "$active" in
      ""|"."|".."|*/*|*\\*)
        printf 'dev_env/config/.active is invalid: %s\n' "$active" >&2
        exit 1
        ;;
    esac
    config="$repo_root/dev_env/config/$active"
    if [[ -f "dev_env/config/custom/$active" && ! -L "dev_env/config/custom/$active" ]]; then
      config="$repo_root/dev_env/config/custom/$active"
    elif [[ ! -f "$config" || -L "$config" ]]; then
      printf 'active dev config is missing or unsafe: %s\n' "$config" >&2
      exit 1
    fi
    ;;
  *) printf 'usage: %s [--bare]\n' "$0" >&2; exit 2 ;;
esac
exec go run ./cmd/okt --config "$config" --project omakiten tui
