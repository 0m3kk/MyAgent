#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA_DIR="${MYAGENT_DATA_DIR:-$ROOT/.data}"
WORKSPACE="${MYAGENT_WORKSPACE:-$DATA_DIR/workspace}"
ADDR="${MYAGENT_ADDR:-127.0.0.1:8787}"
mkdir -p "$DATA_DIR" "$WORKSPACE"
rm -f "$DATA_DIR/api.json"

if [ ! -d "$ROOT/ui/node_modules" ]; then
  echo "→ installing UI dependencies"
  (cd "$ROOT/ui" && pnpm install)
fi

cleanup() {
  if [ -n "${CORE_PID:-}" ]; then kill "$CORE_PID" 2>/dev/null || true; fi
}
trap cleanup EXIT INT TERM

echo "→ building dev core"
(cd "$ROOT" && go build -o "$DATA_DIR/agentd-dev" ./cmd/agentd)

echo "→ starting Go core on $ADDR"
MYAGENT_DEV=1 MYAGENT_DATA_DIR="$DATA_DIR" MYAGENT_WORKSPACE="$WORKSPACE" \
  "$DATA_DIR/agentd-dev" -addr "$ADDR" -open=false -ui "$ROOT/ui/dist" \
  >"$DATA_DIR/server.log" 2>&1 &
CORE_PID=$!

for _ in $(seq 1 80); do
  if [ -f "$DATA_DIR/api.json" ]; then break; fi
  sleep 0.25
done
if [ ! -f "$DATA_DIR/api.json" ]; then
  echo "core failed to start; see $DATA_DIR/server.log" >&2
  tail -30 "$DATA_DIR/server.log" >&2 || true
  exit 1
fi
TOKEN="$(python3 -c "import json;print(json.load(open('$DATA_DIR/api.json'))['token'])")"
echo
echo "  MyAgent dev"
echo "  UI:  http://127.0.0.1:5173/?token=$TOKEN"
echo "  API: http://$ADDR"
echo "  Log: $DATA_DIR/server.log"
echo
(cd "$ROOT/ui" && MYAGENT_API="http://$ADDR" pnpm exec vite --host 127.0.0.1)
