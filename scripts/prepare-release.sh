#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

repo="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}"
version="$(gh api "repos/$repo/contents/.release-please-manifest.json?ref=master" \
  --jq '.content | @base64d | fromjson | .["."]')"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then
  printf 'release: invalid manifest version: %s\n' "$version" >&2
  exit 1
fi
tag="v$version"
commit="$(gh api --paginate "repos/$repo/releases" \
  --jq ".[] | select(.draft and .tag_name == \"$tag\") | .target_commitish")"

output() {
  printf '%s\n' "$@"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    printf '%s\n' "$@" >>"$GITHUB_OUTPUT"
  fi
}

if [[ -z "$commit" ]]; then
  output release_created=false
  exit 0
fi
if [[ ! "$commit" =~ ^[0-9a-f]{40}$ ]]; then
  printf 'release: draft %s must target an exact commit: %s\n' "$tag" "$commit" >&2
  exit 1
fi

count="$(gh api "repos/$repo/git/matching-refs/tags/$tag" \
  --jq "map(select(.ref == \"refs/tags/$tag\")) | length")"
if [[ "$count" == 0 ]]; then
  gh api --method POST "repos/$repo/git/refs" -f "ref=refs/tags/$tag" -f "sha=$commit" >/dev/null
fi
tag_commit="$(gh api "repos/$repo/commits/$tag" --jq .sha)"
if [[ "$tag_commit" != "$commit" ]]; then
  printf 'release: tag %s targets %s; draft targets %s\n' "$tag" "$tag_commit" "$commit" >&2
  exit 1
fi
output release_created=true "tag_name=$tag"
