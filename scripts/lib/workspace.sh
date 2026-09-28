#!/usr/bin/env bash
# Resolve the workspace without changing the caller's system temporary directory.
repo_root="${MISE_PROJECT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
cd "$repo_root"
