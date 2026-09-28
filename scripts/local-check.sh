#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

[[ "${OKT_SKIP_LOCAL_CHECK:-0}" != 1 ]] || exit 0
dry_run="${usage_dry_run:-false}"
pre_push=false
sha="${usage_sha:-${OKT_LOCAL_CHECK_SHA:-HEAD}}"
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=true ;;
    --pre-push) pre_push=true ;;
    --sha=*) sha="${arg#--sha=}" ;;
    *) printf 'local-check: unknown argument: %s\n' "$arg" >&2; exit 2 ;;
  esac
done
sha="$(git rev-parse --verify "${sha}^{commit}")"
verify_checkout() {
  if [[ "$(git rev-parse HEAD)" != "$sha" || -n "$(git status --porcelain)" ]]; then
    printf 'local-check: the checked commit must be HEAD in a clean checkout.\n' >&2
    return 1
  fi
}
verify_checkout

start="$(date +%s)"
rc=0
if [[ "$dry_run" == true ]]; then
  printf '[dry-run] would run: mise run check for %s\n' "$sha"
else
  mise run check || rc=$?
  verify_checkout || exit 1
fi
elapsed=$(( $(date +%s) - start ))
state=success
description="mise run check passed in ${elapsed}s"
if (( rc != 0 )); then
  state=failure
  description="mise run check failed (rc=$rc) after ${elapsed}s"
fi

if [[ "$pre_push" == true ]] && (( rc != 0 )); then
  printf 'local-check: %s; aborting push.\n' "$description" >&2
  exit "$rc"
fi
if ! slug="$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null)"; then
  printf 'local-check: checks completed; GitHub authentication or repository lookup failed.\n' >&2
  [[ "$pre_push" == true ]] && exit "$rc"
  exit 1
fi

export OKT_CHECK_REPO="$slug" OKT_CHECK_SHA="$sha" OKT_CHECK_STATE="$state"
export OKT_CHECK_DESCRIPTION="$description" OKT_CHECK_WAIT="$pre_push" OKT_CHECK_DRY_RUN="$dry_run"
if [[ "$pre_push" == true && "$dry_run" != true ]]; then
  mkdir -p .tmp/local-check
  nohup "$repo_root/scripts/post-check-status.sh" \
    </dev/null >".tmp/local-check/$sha.log" 2>&1 &
  disown 2>/dev/null || true
else
  scripts/post-check-status.sh
fi
exit "$rc"
