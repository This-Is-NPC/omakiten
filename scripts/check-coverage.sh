#!/usr/bin/env bash
# Enforce aggregate statement coverage with the portable ignored Go checker.
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
exec go run "$repo/scripts/check-coverage.go" "$@"
