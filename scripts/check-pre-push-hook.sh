#!/usr/bin/env bash
set -euo pipefail

hooks_path=$(git config core.hooksPath 2>/dev/null || echo ".git/hooks")
hook="$hooks_path/pre-push"

if [[ ! -x "$hook" ]]; then
    echo "pre-push hook not installed. Run:" >&2
    echo "  cp scripts/pre-push-hook.sh $hook && chmod +x $hook" >&2
    exit 1
fi
