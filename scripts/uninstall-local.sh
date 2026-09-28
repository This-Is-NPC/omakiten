#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

: "${HOME:?HOME is not set}"
# Use the bootstrap uninstaller when no runnable binary exists.
installed_at="$HOME/.local/bin/okt"
if [ -x "$installed_at" ]; then
  "$installed_at" uninstall --yes
else
  rm -f "$installed_at"
  INSTALL_DIR="$HOME/.local/bin" "$PWD/uninstall.sh"
fi
