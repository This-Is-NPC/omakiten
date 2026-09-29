#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"

"$PWD/.tmp/build/okt" setup --update --skip-wrapper --skip-harnesses
