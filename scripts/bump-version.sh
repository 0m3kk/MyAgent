#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

usage() {
  cat >&2 <<'EOF'
Usage: scripts/bump-version.sh <patch|minor|major> [--dry-run]

Bumps the repository version from the latest vX.Y.Z tag, updates:
  - VERSION
  - cmd/agentd/main.go
  - app/Info.plist
  - ui/package.json

Prints the new version (without a leading "v") to stdout.
EOF
  exit 2
}

BUMP="${1:-}"
case "$BUMP" in
  patch|minor|major) ;;
  *) usage ;;
esac

DRY_RUN=0
if [ "${2:-}" = "--dry-run" ]; then
  DRY_RUN=1
elif [ -n "${2:-}" ]; then
  usage
fi

CURRENT=""
LATEST_TAG="$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1 || true)"
if [ -n "$LATEST_TAG" ]; then
  CURRENT="${LATEST_TAG#v}"
elif [ -f VERSION ]; then
  CURRENT="$(tr -d '[:space:]' < VERSION)"
fi

if [[ ! "$CURRENT" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "Cannot determine the current version from a vX.Y.Z tag or VERSION file." >&2
  exit 1
fi

MAJOR="${BASH_REMATCH[1]}"
MINOR="${BASH_REMATCH[2]}"
PATCH="${BASH_REMATCH[3]}"

case "$BUMP" in
  major)
    MAJOR=$((MAJOR + 1))
    MINOR=0
    PATCH=0
    ;;
  minor)
    MINOR=$((MINOR + 1))
    PATCH=0
    ;;
  patch)
    PATCH=$((PATCH + 1))
    ;;
esac

NEXT="${MAJOR}.${MINOR}.${PATCH}"
NEXT_TAG="v${NEXT}"

if git rev-parse --verify --quiet "refs/tags/${NEXT_TAG}" >/dev/null; then
  echo "Tag ${NEXT_TAG} already exists." >&2
  exit 1
fi

echo "→ ${CURRENT} → ${NEXT} (${BUMP})" >&2

if [ "$DRY_RUN" = 1 ]; then
  echo "$NEXT"
  exit 0
fi

perl -0pi -e 's/^var version = "[^"]*"/var version = "'"$NEXT"'"/m' cmd/agentd/main.go
perl -0pi -e 's{(<key>CFBundleShortVersionString</key>\s*<string>)[^<]*(</string>)}{${1}'"$NEXT"'${2}}' app/Info.plist
perl -0pi -e 's/("version":\s*")[^"]*(")/${1}'"$NEXT"'${2}/' ui/package.json
printf '%s\n' "$NEXT" > VERSION

grep -q "var version = \"${NEXT}\"" cmd/agentd/main.go
grep -q "<string>${NEXT}</string>" app/Info.plist
grep -q "\"version\": \"${NEXT}\"" ui/package.json
[ "$(tr -d '[:space:]' < VERSION)" = "$NEXT" ]

echo "$NEXT"
