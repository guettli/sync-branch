#!/usr/bin/env bash
set -euo pipefail

# Only enforce on pushes to main
pushing_to_main=0
while read -r _local_ref _local_sha remote_ref _remote_sha; do
    if [[ "$remote_ref" == "refs/heads/main" ]]; then
        pushing_to_main=1
    fi
done
[[ $pushing_to_main -eq 1 ]] || exit 0

latest=$(git tag --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1)
if [[ -z "$latest" ]]; then
    exit 0
fi

check_file() {
    local file="$1"
    local rev
    rev=$(grep -oP '(?<=rev: )v[0-9]+\.[0-9]+\.[0-9]+' "$file" || true)
    if [[ "$rev" != "$latest" ]]; then
        echo "pre-push: $file has rev '$rev', but latest tag is '$latest'" >&2
        return 1
    fi
}

failed=0
check_file .pre-commit-config.yaml || failed=1
check_file README.md               || failed=1

if [[ $failed -eq 1 ]]; then
    echo "pre-push: run ./scripts/release.sh to sync versions before pushing." >&2
    exit 1
fi
