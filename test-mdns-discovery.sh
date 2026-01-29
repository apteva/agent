#!/bin/bash
# Test mDNS-based agent discovery

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  mDNS Agent Discovery Test                  ║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════════════╝${NC}"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    pkill -f "PORT=4015" || true
    pkill -f "PORT=4016" || true
    sleep 2
    echo -e "${GREEN}✓ Cleanup complete${NC}"
}

trap cleanup EXIT

# Kill any existing agents on ports
pkill -f "PORT=4015" || true
pkill -f "PORT=4016" || true
sleep 2

echo -e "${BLUE}Step 1: Starting Agent A (Coordinator)${NC}"
echo "  - Port: 4015"
echo "  - Group: production"
echo "  - Config: config-agent-a.json"
echo ""

PORT=4015 CONFIG_PATH=config-agent-a.json ./agent-core > agent-a.log 2>&1 &
AGENT_A_PID=$!

sleep 4

if curl -s http://localhost:4015/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Agent A started successfully${NC}"
else
    echo -e "${RED}✗ Agent A failed to start${NC}"
    cat agent-a.log
    exit 1
fi
echo ""

echo -e "${BLUE}Step 2: Starting Agent B (Worker)${NC}"
echo "  - Port: 4016"
echo "  - Group: production"
echo "  - Config: config-agent-b.json"
echo ""

PORT=4016 CONFIG_PATH=config-agent-b.json ./agent-core > agent-b.log 2>&1 &
AGENT_B_PID=$!

sleep 4

if curl -s http://localhost:4016/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Agent B started successfully${NC}"
else
    echo -e "${RED}✗ Agent B failed to start${NC}"
    cat agent-b.log
    exit 1
fi
echo ""

echo -e "${BLUE}Step 3: Waiting for mDNS discovery (30s for first discovery cycle)${NC}"
echo "  Discovery happens automatically via mDNS broadcast..."
sleep 35
echo -e "${GREEN}✓ Discovery period complete${NC}"
echo ""

echo -e "${BLUE}Step 4: Checking Agent A's logs for discovery${NC}"
echo ""
if grep -q "mDNS discovery: found" agent-a.log; then
    echo -e "${GREEN}✓ Agent A performed discovery${NC}"
    grep "mDNS discovery: found" agent-a.log | tail -2
    echo ""
    grep "agent-worker-001" agent-a.log | tail -1
else
    echo -e "${YELLOW}⚠  No discovery messages in Agent A logs${NC}"
    echo "Recent logs:"
    tail -20 agent-a.log
fi
echo ""

echo -e "${BLUE}Step 5: Checking Agent B's logs for discovery${NC}"
echo ""
if grep -q "mDNS discovery: found" agent-b.log; then
    echo -e "${GREEN}✓ Agent B performed discovery${NC}"
    grep "mDNS discovery: found" agent-b.log | tail -2
    echo ""
    grep "agent-coordinator-001" agent-b.log | tail -1
else
    echo -e "${YELLOW}⚠  No discovery messages in Agent B logs${NC}"
    echo "Recent logs:"
    tail -20 agent-b.log
fi
echo ""

echo -e "${BLUE}Step 6: Testing list_available_agents on Agent A${NC}"
RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "List all available agents you can work with", "stream": false}')

echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
echo ""

if echo "$RESPONSE" | grep -qi "agent-worker-001\|worker"; then
    echo -e "${GREEN}✓ Agent A can see Agent B!${NC}"
else
    echo -e "${YELLOW}⚠  Agent A might not have discovered Agent B yet${NC}"
    echo -e "${YELLOW}   Check logs for mDNS issues${NC}"
fi
echo ""

echo -e "${BLUE}Step 7: Testing call_agent from A to B${NC}"
RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Call agent-worker-001 and ask what they can help with", "stream": false}')

echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
echo ""

if echo "$RESPONSE" | grep -qi "success\|help"; then
    echo -e "${GREEN}✓ Agent A successfully called Agent B!${NC}"
else
    echo -e "${YELLOW}⚠  Call might have failed${NC}"
fi
echo ""

echo "═══════════════════════════════════════════════"
echo -e "${GREEN}Test Complete!${NC}"
echo ""
echo "Review logs:"
echo "  Agent A: tail -f agent-a.log"
echo "  Agent B: tail -f agent-b.log"
echo ""
echo "Check discovery status:"
echo "  curl http://localhost:4015/config | jq '.agents'"
echo "  curl http://localhost:4016/config | jq '.agents'"
echo ""
