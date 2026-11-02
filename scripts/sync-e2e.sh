#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

out=.tmp/sync-e2e
export COMPOSE_FILE=scripts/sync-e2e/compose.yaml COMPOSE_PROJECT_NAME=okt-sync-e2e

teardown() { docker compose down -v --remove-orphans >/dev/null 2>&1 || true; }
trap teardown EXIT
teardown

mkdir -p "$out/bin"
case "$(go env GOARCH)" in
  amd64 | arm64) ;;
  *) echo "unsupported host architecture: $(go env GOARCH)" >&2; exit 1 ;;
esac
for cmd in okt okt-server; do
  CGO_ENABLED=0 GOOS=linux go build -trimpath -o "$out/bin/$cmd" "./cmd/$cmd"
done

OKT_E2E_TOKEN="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
export OKT_E2E_TOKEN
(umask 077 && printf '%s\n' "$OKT_E2E_TOKEN" >"$out/token")

docker compose up -d --build --wait
status=0
scripts/sync-e2e/scenario.sh || status=$?
exit "$status"
