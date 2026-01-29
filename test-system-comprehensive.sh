#!/bin/bash

echo "🧪 Comprehensive System Context Test"
echo "====================================="
echo ""

# Test 1: With system context
echo "📝 Test 1: WITH system context"
echo "-------------------------------"
echo "System: 'User name is Alice Johnson. User timezone is EST.'"
echo "Message: 'What is my name and timezone?'"
echo ""

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What is my name and timezone?",
    "system": "User name is Alice Johnson. User timezone is EST.",
    "stream": false
  }')

SUCCESS=$(echo "$RESPONSE" | jq -r '.success')
MODEL=$(echo "$RESPONSE" | jq -r '.model')
REPLY=$(echo "$RESPONSE" | jq -r '.response')

echo "✅ Success: $SUCCESS"
echo "📦 Model: $MODEL"
echo "💬 Response:"
echo "$REPLY"
echo ""
echo ""

# Test 2: Without system context
echo "📝 Test 2: WITHOUT system context (baseline)"
echo "---------------------------------------------"
echo "System: (none)"
echo "Message: 'What is my name and timezone?'"
echo ""

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What is my name and timezone?",
    "stream": false
  }')

SUCCESS=$(echo "$RESPONSE" | jq -r '.success')
MODEL=$(echo "$RESPONSE" | jq -r '.model')
REPLY=$(echo "$RESPONSE" | jq -r '.response')

echo "✅ Success: $SUCCESS"
echo "📦 Model: $MODEL"
echo "💬 Response:"
echo "$REPLY"
echo ""
echo ""

# Test 3: Array-style context
echo "📝 Test 3: Testing with task context"
echo "-------------------------------------"
echo "System: 'User is working on Project X. Deadline is Friday. Priority: high urgency.'"
echo "Message: 'What should I focus on?'"
echo ""

RESPONSE=$(curl -s -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "What should I focus on?",
    "system": "User is working on Project X. Deadline is Friday. Priority: high urgency.",
    "stream": false
  }')

SUCCESS=$(echo "$RESPONSE" | jq -r '.success')
MODEL=$(echo "$RESPONSE" | jq -r '.model')
REPLY=$(echo "$RESPONSE" | jq -r '.response')

echo "✅ Success: $SUCCESS"
echo "📦 Model: $MODEL"
echo "💬 Response:"
echo "$REPLY"
echo ""
echo ""

echo "🎉 All tests completed!"
echo ""
echo "Summary:"
echo "  Test 1: Should mention 'Alice Johnson' and 'EST'"
echo "  Test 2: Should NOT know name, may infer CET from server time"
echo "  Test 3: Should prioritize Project X with Friday deadline awareness"
