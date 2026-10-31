#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="$ROOT/dist/MyAgent.app"
CONTENTS="$APP/Contents"
VERSION="${VERSION:-1.0.0}"
VERSION="${VERSION#v}"
BUILD_NUMBER="${BUILD_NUMBER:-$(git -C "$ROOT" rev-list --count HEAD 2>/dev/null || echo 2)}"

if [ ! -d "$ROOT/ui/node_modules" ]; then
  echo "→ installing UI dependencies"
  (cd "$ROOT/ui" && pnpm install --frozen-lockfile)
fi

echo "→ building UI"
(cd "$ROOT/ui" && pnpm run build)

echo "→ building Go core"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources/ui" "$ROOT/bin"
(cd "$ROOT" && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$CONTENTS/Resources/agentd" ./cmd/agentd)
(cd "$ROOT" && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$ROOT/bin/agentctl" ./cmd/agentctl)

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
  -o "$CONTENTS/MacOS/MyAgent" "$ROOT/app/main.swift" \
  -framework Cocoa -framework WebKit

cp "$ROOT/app/Info.plist" "$CONTENTS/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$CONTENTS/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $BUILD_NUMBER" "$CONTENTS/Info.plist"
cp "$ROOT/app/AppIcon.icns" "$CONTENTS/Resources/AppIcon.icns"
cp "$SPARKLE_DIR/LICENSE" "$CONTENTS/Resources/Sparkle-LICENSE.txt"
printf "APPL????" > "$CONTENTS/PkgInfo"

echo "→ signing Sparkle helpers (ad-hoc)"
SPARKLE_B="$CONTENTS/Frameworks/Sparkle.framework/Versions/B"
for xpc in "$SPARKLE_B/XPCServices/"*.xpc; do
  codesign --force --sign - --preserve-metadata=entitlements,flags,runtime "$xpc" >/dev/null
done
codesign --force --sign - --preserve-metadata=entitlements,flags,runtime "$SPARKLE_B/Updater.app" >/dev/null
codesign --force --sign - --preserve-metadata=entitlements,flags,runtime "$SPARKLE_B/Autoupdate" >/dev/null
codesign --force --sign - --preserve-metadata=entitlements,flags,runtime "$CONTENTS/Frameworks/Sparkle.framework" >/dev/null

echo "→ signing app (ad-hoc)"
codesign --force --sign - "$APP" >/dev/null

# Nudge LaunchServices so Finder/Dock pick up a newly generated icon even when
# the app bundle path is reused across local builds.
touch "$APP"
if [ -x /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister ]; then
  /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$APP" >/dev/null 2>&1 || true
fi

echo
echo "Built $APP"
echo "Open it with: open \"$APP\""
