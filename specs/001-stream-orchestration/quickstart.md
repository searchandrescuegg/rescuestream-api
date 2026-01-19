# Quickstart: Stream Orchestration API

## Prerequisites

- Go 1.22+
- Docker and Docker Compose
- PostgreSQL 15+ (or use Docker)

## Environment Setup

Create a `.env` file in the project root:

```bash
# Database
DATABASE_URL=postgres://postgres:postgres@localhost:5432/rescuestream?sslmode=disable

# API Configuration
API_PORT=8080
API_SECRET=your-shared-secret-here

# MediaMTX Integration
MEDIAMTX_API_URL=http://localhost:9997
MEDIAMTX_PUBLIC_URL=http://localhost:8889

# Observability (existing config)
LOG_LEVEL=debug
METRICS_ENABLED=true
METRICS_PORT=8081
TRACING_ENABLED=false
TRACING_SERVICE=rescuestream-api
```

## Local Development

### 1. Start PostgreSQL

```bash
docker run -d \
  --name rescuestream-postgres \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=rescuestream \
  -p 5432:5432 \
  postgres:15
```

### 2. Run Database Migrations

```bash
# Migrations will be applied automatically on startup
# Or run manually:
go run ./cmd/migrate up
```

### 3. Start MediaMTX (for integration testing)

```bash
docker run -d \
  --name mediamtx \
  -p 8554:8554 \
  -p 8889:8889 \
  -p 9997:9997 \
  -p 1935:1935 \
  bluenviron/mediamtx:latest
```

### 4. Configure MediaMTX for External Auth

Edit MediaMTX configuration (`mediamtx.yml`):

```yaml
authMethod: http
authHTTPAddress: http://host.docker.internal:8080/auth

# Exclude read actions from auth (viewers don't need auth)
authHTTPExclude:
  - action: read
  - action: playback

# Lifecycle hooks
paths:
  all:
    runOnReady: >
      curl -X POST http://host.docker.internal:8080/webhook/ready
      -H "Content-Type: application/json"
      -d '{"path":"$MTX_PATH","source_type":"$MTX_SOURCE_TYPE","source_id":"$MTX_SOURCE_ID"}'
    runOnNotReady: >
      curl -X POST http://host.docker.internal:8080/webhook/not-ready
      -H "Content-Type: application/json"
      -d '{"path":"$MTX_PATH"}'
```

### 5. Start the API

```bash
go run ./cmd/rescuestream-api
```

## Testing the API

### Create a Broadcaster

```bash
curl -X POST http://localhost:8080/broadcasters \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-shared-secret-here" \
  -d '{"display_name": "Test Broadcaster"}'
```

Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "display_name": "Test Broadcaster",
  "metadata": {},
  "created_at": "2026-01-17T10:00:00Z",
  "updated_at": "2026-01-17T10:00:00Z"
}
```

### Create a Stream Key

```bash
curl -X POST http://localhost:8080/stream-keys \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-shared-secret-here" \
  -d '{"broadcaster_id": "550e8400-e29b-41d4-a716-446655440000"}'
```

Response:
```json
{
  "id": "660e8400-e29b-41d4-a716-446655440001",
  "key_value": "sk_7Hj2kL9mN4pQ8rS1tU6vW3xY5zA0bC2dE4fG6hI8jK0",
  "broadcaster_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "active",
  "created_at": "2026-01-17T10:01:00Z"
}
```

**Important**: Save the `key_value` - it won't be shown again!

### Start a Stream (using OBS or ffmpeg)

Using ffmpeg:
```bash
ffmpeg -re -f lavfi -i testsrc=size=1280x720:rate=30 \
  -f lavfi -i sine=frequency=1000:sample_rate=44100 \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -c:a aac \
  -f flv "rtmp://localhost:1935/sk_7Hj2kL9mN4pQ8rS1tU6vW3xY5zA0bC2dE4fG6hI8jK0"
```

Using OBS:
1. Settings → Stream
2. Service: Custom
3. Server: `rtmp://localhost:1935`
4. Stream Key: `sk_7Hj2kL9mN4pQ8rS1tU6vW3xY5zA0bC2dE4fG6hI8jK0`

### List Active Streams

```bash
curl http://localhost:8080/streams \
  -H "X-API-Key: your-shared-secret-here"
```

Response:
```json
{
  "streams": [
    {
      "id": "770e8400-e29b-41d4-a716-446655440002",
      "stream_key_id": "660e8400-e29b-41d4-a716-446655440001",
      "path": "sk_7Hj2kL9mN4pQ8rS1tU6vW3xY5zA0bC2dE4fG6hI8jK0",
      "status": "active",
      "started_at": "2026-01-17T10:02:00Z",
      "urls": {
        "hls": "http://localhost:8889/sk_7Hj2kL9mN4pQ8rS1tU6vW3xY5zA0bC2dE4fG6hI8jK0/index.m3u8",
        "webrtc": "http://localhost:8889/sk_7Hj2kL9mN4pQ8rS1tU6vW3xY5zA0bC2dE4fG6hI8jK0/whep"
      }
    }
  ],
  "count": 1
}
```

### Revoke a Stream Key

```bash
curl -X DELETE http://localhost:8080/stream-keys/660e8400-e29b-41d4-a716-446655440001 \
  -H "X-API-Key: your-shared-secret-here"
```

## Running Tests

### Unit Tests

```bash
go test ./internal/... -v
```

### Integration Tests (requires Docker)

```bash
go test ./tests/integration/... -v
```

### All Tests with Coverage

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Docker Compose (Full Stack)

```yaml
# docker-compose.yml
version: '3.8'

services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: rescuestream
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

  mediamtx:
    image: bluenviron/mediamtx:latest
    ports:
      - "8554:8554"   # RTSP
      - "8889:8889"   # HLS/WebRTC
      - "9997:9997"   # API
      - "1935:1935"   # RTMP
    volumes:
      - ./docker/mediamtx/mediamtx.yml:/mediamtx.yml

  api:
    build: .
    ports:
      - "8080:8080"
      - "8081:8081"
    environment:
      DATABASE_URL: postgres://postgres:postgres@postgres:5432/rescuestream?sslmode=disable
      API_PORT: "8080"
      API_SECRET: your-shared-secret-here
      MEDIAMTX_API_URL: http://mediamtx:9997
      MEDIAMTX_PUBLIC_URL: http://localhost:8889
      LOG_LEVEL: debug
    depends_on:
      - postgres
      - mediamtx

volumes:
  postgres_data:
```

Start all services:
```bash
docker-compose up -d
```

## Common Issues

### "connection refused" on auth endpoint

MediaMTX can't reach your API. If running MediaMTX in Docker:
- Use `host.docker.internal` instead of `localhost` on Mac/Windows
- Use the Docker network IP or container name on Linux

### Stream key rejected but key is valid

Check:
1. Key status is `active` (not `revoked` or `expired`)
2. Key is not already in use by another stream
3. MediaMTX is sending the key in the `password` field

### Streams don't appear in list

The webhooks might not be configured. Verify:
1. `runOnReady` hook is configured in MediaMTX
2. API is receiving POST requests to `/webhook/ready`
3. Check API logs for errors

## API Reference

See the full OpenAPI specification at:
- Local: http://localhost:8080/docs (if Swagger UI enabled)
- File: [contracts/openapi.yaml](contracts/openapi.yaml)
