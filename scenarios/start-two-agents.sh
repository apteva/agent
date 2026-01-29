#!/bin/bash

# Script to start two agents for agent-to-agent communication testing
# Agent A (Research) on port 4015
# Agent B (Code) on port 4016

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Get the directory of this script
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Load environment variables
if [ -f "$PROJECT_ROOT/.env" ]; then
    export $(grep -v '^#' "$PROJECT_ROOT/.env" | xargs)
    echo -e "${GREEN}✓ Loaded environment variables from .env${NC}"
else
    echo -e "${YELLOW}⚠ No .env file found - API keys may be missing${NC}"
fi
echo ""

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Starting Two-Agent Test Environment${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if already running
if lsof -Pi :4015 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo -e "${RED}Error: Port 4015 is already in use${NC}"
    echo "Kill the process with: kill \$(lsof -ti:4015)"
    exit 1
fi

if lsof -Pi :4016 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
    echo -e "${RED}Error: Port 4016 is already in use${NC}"
    echo "Kill the process with: kill \$(lsof -ti:4016)"
    exit 1
fi

# Create log directory
LOG_DIR="$SCRIPT_DIR/logs"
mkdir -p "$LOG_DIR"

# Start Agent B (Code Assistant) on port 4016 first
echo -e "${YELLOW}Starting Agent B (Code Assistant) on port 4016...${NC}"
cd "$PROJECT_ROOT"
CONFIG_PATH="$SCRIPT_DIR/configs/agent-b-code.json" \
PORT=4016 \
DB_PATH="$SCRIPT_DIR/agent-b.db" \
go run main.go > "$LOG_DIR/agent-b.log" 2>&1 &
AGENT_B_PID=$!
echo -e "${GREEN}Agent B started with PID: $AGENT_B_PID${NC}"

# Wait for Agent B to be ready
echo -n "Waiting for Agent B to be ready"
for i in {1..30}; do
    if curl -s http://localhost:4016/health > /dev/null 2>&1; then
        echo -e " ${GREEN}✓${NC}"
        break
    fi
    echo -n "."
    sleep 1
    if [ $i -eq 30 ]; then
        echo -e " ${RED}✗${NC}"
        echo -e "${RED}Agent B failed to start${NC}"
        kill $AGENT_B_PID 2>/dev/null || true
        exit 1
    fi
done

# Start Agent A (Research Assistant) on port 4015
echo -e "${YELLOW}Starting Agent A (Research Assistant) on port 4015...${NC}"
CONFIG_PATH="$SCRIPT_DIR/configs/agent-a-research.json" \
PORT=4015 \
DB_PATH="$SCRIPT_DIR/agent-a.db" \
go run main.go > "$LOG_DIR/agent-a.log" 2>&1 &
AGENT_A_PID=$!
echo -e "${GREEN}Agent A started with PID: $AGENT_A_PID${NC}"

# Wait for Agent A to be ready
echo -n "Waiting for Agent A to be ready"
for i in {1..30}; do
    if curl -s http://localhost:4015/health > /dev/null 2>&1; then
        echo -e " ${GREEN}✓${NC}"
        break
    fi
    echo -n "."
    sleep 1
    if [ $i -eq 30 ]; then
        echo -e " ${RED}✗${NC}"
        echo -e "${RED}Agent A failed to start${NC}"
        kill $AGENT_A_PID 2>/dev/null || true
        kill $AGENT_B_PID 2>/dev/null || true
        exit 1
    fi
done

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Both Agents Running Successfully!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}Agent A (Research):${NC} http://localhost:4015"
echo -e "${BLUE}Agent B (Code):${NC}     http://localhost:4016"
echo ""
echo -e "${YELLOW}PIDs:${NC}"
echo "  Agent A: $AGENT_A_PID"
echo "  Agent B: $AGENT_B_PID"
echo ""
echo -e "${YELLOW}Logs:${NC}"
echo "  Agent A: $LOG_DIR/agent-a.log"
echo "  Agent B: $LOG_DIR/agent-b.log"
echo ""
echo -e "${BLUE}Monitor Agent A events:${NC}"
echo "  curl -N 'http://localhost:4015/events?category=AGENT'"
echo ""
echo -e "${BLUE}Test agent communication:${NC}"
echo "  curl -X POST http://localhost:4015/chat \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"message\": \"Can you ask the Code Assistant to write a Python function that adds two numbers?\"}'"
echo ""
echo -e "${RED}To stop both agents:${NC}"
echo "  kill $AGENT_A_PID $AGENT_B_PID"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop all agents and exit${NC}"
echo ""

# Save PIDs to file for easy cleanup
echo "$AGENT_A_PID $AGENT_B_PID" > "$SCRIPT_DIR/.agent-pids"

# Function to cleanup on exit
cleanup() {
    echo ""
    echo -e "${YELLOW}Stopping agents...${NC}"
    kill $AGENT_A_PID 2>/dev/null || true
    kill $AGENT_B_PID 2>/dev/null || true
    rm -f "$SCRIPT_DIR/.agent-pids"
    echo -e "${GREEN}Agents stopped${NC}"
}

trap cleanup EXIT INT TERM

# Keep script running and tail both logs
echo -e "${BLUE}Tailing logs (Ctrl+C to stop):${NC}"
echo ""
tail -f "$LOG_DIR/agent-a.log" "$LOG_DIR/agent-b.log"
