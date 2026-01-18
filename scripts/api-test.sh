#!/bin/bash
# API Testing Script for RescueStream API
# Usage: ./scripts/api-test.sh <METHOD> <PATH> [BODY]
# Example: ./scripts/api-test.sh GET /health
# Example: ./scripts/api-test.sh POST /broadcasters '{"display_name":"Test"}'

set -e

# Configuration
API_HOST="${API_HOST:-http://localhost:8080}"
API_KEY="${API_KEY:-admin}"
API_SECRET="${API_SECRET:-dev-secret-change-in-production}"

METHOD="${1:-GET}"
URL_PATH="${2:-/health}"
BODY="${3:-}"

# Generate timestamp (Unix epoch seconds)
TIMESTAMP=$(/bin/date +%s)

# Build string to sign: METHOD\nPATH\nTIMESTAMP\nBODY
STRING_TO_SIGN="${METHOD}
${URL_PATH}
${TIMESTAMP}
${BODY}"

# Generate HMAC-SHA256 signature
SIGNATURE=$(echo -n "$STRING_TO_SIGN" | openssl dgst -sha256 -hmac "$API_SECRET" | awk '{print $2}')

echo "=== Request ==="
echo "Method: $METHOD"
echo "URL: ${API_HOST}${URL_PATH}"
echo "Timestamp: $TIMESTAMP"
echo "Signature: $SIGNATURE"
if [ -n "$BODY" ]; then
    echo "Body: $BODY"
fi
echo ""

# Make the request
if [ -n "$BODY" ]; then
    curl -s -X "$METHOD" "${API_HOST}${URL_PATH}" \
        -H "Content-Type: application/json" \
        -H "X-API-Key: $API_KEY" \
        -H "X-Timestamp: $TIMESTAMP" \
        -H "X-Signature: $SIGNATURE" \
        -d "$BODY" | jq .
else
    curl -s -X "$METHOD" "${API_HOST}${URL_PATH}" \
        -H "X-API-Key: $API_KEY" \
        -H "X-Timestamp: $TIMESTAMP" \
        -H "X-Signature: $SIGNATURE" | jq .
fi
