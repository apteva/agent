#!/bin/bash

echo "🧪 Testing System Context Feature"
echo "=================================="
echo ""

# Test 1: String context
echo "📝 Test 1: String system context"
echo "--------------------------------"

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What timezone am I in? What is my name?",
    "system": "User is in EST timezone. User name is Alice."
  }')

echo "Response:"
echo "$RESPONSE" | head -50
echo ""
echo ""

# Test 2: Without system context (baseline)
echo "📝 Test 2: Without system context (baseline)"
echo "--------------------------------------------"

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What timezone am I in? What is my name?"
  }')

echo "Response:"
echo "$RESPONSE" | head -50
echo ""
echo ""

# Test 3: Empty system context (should be ignored)
echo "📝 Test 3: Empty system context"
echo "--------------------------------"

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Hello",
    "system": ""
  }')

echo "Response:"
echo "$RESPONSE" | head -30
echo ""
echo ""

echo "✅ Test complete!"
echo ""
echo "Expected behavior:"
echo "  Test 1: Should mention EST timezone and name Alice"
echo "  Test 2: Should NOT know timezone or name (baseline)"
echo "  Test 3: Should work normally (empty system ignored)"
