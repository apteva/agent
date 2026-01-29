#!/bin/bash

# Live Scenario Runner - Single Command
# Usage: ./live.sh <scenario-name>

if [ -z "$1" ]; then
    echo "Usage: ./live.sh <scenario-name>"
    echo ""
    go run run-scenario.go -list
    exit 1
fi

SCENARIO="$1"

# Get config for the scenario
CONFIG=""
case "$SCENARIO" in
    notification|multi-tool)
        CONFIG="configs/basic-tools.json"
        ;;
    operator)
        CONFIG="configs/operator-mode.json"
        ;;
    mcp-notification)
        CONFIG="configs/mcp-tools.json"
        ;;
    task-management)
        CONFIG="configs/task-management.json"
        ;;
    *)
        echo "Unknown scenario: $SCENARIO"
        go run run-scenario.go -list
        exit 1
        ;;
esac

echo "🚀 Live Scenario: $SCENARIO"
echo ""

# Check if server is already running on 4016
if lsof -Pi :4016 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo "⚠️  Server already running on port 4016. Stopping it..."
    lsof -ti:4016 | xargs kill
    sleep 1
fi

# Copy config and modify agent ID to be unique
echo "📋 Using config: $CONFIG"
cp "$CONFIG" ../agent-config.json

# Generate unique agent ID using timestamp
TIMESTAMP=$(date +%s%N)
UNIQUE_ID="agent-test-$(echo $TIMESTAMP | tail -c 10)"

# Update agent ID in the config using jq
if command -v jq >/dev/null 2>&1; then
    jq --arg id "$UNIQUE_ID" '.agent.id = $id' ../agent-config.json > ../agent-config.json.tmp && mv ../agent-config.json.tmp ../agent-config.json
    echo "🔑 Generated unique agent ID: $UNIQUE_ID"
else
    echo "⚠️  Warning: jq not found, using default agent ID from config"
fi

echo ""
echo "📄 Agent Configuration:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
cat ../agent-config.json | jq -r '
.agent |
"  ID: \(.id)
  Name: \(.name)
  Model: \(.llm.model)
  Tools: \(.llm.tools | join(", "))
  Builtin Tools: \(if (.llm.builtin_tools | length) > 0 then (.llm.builtin_tools | map(.name) | join(", ")) else "none" end)
  Operator: \(if .operator.enabled then "enabled" else "disabled" end)
  Vision: \(if .llm.vision.enabled then "enabled" else "disabled" end)"
'
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Start server in background
echo "🔧 Starting server on port 4016..."
cd ..
PORT=4016 go run main.go > /dev/null 2>&1 &
SERVER_PID=$!
cd scenarios

# Register cleanup to kill server when script exits
trap "echo ''; echo '🛑 Stopping server...'; kill $SERVER_PID 2>/dev/null; wait $SERVER_PID 2>/dev/null" EXIT INT TERM

# Wait for server to be ready
echo "⏳ Waiting for server to start..."
MAX_ATTEMPTS=30
for i in $(seq 1 $MAX_ATTEMPTS); do
    if curl -s http://localhost:4016/health > /dev/null 2>&1; then
        echo "✅ Server ready!"
        echo ""
        break
    fi
    if [ $i -eq $MAX_ATTEMPTS ]; then
        echo "❌ Server failed to start"
        exit 1
    fi
    sleep 0.5
done

# Run the scenario
go run run-scenario.go -scenario "$SCENARIO"

# Server will be stopped by trap on exit
