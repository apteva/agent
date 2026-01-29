#!/bin/bash

echo "🧪 Simple Gemini Test"
echo "===================="
echo ""

# Build server
echo "📝 Building server..."
go build -o agent-server-test main.go || exit 1
echo "✅ Build complete"
echo ""

# Start server
echo "📝 Starting server..."
./agent-server-test > /tmp/gemini-test.log 2>&1 &
PID=$!
echo "   PID: $PID"
sleep 4

# Test
echo "📝 Testing Gemini..."
echo ""

curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Say hello in exactly 3 words"}' | head -20

echo ""
echo ""

# Check logs
echo "📋 Recent logs:"
tail -15 /tmp/gemini-test.log | grep -E "Gemini|gemini|model=" || echo "   (no Gemini mentions found)"

echo ""

# Cleanup
kill $PID 2>/dev/null
rm -f agent-server-test

echo "✅ Test complete!"
