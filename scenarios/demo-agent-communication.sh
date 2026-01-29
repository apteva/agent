#!/bin/bash

# One-command demo for agent-to-agent communication
# Starts both agents, runs tests, shows results, then cleanup

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
LOG_DIR="$SCRIPT_DIR/logs"

# Load environment variables
if [ -f "$PROJECT_ROOT/.env" ]; then
    export $(grep -v '^#' "$PROJECT_ROOT/.env" | xargs)
fi

echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Agent-to-Agent Communication Demo    ║${NC}"
echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    # Kill monitor processes first
    if [ -n "$MONITOR_A_PID" ]; then
        kill -9 $MONITOR_A_PID 2>/dev/null || true
        pkill -9 -P $MONITOR_A_PID 2>/dev/null || true
    fi
    if [ -n "$MONITOR_B_PID" ]; then
        kill -9 $MONITOR_B_PID 2>/dev/null || true
        pkill -9 -P $MONITOR_B_PID 2>/dev/null || true
    fi
    # Kill agent processes
    if [ -n "$AGENT_A_PID" ]; then
        kill -9 $AGENT_A_PID 2>/dev/null || true
    fi
    if [ -n "$AGENT_B_PID" ]; then
        kill -9 $AGENT_B_PID 2>/dev/null || true
    fi
    # Force kill any remaining processes on these ports
    lsof -ti:4015,4016 | xargs kill -9 2>/dev/null || true
    # Also kill any go run processes
    pkill -9 -f "CONFIG_PATH.*scenarios" 2>/dev/null || true
    rm -f "$SCRIPT_DIR/.agent-pids"
    sleep 1
    echo -e "${GREEN}Cleanup complete${NC}"
}

trap cleanup EXIT INT TERM

# Check prerequisites
if ! command -v jq &> /dev/null; then
    echo -e "${YELLOW}Warning: jq not installed. Install for better output formatting:${NC}"
    echo "  brew install jq"
    echo ""
fi

# Create log directory
mkdir -p "$LOG_DIR"

# Kill any existing processes on these ports
if lsof -Pi :4015 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo -e "${YELLOW}Killing existing process on port 4015...${NC}"
    kill $(lsof -ti:4015) 2>/dev/null || true
    sleep 2
fi

if lsof -Pi :4016 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo -e "${YELLOW}Killing existing process on port 4016...${NC}"
    kill $(lsof -ti:4016) 2>/dev/null || true
    sleep 2
fi

# Start Agent B (Code Assistant)
echo -e "${CYAN}[1/4] Starting Agent B (Code Assistant) on port 4016...${NC}"
cd "$PROJECT_ROOT"
CONFIG_PATH="$SCRIPT_DIR/configs/agent-b-code.json" \
PORT=4016 \
DB_PATH="$SCRIPT_DIR/agent-b.db" \
go run main.go > "$LOG_DIR/agent-b.log" 2>&1 &
AGENT_B_PID=$!

# Wait for Agent B
echo -n "      Waiting for Agent B"
for i in {1..30}; do
    if curl -s http://localhost:4016/health > /dev/null 2>&1; then
        echo -e " ${GREEN}✓${NC}"
        break
    fi
    echo -n "."
    sleep 1
    if [ $i -eq 30 ]; then
        echo -e " ${RED}✗ Failed${NC}"
        exit 1
    fi
done

# Start Agent A (Research Assistant)
echo -e "${CYAN}[2/4] Starting Agent A (Research Assistant) on port 4015...${NC}"
CONFIG_PATH="$SCRIPT_DIR/configs/agent-a-research.json" \
PORT=4015 \
DB_PATH="$SCRIPT_DIR/agent-a.db" \
go run main.go > "$LOG_DIR/agent-a.log" 2>&1 &
AGENT_A_PID=$!

# Wait for Agent A
echo -n "      Waiting for Agent A"
for i in {1..30}; do
    if curl -s http://localhost:4015/health > /dev/null 2>&1; then
        echo -e " ${GREEN}✓${NC}"
        break
    fi
    echo -n "."
    sleep 1
    if [ $i -eq 30 ]; then
        echo -e " ${RED}✗ Failed${NC}"
        exit 1
    fi
done

echo ""
echo -e "${GREEN}✓ Both agents started successfully${NC}"
echo ""

# Give agents a moment to fully initialize
sleep 2

# Function to monitor and display events
show_event() {
    local agent=$1
    local color=$2
    local line=$3

    if [[ "$line" =~ "POST" ]] && [[ "$line" =~ "chat" ]]; then
        echo -e "  ${color}[${agent}]${NC} 📨 Received chat request"
    elif [[ "$line" =~ "📝 Adding tool_result: tool=call_agent" ]]; then
        if [[ "$line" =~ "error" ]]; then
            # Extract full error message
            error=$(echo "$line" | sed -n 's/.*"error":"\([^"]*\)".*/\1/p')
            echo -e "  ${color}[${agent}]${NC} ❌ call_agent error: ${error}"
        else
            duration=$(echo "$line" | sed -n 's/.*"duration_ms":\([0-9]*\).*/\1/p')
            agent_name=$(echo "$line" | sed -n 's/.*"agent_name":"\([^"]*\)".*/\1/p')
            echo -e "  ${color}[${agent}]${NC} ✅ call_agent → ${agent_name} (${duration}ms)"
        fi
    elif [[ "$line" =~ "📝 Adding tool_result: tool=list_available_agents" ]]; then
        count=$(echo "$line" | sed -n 's/.*"count":\([0-9]*\).*/\1/p')
        echo -e "  ${color}[${agent}]${NC} 📋 Discovered ${count} available agent(s)"
    elif [[ "$line" =~ "🔵 Iteration" ]]; then
        iter=$(echo "$line" | sed -n 's/.*Iteration \([0-9]*\).*/\1/p')
        tools=$(echo "$line" | sed -n 's/.*\([0-9]*\) tool result.*/\1/p')
        echo -e "  ${color}[${agent}]${NC} 🔄 Iteration ${iter} (${tools} tool calls)"
    elif [[ "$line" =~ "✅ Conversation complete" ]]; then
        echo -e "  ${color}[${agent}]${NC} ✅ Response complete"
    fi
}

# Run demo tests
echo -e "${CYAN}[3/4] Running Demo Tests (with live events)${NC}"
echo ""

# Test 1: Simple delegation
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${YELLOW}Demo 1: Simple Code Delegation${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "${CYAN}User → Agent A:${NC}"
echo "  'Ask the Code Assistant to write a function that adds two numbers'"
echo ""
echo -e "${CYAN}Live Event Stream:${NC}"

# Start log monitors in background
tail -f "$LOG_DIR/agent-a.log" 2>/dev/null | while IFS= read -r line; do
    show_event "Agent A" "$GREEN" "$line"
done &
MONITOR_A_PID=$!

tail -f "$LOG_DIR/agent-b.log" 2>/dev/null | while IFS= read -r line; do
    show_event "Agent B" "$CYAN" "$line"
done &
MONITOR_B_PID=$!

# Give monitors time to start
sleep 1

# Send request
RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
    -H "Content-Type: application/json" \
    -d '{"message": "Please ask the Code Assistant to write a simple Python function called add_numbers that takes two parameters and returns their sum. Keep it very simple.", "stream": false}')

# Wait a moment for events to display
sleep 2

# Kill monitors and their child processes
kill $MONITOR_A_PID $MONITOR_B_PID 2>/dev/null || true
pkill -P $MONITOR_A_PID 2>/dev/null || true
pkill -P $MONITOR_B_PID 2>/dev/null || true
sleep 0.5

echo ""
echo -e "${YELLOW}Agent A Final Response:${NC}"
if command -v jq &> /dev/null; then
    echo "$RESPONSE" | jq -r '.response' 2>/dev/null | head -20
else
    echo "$RESPONSE" | grep -o '"response":"[^"]*"' | cut -d'"' -f4 | head -20
fi
echo ""
echo -e "${GREEN}✓ Demo 1 Complete${NC}"
echo ""
sleep 2

# Test 2: List agents
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${YELLOW}Demo 2: Discover Available Agents${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "${CYAN}User → Agent A:${NC}"
echo "  'What other agents can you work with?'"
echo ""
echo -e "${CYAN}Live Event Stream:${NC}"

# Start log monitors
tail -f "$LOG_DIR/agent-a.log" 2>/dev/null | while IFS= read -r line; do
    show_event "Agent A" "$GREEN" "$line"
done &
MONITOR_A_PID=$!

sleep 1

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
    -H "Content-Type: application/json" \
    -d '{"message": "List all the agents you can communicate with", "stream": false}')

sleep 2
kill $MONITOR_A_PID 2>/dev/null || true
pkill -P $MONITOR_A_PID 2>/dev/null || true
sleep 0.5

echo ""
echo -e "${YELLOW}Agent A Final Response:${NC}"
if command -v jq &> /dev/null; then
    echo "$RESPONSE" | jq -r '.response' 2>/dev/null
else
    echo "$RESPONSE"
fi
echo ""
echo -e "${GREEN}✓ Demo 2 Complete${NC}"
echo ""
sleep 2

# Test 3: Complex scenario
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${YELLOW}Demo 3: Complex Multi-Agent Task${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "${CYAN}User → Agent A:${NC}"
echo "  'I need a function to calculate factorial. Ask the Code Assistant.'"
echo ""
echo -e "${CYAN}Live Event Stream:${NC}"

# Start log monitors for both agents
tail -f "$LOG_DIR/agent-a.log" 2>/dev/null | while IFS= read -r line; do
    show_event "Agent A" "$GREEN" "$line"
done &
MONITOR_A_PID=$!

tail -f "$LOG_DIR/agent-b.log" 2>/dev/null | while IFS= read -r line; do
    show_event "Agent B" "$CYAN" "$line"
done &
MONITOR_B_PID=$!

sleep 1

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
    -H "Content-Type: application/json" \
    -d '{"message": "I need a Python function that calculates the factorial of a number. Please ask the Code Assistant to write it with error handling for negative numbers.", "stream": false}')

sleep 2
kill $MONITOR_A_PID $MONITOR_B_PID 2>/dev/null || true
pkill -P $MONITOR_A_PID 2>/dev/null || true
pkill -P $MONITOR_B_PID 2>/dev/null || true
sleep 0.5

echo ""
echo -e "${YELLOW}Agent A Final Response:${NC}"
if command -v jq &> /dev/null; then
    echo "$RESPONSE" | jq -r '.response' 2>/dev/null | head -30
else
    echo "$RESPONSE" | head -30
fi
echo ""
echo -e "${GREEN}✓ Demo 3 Complete${NC}"
echo ""

# Show event statistics
echo -e "${CYAN}[4/4] Event Statistics${NC}"
echo ""
echo -e "${YELLOW}Agent Communication Events:${NC}"
STATS=$(curl -s http://localhost:4015/events/stats)
if command -v jq &> /dev/null; then
    echo "$STATS" | jq '.'
else
    echo "$STATS"
fi
echo ""

# Summary
echo ""
echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║         Demo Complete! ✓               ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════╝${NC}"
echo ""
echo -e "${GREEN}What happened:${NC}"
echo "  1. Agent A (Research) received user requests"
echo "  2. Agent A used call_agent tool to delegate to Agent B"
echo "  3. Agent B (Code) responded with implementations"
echo "  4. Agent A returned results to user"
echo ""
echo -e "${YELLOW}Logs saved to:${NC}"
echo "  Agent A: $LOG_DIR/agent-a.log"
echo "  Agent B: $LOG_DIR/agent-b.log"
echo ""
echo -e "${CYAN}Try it yourself:${NC}"
echo "  1. Keep agents running: ./start-two-agents.sh"
echo "  2. Send requests: curl -X POST http://localhost:4015/chat \\"
echo "                      -H 'Content-Type: application/json' \\"
echo "                      -d '{\"message\": \"Your message\", \"stream\": false}'"
echo "  3. Monitor events: curl -N 'http://localhost:4015/events?category=AGENT'"
echo ""
echo -e "${YELLOW}Read more:${NC} scenarios/AGENT_COMMUNICATION.md"
echo ""
