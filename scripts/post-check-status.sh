#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

: "${OKT_CHECK_REPO:?missing repository}" "${OKT_CHECK_SHA:?missing SHA}"
: "${OKT_CHECK_STATE:?missing state}" "${OKT_CHECK_DESCRIPTION:?missing description}"
if [[ "${OKT_CHECK_DRY_RUN:-false}" == true ]]; then
  printf '[dry-run] POST /repos/%s/statuses/%s state=%s wait=%s description=%s\n' \
    "$OKT_CHECK_REPO" "$OKT_CHECK_SHA" "$OKT_CHECK_STATE" "${OKT_CHECK_WAIT:-false}" "$OKT_CHECK_DESCRIPTION"
  exit 0
fi
if [[ "${OKT_CHECK_WAIT:-false}" == true ]]; then
  reachable=false
  for ((attempt = 0; attempt < 60; attempt++)); do
    if gh api -X GET "repos/$OKT_CHECK_REPO/commits/$OKT_CHECK_SHA" --silent >/dev/null 2>&1; then
      reachable=true
      break
    fi
    sleep 1
  done
  if [[ "$reachable" != true ]]; then
    printf 'local-check: commit was not reachable within 60 seconds.\n' >&2
    exit 1
  fi
fi
exec gh api --silent -X POST "repos/$OKT_CHECK_REPO/statuses/$OKT_CHECK_SHA" \
  -f "state=$OKT_CHECK_STATE" -f 'context=local-check' -f "description=$OKT_CHECK_DESCRIPTION"
