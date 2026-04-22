# rescuestream-api Development Guidelines

Auto-generated from all feature plans. Last updated: 2026-01-17

## Active Technologies
- Go 1.25 (existing; see `go.mod`) (003-multi-tenant-platform)
- NeonDB (PostgreSQL 15+ compatible). Migration tooling unchanged (`golang-migrate`). Connection string pooled via Neon's pooler endpoint (`*-pooler.neon.tech`) for request handlers; non-pooled endpoint used by the migration command. (003-multi-tenant-platform)

- Go 1.22+ (001-stream-orchestration)

## Project Structure

```text
src/
tests/
```

## Commands

# Add commands for Go 1.22+

## Code Style

Go 1.22+: Follow standard conventions

## Recent Changes
- 003-multi-tenant-platform: Added Go 1.25 (existing; see `go.mod`)
- 003-multi-tenant-platform: Added Go 1.25 (existing; see `go.mod`)

- 001-stream-orchestration: Added Go 1.22+

<!-- MANUAL ADDITIONS START -->
<!-- MANUAL ADDITIONS END -->
