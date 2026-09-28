#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

scripts=(install.sh uninstall.sh)
while IFS= read -r -d '' path; do
  scripts+=("$path")
done < <(find scripts -type f \( -name '*.sh' -o -path 'scripts/hooks/*' \) -print0)
for path in "${scripts[@]}"; do
  bash -n "$path"
done
exec shellcheck --external-sources --source-path=SCRIPTDIR "${scripts[@]}"
