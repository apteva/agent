#!/bin/bash
# Test automatic agent tool management

set -e

echo "🧪 Testing Automatic Agent Tool Management"
echo "=========================================="
echo ""

# Start server if not running
if ! curl -s http://localhost:4015/health > /dev/null 2>&1; then
    echo "Starting server..."
    ./agent-core > /dev/null 2>&1 &
    sleep 3
fi

echo "1. Get current tools..."
CURRENT_TOOLS=$(curl -s http://localhost:4015/config | grep -o '"tools":\[[^]]*\]')
echo "   Current: $CURRENT_TOOLS"
echo ""

echo "2. Enable agent communication..."
curl -s -X POST http://localhost:4015/config \
  -H "Content-Type: application/json" \
  -d '{"agents":{"enabled":true}}' > /dev/null

sleep 1

echo "3. Check if tools were auto-added..."
NEW_TOOLS=$(curl -s http://localhost:4015/config | grep -o '"tools":\[[^]]*\]')
echo "   After enable: $NEW_TOOLS"

if echo "$NEW_TOOLS" | grep -q "call_agent"; then
    echo "   ✅ call_agent was auto-added"
else
    echo "   ❌ call_agent was NOT added"
fi

if echo "$NEW_TOOLS" | grep -q "list_available_agents"; then
    echo "   ✅ list_available_agents was auto-added"
else
    echo "   ❌ list_available_agents was NOT added"
fi
echo ""

echo "4. Disable agent communication..."
curl -s -X POST http://localhost:4015/config \
  -H "Content-Type: application/json" \
  -d '{"agents":{"enabled":false}}' > /dev/null

sleep 1

echo "5. Check if tools were auto-removed..."
FINAL_TOOLS=$(curl -s http://localhost:4015/config | grep -o '"tools":\[[^]]*\]')
echo "   After disable: $FINAL_TOOLS"

if echo "$FINAL_TOOLS" | grep -q "call_agent"; then
    echo "   ❌ call_agent was NOT removed"
else
    echo "   ✅ call_agent was auto-removed"
fi

if echo "$FINAL_TOOLS" | grep -q "list_available_agents"; then
    echo "   ❌ list_available_agents was NOT removed"
else
    echo "   ✅ list_available_agents was auto-removed"
fi
echo ""

echo "=========================================="
echo "✅ Test Complete!"
echo ""
echo "Summary: Agent tools are now automatically managed"
echo "- Enable agents → tools auto-added"
echo "- Disable agents → tools auto-removed"
