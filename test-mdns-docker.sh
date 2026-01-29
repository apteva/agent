#!/bin/bash
# Test mDNS discovery in Docker

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  mDNS Discovery Docker Test                  ║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════════════╝${NC}"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    docker stop agent-test-a agent-test-b 2>/dev/null || true
    docker rm agent-test-a agent-test-b 2>/dev/null || true
    echo -e "${GREEN}✓ Cleanup complete${NC}"
}

trap cleanup EXIT

echo -e "${BLUE}Step 1: Starting Agent A in Docker${NC}"
docker run -d \
  --name agent-test-a \
  --network bridge \
  -p 4015:4015 \
  -e PORT=4015 \
  -e CONFIG_PATH=/app/config-agent-a.json \
  -v $(pwd)/scenarios/configs/agent-a-research.json:/app/config-agent-a.json:ro \
  agent-core:latest

sleep 3

if docker ps | grep -q agent-test-a; then
    echo -e "${GREEN}✓ Agent A running${NC}"
else
    echo -e "${RED}✗ Agent A failed to start${NC}"
    docker logs agent-test-a
    exit 1
fi
echo ""

echo -e "${BLUE}Step 2: Check Agent A's mDNS initialization${NC}"
docker logs agent-test-a 2>&1 | grep -i "mdns\|discovery" | head -10
echo ""

echo -e "${BLUE}Step 3: Starting Agent B in Docker${NC}"
docker run -d \
  --name agent-test-b \
  --network bridge \
  -p 4016:4016 \
  -e PORT=4016 \
  -e CONFIG_PATH=/app/config-agent-b.json \
  -v $(pwd)/scenarios/configs/agent-b-code.json:/app/config-agent-b.json:ro \
  agent-core:latest

sleep 3

if docker ps | grep -q agent-test-b; then
    echo -e "${GREEN}✓ Agent B running${NC}"
else
    echo -e "${RED}✗ Agent B failed to start${NC}"
    docker logs agent-test-b
    exit 1
fi
echo ""

echo -e "${BLUE}Step 4: Check Agent B's mDNS initialization${NC}"
docker logs agent-test-b 2>&1 | grep -i "mdns\|discovery" | head -10
echo ""

echo -e "${BLUE}Step 5: Waiting for mDNS discovery (5 seconds)${NC}"
echo "  - Initial discovery: immediate"
echo "  - Follow-up scan: 3 seconds"
echo "  - Total wait: 5 seconds"
sleep 5
echo ""

echo -e "${BLUE}Step 6: Check Agent A's discovery logs${NC}"
docker logs agent-test-a 2>&1 | grep "mDNS discovery: found" | tail -5
echo ""

echo -e "${BLUE}Step 7: Check Agent B's discovery logs${NC}"
docker logs agent-test-b 2>&1 | grep "mDNS discovery: found" | tail -5
echo ""

echo -e "${BLUE}Step 8: Test list_available_agents via API${NC}"
RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "List all available agents", "stream": false}')

echo "$RESPONSE" | jq -r '.response' 2>/dev/null | head -20
echo ""

if echo "$RESPONSE" | grep -qi "agent-code-001\|Code Assistant"; then
    echo -e "${GREEN}✓ Agent A discovered Agent B via mDNS!${NC}"
else
    echo -e "${RED}✗ Agent A did not discover Agent B${NC}"
    echo -e "${YELLOW}Response:${NC}"
    echo "$RESPONSE" | jq '.' 2>/dev/null || echo "$RESPONSE"
fi
echo ""

echo -e "${BLUE}Step 9: Test actual agent call${NC}"
CALL_RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Call agent-code-001 and ask what you can help with", "stream": false}')

echo "$CALL_RESPONSE" | jq -r '.response' 2>/dev/null | head -15
echo ""

if echo "$CALL_RESPONSE" | grep -qi "code\|python\|javascript"; then
    echo -e "${GREEN}✓ Agent A successfully called Agent B!${NC}"
else
    echo -e "${YELLOW}⚠  Call might have failed${NC}"
fi
echo ""

echo "══════════════════════════════════════════════"
echo -e "${GREEN}mDNS Test Complete!${NC}"
echo ""
echo "Diagnostics:"
echo "  View Agent A logs: docker logs agent-test-a"
echo "  View Agent B logs: docker logs agent-test-b"
echo "  Check Agent A config: curl http://localhost:4015/config | jq '.agents'"
echo "  Check Agent B config: curl http://localhost:4016/config | jq '.agents'"
echo ""
echo "Manual testing:"
echo "  curl -X POST http://localhost:4015/chat \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"message\": \"List available agents\", \"stream\": false}'"
echo ""
