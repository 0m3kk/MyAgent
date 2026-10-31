#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
fi
VERSION="${VERSION//\//-}"
ARCH="$(uname -m)"

echo "→ releasing MyAgent ${VERSION} (${ARCH})"
"$ROOT/scripts/build-macos.sh"

OUT="$ROOT/dist/release"
mkdir -p "$OUT"
ZIP="$OUT/MyAgent-${VERSION}-macOS-${ARCH}.zip"
rm -f "$ZIP" "$ZIP.sha256"
ditto -c -k --sequesterRsrc --keepParent "$ROOT/dist/MyAgent.app" "$ZIP"
(cd "$OUT" && shasum -a 256 "$(basename "$ZIP")" > "$(basename "$ZIP").sha256")

echo
echo "Release artifacts:"
ls -lh "$ZIP" "$ZIP.sha256"
echo
echo "Note: this bundle is ad-hoc signed. Public distribution requires Developer ID signing and notarization."
