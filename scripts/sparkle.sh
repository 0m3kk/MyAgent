#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="2.10.1"
SHA256="4c2e48a3e50ebcc2c88a58a4c7264a1a99c3c31857ce754357afb9cdb323022a"
DEST="$ROOT/.cache/sparkle/$VERSION"
FRAMEWORK="$DEST/Sparkle.xcframework/macos-arm64_x86_64/Sparkle.framework"

if [ -d "$FRAMEWORK" ] && [ -x "$DEST/bin/generate_appcast" ]; then
  printf '%s\n' "$DEST"
  exit 0
fi

echo "→ fetching Sparkle $VERSION" >&2
rm -rf "$DEST"
mkdir -p "$DEST"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/myagent-sparkle.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "https://github.com/sparkle-project/Sparkle/releases/download/$VERSION/Sparkle-for-Swift-Package-Manager.zip" \
  -o "$TMP/sparkle.zip"
printf '%s  %s\n' "$SHA256" "$TMP/sparkle.zip" | shasum -a 256 -c - >/dev/null
unzip -q "$TMP/sparkle.zip" -d "$DEST"

printf '%s\n' "$DEST"
