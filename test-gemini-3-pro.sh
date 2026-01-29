#!/bin/bash

echo "🧪 Testing Gemini 3 Pro Preview Model"
echo "======================================"
echo ""

# Update config to use gemini-3-pro-preview
echo "📝 Updating config to use gemini-3-pro-preview..."
cat > /tmp/test-config.json <<EOF
{
  "agent": {
    "llm": {
      "provider": "gemini",
      "model": "gemini-3-pro-preview",
      "max_tokens": 4000,
      "temperature": 1.0
    }
  }
}
EOF

# Backup existing config
cp agent-config.json agent-config.json.test-backup 2>/dev/null || true

# Temporarily update main config
jq '.agent.llm.provider = "gemini" | .agent.llm.model = "gemini-3-pro-preview"' agent-config.json > /tmp/config-temp.json
mv /tmp/config-temp.json agent-config.json

echo "✅ Config updated to gemini-3-pro-preview"
echo ""

# Build server
echo "📝 Building server..."
go build -o agent-server-test main.go || exit 1
echo "✅ Build complete"
echo ""

# Start server
echo "📝 Starting server..."
./agent-server-test > /tmp/gemini-3-test.log 2>&1 &
PID=$!
echo "   PID: $PID"
sleep 4

# Test
echo "📝 Testing gemini-3-pro-preview..."
echo ""

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Say hello in exactly 3 words"}')

echo "$RESPONSE" | head -20

echo ""
echo ""

# Check logs for model confirmation
echo "📋 Model confirmation in logs:"
grep -E "gemini-3-pro-preview|model=" /tmp/gemini-3-test.log | tail -5

echo ""

# Cleanup
kill $PID 2>/dev/null
rm -f agent-server-test

# Restore config
if [ -f agent-config.json.test-backup ]; then
    mv agent-config.json.test-backup agent-config.json
    echo "✅ Config restored"
fi

echo ""
echo "View full logs: cat /tmp/gemini-3-test.log"
echo "✅ Test complete!"
