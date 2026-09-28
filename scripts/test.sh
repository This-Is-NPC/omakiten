#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

mkdir -p .tmp/coverage
profile=.tmp/coverage/coverage.out
summary=.tmp/coverage/coverage.func
rm -f "$profile" "$summary"
scripts/check-coverage_test.sh
go test -count=1 -coverpkg=./... -coverprofile="$profile" ./...
go tool cover -func="$profile" > "$summary"
scripts/check-coverage.sh "$profile" "$summary" .
go test -race -count=1 -run 'Concurrent|Concurrency|^TestBus' ./internal/...
