#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

mkdir -p .tmp/build
version="$(git describe --tags --always --dirty 2>/dev/null || printf dev)"
exec go build -ldflags "-X main.version=${version}" -o .tmp/build/okt ./cmd/okt
