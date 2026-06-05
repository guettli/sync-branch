#!/usr/bin/env bash
set -euo pipefail

pushing_to_main=0
while read -r _local_ref _local_sha remote_ref _remote_sha; do
    if [[ "$remote_ref" == "refs/heads/main" ]]; then
        pushing_to_main=1
    fi
done
[[ $pushing_to_main -eq 1 ]] || exit 0

# If HEAD is already a version-tagged release commit, allow the push
if git tag --points-at HEAD | grep -qE '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    exit 0
fi

echo "pre-push: bumping version before push to main..." >&2
./scripts/release.sh >&2

echo "" >&2
echo "Version bumped. Push again with:" >&2
echo "  git push origin main --follow-tags" >&2
exit 1
