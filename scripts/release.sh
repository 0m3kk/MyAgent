#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi
VERSION="${VERSION//\//-}"
export VERSION
ARCH="$(uname -m)"
SIGN_ID="${SIGN_ID:-MyAgent Self-Signed}"
export SIGN_ID

echo "→ releasing MyAgent ${VERSION} (${ARCH})"
"$ROOT/scripts/build-macos.sh"

OUT="$ROOT/dist/release"
mkdir -p "$OUT"
ZIP="$OUT/MyAgent-${VERSION}-macOS-${ARCH}.zip"
rm -f "$ZIP" "$ZIP.sha256"
ditto -c -k --sequesterRsrc --keepParent "$ROOT/dist/MyAgent.app" "$ZIP"
(cd "$OUT" && shasum -a 256 "$(basename "$ZIP")" > "$(basename "$ZIP").sha256")

echo "→ generating Sparkle appcast"
SPARKLE_DIR="$("$ROOT/scripts/sparkle.sh")"
SPARKLE_ACCOUNT="${SPARKLE_ACCOUNT:-com.myagent.desktop}"
STAGE="$(mktemp -d "$OUT/.appcast.XXXXXX")"
trap 'rm -rf "$STAGE"' EXIT
cp "$ZIP" "$STAGE/"
rm -f "$OUT/appcast.xml"

if [ -n "${CI:-}" ] && [ -z "${SPARKLE_PRIVATE_KEY:-}" ]; then
  echo "SPARKLE_PRIVATE_KEY is required to sign updates in CI." >&2
  exit 1
fi
if [ -n "${SPARKLE_PRIVATE_KEY:-}" ]; then
  printf '%s\n' "$SPARKLE_PRIVATE_KEY" | \
    "$SPARKLE_DIR/bin/generate_appcast" --ed-key-file - -o "$OUT/appcast.xml" "$STAGE"
else
  "$SPARKLE_DIR/bin/generate_appcast" --account "$SPARKLE_ACCOUNT" -o "$OUT/appcast.xml" "$STAGE"
fi
rm -rf "$STAGE"
trap - EXIT

echo
echo "Release artifacts:"
ls -lh "$ZIP" "$ZIP.sha256" "$OUT/appcast.xml"
echo
if codesign -d -r- "$ROOT/dist/MyAgent.app" 2>&1 | grep -q "cdhash"; then
  echo "Note: this bundle is ad-hoc signed. Auto-update works without an Apple Developer account,"
  echo "but macOS may ask for permissions again after each update. Run 'mise run sign-setup'"
  echo "once to use a stable self-signed identity, or use a Developer ID + notarization."
else
  echo "Note: this bundle is signed with the local self-signed identity \"$SIGN_ID\"."
  echo "First install may still need Open Anyway; Sparkle updates keep the same identity."
fi
