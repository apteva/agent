#!/bin/bash

# Demo: Multi-Agent with MCP Test Mode
# Coordinator Agent (no MCP) → Worker Agent (MCP test mode) → Mock notification

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

echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Multi-Agent MCP Test Mode Demo                   ║${NC}"
echo -e "${BLUE}║  Coordinator → Worker (MCP test_mode=true)        ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    # Kill monitor processes first
    if [ -n "$MONITOR_COORD_PID" ]; then
        kill -9 $MONITOR_COORD_PID 2>/dev/null || true
        pkill -9 -P $MONITOR_COORD_PID 2>/dev/null || true
    fi
    if [ -n "$MONITOR_WORKER_PID" ]; then
        kill -9 $MONITOR_WORKER_PID 2>/dev/null || true
        pkill -9 -P $MONITOR_WORKER_PID 2>/dev/null || true
    fi
    # Kill agent processes
    if [ -n "$WORKER_PID" ]; then
        kill -9 $WORKER_PID 2>/dev/null || true
    fi
    if [ -n "$COORDINATOR_PID" ]; then
        kill -9 $COORDINATOR_PID 2>/dev/null || true
    fi
    # Force kill any remaining processes on these ports
    lsof -ti:4015,4016 | xargs kill -9 2>/dev/null || true
    # Also kill any go run processes that might be stuck
    pkill -9 -f "CONFIG_PATH.*scenarios" 2>/dev/null || true
    sleep 1
    echo -e "${GREEN}Cleanup complete${NC}"
}

trap cleanup EXIT INT TERM

# Create log directory
mkdir -p "$LOG_DIR"

# Kill any existing processes
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

# Note: Assumes MCP API and other services are already running
echo -e "${CYAN}[0/3] Prerequisites check...${NC}"
echo -e "${YELLOW}Note: Assuming API Gateway and MCP services are running${NC}"
echo ""

# Start Worker Agent (with MCP test mode)
echo -e "${CYAN}[1/3] Starting Worker Agent (MCP test mode) on port 4016...${NC}"
cd "$PROJECT_ROOT"
CONFIG_PATH="$SCRIPT_DIR/configs/agent-worker-mcp-test.json" \
PORT=4016 \
DB_PATH="$SCRIPT_DIR/worker.db" \
go run main.go > "$LOG_DIR/worker.log" 2>&1 &
WORKER_PID=$!

# Wait for Worker
echo -n "      Waiting for Worker Agent"
for i in {1..30}; do
    if curl -s http://localhost:4016/health > /dev/null 2>&1; then
        echo -e " ${GREEN}✓${NC}"
        break
    fi
    echo -n "."
    sleep 1
    if [ $i -eq 30 ]; then
        echo -e " ${RED}✗ Failed${NC}"
        cat "$LOG_DIR/worker.log"
        exit 1
    fi
done

# Start Coordinator Agent (no MCP)
echo -e "${CYAN}[2/3] Starting Coordinator Agent (no MCP) on port 4015...${NC}"
CONFIG_PATH="$SCRIPT_DIR/configs/agent-coordinator-mcp-test.json" \
PORT=4015 \
DB_PATH="$SCRIPT_DIR/coordinator.db" \
go run main.go > "$LOG_DIR/coordinator.log" 2>&1 &
COORDINATOR_PID=$!

# Wait for Coordinator
echo -n "      Waiting for Coordinator Agent"
for i in {1..30}; do
    if curl -s http://localhost:4015/health > /dev/null 2>&1; then
        echo -e " ${GREEN}✓${NC}"
        break
    fi
    echo -n "."
    sleep 1
    if [ $i -eq 30 ]; then
        echo -e " ${RED}✗ Failed${NC}"
        cat "$LOG_DIR/coordinator.log"
        exit 1
    fi
done

echo ""
echo -e "${GREEN}✓ Both agents started successfully${NC}"
echo ""
echo -e "${CYAN}Agent Setup:${NC}"
echo -e "  ${GREEN}Coordinator Agent (Port 4015):${NC}"
echo "    - NO MCP tools"
echo "    - Can delegate to Worker"
echo "    - Tools: get_time, call_agent, list_available_agents"
echo ""
echo -e "  ${CYAN}Worker Agent (Port 4016):${NC}"
echo "    - MCP enabled with ${YELLOW}test_mode=true${NC}"
echo "    - Has send-notification MCP tool"
echo "    - Will return MOCK responses (no credentials needed)"
echo ""

sleep 2

# Function to format and display SSE events
format_event() {
    local agent=$1
    local color=$2
    local line=$3

    # Parse JSON event data
    if [[ "$line" =~ ^data:\ \{.*\}$ ]]; then
        data="${line#data: }"

        type=$(echo "$data" | grep -o '"type":"[^"]*"' | head -1 | cut -d'"' -f4)
        category=$(echo "$data" | grep -o '"category":"[^"]*"' | cut -d'"' -f4)
        tool_name=$(echo "$data" | grep -o '"tool_name":"[^"]*"' | cut -d'"' -f4)
        # Try multiple field names for agent names
        agent_name=$(echo "$data" | grep -o '"target_agent_name":"[^"]*"' | cut -d'"' -f4)
        if [ -z "$agent_name" ]; then
            agent_name=$(echo "$data" | grep -o '"target_agent":"[^"]*"' | cut -d'"' -f4)
        fi
        if [ -z "$agent_name" ]; then
            agent_name=$(echo "$data" | grep -o '"agent_name":"[^"]*"' | cut -d'"' -f4)
        fi
        duration=$(echo "$data" | grep -o '"duration_ms":[0-9]*' | cut -d':' -f2)
        success=$(echo "$data" | grep -o '"success":[^,}]*' | cut -d':' -f2)

        case "$type" in
            message_received)
                echo -e "  ${color}[${agent}]${NC} 📨 Message received"
                ;;
            agent_call_started)
                echo -e "  ${color}[${agent}]${NC} 🚀 Calling ${agent_name}..."
                ;;
            agent_call_completed)
                echo -e "  ${color}[${agent}]${NC} ✅ Agent call complete: ${agent_name} (${duration}ms)"
                ;;
            tool_invocation)
                if [[ "$tool_name" == "send-notification" ]]; then
                    echo -e "  ${color}[${agent}]${NC} 🔧 Tool: ${tool_name} (via MCP)"
                else
                    echo -e "  ${color}[${agent}]${NC} 🔧 Tool: ${tool_name}"
                fi
                ;;
            tool_result)
                if [[ "$data" =~ "_mock" ]]; then
                    echo -e "  ${color}[${agent}]${NC}    └─ ${tool_name} → MOCK response"
                else
                    echo -e "  ${color}[${agent}]${NC}    └─ ${tool_name} → success"
                fi
                ;;
            mcp_tool_execution)
                # Only show if this is the result (has duration), not the start
                if [ -n "$duration" ]; then
                    if [[ "$data" =~ "_mock.*true" ]] || [[ "$success" == "true" ]]; then
                        echo -e "  ${color}[${agent}]${NC}    └─ 🔌 MCP ${tool_name} → MOCK (${duration}ms)"
                    else
                        echo -e "  ${color}[${agent}]${NC}    └─ 🔌 MCP ${tool_name} (${duration}ms)"
                    fi
                fi
                ;;
            response_complete)
                echo -e "  ${color}[${agent}]${NC} ✅ Response sent to user"
                ;;
        esac
    fi
}

# Run the test scenario
echo -e "${CYAN}[3/3] Running Test Scenario${NC}"
echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${YELLOW}Test: MCP Tool Call via Agent Delegation${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "${CYAN}User → Coordinator:${NC}"
echo "  'Please ask the Notification Worker to send a notification'"
echo ""
echo -e "${CYAN}Live Event Stream:${NC}"

# Monitor SSE events from both agents
(
    curl -N -s "http://localhost:4015/events?category=AGENT,TOOL,MCP,CHAT" | while IFS= read -r line; do
        format_event "Coordinator" "$GREEN" "$line"
    done
) &
MONITOR_COORD_PID=$!

(
    curl -N -s "http://localhost:4016/events?category=AGENT,TOOL,MCP,CHAT" | while IFS= read -r line; do
        format_event "Worker" "$CYAN" "$line"
    done
) &
MONITOR_WORKER_PID=$!

# Give monitors time to start
sleep 1

# Send the test request
RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
    -H "Content-Type: application/json" \
    -d '{
        "message": "Please ask the Notification Worker to send a notification with the message: MCP Test Mode Demo - Everything Working!",
        "stream": false
    }')

# Wait for events to display (need enough time for full delegation flow)
sleep 5

# Kill monitors
kill $MONITOR_COORD_PID $MONITOR_WORKER_PID 2>/dev/null || true
pkill -P $MONITOR_COORD_PID 2>/dev/null || true
pkill -P $MONITOR_WORKER_PID 2>/dev/null || true
sleep 0.5

echo ""
echo -e "${GREEN}✓ Initial test completed${NC}"
echo ""

# Check Worker logs for MCP activity
echo -e "${YELLOW}Verification:${NC}"
if grep -q "_mock.*true" "$LOG_DIR/worker.log"; then
    echo -e "  ${GREEN}✓${NC} Mock response received from MCP (test_mode working!)"
else
    echo -e "  ${YELLOW}⚠${NC} Check logs for mock responses"
fi
echo ""

# Summary
echo ""
echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║         Agents Running - Ready for Testing         ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${GREEN}✓ Both agents are running and ready${NC}"
echo ""
echo -e "${YELLOW}Agent Endpoints:${NC}"
echo "  Coordinator: http://localhost:4015"
echo "  Worker:      http://localhost:4016"
echo ""
echo -e "${CYAN}Try These Commands:${NC}"
echo ""
echo "  ${YELLOW}# Send a test notification${NC}"
echo "  curl -X POST http://localhost:4015/chat \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"message\": \"Send a notification saying Hello World\", \"stream\": false}'"
echo ""
echo "  ${YELLOW}# Watch events in real-time${NC}"
echo "  curl -N 'http://localhost:4015/events?category=AGENT,TOOL,MCP'"
echo ""
echo "  ${YELLOW}# Direct Worker test${NC}"
echo "  curl -X POST http://localhost:4016/chat \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"message\": \"Send notification: Direct test\", \"stream\": false}'"
echo ""
echo -e "${YELLOW}Logs:${NC}"
echo "  tail -f $LOG_DIR/coordinator.log"
echo "  tail -f $LOG_DIR/worker.log"
echo ""
echo -e "${RED}Press Ctrl+C to stop agents and cleanup${NC}"
echo ""

# Wait indefinitely until user interrupts
wait
