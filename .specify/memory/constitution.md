<!--
SYNC IMPACT REPORT
==================
Version change: N/A → 1.0.0 (Initial ratification)
Modified principles: N/A (Initial creation)
Added sections:
  - Core Principles (6 principles)
  - Technical Constraints
  - Development Workflow
  - Governance
Removed sections: N/A
Templates requiring updates:
  - .specify/templates/plan-template.md ✅ (compatible - no changes needed)
  - .specify/templates/spec-template.md ✅ (compatible - no changes needed)
  - .specify/templates/tasks-template.md ✅ (compatible - no changes needed)
  - .specify/templates/checklist-template.md ✅ (compatible - no changes needed)
  - .specify/templates/agent-file-template.md ✅ (compatible - no changes needed)
Follow-up TODOs: None
-->

# RescueStream API Constitution

## Core Principles

### I. Go Standards Compliance

All code MUST conform to official Go standards and idiomatic practices:

- Code MUST pass `go vet`, `golangci-lint`, and `go fmt` without errors or warnings
- Package naming MUST follow Go conventions (lowercase, single-word preferred)
- Error handling MUST use explicit error returns, never panic for expected conditions
- Exported identifiers MUST have documentation comments
- Code MUST follow effective Go guidelines including proper use of interfaces,
  embedding, and composition over inheritance

**Rationale**: Consistent Go idioms ensure maintainability, reduce cognitive load for
contributors, and leverage the ecosystem's tooling effectively.

### II. Functional Options Pattern

All clients for third-party services (databases, caches, external APIs) MUST use the
functional options pattern for configuration:

- Client constructors MUST accept variadic functional options: `func NewClient(opts ...Option)`
- Options MUST be implemented as functions returning an `Option` type
- Sensible defaults MUST be provided when no options are specified
- Options MUST be composable and order-independent where possible

**Rationale**: Functional options provide flexible, extensible APIs that maintain
backward compatibility as requirements evolve, while keeping constructors clean
and self-documenting.

### III. Comprehensive Testing

Every endpoint and service MUST have corresponding tests:

- Integration tests MUST use `testcontainers-go` for database and external service
  dependencies
- HTTP endpoint tests MUST use `net/http/httptest` for handler testing
- Assertions MUST use `stretchr/testify` packages (`assert`, `require`, `mock`)
- Test coverage SHOULD exceed 80% for business logic; MUST exceed 60% overall
- Tests MUST be deterministic and not depend on external network resources

**Rationale**: Comprehensive testing with real dependencies via containers catches
integration issues early while maintaining fast, reliable CI pipelines.

### IV. RFC 9457 Error Responses

ALL error responses MUST conform to RFC 9457 (Problem Details for HTTP APIs):

- Error responses MUST use the `github.com/alpineworks/rfc9457` package
- Every error MUST include: `type`, `title`, `status`, and `detail` fields
- Error types MUST be URIs that can resolve to human-readable documentation
- The `instance` field SHOULD be included for request-specific debugging
- Validation errors MUST include structured `errors` array with field-level details

**Rationale**: Standardized error responses enable consistent client error handling,
improve debugging, and provide machine-readable error categorization.

### V. JSON-Only Protocol

All API requests and responses MUST use JSON encoding exclusively:

- Content-Type MUST be `application/json` for all request and response bodies
- Requests with non-JSON content types MUST be rejected with 415 Unsupported Media Type
- Empty responses MUST return 204 No Content (not empty JSON objects)
- Field naming MUST use `snake_case` consistently
- Date/time fields MUST use RFC 3339 format

**Rationale**: JSON-only simplifies client implementations, reduces parsing ambiguity,
and aligns with the Next.js frontend expectations.

### VI. Performance Requirements

All API endpoints MUST meet strict performance targets:

- Response time MUST be under 50ms at p95 for cached/simple operations
- Response time SHOULD be under 100ms at p95 for database operations
- Endpoints MUST implement appropriate timeouts and context cancellation
- Database queries MUST use connection pooling and prepared statements
- Expensive operations MUST be profiled and optimized before deployment

**Rationale**: Sub-50ms response times ensure responsive user experience and enable
the API to serve as a low-latency intermediary between MediaMTX and the frontend.

## Technical Constraints

### Authentication & Security

- Frontend requests MUST be authenticated using shared secrets
- Shared secrets MUST be transmitted via secure headers, never in URLs
- All secrets MUST be loaded from environment variables, never hardcoded
- HTTPS MUST be enforced in production environments

### Dependencies

- External dependencies MUST be vendored or use Go modules with checksums
- The `github.com/alpineworks/rfc9457` package is REQUIRED for error handling
- MediaMTX integration MUST use official APIs where available

### Observability

- Structured JSON logging MUST be used via `slog`
- OpenTelemetry metrics and tracing SHOULD be enabled per README configuration
- Request IDs MUST be propagated through all log entries and traces

## Development Workflow

### Code Review Requirements

- All changes MUST be submitted via pull request
- PRs MUST pass all CI checks: linting, tests, and build
- PRs MUST include tests for new functionality
- Breaking API changes MUST be documented and versioned

### Testing Gates

- Unit tests MUST pass before integration tests run
- Integration tests with testcontainers MUST pass before merge
- Performance benchmarks SHOULD be run for latency-sensitive changes

### Commit Standards

- Commits MUST follow conventional commit format
- Breaking changes MUST be marked with `!` or `BREAKING CHANGE` footer

## Governance

This constitution supersedes all other development practices for the RescueStream API.
Amendments require:

1. Documented proposal with rationale
2. Review period of at least 48 hours
3. Migration plan for existing code if principles change
4. Version increment following semantic versioning

All pull requests and code reviews MUST verify compliance with these principles.
Complexity beyond these standards MUST be explicitly justified in PR descriptions.

**Version**: 1.0.0 | **Ratified**: 2026-01-17 | **Last Amended**: 2026-01-17
