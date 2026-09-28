#!/usr/bin/env bash
# Enforce aggregate statement coverage with the portable ignored Go checker.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"
exec go run "$repo_root/scripts/check-coverage.go" "$@"
