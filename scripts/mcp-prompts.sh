#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"
go run ./cmd/okt --config dev_env/config/omakase.yaml init --name Omakiten --slug omakiten >/dev/null
exec go run ./cmd/okt --config dev_env/config/omakase.yaml --project omakiten mcp prompts
