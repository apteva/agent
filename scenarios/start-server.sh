#!/bin/bash

# Start server for scenarios with the right config
# Usage: ./start-server.sh [config-file]

CONFIG="${1:-configs/basic-tools.json}"

echo "🚀 Starting server for scenarios..."
echo "📋 Using config: $CONFIG"
echo ""

# Check if already running
if lsof -Pi :4016 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo "⚠️  Server already running on port 4016"
    echo "Kill it first: lsof -ti:4016 | xargs kill"
    exit 1
fi

# Copy config
echo "Copying config to ../agent-config.json..."
cp "$CONFIG" ../agent-config.json

# Generate unique agent ID
TIMESTAMP=$(date +%s%N)
cd ..
# Start server
echo ""
echo "Starting server on port 4016..."
echo "Press Ctrl+C to stop"
echo ""
PORT=4016 go run main.go
