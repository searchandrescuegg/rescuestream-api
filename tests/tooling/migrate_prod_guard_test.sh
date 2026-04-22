#!/usr/bin/env bash
# Exercises the `just migrate-prod` guard (spec 003-multi-tenant-platform T004b).
# All cases run with DRY_RUN=1 so `golang-migrate` never executes; the guard
# itself is what's under test.
#
# Run from repo root:
#   bash tests/tooling/migrate_prod_guard_test.sh
#
# Exit code: 0 on all-green, 1 on any assertion failure.

set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

fail=0
total=0

assert_exit() {
  local label="$1" expected="$2" actual="$3"
  total=$((total + 1))
  if [ "$actual" != "$expected" ]; then
    echo "FAIL [$label]: expected exit $expected, got $actual" >&2
    fail=$((fail + 1))
  else
    echo "ok   [$label]: exit $actual"
  fi
}

# 1. Unset DATABASE_MIGRATION_URL → must refuse (exit 2).
unset DATABASE_MIGRATION_URL
set +e
DRY_RUN=1 just migrate-prod >/dev/null 2>&1
code=$?
set -e
assert_exit "unset var" 2 "$code"

# 2. Pooled Neon hostname → must refuse (exit 2).
set +e
DATABASE_MIGRATION_URL="postgres://user:pw@ep-foo-123-pooler.us-west-2.aws.neon.tech/rescuestream?sslmode=require" \
  DRY_RUN=1 just migrate-prod >/dev/null 2>&1
code=$?
set -e
assert_exit "pooled host" 2 "$code"

# 3. Non-Neon host → must refuse (exit 2).
set +e
DATABASE_MIGRATION_URL="postgres://user:pw@example.com:5432/rescuestream?sslmode=disable" \
  DRY_RUN=1 just migrate-prod >/dev/null 2>&1
code=$?
set -e
assert_exit "non-neon host" 2 "$code"

# 4. Non-pooled Neon host with DRY_RUN=1 → guard passes, returns 0 without migrating.
set +e
DATABASE_MIGRATION_URL="postgres://user:pw@ep-foo-123.us-west-2.aws.neon.tech/rescuestream?sslmode=require" \
  DRY_RUN=1 just migrate-prod >/dev/null 2>&1
code=$?
set -e
assert_exit "valid neon host, dry-run" 0 "$code"

echo
if [ "$fail" -eq 0 ]; then
  echo "PASS: $total/$total"
  exit 0
else
  echo "FAIL: $fail/$total" >&2
  exit 1
fi
