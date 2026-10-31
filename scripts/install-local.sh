#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE="$ROOT/dist/MyAgent.app"
DEST="${1:-/Applications/MyAgent.app}"

if [ ! -d "$SOURCE" ]; then
  echo "error: $SOURCE not found. Run: mise run app" >&2
  exit 1
fi

if pgrep -x MyAgent >/dev/null; then
  echo "→ quitting the running MyAgent"
  osascript -e 'tell application id "com.myagent.desktop" to quit' >/dev/null 2>&1 \
    || pkill -x MyAgent || true
  sleep 1
fi

echo "→ installing to $DEST"
rm -rf "$DEST"
ditto "$SOURCE" "$DEST"

# Local builds are copied from dist/, so they normally have no quarantine bit.
# Remove it defensively so the first launch does not hit Gatekeeper.
xattr -dr com.apple.quarantine "$DEST" 2>/dev/null || true

open "$DEST"
echo "✓ Installed and launched $DEST"
