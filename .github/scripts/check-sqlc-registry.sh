#!/usr/bin/env bash
# Verifies every pkg/app/*/db/schema.sql is registered in sqlc.yaml.
# Without this check, a brand-new app's schema is silently skipped by
# `sqlc generate` (no Go code generated) and git diff passes.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

found=$(find pkg/app -name schema.sql | sort)
missing_files=()

for f in $found; do
    if ! grep -q "$f" sqlc.yaml; then
        missing_files+=("$f")
    fi
done

count=${#missing_files[@]}

if [ "$count" -gt 0 ]; then
    echo "::error::$count schema file(s) not registered in sqlc.yaml:"
    for f in "${missing_files[@]}"; do
        echo "  - $f"
        echo "::error file=$f::not registered (add a new sql block in sqlc.yaml)"
    done
    exit 1
fi

echo "all schema files registered in sqlc.yaml"
