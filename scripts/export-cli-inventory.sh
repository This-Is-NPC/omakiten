#!/usr/bin/env bash
set -euo pipefail

mkdir -p .tmp/knowledge
.tmp/build/okt knowledge export-cli > .tmp/knowledge/cli.json.tmp
mv .tmp/knowledge/cli.json.tmp .tmp/knowledge/cli.json
