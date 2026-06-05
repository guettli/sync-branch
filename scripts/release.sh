#!/usr/bin/env bash
set -euo pipefail

latest=$(git tag --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1)
if [[ -z "$latest" ]]; then
    echo "No existing semver tag found." >&2
    exit 1
fi

major=$(echo "$latest" | cut -d. -f1 | tr -d v)
minor=$(echo "$latest" | cut -d. -f2)
patch=$(echo "$latest" | cut -d. -f3)
next="v${major}.${minor}.$((patch + 1))"

echo "Latest tag: $latest  →  Next: $next"

sed -i "s|rev: ${latest}|rev: ${next}|g" README.md

git add README.md
git commit --no-verify -m "chore: release ${next}"
git tag -a "${next}" -m "release ${next}"

echo "Tagged ${next}. Run 'git push && git push --tags' to publish."
