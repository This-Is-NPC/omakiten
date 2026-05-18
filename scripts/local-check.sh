#!/usr/bin/env bash
# local-check.sh — run `mise run check` and post a Commit Statuses API
# call to the current HEAD SHA so `master` branch protection can gate
# merges on it.
#
# Usage:
#   scripts/local-check.sh                 # run check, post status
#   scripts/local-check.sh --dry-run       # print payload, do not POST
#   scripts/local-check.sh --sha=<sha>     # override SHA (else HEAD)
#   OKT_SKIP_LOCAL_CHECK=1 …               # no-op (used by hooks/bots)
#   OKT_LOCAL_CHECK_SHA=<sha> …            # same as --sha=

set -euo pipefail

if [[ "${OKT_SKIP_LOCAL_CHECK:-0}" == 1 ]]; then
  exit 0
fi

DRY_RUN=0
SHA="${OKT_LOCAL_CHECK_SHA:-}"
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --sha=*) SHA="${arg#--sha=}" ;;
    -h|--help)
      sed -n '2,12p' "$0"
      exit 0
      ;;
    *)
      printf 'local-check: unknown arg: %s\n' "$arg" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$SHA" ]]; then
  SHA="$(git rev-parse HEAD)"
fi

CONTEXT="local-check"

if ! REPO_SLUG="$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null)"; then
  printf 'local-check: gh CLI not authenticated or no repo context\n' >&2
  exit 1
fi

post_status() {
  local state="$1" desc="$2"
  if (( DRY_RUN )); then
    printf '[dry-run] POST /repos/%s/statuses/%s state=%s context=%s desc=%q\n' \
      "$REPO_SLUG" "$SHA" "$state" "$CONTEXT" "$desc"
    return 0
  fi
  gh api \
    --silent \
    -X POST \
    "repos/$REPO_SLUG/statuses/$SHA" \
    -f "state=$state" \
    -f "context=$CONTEXT" \
    -f "description=$desc"
}

post_status pending "mise run check — running locally"

start="$(date +%s)"
rc=0
if (( DRY_RUN )); then
  printf '[dry-run] would run: mise run check\n'
else
  mise run check || rc=$?
fi
elapsed=$(( $(date +%s) - start ))

if (( rc == 0 )); then
  post_status success "mise run check — passed in ${elapsed}s"
else
  post_status failure "mise run check — failed (rc=$rc) after ${elapsed}s"
  exit "$rc"
fi
