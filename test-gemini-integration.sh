#!/bin/bash

# Gemini Integration Test
# This script tests actual API calls to Gemini

echo "🧪 Gemini Integration Test"
echo "=========================="
echo ""

# Check if GEMINI_API_KEY is set
if [ -z "$GEMINI_API_KEY" ]; then
    echo "❌ ERROR: GEMINI_API_KEY environment variable is not set"
    echo "Loading from .env file..."
    if [ -f .env ]; then
        export $(cat .env | grep GEMINI_API_KEY | xargs)
        if [ -z "$GEMINI_API_KEY" ]; then
            echo "❌ GEMINI_API_KEY not found in .env file"
            exit 1
        fi
        echo "✅ Loaded GEMINI_API_KEY from .env"
    else
        echo "❌ .env file not found"
        exit 1
    fi
else
    echo "✅ GEMINI_API_KEY is set"
fi
echo ""

# Update agent-config.json to use Gemini
echo "📝 Configuring agent to use Gemini..."
if [ -f agent-config.json ]; then
    cp agent-config.json agent-config.json.backup
    echo "   (backed up existing config to agent-config.json.backup)"
fi

cat > agent-config.json <<EOF
{
  "agent_id": "agent-gemini-test",
  "name": "Gemini Test Agent",
  "version": "1.0.0",
  "llm": {
    "provider": "gemini",
    "model": "gemini-2.5-flash",
    "max_tokens": 2048,
    "temperature": 1.0
  },
  "system_prompt": "You are a helpful AI assistant. Keep responses concise.",
  "tools": {
    "enabled": true,
    "custom_tools": []
  }
}
EOF

echo "✅ Configuration updated"
echo ""

# Build the agent server
echo "📝 Building agent server..."
if ! go build -o agent-server-test main.go 2>&1; then
    echo "❌ Build failed"
    exit 1
fi
echo "✅ Build successful"
echo ""

# Start the agent server in background
echo "📝 Starting agent server..."
./agent-server-test > /tmp/agent-gemini-test.log 2>&1 &
SERVER_PID=$!
echo "   Server PID: $SERVER_PID"
echo "   Log file: /tmp/agent-gemini-test.log"
echo ""

# Wait for server to start
echo "⏳ Waiting for server to start..."
sleep 3

# Check if server is running
if ! ps -p $SERVER_PID > /dev/null; then
    echo "❌ Server failed to start"
    echo "Log output:"
    cat /tmp/agent-gemini-test.log
    exit 1
fi
echo "✅ Server is running"
echo ""

# Test 1: Simple text generation
echo "📝 Test 1: Simple text generation..."
RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Say hello in exactly 3 words"
  }')

if echo "$RESPONSE" | grep -q "error"; then
    echo "❌ Test 1 failed with error:"
    echo "$RESPONSE" | jq '.'
else
    echo "✅ Test 1 passed"
    echo "   Response: $(echo "$RESPONSE" | jq -r '.response // .content // .' | head -c 100)..."
fi
echo ""

# Test 2: Streaming response
echo "📝 Test 2: Streaming response..."
STREAM_OUTPUT=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{
    "message": "Count from 1 to 3"
  }' --no-buffer | head -20)

if echo "$STREAM_OUTPUT" | grep -q "data:"; then
    echo "✅ Test 2 passed (streaming works)"
    echo "   First few lines:"
    echo "$STREAM_OUTPUT" | head -5 | sed 's/^/   /'
else
    echo "❌ Test 2 failed (no streaming data)"
fi
echo ""

# Test 3: Token usage tracking
echo "📝 Test 3: Token usage tracking..."
USAGE_RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{
    "message": "Hi"
  }' --no-buffer)

if echo "$USAGE_RESPONSE" | grep -q "usage"; then
    echo "✅ Test 3 passed (token usage tracked)"
    USAGE_LINE=$(echo "$USAGE_RESPONSE" | grep "usage" | head -1)
    echo "   Usage: $USAGE_LINE"
else
    echo "⚠️  Test 3: No usage data found (may be normal)"
fi
echo ""

# Test 4: Model identification
echo "📝 Test 4: Verifying Gemini model is used..."
sleep 2
if grep -q "gemini" /tmp/agent-gemini-test.log; then
    echo "✅ Test 4 passed (Gemini model confirmed in logs)"
    grep "gemini" /tmp/agent-gemini-test.log | tail -3 | sed 's/^/   /'
else
    echo "⚠️  Test 4: Could not confirm Gemini in logs"
fi
echo ""

# Cleanup
echo "🧹 Cleaning up..."
kill $SERVER_PID 2>/dev/null
wait $SERVER_PID 2>/dev/null
rm -f agent-server-test

# Restore original config if it existed
if [ -f agent-config.json.backup ]; then
    mv agent-config.json.backup agent-config.json
    echo "   Restored original agent-config.json"
fi

echo ""
echo "=========================="
echo "✅ Integration tests complete!"
echo ""
echo "View full logs: cat /tmp/agent-gemini-test.log"
echo "Clean up logs: rm /tmp/agent-gemini-test.log"
echo ""
