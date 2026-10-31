#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SPARKLE_DIR="$("$ROOT/scripts/sparkle.sh")"
ACCOUNT="${SPARKLE_ACCOUNT:-com.myagent.desktop}"

case "${1:-}" in
  public)
    "$SPARKLE_DIR/bin/generate_keys" --account "$ACCOUNT" -p
    ;;
  export)
    OUT="${2:-$HOME/.myagent-sparkle-private-key}"
    umask 077
    "$SPARKLE_DIR/bin/generate_keys" --account "$ACCOUNT" -x "$OUT"
    chmod 600 "$OUT"
    echo "Exported the Sparkle private key to $OUT"
    echo "Add its contents to the GitHub Actions secret SPARKLE_PRIVATE_KEY, then delete the file."
    ;;
  *)
    echo "Usage: $0 public | export [path]" >&2
    exit 1
    ;;
esac
