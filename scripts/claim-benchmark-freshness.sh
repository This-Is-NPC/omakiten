#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"
exec go test ./internal/mcp -run '^TestClaimNextCeilingReferenceFreshness$' -count=1
