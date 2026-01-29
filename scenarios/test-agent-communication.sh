#!/bin/bash

# Test script for agent-to-agent communication
# Sends requests to Agent A which should delegate to Agent B

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

AGENT_A_URL="http://localhost:4015"
AGENT_B_URL="http://localhost:4016"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Agent-to-Agent Communication Tests${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if agents are running
check_agent() {
    local url=$1
    local name=$2
    if ! curl -s "$url/health" > /dev/null 2>&1; then
        echo -e "${RED}✗ $name is not running at $url${NC}"
        echo -e "${YELLOW}Start agents with: ./start-two-agents.sh${NC}"
        exit 1
    fi
    echo -e "${GREEN}✓ $name is running${NC}"
}

echo "Checking agents..."
check_agent "$AGENT_A_URL" "Agent A (Research)"
check_agent "$AGENT_B_URL" "Agent B (Code)"
echo ""

# Test 1: List available agents
echo -e "${CYAN}Test 1: Agent A lists available agents${NC}"
echo "Request: 'What other agents can you work with?'"
echo ""

RESPONSE=$(curl -s -X POST "$AGENT_A_URL/chat" \
    -H "Content-Type: application/json" \
    -d '{"message": "List the available agents you can communicate with", "stream": false}')

echo -e "${YELLOW}Response:${NC}"
echo "$RESPONSE" | jq -r '.response' 2>/dev/null || echo "$RESPONSE"
echo ""
echo -e "${GREEN}✓ Test 1 Complete${NC}"
echo ""
echo "---"
echo ""

# Test 2: Agent A delegates to Agent B
echo -e "${CYAN}Test 2: Agent A delegates coding task to Agent B${NC}"
echo "Request: 'Ask the Code Assistant to write a Python function that calculates factorial'"
echo ""

RESPONSE=$(curl -s -X POST "$AGENT_A_URL/chat" \
    -H "Content-Type: application/json" \
    -d '{"message": "Please ask the Code Assistant to write a Python function that calculates the factorial of a number. Make sure it includes error handling.", "stream": false}')

THREAD_ID=$(echo "$RESPONSE" | jq -r '.thread_id' 2>/dev/null)
echo -e "${YELLOW}Thread ID:${NC} $THREAD_ID"
echo ""
echo -e "${YELLOW}Response:${NC}"
echo "$RESPONSE" | jq -r '.response' 2>/dev/null || echo "$RESPONSE"
echo ""
echo -e "${GREEN}✓ Test 2 Complete${NC}"
echo ""
echo "---"
echo ""

# Test 3: Check Agent A's event log for agent communication
echo -e "${CYAN}Test 3: Check Agent A's event logs${NC}"
echo "Fetching recent AGENT events..."
echo ""

EVENTS=$(curl -s "$AGENT_A_URL/events/stats")
echo -e "${YELLOW}Event Bus Stats:${NC}"
echo "$EVENTS" | jq '.' 2>/dev/null || echo "$EVENTS"
echo ""
echo -e "${GREEN}✓ Test 3 Complete${NC}"
echo ""
echo "---"
echo ""

# Test 4: Direct test of Agent B (verify it responds without agent communication)
echo -e "${CYAN}Test 4: Direct request to Agent B (no agent tools)${NC}"
echo "Request: 'Write a simple hello world in Python'"
echo ""

RESPONSE=$(curl -s -X POST "$AGENT_B_URL/chat" \
    -H "Content-Type: application/json" \
    -d '{"message": "Write a simple hello world program in Python", "stream": false}')

echo -e "${YELLOW}Response:${NC}"
echo "$RESPONSE" | jq -r '.response' 2>/dev/null || echo "$RESPONSE"
echo ""
echo -e "${GREEN}✓ Test 4 Complete${NC}"
echo ""
echo "---"
echo ""

# Test 5: Complex delegation scenario
echo -e "${CYAN}Test 5: Complex scenario - Research + Code Implementation${NC}"
echo "Request: 'Design a data processing pipeline and have it implemented'"
echo ""

RESPONSE=$(curl -s -X POST "$AGENT_A_URL/chat" \
    -H "Content-Type: application/json" \
    -d '{"message": "I need to process CSV data. First analyze what we need, then ask the Code Assistant to implement a Python function that: 1) Reads a CSV file, 2) Filters rows where amount > 100, 3) Groups by category and sums the amounts. Keep it simple.", "stream": false}')

echo -e "${YELLOW}Response:${NC}"
echo "$RESPONSE" | jq -r '.response' 2>/dev/null || echo "$RESPONSE"
echo ""
echo -e "${GREEN}✓ Test 5 Complete${NC}"
echo ""

# Summary
echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}All Tests Complete!${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${YELLOW}Monitor live agent communication events:${NC}"
echo "  curl -N '$AGENT_A_URL/events?category=AGENT'"
echo ""
echo -e "${YELLOW}View Agent A messages:${NC}"
echo "  curl -s '$AGENT_A_URL/threads' | jq '.'"
echo ""
echo -e "${YELLOW}View Agent B messages:${NC}"
echo "  curl -s '$AGENT_B_URL/threads' | jq '.'"
echo ""
