#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
rm -rf "$ROOT/.e2e-data"
mkdir -p "$ROOT/.e2e-data/workspace"
cd "$ROOT"
exec env MYAGENT_DEV=1 MYAGENT_DATA_DIR="$ROOT/.e2e-data" MYAGENT_WORKSPACE="$ROOT/.e2e-data/workspace" \
  go run ./cmd/agentd -addr 127.0.0.1:8787 -open=false -ui "$ROOT/ui/dist"
