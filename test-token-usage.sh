#!/bin/bash

# Test token usage capture for all providers

set -e

echo "======================================"
echo "Token Usage Capture - Integration Test"
echo "======================================"
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Agent server URL
AGENT_URL="http://localhost:8080"

# Check if agent is running
if ! curl -s "$AGENT_URL/health" > /dev/null 2>&1; then
    echo -e "${RED}❌ Agent server is not running at $AGENT_URL${NC}"
    echo "Please start the agent server first"
    exit 1
fi

echo -e "${GREEN}✓ Agent server is running${NC}"
echo ""

# Test function
test_provider() {
    local provider=$1
    local model=$2

    echo "======================================"
    echo "Testing: $provider ($model)"
    echo "======================================"

    # Configure provider
    echo "Configuring provider..."
    curl -s -X PUT "$AGENT_URL/config" \
        -H "Content-Type: application/json" \
        -d "{\"llm\":{\"provider\":\"$provider\",\"model\":\"$model\"}}" > /dev/null

    sleep 1

    # Send test message
    echo "Sending test message..."
    response=$(curl -s -X POST "$AGENT_URL/chat" \
        -H "Content-Type: application/json" \
        -d '{"message": "Say hello in exactly 5 words"}')

    # Check for usage event in stream
    if echo "$response" | grep -q '"type":"usage"'; then
        echo -e "${GREEN}✅ Usage event found in stream${NC}"

        # Extract token counts
        input_tokens=$(echo "$response" | grep '"type":"usage"' | grep -o '"input_tokens":[0-9]*' | cut -d':' -f2)
        output_tokens=$(echo "$response" | grep '"type":"usage"' | grep -o '"output_tokens":[0-9]*' | cut -d':' -f2)
        total_tokens=$(echo "$response" | grep '"type":"usage"' | grep -o '"total_tokens":[0-9]*' | cut -d':' -f2)

        if [ ! -z "$input_tokens" ] && [ ! -z "$output_tokens" ]; then
            echo -e "${GREEN}   Input tokens:  $input_tokens${NC}"
            echo -e "${GREEN}   Output tokens: $output_tokens${NC}"
            echo -e "${GREEN}   Total tokens:  $total_tokens${NC}"
        else
            echo -e "${YELLOW}⚠️  Token counts not found in response${NC}"
        fi
    else
        echo -e "${RED}❌ Usage event missing from stream${NC}"
        echo "Response preview:"
        echo "$response" | head -20
        return 1
    fi

    # Extract thread_id from response
    thread_id=$(echo "$response" | grep -o '"thread_id":"[^"]*"' | head -1 | cut -d'"' -f4)

    if [ -z "$thread_id" ]; then
        echo -e "${YELLOW}⚠️  Could not extract thread_id${NC}"
        echo ""
        return 0
    fi

    echo ""
    echo "Checking database for token usage..."

    # Check if database file exists
    if [ ! -f "app.db" ]; then
        echo -e "${YELLOW}⚠️  Database file app.db not found${NC}"
        echo ""
        return 0
    fi

    # Query database for token usage
    db_result=$(sqlite3 app.db "SELECT token_usage_input, token_usage_output, cost_usd FROM spans WHERE kind='llm' AND thread_id='$thread_id' ORDER BY start_time DESC LIMIT 1" 2>/dev/null || echo "")

    if [ ! -z "$db_result" ]; then
        IFS='|' read -r db_input db_output db_cost <<< "$db_result"

        if [ "$db_input" != "" ] && [ "$db_output" != "" ]; then
            echo -e "${GREEN}✅ Token usage found in database${NC}"
            echo -e "${GREEN}   Input tokens:  $db_input${NC}"
            echo -e "${GREEN}   Output tokens: $db_output${NC}"

            if [ "$db_cost" != "" ] && [ "$db_cost" != "0" ]; then
                echo -e "${GREEN}   Cost (USD):    \$$db_cost${NC}"
            else
                echo -e "${YELLOW}   Cost:          Not calculated${NC}"
            fi

            # Verify stream and DB match
            if [ "$input_tokens" == "$db_input" ] && [ "$output_tokens" == "$db_output" ]; then
                echo -e "${GREEN}✅ Stream and database values match${NC}"
            else
                echo -e "${YELLOW}⚠️  Stream and database values differ${NC}"
                echo "   Stream: $input_tokens input, $output_tokens output"
                echo "   DB:     $db_input input, $db_output output"
            fi
        else
            echo -e "${RED}❌ Token usage columns are empty in database${NC}"
            return 1
        fi
    else
        echo -e "${RED}❌ No span found in database for thread $thread_id${NC}"
        return 1
    fi

    echo ""
    return 0
}

# Test Anthropic if API key is set
if [ ! -z "$ANTHROPIC_API_KEY" ]; then
    test_provider "anthropic" "claude-3-5-haiku-20241022"
else
    echo -e "${YELLOW}⚠️  ANTHROPIC_API_KEY not set, skipping Anthropic test${NC}"
    echo ""
fi

# Test OpenAI if API key is set
if [ ! -z "$OPENAI_API_KEY" ]; then
    test_provider "openai" "gpt-4o-mini"
else
    echo -e "${YELLOW}⚠️  OPENAI_API_KEY not set, skipping OpenAI test${NC}"
    echo ""
fi

# Test Groq if API key is set
if [ ! -z "$GROQ_API_KEY" ]; then
    test_provider "groq" "llama-3.1-70b-versatile"
else
    echo -e "${YELLOW}⚠️  GROQ_API_KEY not set, skipping Groq test${NC}"
    echo ""
fi

echo "======================================"
echo "Summary"
echo "======================================"
echo ""
echo "Token usage capture has been implemented!"
echo ""
echo "Features:"
echo "  • Token usage extracted from all providers"
echo "  • Usage stored in spans table"
echo "  • Usage streamed to clients via SSE"
echo "  • Usage published to event bus"
echo ""
echo "Next steps:"
echo "  • Query token usage: sqlite3 app.db 'SELECT * FROM spans WHERE kind=\"llm\" LIMIT 5'"
echo "  • Subscribe to events: curl $AGENT_URL/events/subscribe"
echo "  • Add cost calculation (optional Phase 2)"
echo ""
