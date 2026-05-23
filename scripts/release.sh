#!/bin/bash
set -e

VERSION_FILE=".version"
CURRENT=$(cat "$VERSION_FILE" 2>/dev/null || echo "0.0.0")

IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT"

case "$1" in
  patch)
    NEW="$MAJOR.$MINOR.$((PATCH + 1))"
    ;;
  minor)
    NEW="$MAJOR.$((MINOR + 1)).0"
    ;;
  major)
    NEW="$((MAJOR + 1)).0.0"
    ;;
  *)
    echo "Usage: $0 {patch|minor|major}"
    exit 1
    ;;
esac

echo "$NEW" > "$VERSION_FILE"
git commit --only "$VERSION_FILE" --message "Release v${NEW}" --no-edit
git tag -m "Release v${NEW}" "v${NEW}"

echo ""
echo "✓ Tagged v${NEW}"
echo "  Run 'git push origin main --tags' to trigger the release build."
