# Contract tests

HTTP-contract assertions derived from [specs/003-multi-tenant-platform/contracts/api-routes.md](../../specs/003-multi-tenant-platform/contracts/api-routes.md).

## Convention

- **One `*_contract_test.go` file per resource.** Example: `orgs_contract_test.go`, `rooms_contract_test.go`, `devices_contract_test.go`.
- Each test file exercises every endpoint documented for that resource in `api-routes.md`. The suite MUST cover:
  - Happy-path status code + response shape.
  - Authorization denials for the wrong role (super-admin vs org-admin vs member).
  - Tenancy denials across organizations.
  - Idempotency where the contract specifies it.
  - RFC 9457 problem-details responses for every documented failure mode (correct `type`, `title`, `status`, `detail`, and — where specified — `instance` metadata like `current_version`).
- **Shared harness.** Contract tests spin up a real Postgres via `testcontainers-go` (constitution III) and mount the API handlers under `net/http/httptest`. Use `stretchr/testify` (`assert` / `require`) for assertions.
- **No network.** Contract tests MUST NOT depend on live external services (Google OAuth, MediaMTX, Tailscale). Stub those at interface boundaries.
- **Run with** `just test-contract`.

## Mapping contract sections → test files

| Section in `api-routes.md` | Test file |
|---|---|
| §2 Organizations | `orgs_contract_test.go` |
| §3 Teams | `teams_contract_test.go` |
| §4 Members | `members_contract_test.go` |
| §5 Tags | `tags_contract_test.go` |
| §6 Devices | `devices_contract_test.go` |
| §7 Rooms | `rooms_contract_test.go` |
| §8 Sessions / auth | `sessions_contract_test.go` |
| §9 Super-admins | `superadmins_contract_test.go` |
| §10 Audit logs | `audit_contract_test.go` |
| §11 Stream-events (SSE) | `stream_events_contract_test.go` |
| §12 Auth webhook (MediaMTX) | `auth_webhook_contract_test.go` |
| §13 ACL preview | `acl_preview_contract_test.go` |

Update this table alongside any new resource you add to `api-routes.md`.

## Failing tests on contract drift

When `api-routes.md` changes, the matching contract test MUST be updated in the same PR. Contract tests are the authoritative check that the shipped API matches the documented surface.
