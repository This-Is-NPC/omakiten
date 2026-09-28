#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

if [ -z "${HOME:-}" ]; then
  printf 'HOME is not set\n' >&2
  exit 1
fi
# Use the bootstrap uninstaller when no runnable binary exists.
installed_at="$HOME/.local/bin/okt"
if [ -x "$installed_at" ]; then
  "$installed_at" uninstall --yes
else
  rm -f "$installed_at"
  INSTALL_DIR="$HOME/.local/bin" "$PWD/uninstall.sh"
fi
