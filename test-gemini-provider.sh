#!/bin/bash

# Test Gemini Provider Implementation
# This script tests the basic functionality of the Gemini provider

echo "🧪 Testing Gemini Provider Implementation"
echo "=========================================="
echo ""

# Check if GEMINI_API_KEY is set
if [ -z "$GEMINI_API_KEY" ]; then
    echo "❌ ERROR: GEMINI_API_KEY environment variable is not set"
    echo "Please set it with: export GEMINI_API_KEY=your-api-key-here"
    exit 1
fi

echo "✅ GEMINI_API_KEY is set"
echo ""

# Test 1: Basic compilation
echo "📝 Test 1: Compiling Gemini provider..."
cd /Users/marcoschwartz/Documents/code/frontends/apteva/agent

if go build -o /dev/null ./providers/gemini.go 2>&1 | grep -q "error"; then
    echo "❌ Compilation failed"
    go build ./providers/gemini.go
    exit 1
else
    echo "✅ Gemini provider compiles successfully"
fi
echo ""

# Test 2: Check translator compilation
echo "📝 Test 2: Compiling translator with Gemini types..."
if go build -o /dev/null ./providers/translator.go 2>&1 | grep -q "error"; then
    echo "❌ Translator compilation failed"
    go build ./providers/translator.go
    exit 1
else
    echo "✅ Translator with Gemini types compiles successfully"
fi
echo ""

# Test 3: Check stream processor compilation
echo "📝 Test 3: Compiling Gemini stream processor..."
if go build -o /dev/null ./stream/gemini_processor.go 2>&1 | grep -q "error"; then
    echo "❌ Stream processor compilation failed"
    go build ./stream/gemini_processor.go
    exit 1
else
    echo "✅ Gemini stream processor compiles successfully"
fi
echo ""

# Test 4: Build full agent with Gemini support
echo "📝 Test 4: Building full agent server..."
if go build -o agent-server-test main.go 2>&1; then
    echo "✅ Full agent server builds successfully"
    rm -f agent-server-test
else
    echo "❌ Full agent server build failed"
    exit 1
fi
echo ""

echo "=========================================="
echo "✅ All compilation tests passed!"
echo ""
echo "Next steps:"
echo "1. Set GEMINI_API_KEY in your environment"
echo "2. Update agent-config.json to use Gemini:"
echo "   {"
echo "     \"llm\": {"
echo "       \"provider\": \"gemini\","
echo "       \"model\": \"gemini-2.5-flash\","
echo "       \"max_tokens\": 8192,"
echo "       \"temperature\": 1.0"
echo "     }"
echo "   }"
echo "3. Run the agent server: ./agent-server"
echo "4. Test with: curl http://localhost:8080/api/chat"
echo ""
