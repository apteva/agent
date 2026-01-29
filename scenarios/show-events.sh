#!/bin/bash

# Show combined event stream from both agents during demo

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m'

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Load environment variables
if [ -f "$PROJECT_ROOT/.env" ]; then
    export $(grep -v '^#' "$PROJECT_ROOT/.env" | xargs)
fi

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Agent-to-Agent Communication - Event Stream Demo         ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Kill any existing processes
kill $(lsof -ti:4015) 2>/dev/null || true
kill $(lsof -ti:4016) 2>/dev/null || true
sleep 2

# Create log directory
LOG_DIR="$SCRIPT_DIR/logs"
mkdir -p "$LOG_DIR"

# Start Agent B (Code Assistant)
echo -e "${CYAN}Starting Agent B (Code Assistant) on port 4016...${NC}"
cd "$PROJECT_ROOT"
CONFIG_PATH="$SCRIPT_DIR/configs/agent-b-code.json" \
PORT=4016 \
DB_PATH="$SCRIPT_DIR/agent-b.db" \
go run main.go > "$LOG_DIR/agent-b-events.log" 2>&1 &
AGENT_B_PID=$!

# Wait for Agent B
sleep 3
curl -s http://localhost:4016/health > /dev/null 2>&1 || { echo "Agent B failed to start"; exit 1; }
echo -e "${GREEN}✓ Agent B ready${NC}"

# Start Agent A (Research Assistant)
echo -e "${CYAN}Starting Agent A (Research Assistant) on port 4015...${NC}"
CONFIG_PATH="$SCRIPT_DIR/configs/agent-a-research.json" \
PORT=4015 \
DB_PATH="$SCRIPT_DIR/agent-a.db" \
go run main.go > "$LOG_DIR/agent-a-events.log" 2>&1 &
AGENT_A_PID=$!

# Wait for Agent A
sleep 3
curl -s http://localhost:4015/health > /dev/null 2>&1 || { echo "Agent A failed to start"; exit 1; }
echo -e "${GREEN}✓ Agent A ready${NC}"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Stopping agents...${NC}"
    kill $AGENT_A_PID 2>/dev/null || true
    kill $AGENT_B_PID 2>/dev/null || true
    sleep 1
    echo -e "${GREEN}Done${NC}"
}
trap cleanup EXIT INT TERM

# Function to format timestamp
format_time() {
    date -r $1 "+%H:%M:%S.%3N" 2>/dev/null || date -d @$1 "+%H:%M:%S" 2>/dev/null || echo "00:00:00"
}

# Function to parse and display events
parse_log() {
    local agent=$1
    local log_file=$2
    local color=$3

    tail -f "$log_file" | while IFS= read -r line; do
        # Extract timestamp
        timestamp=$(echo "$line" | grep -oE '^[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}' || echo "")

        # Format output based on content
        if [[ "$line" =~ "📝 Adding tool_result: tool=" ]]; then
            tool=$(echo "$line" | grep -oP 'tool=\K[^ ,]+' || echo "unknown")
            preview=$(echo "$line" | grep -oP 'preview=\K.*' || echo "")
            echo -e "${color}[$agent] 🔧 Tool result: $tool${NC}"
            echo -e "  ${preview:0:100}..."
        elif [[ "$line" =~ "call_agent" ]] && [[ "$line" =~ "Calling agent" ]]; then
            agent_id=$(echo "$line" | grep -oP 'agent_id=\K[^ ]+' || echo "")
            echo -e "${color}[$agent] 📞 Calling agent: $agent_id${NC}"
        elif [[ "$line" =~ "POST.*chat" ]]; then
            echo -e "${color}[$agent] 📨 Received chat request${NC}"
        elif [[ "$line" =~ "🔵 Iteration" ]]; then
            iter=$(echo "$line" | grep -oP 'Iteration \K[0-9]+' || echo "")
            tools=$(echo "$line" | grep -oP '\K[0-9]+ tool' || echo "")
            echo -e "${color}[$agent] 🔄 Iteration $iter complete ($tools results)${NC}"
        elif [[ "$line" =~ "✅ Conversation complete" ]]; then
            echo -e "${color}[$agent] ✅ Response complete${NC}"
        fi
    done
}

# Start monitoring both logs in background
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Starting event stream (Ctrl+C to stop)...${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo ""

# Monitor both agents
parse_log "Agent A" "$LOG_DIR/agent-a-events.log" "$GREEN" &
MONITOR_A_PID=$!

parse_log "Agent B" "$LOG_DIR/agent-b-events.log" "$MAGENTA" &
MONITOR_B_PID=$!

# Give monitors time to start
sleep 2

# Send test request
echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${YELLOW}Sending: 'Write a Python function to add two numbers'${NC}"
echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

curl -s -X POST http://localhost:4015/chat \
    -H "Content-Type: application/json" \
    -d '{"message": "Ask the Code Assistant to write a simple Python function that adds two numbers", "stream": false}' > /tmp/response.json &

# Wait for response
wait

echo ""
echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${YELLOW}Final Response:${NC}"
echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
cat /tmp/response.json | jq -r '.response' 2>/dev/null || cat /tmp/response.json
echo ""

# Keep monitoring for a few more seconds
sleep 3

# Kill monitors
kill $MONITOR_A_PID $MONITOR_B_PID 2>/dev/null || true
