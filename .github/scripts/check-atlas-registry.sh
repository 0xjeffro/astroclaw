#!/usr/bin/env bash
# Verifies every pkg/app/*/db/schema.sql is registered in atlas.hcl.
# Without this check, a brand-new app's schema is silently skipped by
# `atlas migrate diff` (no migration generated) and git diff passes.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

found=$(find pkg/app -name schema.sql | sort)
missing_files=()

for f in $found; do
    if ! grep -q "file://$f" atlas.hcl; then
        missing_files+=("$f")
    fi
done

count=${#missing_files[@]}

if [ "$count" -gt 0 ]; then
    echo "::error::$count schema file(s) not registered in atlas.hcl:"
    for f in "${missing_files[@]}"; do
        echo "  - $f"
        echo "::error file=$f::not registered (add 'file://$f' to env \"local\" src list in atlas.hcl)"
    done
    exit 1
fi

echo "all schema files registered in atlas.hcl"
