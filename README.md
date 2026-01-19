# RescueStream API

A live streaming orchestration API for managing broadcasters, stream keys, and active streams. Integrates with [MediaMTX](https://github.com/bluenviron/mediamtx) for RTMP/RTSP/WebRTC streaming.

## Features

- **Broadcaster Management** - Create and manage broadcaster accounts
- **Stream Key Management** - Generate, revoke, and track stream keys with expiration support
- **Stream Lifecycle** - Track active and ended streams with metadata
- **MediaMTX Integration** - Authentication webhooks and stream lifecycle events
- **Multi-Protocol Playback** - HLS and WebRTC (WHEP) playback URLs
- **HMAC Authentication** - Secure API access with signature-based authentication
- **Observability** - OpenTelemetry metrics and tracing via [ootel](https://alpineworks.io/ootel)

## API Endpoints

### Public Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| POST | `/auth` | MediaMTX stream authentication |
| POST | `/webhook/ready` | Stream started webhook |
| POST | `/webhook/not-ready` | Stream ended webhook |

### Protected Endpoints (Require HMAC Auth)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/broadcasters` | List all broadcasters |
| POST | `/broadcasters` | Create a broadcaster |
| GET | `/broadcasters/{id}` | Get broadcaster by ID |
| PATCH | `/broadcasters/{id}` | Update a broadcaster |
| DELETE | `/broadcasters/{id}` | Delete a broadcaster |
| GET | `/stream-keys` | List all stream keys |
| POST | `/stream-keys` | Create a stream key |
| GET | `/stream-keys/{id}` | Get stream key by ID |
| DELETE | `/stream-keys/{id}` | Revoke a stream key |
| GET | `/streams` | List all streams |
| GET | `/streams/{id}` | Get stream by ID |

## Authentication

Protected endpoints require HMAC-SHA256 signature authentication with three headers:

```
X-API-Key: <api-key>
X-Timestamp: <unix-timestamp>
X-Signature: <hmac-signature>
```

**Signature calculation:**

```
stringToSign = METHOD + "\n" + PATH + "\n" + TIMESTAMP + "\n" + BODY
signature = hex(HMAC-SHA256(stringToSign, API_SECRET))
```

## Configuration

Configuration is managed through environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `API_PORT` | HTTP server port | `8080` |
| `API_SECRET` | HMAC secret for authentication | *required* |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://...localhost:5432/rescuestream` |
| `MEDIAMTX_API_URL` | MediaMTX API endpoint | `http://localhost:9997` |
| `MEDIAMTX_PUBLIC_URL` | Public MediaMTX URL for playback | `http://localhost:8889` |
| `LOG_LEVEL` | Logging level (debug, info, warn, error) | `error` |
| `METRICS_ENABLED` | Enable Prometheus metrics | `true` |
| `METRICS_PORT` | Port for metrics endpoint | `8081` |
| `LOCAL` | Use OTLP gRPC exporter instead of Prometheus | `false` |
| `TRACING_ENABLED` | Enable distributed tracing | `false` |
| `TRACING_SAMPLERATE` | Trace sampling rate | `0.01` |
| `TRACING_SERVICE` | Service name for traces | `rescuestream-api` |
| `TRACING_VERSION` | Service version for traces | - |

## Getting Started

### Prerequisites

- Go 1.22+
- PostgreSQL 15+
- MediaMTX (for streaming)
- Docker & Docker Compose (for local development)

### Run Locally with Docker Compose

```bash
docker-compose up
```

This starts:

- **API Server**: http://localhost:8080
- **Metrics**: http://localhost:8081
- **PostgreSQL**: localhost:5432
- **MediaMTX RTMP**: localhost:1935
- **MediaMTX HLS**: http://localhost:8888
- **MediaMTX WebRTC**: http://localhost:8889
- **Grafana UI**: http://localhost:3000

### Development Commands

```bash
# Build the API
make build

# Run locally (requires DATABASE_URL and API_SECRET)
make run

# Run tests
make test

# Run linter
make lint

# Run database migrations
make migrate

# Rollback last migration
make migrate-down

# Create a new migration
make migrate-create
```

### Streaming with OBS

1. Create a broadcaster and stream key via the API
2. In OBS, set the stream URL to: `rtmp://localhost:1935/{stream_key}`
3. Start streaming
4. View via HLS: `http://localhost:8888/{stream_key}/index.m3u8`
5. Or WebRTC: `http://localhost:8889/{stream_key}/whep`

## Project Structure

```
.
├── cmd/
│   ├── rescuestream-api/   # Main API server
│   └── migrate/            # Database migration CLI
├── internal/
│   ├── config/             # Environment-based configuration
│   ├── database/           # PostgreSQL repositories and migrations
│   ├── domain/             # Domain entities and interfaces
│   ├── handler/            # HTTP handlers and middleware
│   ├── server/             # HTTP server setup and routing
│   ├── service/            # Business logic services
│   └── testutil/           # Test utilities
├── docker/
│   ├── grafana/            # Grafana dashboard provisioning
│   └── mediamtx/           # MediaMTX configuration
├── docs/                   # Integration guides
├── specs/                  # Feature specifications
├── Dockerfile              # Multi-stage build
└── docker-compose.yml      # Local development stack
```

## CI/CD

Pull requests are validated with:

- **commitlint**: Conventional commit message enforcement
- **golangci-lint**: Go linting
- **yamllint**: YAML linting
- **hadolint**: Dockerfile linting
- **go test**: Unit tests with race detection

## Documentation

- [Next.js Integration Guide](docs/nextjs-integration.md) - Client library and React components
