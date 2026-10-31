#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="$ROOT/dist/MyAgent.app"
CONTENTS="$APP/Contents"

if [ ! -d "$ROOT/ui/node_modules" ]; then
  echo "→ installing UI dependencies"
  (cd "$ROOT/ui" && npm ci)
fi

echo "→ building UI"
(cd "$ROOT/ui" && npm run build)

echo "→ building Go core"
mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources/ui" "$ROOT/bin"
(cd "$ROOT" && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$CONTENTS/Resources/agentd" ./cmd/agentd)
(cd "$ROOT" && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$ROOT/bin/agentctl" ./cmd/agentctl)

echo "→ copying UI"
rm -rf "$CONTENTS/Resources/ui"
mkdir -p "$CONTENTS/Resources/ui"
cp -R "$ROOT/ui/dist/." "$CONTENTS/Resources/ui/"

echo "→ building native shell"
swiftc -O -o "$CONTENTS/MacOS/MyAgent" "$ROOT/app/main.swift" -framework Cocoa -framework WebKit

cp "$ROOT/app/Info.plist" "$CONTENTS/Info.plist"
cp "$ROOT/app/AppIcon.icns" "$CONTENTS/Resources/AppIcon.icns"
printf "APPL????" > "$CONTENTS/PkgInfo"

echo "→ signing (ad-hoc)"
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || true

# Nudge LaunchServices so Finder/Dock pick up a newly generated icon even when
# the app bundle path is reused across local builds.
touch "$APP"
if [ -x /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister ]; then
  /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$APP" >/dev/null 2>&1 || true
fi

echo
echo "Built $APP"
echo "Open it with: open \"$APP\""
