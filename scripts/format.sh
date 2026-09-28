#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

case "${1:-}" in
  "") exec gofmt -w internal cmd defaults rules scripts ;;
  --check)
    unformatted="$(gofmt -l internal cmd defaults rules scripts)"
    if [[ -n "$unformatted" ]]; then
      printf 'FAIL: these files are not gofmt-clean:\n%s\nRun mise run fmt to fix them.\n' "$unformatted" >&2
      exit 1
    fi
    ;;
  *) printf 'usage: %s [--check]\n' "$0" >&2; exit 2 ;;
esac
