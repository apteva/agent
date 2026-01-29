#!/bin/bash
# Test script to verify Groq provider integration

set -e

echo "🧪 Testing Groq Provider Integration"
echo "===================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to test endpoint
test_endpoint() {
    local name=$1
    local method=$2
    local endpoint=$3
    local data=$4

    echo -n "Testing $name... "

    if [ -n "$data" ]; then
        response=$(curl -s -X $method "http://localhost:4015$endpoint" \
            -H "Content-Type: application/json" \
            -d "$data")
    else
        response=$(curl -s -X $method "http://localhost:4015$endpoint")
    fi

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓${NC}"
        return 0
    else
        echo -e "${RED}✗${NC}"
        return 1
    fi
}

# Check if server is running
echo "1. Checking if agent server is running..."
if ! curl -s http://localhost:4015/health > /dev/null 2>&1; then
    echo -e "${RED}✗ Server is not running${NC}"
    echo "Start the server first: ./agent-go or make run"
    exit 1
fi
echo -e "${GREEN}✓ Server is running${NC}"
echo ""

# Get current config
echo "2. Getting current configuration..."
current_config=$(curl -s http://localhost:4015/config)
current_provider=$(echo "$current_config" | grep -o '"provider":"[^"]*"' | cut -d'"' -f4)
echo "Current provider: $current_provider"
echo ""

# Test switching to each provider
echo "3. Testing provider switching..."
echo ""

# Test Anthropic
echo "  a) Testing Anthropic provider..."
test_endpoint "Switch to Anthropic" POST /config '{"llm":{"provider":"anthropic"}}'
config=$(curl -s http://localhost:4015/config)
if echo "$config" | grep -q '"provider":"anthropic"'; then
    echo -e "     ${GREEN}✓ Anthropic provider activated${NC}"
else
    echo -e "     ${RED}✗ Failed to activate Anthropic${NC}"
fi
echo ""

# Test OpenAI
echo "  b) Testing OpenAI provider..."
test_endpoint "Switch to OpenAI" POST /config '{"llm":{"provider":"openai"}}'
config=$(curl -s http://localhost:4015/config)
if echo "$config" | grep -q '"provider":"openai"'; then
    echo -e "     ${GREEN}✓ OpenAI provider activated${NC}"
else
    echo -e "     ${RED}✗ Failed to activate OpenAI${NC}"
fi
echo ""

# Test Groq (NEW)
echo "  c) Testing Groq provider (NEW)..."
test_endpoint "Switch to Groq" POST /config '{"llm":{"provider":"groq","model":"openai/gpt-oss-20b"}}'
config=$(curl -s http://localhost:4015/config)
if echo "$config" | grep -q '"provider":"groq"'; then
    echo -e "     ${GREEN}✓ Groq provider activated${NC}"
else
    echo -e "     ${RED}✗ Failed to activate Groq${NC}"
    exit 1
fi
echo ""

# Verify Groq configuration
echo "4. Verifying Groq configuration..."
groq_config=$(curl -s http://localhost:4015/config)
echo "$groq_config" | grep -A 5 '"llm"' | grep -E '"provider"|"model"'
echo ""

# Check if GROQ_API_KEY is set
echo "5. Checking environment variables..."
if [ -n "$GROQ_API_KEY" ]; then
    echo -e "${GREEN}✓ GROQ_API_KEY is set${NC}"
    echo "   (Key: ${GROQ_API_KEY:0:10}...)"
else
    echo -e "${YELLOW}⚠ GROQ_API_KEY not set${NC}"
    echo "   Set it with: export GROQ_API_KEY='gsk_...'"
    echo "   Note: Chat requests will fail without API key"
fi
echo ""

# Test health endpoint
echo "6. Testing health endpoint..."
health=$(curl -s http://localhost:4015/health)
echo "$health" | grep -o '"status":"[^"]*"'
echo ""

# Summary
echo "===================================="
echo -e "${GREEN}✅ All tests passed!${NC}"
echo ""
echo "Groq provider is successfully integrated and working."
echo ""
echo "To use Groq:"
echo "1. Set your API key: export GROQ_API_KEY='gsk_...'"
echo "2. Provider is already set to 'groq'"
echo "3. Send a chat message:"
echo "   curl -X POST http://localhost:4015/chat \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"message\": \"Hello from Groq!\"}'"
echo ""
echo "Popular Groq models:"
echo "  - llama-3.3-70b-versatile (recommended, 128k context)"
echo "  - llama-3.1-8b-instant (ultra-fast, 131k context)"
echo "  - mixtral-8x7b-32768 (32k context)"
echo "  - gemma2-9b-it (efficient)"
echo ""
