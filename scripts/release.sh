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

# Collect commit messages from unpushed local commits
UPSTREAM=$(git rev-parse --abbrev-ref '@{upstream}' 2>/dev/null || echo "origin/main")
LOCAL_COMMITS=$(git log "$UPSTREAM"..HEAD --oneline 2>/dev/null | wc -l | tr -d ' ')

if [ "$LOCAL_COMMITS" -gt "0" ]; then
  # Gather messages before squashing
  MESSAGES=$(git log "$UPSTREAM"..HEAD --format="- %s" --reverse)

  # Soft-reset to upstream, stage everything, create one squashed commit
  git reset --soft "$UPSTREAM"
fi

# Bump version file
echo "$NEW" > "$VERSION_FILE"
git add -A

# Build the commit message
COMMIT_MSG="Release v${NEW}"
if [ -n "$MESSAGES" ]; then
  COMMIT_MSG="$COMMIT_MSG

$MESSAGES"
fi

git commit -m "$COMMIT_MSG"
git tag -a "v${NEW}" -m "Release v${NEW}"

# Push
git push origin main --tags

echo ""
echo "✓ Released v${NEW}"
