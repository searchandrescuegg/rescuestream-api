# rescuestream-api task runner
# Replaces the legacy Makefile. Run `just --list` to see every recipe.

# Default recipe: list everything.
default:
    @just --list

# --- Setup ---------------------------------------------------------------

# Install commitlint globally (requires `nvm use --lts` or `nvm use node`).
setup:
    npm install -g @commitlint/config-conventional @commitlint/cli

# Wire git hooks into .githooks/.
hooks:
    git config --local core.hooksPath .githooks/

# --- Go build/run --------------------------------------------------------

build:
    go build -o bin/rescuestream-api ./cmd/rescuestream-api

run:
    go run ./cmd/rescuestream-api

# --- Tests ---------------------------------------------------------------

test:
    go test -v -race -coverprofile=coverage.out ./...

test-unit:
    go test -v -race ./internal/...

test-integration:
    go test -v -race ./tests/integration/...

test-contract:
    go test -v -race ./tests/contract/...

# --- Lint & format -------------------------------------------------------

lint:
    golangci-lint run ./...

fmt:
    go fmt ./...
    goimports -w -local github.com/searchandrescuegg/rescuestream-api .

verify: lint test

# --- Migrations ----------------------------------------------------------

# Apply migrations forward against the local Docker Postgres (reads DATABASE_URL).
migrate-local:
    go run ./cmd/migrate up

# Roll migrations back locally.
migrate-local-down:
    go run ./cmd/migrate down

# Create a new migration pair (usage: `just migrate-create add_foo`).
migrate-create name:
    migrate create -ext sql -dir internal/database/migrations -seq {{ name }}

# Apply migrations to PRODUCTION (Neon).
# Required env: DATABASE_MIGRATION_URL pointing at the *non-pooled* Neon host.
# The guard below refuses unset vars, pooled hostnames, and non-Neon hosts so
# operators cannot accidentally run migrations against the request-path pool
# (golang-migrate's advisory locks require a session-scoped connection) or
# against an unrelated database.
#
# If DRY_RUN=1 is set, the guard runs but `golang-migrate` does NOT execute;
# used by tests/tooling/migrate_prod_guard_test.sh.
migrate-prod:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -z "${DATABASE_MIGRATION_URL:-}" ]; then
      echo "refusing: DATABASE_MIGRATION_URL must be set for prod migrations" >&2
      exit 2
    fi
    case "$DATABASE_MIGRATION_URL" in
      *-pooler.*neon.tech*)
        echo "refusing: DATABASE_MIGRATION_URL points at a Neon pooled endpoint; use the non-pooled host" >&2
        exit 2
        ;;
      *.neon.tech*) ;;
      *)
        echo "refusing: DATABASE_MIGRATION_URL is not a neon.tech host" >&2
        exit 2
        ;;
    esac
    if [ "${DRY_RUN:-0}" = "1" ]; then
      echo "ok: guard passed; DRY_RUN=1 so not executing migrations"
      exit 0
    fi
    DATABASE_URL="$DATABASE_MIGRATION_URL" go run ./cmd/migrate up

# --- Housekeeping --------------------------------------------------------

clean:
    rm -rf bin/ coverage.out
