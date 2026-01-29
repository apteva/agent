#!/bin/bash

echo "Testing Agent Event System"
echo "============================"
echo ""

# Start event monitoring in background
echo "Starting event monitor..."
curl -N -s "http://localhost:4015/events?category=CHAT,TOOL,DATABASE,LLM,MCP&tail=0" 2>/dev/null | while IFS= read -r line; do
    if [[ $line == data:* ]]; then
        # Extract JSON from SSE data line
        json=${line#data: }
        # Pretty print with jq if available, otherwise just echo
        if command -v jq &> /dev/null; then
            echo "$json" | jq -C '.'
        else
            echo "$json"
        fi
        echo "---"
    fi
done &

MONITOR_PID=$!

# Give monitor time to connect
sleep 1

echo ""
echo "Sending test chat message..."
echo "============================"

# Send a test message that will trigger events
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What time is it?"
  }' \
  --no-buffer 2>/dev/null | while IFS= read -r line; do
    if [[ $line == data:* ]]; then
        # Show streaming response
        json=${line#data: }
        echo "Response: $json"
    fi
done

echo ""
echo "Chat complete. Waiting for final events..."
sleep 2

# Kill the monitor
kill $MONITOR_PID 2>/dev/null

echo ""
echo "Test complete!"
echo ""
echo "To monitor events continuously, run:"
echo "  curl -N 'http://localhost:4015/events'"
echo ""
echo "Or open the web monitor:"
echo "  open examples/event-monitor.html"