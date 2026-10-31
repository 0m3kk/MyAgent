#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="$ROOT/dist/MyAgent.app"
CONTENTS="$APP/Contents"
REQUESTED_VERSION="${VERSION:-}"
if [ -z "$REQUESTED_VERSION" ]; then
  REQUESTED_VERSION="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo 1.0.0)"
fi
VERSION="${REQUESTED_VERSION#v}"
BUILD_NUMBER="${BUILD_NUMBER:-}"
if [ -z "$BUILD_NUMBER" ]; then
  # For a clean semver release, derive the build number from the version so
  # re-tagging the same commit still produces a newer Sparkle version.
  # Unreleased local builds keep using the commit count.
  if [[ "$VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    BUILD_NUMBER=$((10#${BASH_REMATCH[1]} * 1000000 + 10#${BASH_REMATCH[2]} * 1000 + 10#${BASH_REMATCH[3]}))
  else
    BUILD_NUMBER="$(git -C "$ROOT" rev-list --count HEAD 2>/dev/null || echo 2)"
  fi
fi

SIGN_ID="${SIGN_ID:-MyAgent Self-Signed}"
if [ "$SIGN_ID" != "-" ] && ! security find-identity -v -p codesigning 2>/dev/null \
  | grep -F "\"$SIGN_ID\"" >/dev/null; then
  echo "→ signing identity \"$SIGN_ID\" not found; using ad-hoc signing" >&2
  echo "  Run: mise run sign-setup" >&2
  SIGN_ID="-"
fi

if [ ! -d "$ROOT/ui/node_modules" ]; then
  echo "→ installing UI dependencies"
  (cd "$ROOT/ui" && pnpm install --frozen-lockfile)
fi

echo "→ building UI"
(cd "$ROOT/ui" && pnpm run build)

echo "→ building Go core"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources/ui" "$ROOT/bin"
(cd "$ROOT" && CGO_ENABLED=1 go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$CONTENTS/Resources/agentd" ./cmd/agentd)
(cd "$ROOT" && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" \
  -o "$ROOT/bin/agentctl" ./cmd/agentctl)

echo "→ copying UI"
rm -rf "$CONTENTS/Resources/ui"
mkdir -p "$CONTENTS/Resources/ui"
cp -R "$ROOT/ui/dist/." "$CONTENTS/Resources/ui/"

echo "→ embedding Sparkle"
SPARKLE_DIR="$("$ROOT/scripts/sparkle.sh")"
SPARKLE_FRAMEWORK="$SPARKLE_DIR/Sparkle.xcframework/macos-arm64_x86_64/Sparkle.framework"
mkdir -p "$CONTENTS/Frameworks"
rm -rf "$CONTENTS/Frameworks/Sparkle.framework"
ditto "$SPARKLE_FRAMEWORK" "$CONTENTS/Frameworks/Sparkle.framework"

echo "→ building native shell"
swiftc -O \
  -F "$CONTENTS/Frameworks" \
  -framework Sparkle \
  -Xlinker -rpath -Xlinker "@executable_path/../Frameworks" \
  -o "$CONTENTS/MacOS/MyAgent" "$ROOT/app/main.swift" "$ROOT/app/Workspaces.swift" \
  -framework Cocoa -framework WebKit

cp "$ROOT/app/Info.plist" "$CONTENTS/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$CONTENTS/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $BUILD_NUMBER" "$CONTENTS/Info.plist"
cp "$ROOT/app/AppIcon.icns" "$CONTENTS/Resources/AppIcon.icns"
cp "$SPARKLE_DIR/LICENSE" "$CONTENTS/Resources/Sparkle-LICENSE.txt"
printf "APPL????" > "$CONTENTS/PkgInfo"

echo "→ signing Sparkle helpers ($SIGN_ID)"
SPARKLE_B="$CONTENTS/Frameworks/Sparkle.framework/Versions/B"
for xpc in "$SPARKLE_B/XPCServices/"*.xpc; do
  codesign --force --sign "$SIGN_ID" --preserve-metadata=entitlements,flags,runtime "$xpc" >/dev/null
done
codesign --force --sign "$SIGN_ID" --preserve-metadata=entitlements,flags,runtime "$SPARKLE_B/Updater.app" >/dev/null
codesign --force --sign "$SIGN_ID" --preserve-metadata=entitlements,flags,runtime "$SPARKLE_B/Autoupdate" >/dev/null
codesign --force --sign "$SIGN_ID" --preserve-metadata=entitlements,flags,runtime "$CONTENTS/Frameworks/Sparkle.framework" >/dev/null

echo "→ signing app ($SIGN_ID)"
codesign --force --sign "$SIGN_ID" "$APP" >/dev/null

echo "→ verifying signature"
codesign --verify --deep --strict "$APP" >/dev/null
codesign -d -r- "$APP" 2>&1 | sed 's/^/  /'

# Nudge LaunchServices so Finder/Dock pick up a newly generated icon even when
# the app bundle path is reused across local builds.
touch "$APP"
if [ -x /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister ]; then
  /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$APP" >/dev/null 2>&1 || true
fi

echo
echo "Built $APP"
echo "Open it with: open \"$APP\""
