#!/usr/bin/env bash
# local-check.sh — run `mise run check` and post a Commit Statuses API
# call (`context=local-check`) to a given SHA so `master` branch
# protection can gate merges on it.
#
# Usage:
#   scripts/local-check.sh                 # check + post for HEAD (SHA must be on remote)
#   scripts/local-check.sh --pre-push      # check, then BACKGROUND-post once
#                                          # SHA is reachable on origin (used by
#                                          # the pre-push hook because the SHA
#                                          # isn't uploaded yet when the hook
#                                          # fires).
#   scripts/local-check.sh --post-only --state=success [--sha=<sha>]
#                                          # skip check, just post.
#   scripts/local-check.sh --dry-run [...] # print API calls, do not POST.
#   scripts/local-check.sh --sha=<sha>     # override SHA (else HEAD).
#
# Env:
#   OKT_SKIP_LOCAL_CHECK=1   no-op (bot pushes, intentional skip)
#   OKT_LOCAL_CHECK_SHA=<s>  same as --sha=

set -euo pipefail

if [[ "${OKT_SKIP_LOCAL_CHECK:-0}" == 1 ]]; then
  exit 0
fi

DRY_RUN=0
PRE_PUSH=0
POST_ONLY=0
STATE=""
SHA="${OKT_LOCAL_CHECK_SHA:-}"
for arg in "$@"; do
  case "$arg" in
    --dry-run)   DRY_RUN=1 ;;
    --pre-push)  PRE_PUSH=1 ;;
    --post-only) POST_ONLY=1 ;;
    --state=*)   STATE="${arg#--state=}" ;;
    --sha=*)     SHA="${arg#--sha=}" ;;
    -h|--help)
      sed -n '2,22p' "$0"
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

resolve_repo_slug() {
  if ! REPO_SLUG="$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null)"; then
    return 1
  fi
  printf '%s' "$REPO_SLUG"
}

post_status_now() {
  local state="$1" desc="$2"
  if (( DRY_RUN )); then
    printf '[dry-run] POST /repos/%s/statuses/%s state=%s context=%s desc=%q\n' \
      "$REPO_SLUG" "$SHA" "$state" "$CONTEXT" "$desc"
    return 0
  fi
  gh api --silent -X POST \
    "repos/$REPO_SLUG/statuses/$SHA" \
    -f "state=$state" \
    -f "context=$CONTEXT" \
    -f "description=$desc"
}

post_status_when_reachable() {
  local state="$1" desc="$2"
  if (( DRY_RUN )); then
    printf '[dry-run] background-poll until SHA reachable, then POST /repos/%s/statuses/%s state=%s\n' \
      "$REPO_SLUG" "$SHA" "$state"
    return 0
  fi
  setsid nohup bash -c '
    slug="$1"; sha="$2"; state="$3"; ctx="$4"; desc="$5"
    for _ in $(seq 1 60); do
      if gh api -X GET "repos/$slug/commits/$sha" --silent >/dev/null 2>&1; then
        gh api --silent -X POST "repos/$slug/statuses/$sha" \
          -f "state=$state" -f "context=$ctx" -f "description=$desc"
        exit 0
      fi
      sleep 1
    done
    exit 0
  ' bash "$REPO_SLUG" "$SHA" "$state" "$CONTEXT" "$desc" \
    </dev/null >/dev/null 2>&1 &
  disown 2>/dev/null || true
}

if ! REPO_SLUG="$(resolve_repo_slug)"; then
  if (( PRE_PUSH )); then
    printf 'local-check: gh not authenticated — check ran but status will not be posted.\n' >&2
    exit 0
  fi
  printf 'local-check: gh CLI not authenticated or no repo context\n' >&2
  exit 1
fi

if (( POST_ONLY )); then
  if [[ -z "$STATE" ]]; then
    printf 'local-check: --post-only requires --state=...\n' >&2
    exit 2
  fi
  post_status_now "$STATE" "mise run check — ${STATE} (post-only)"
  exit 0
fi

start="$(date +%s)"
rc=0
if (( DRY_RUN )); then
  printf '[dry-run] would run: mise run check\n'
else
  mise run check || rc=$?
fi
elapsed=$(( $(date +%s) - start ))

if (( rc == 0 )); then
  state=success
  desc="mise run check — passed in ${elapsed}s"
else
  state=failure
  desc="mise run check — failed (rc=$rc) after ${elapsed}s"
fi

if (( PRE_PUSH )); then
  if (( rc != 0 )); then
    printf 'local-check: %s — aborting push.\n' "$desc" >&2
    exit "$rc"
  fi
  post_status_when_reachable "$state" "$desc"
  exit 0
fi

post_status_now pending "mise run check — running locally"
post_status_now "$state" "$desc"
if (( rc != 0 )); then
  exit "$rc"
fi
