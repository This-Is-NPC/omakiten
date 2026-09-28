#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

# `tui:bare` is the preservation path. The regular dev install represents a
# fresh install, so legacy custom entities must not leak across schema changes.
for sub in config skills laws personas templates themes notifications languages; do
  rm -rf "$PWD/dev_env/$sub/custom"
done
"$PWD/.tmp/build/okt" setup --update --skip-wrapper --skip-harnesses
