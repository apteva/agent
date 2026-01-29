#!/bin/bash

# Scenario Runner Helper Script
# Usage: ./run.sh <scenario-name>

if [ -z "$1" ]; then
    echo "Usage: ./run.sh <scenario-name>"
    echo ""
    go run run-scenario.go -list
    exit 1
fi

SCENARIO="$1"

# Check if server is already running on 4016
if lsof -Pi :4016 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo "✅ Server already running on port 4016"
    echo ""
    go run run-scenario.go -scenario "$SCENARIO"
else
    echo "⚠️  Server not running on port 4016. Start it manually:"
    echo "   PORT=4016 go run main.go"
    echo ""
    echo "Or run the scenario directly (it will wait for server):"
    echo "   go run run-scenario.go -scenario $SCENARIO"
    exit 1
fi
