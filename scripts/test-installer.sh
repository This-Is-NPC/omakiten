#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib/workspace.sh"
exec go test ./internal/cli ./internal/installer ./internal/installscript \
  -run 'TestCLISetupHeadless|Test.*Wrapper|TestInstaller|TestRealCosign|TestLocalCheck'
