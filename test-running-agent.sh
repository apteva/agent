#!/bin/bash
# Test a running agent container

PORT=${1:-32729}

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Testing Agent on Port $PORT                  ${NC}"
echo -e "${BLUE}╚══════════════════════════════════════════════╝${NC}"
echo ""

# Test 1: Health check
echo -e "${BLUE}Test 1: Health Check${NC}"
HEALTH=$(curl -s http://localhost:$PORT/health 2>/dev/null)
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Agent is responding${NC}"
    echo "$HEALTH" | jq '.' 2>/dev/null || echo "$HEALTH"

    # Extract version
    VERSION=$(echo "$HEALTH" | jq -r '.version' 2>/dev/null)
    if [ ! -z "$VERSION" ]; then
        echo -e "${GREEN}  Version: $VERSION${NC}"
    fi
else
    echo -e "${RED}✗ Agent is not responding on port $PORT${NC}"
    exit 1
fi
echo ""

# Test 2: Config check
echo -e "${BLUE}Test 2: Configuration${NC}"
CONFIG=$(curl -s http://localhost:$PORT/config 2>/dev/null)
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Config endpoint working${NC}"

    # Show key config details
    echo "Agent Info:"
    echo "$CONFIG" | jq '{id: .id, name: .name, version: .version, model: .llm.model}' 2>/dev/null
    echo ""

    # Check agent communication settings
    echo "Agent Communication:"
    echo "$CONFIG" | jq '.agents' 2>/dev/null
else
    echo -e "${RED}✗ Config endpoint failed${NC}"
fi
echo ""

# Test 3: Simple chat
echo -e "${BLUE}Test 3: Simple Chat (Non-streaming)${NC}"
CHAT_RESPONSE=$(curl -s -X POST http://localhost:$PORT/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello! Just say hi back.", "stream": false}' 2>/dev/null)

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Chat endpoint working${NC}"
    echo "$CHAT_RESPONSE" | jq '.' 2>/dev/null || echo "$CHAT_RESPONSE"
else
    echo -e "${RED}✗ Chat endpoint failed${NC}"
fi
echo ""

# Test 4: Check if agent discovery is enabled
echo -e "${BLUE}Test 4: Agent Discovery Status${NC}"
AGENTS_ENABLED=$(echo "$CONFIG" | jq -r '.agents.enabled' 2>/dev/null)
if [ "$AGENTS_ENABLED" = "true" ]; then
    echo -e "${GREEN}✓ Agent discovery is enabled${NC}"

    GROUP=$(echo "$CONFIG" | jq -r '.agents.group' 2>/dev/null)
    echo "  Group: $GROUP"

    # Try to list available agents
    echo ""
    echo "Testing list_available_agents tool:"
    LIST_RESPONSE=$(curl -s -X POST http://localhost:$PORT/chat \
      -H "Content-Type: application/json" \
      -d '{"message": "List all available agents you can communicate with", "stream": false}' 2>/dev/null)

    echo "$LIST_RESPONSE" | jq -r '.response' 2>/dev/null | head -20
else
    echo -e "${YELLOW}⚠  Agent discovery is disabled${NC}"
fi
echo ""

# Test 5: Docker container info (if we can find it)
echo -e "${BLUE}Test 5: Docker Container Info${NC}"
CONTAINER=$(docker ps --format '{{.ID}}\t{{.Ports}}' | grep ":$PORT->" | awk '{print $1}')
if [ ! -z "$CONTAINER" ]; then
    echo -e "${GREEN}✓ Found container: $CONTAINER${NC}"
    echo ""
    docker inspect $CONTAINER --format '
Container Details:
  Name: {{.Name}}
  Image: {{.Config.Image}}
  Status: {{.State.Status}}
  Started: {{.State.StartedAt}}
  Network: {{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}
' 2>/dev/null

    echo ""
    echo "Recent logs (last 20 lines):"
    echo "────────────────────────────────────────"
    docker logs --tail 20 $CONTAINER 2>&1
else
    echo -e "${YELLOW}⚠  Could not find Docker container on port $PORT${NC}"
fi
echo ""

echo "══════════════════════════════════════════════"
echo -e "${GREEN}Test Complete!${NC}"
echo ""
echo "Useful commands:"
echo "  View logs: docker logs -f \$(docker ps -q --filter publish=$PORT)"
echo "  Check config: curl http://localhost:$PORT/config | jq ."
echo "  Health check: curl http://localhost:$PORT/health | jq ."
echo "  Chat test: curl -X POST http://localhost:$PORT/chat \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"message\": \"Hello\", \"stream\": false}'"
echo ""
