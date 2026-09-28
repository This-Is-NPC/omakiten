#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"
exec go run ./cmd/okt-docs-refresh --root . "$@"
