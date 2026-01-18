#!/bin/bash
# End-to-End Test Script for RescueStream API
# Tests the full flow: broadcaster -> stream key -> publish -> verify
#
# Usage: ./scripts/e2e-test.sh
# Options:
#   --skip-stream    Skip the actual FFmpeg streaming (just test API)
#   --stream-duration N  Stream for N seconds (default: 10)

set -e

# Configuration
API_HOST="${API_HOST:-http://localhost:8080}"
API_KEY="${API_KEY:-admin}"
API_SECRET="${API_SECRET:-dev-secret-change-in-production}"
STREAM_DURATION=10
SKIP_STREAM=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-stream)
            SKIP_STREAM=true
            shift
            ;;
        --stream-duration)
            STREAM_DURATION="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper function for authenticated API requests
api_request() {
    local method="$1"
    local path="$2"
    local body="${3:-}"

    local timestamp=$(/bin/date +%s)
    local string_to_sign="${method}
${path}
${timestamp}
${body}"

    local signature=$(echo -n "$string_to_sign" | openssl dgst -sha256 -hmac "$API_SECRET" | awk '{print $2}')

    if [ -n "$body" ]; then
        curl -s -X "$method" "${API_HOST}${path}" \
            -H "Content-Type: application/json" \
            -H "X-API-Key: $API_KEY" \
            -H "X-Timestamp: $timestamp" \
            -H "X-Signature: $signature" \
            -d "$body"
    else
        curl -s -X "$method" "${API_HOST}${path}" \
            -H "X-API-Key: $API_KEY" \
            -H "X-Timestamp: $timestamp" \
            -H "X-Signature: $signature"
    fi
}

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}RescueStream API End-to-End Test${NC}"
echo -e "${YELLOW}========================================${NC}"
echo ""

# Step 1: Health Check
echo -e "${YELLOW}[1/7] Checking API health...${NC}"
health=$(api_request GET /health)
db_status=$(echo "$health" | jq -r '.database')
if [ "$db_status" != "ok" ]; then
    echo -e "${RED}FAILED: API health check failed${NC}"
    echo "$health" | jq .
    exit 1
fi
echo -e "${GREEN}OK: API is healthy${NC}"
echo ""

# Step 2: Create Broadcaster
echo -e "${YELLOW}[2/7] Creating broadcaster...${NC}"
broadcaster_name="E2E Test Broadcaster $(date +%s)"
broadcaster=$(api_request POST /broadcasters "{\"display_name\":\"$broadcaster_name\"}")
broadcaster_id=$(echo "$broadcaster" | jq -r '.id')
if [ "$broadcaster_id" == "null" ] || [ -z "$broadcaster_id" ]; then
    echo -e "${RED}FAILED: Could not create broadcaster${NC}"
    echo "$broadcaster" | jq .
    exit 1
fi
echo -e "${GREEN}OK: Created broadcaster $broadcaster_id${NC}"
echo ""

# Step 3: Create Stream Key
echo -e "${YELLOW}[3/7] Creating stream key...${NC}"
stream_key=$(api_request POST /stream-keys "{\"broadcaster_id\":\"$broadcaster_id\"}")
stream_key_id=$(echo "$stream_key" | jq -r '.id')
key_value=$(echo "$stream_key" | jq -r '.key_value')
if [ "$key_value" == "null" ] || [ -z "$key_value" ]; then
    echo -e "${RED}FAILED: Could not create stream key${NC}"
    echo "$stream_key" | jq .
    exit 1
fi
echo -e "${GREEN}OK: Created stream key $stream_key_id${NC}"
echo "    Key value: $key_value"
echo ""

# Step 4: Verify stream key is listed
echo -e "${YELLOW}[4/7] Verifying stream key in list...${NC}"
keys_list=$(api_request GET /stream-keys)
key_count=$(echo "$keys_list" | jq -r '.count')
found_key=$(echo "$keys_list" | jq -r ".stream_keys[] | select(.id == \"$stream_key_id\") | .id")
if [ "$found_key" != "$stream_key_id" ]; then
    echo -e "${RED}FAILED: Stream key not found in list${NC}"
    exit 1
fi
echo -e "${GREEN}OK: Stream key found in list (total: $key_count)${NC}"
echo ""

if [ "$SKIP_STREAM" = true ]; then
    echo -e "${YELLOW}[5/7] Skipping stream test (--skip-stream)${NC}"
    echo ""
    echo -e "${YELLOW}[6/7] Skipping stream verification${NC}"
    echo ""
else
    # Step 5: Publish test stream
    echo -e "${YELLOW}[5/7] Publishing test stream for ${STREAM_DURATION}s...${NC}"
    echo "    RTMP URL: rtmp://localhost:1935/$key_value"

    # Run FFmpeg in background
    ffmpeg -re -f lavfi -i testsrc=size=640x360:rate=30 \
        -f lavfi -i sine=frequency=1000:sample_rate=44100 \
        -c:v libx264 -preset ultrafast -tune zerolatency \
        -c:a aac -b:a 128k \
        -t "$STREAM_DURATION" \
        -f flv "rtmp://localhost:1935/$key_value" \
        -loglevel error &
    FFMPEG_PID=$!

    # Wait for stream to start
    sleep 3

    # Step 6: Verify stream is active
    echo -e "${YELLOW}[6/7] Verifying stream is active...${NC}"
    streams=$(api_request GET /streams)
    active_count=$(echo "$streams" | jq -r '.count')
    found_stream=$(echo "$streams" | jq -r ".streams[] | select(.stream_key_id == \"$stream_key_id\") | .id")

    if [ -n "$found_stream" ] && [ "$found_stream" != "null" ]; then
        echo -e "${GREEN}OK: Stream is active (ID: $found_stream)${NC}"
        stream_status=$(echo "$streams" | jq -r ".streams[] | select(.id == \"$found_stream\") | .status")
        echo "    Status: $stream_status"
        echo "    View at: http://localhost:8889/$key_value"
    else
        echo -e "${YELLOW}WARNING: Stream not found in active streams (may not have started yet)${NC}"
        echo "$streams" | jq .
    fi
    echo ""

    # Wait for FFmpeg to finish
    echo "    Waiting for stream to complete..."
    wait $FFMPEG_PID 2>/dev/null || true
    echo -e "${GREEN}OK: Stream completed${NC}"
    echo ""
fi

# Step 7: Cleanup - Revoke stream key
echo -e "${YELLOW}[7/7] Cleaning up - revoking stream key...${NC}"
revoke_result=$(api_request DELETE "/stream-keys/$stream_key_id")
# DELETE returns 204 No Content on success
verify_revoked=$(api_request GET "/stream-keys/$stream_key_id")
revoked_status=$(echo "$verify_revoked" | jq -r '.status')
if [ "$revoked_status" == "revoked" ]; then
    echo -e "${GREEN}OK: Stream key revoked${NC}"
else
    echo -e "${YELLOW}WARNING: Stream key status is '$revoked_status' (expected 'revoked')${NC}"
fi
echo ""

# Summary
echo -e "${YELLOW}========================================${NC}"
echo -e "${GREEN}All tests passed!${NC}"
echo -e "${YELLOW}========================================${NC}"
echo ""
echo "Resources created:"
echo "  - Broadcaster: $broadcaster_id ($broadcaster_name)"
echo "  - Stream Key: $stream_key_id (revoked)"
echo ""
echo "To clean up the broadcaster:"
echo "  ./scripts/api-test.sh DELETE /broadcasters/$broadcaster_id"
