#!/bin/bash
#
# Quick start script for development
# Usage: ./start.sh [options]
#

cd "$(dirname "$0")/.."

# Check if binary exists
BINARY="./go-backend/build/antigravity-proxy"
if [[ "$OSTYPE" == "msys" ]] || [[ "$OSTYPE" == "win32" ]]; then
    BINARY="./go-backend/build/antigravity-proxy.exe"
fi

if [[ ! -f "$BINARY" ]]; then
    echo "Binary not found. Building..."
    cd go-backend
    go build -ldflags="-s -w" -o build/antigravity-proxy ./cmd/server
    cd ..
fi

# Run server
echo "Starting Antigravity Claude Proxy..."
exec "$BINARY" "$@"
