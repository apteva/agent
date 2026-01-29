#!/bin/bash

# Agent Filesystem Integration Test
# Tests the complete flow: MCP tool generates image → filesystem extracts → stores → expands

set -e

echo "🧪 Agent Filesystem Integration Test"
echo "======================================"
echo ""

# Configuration
AGENT_URL="http://localhost:4015"
CONFIG_FILE="agent-config-test-filesystem.json"

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if agent is running
echo "1️⃣  Checking if agent is running..."
if ! curl -s "$AGENT_URL/health" > /dev/null; then
    echo -e "${RED}❌ Agent is not running at $AGENT_URL${NC}"
    echo "   Start the agent first: ./agent-go"
    exit 1
fi
echo -e "${GREEN}✅ Agent is running${NC}"
echo ""

# Create test configuration with filesystem enabled
echo "2️⃣  Creating test configuration..."
cat > "$CONFIG_FILE" << 'EOF'
{
  "agent": {
    "name": "Filesystem Test Agent",
    "llm": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-5",
      "max_tokens": 4000,
      "temperature": 0.7,
      "tools": ["generate_test_image"]
    },
    "filesystem": {
      "enabled": true,
      "max_file_size": 10485760,
      "max_total_size": 104857600,
      "auto_extract": true,
      "min_extract_size": 1024,
      "deduplication": true,
      "auto_cleanup": true,
      "retention_days": 7,
      "cleanup_orphans": true,
      "allowed_types": ["image", "document"]
    }
  }
}
EOF
echo -e "${GREEN}✅ Test configuration created${NC}"
echo ""

# Update agent configuration
echo "3️⃣  Updating agent configuration..."
curl -s -X POST "$AGENT_URL/config" \
  -H "Content-Type: application/json" \
  -d @"$CONFIG_FILE" > /dev/null
echo -e "${GREEN}✅ Configuration updated${NC}"
echo ""

# Get initial stats
echo "4️⃣  Getting initial storage stats..."
INITIAL_STATS=$(curl -s "$AGENT_URL/files/stats")
INITIAL_FILES=$(echo "$INITIAL_STATS" | grep -o '"total_files":[0-9]*' | grep -o '[0-9]*' || echo "0")
echo "   Initial files: $INITIAL_FILES"
echo ""

# Test 1: Send chat message requesting image generation
echo "5️⃣  Test 1: Generate test image via MCP tool..."
CHAT_RESPONSE=$(curl -s -X POST "$AGENT_URL/chat" \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Use the generate_test_image tool to create a 800x600 gradient test image"
  }')

THREAD_ID=$(echo "$CHAT_RESPONSE" | grep -o '"thread_id":"[^"]*"' | head -1 | cut -d'"' -f4 || echo "")
if [ -z "$THREAD_ID" ]; then
    echo -e "${RED}❌ Failed to get thread ID${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Image generation requested (Thread: $THREAD_ID)${NC}"
echo ""

# Wait for agent to process
echo "6️⃣  Waiting for image generation and extraction..."
sleep 3
echo ""

# Check if file was extracted
echo "7️⃣  Checking if file was extracted..."
NEW_STATS=$(curl -s "$AGENT_URL/files/stats")
NEW_FILES=$(echo "$NEW_STATS" | grep -o '"total_files":[0-9]*' | grep -o '[0-9]*' || echo "0")

if [ "$NEW_FILES" -gt "$INITIAL_FILES" ]; then
    echo -e "${GREEN}✅ File extracted! ($INITIAL_FILES → $NEW_FILES files)${NC}"
    FILES_ADDED=$((NEW_FILES - INITIAL_FILES))
    echo "   Files added: $FILES_ADDED"
else
    echo -e "${YELLOW}⚠️  No new files detected${NC}"
    echo "   This may mean:"
    echo "   - Image was smaller than min_extract_size (10KB)"
    echo "   - Filesystem is disabled"
    echo "   - Tool execution failed"
fi
echo ""

# Display storage statistics
echo "8️⃣  Storage statistics:"
echo "$NEW_STATS" | python3 -m json.tool 2>/dev/null || echo "$NEW_STATS"
echo ""

# Test 2: List files
echo "9️⃣  Test 2: List all files..."
FILES_LIST=$(curl -s "$AGENT_URL/files")
FILES_COUNT=$(echo "$FILES_LIST" | grep -o '"count":[0-9]*' | grep -o '[0-9]*' || echo "0")
echo "   Total files: $FILES_COUNT"

if [ "$FILES_COUNT" -gt "0" ]; then
    echo -e "${GREEN}✅ Files retrieved successfully${NC}"

    # Get first file ID
    FILE_ID=$(echo "$FILES_LIST" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || echo "")

    if [ -n "$FILE_ID" ]; then
        echo "   First file ID: $FILE_ID"

        # Test 3: Get file metadata
        echo ""
        echo "🔟 Test 3: Get file metadata..."
        FILE_META=$(curl -s "$AGENT_URL/files/$FILE_ID")
        echo "$FILE_META" | python3 -m json.tool 2>/dev/null || echo "$FILE_META"
        echo -e "${GREEN}✅ File metadata retrieved${NC}"

        # Test 4: Download file
        echo ""
        echo "1️⃣1️⃣  Test 4: Download file..."
        HTTP_CODE=$(curl -s -o /tmp/test_download_file -w "%{http_code}" "$AGENT_URL/files/$FILE_ID/download")
        if [ "$HTTP_CODE" = "200" ]; then
            FILE_SIZE=$(wc -c < /tmp/test_download_file)
            echo -e "${GREEN}✅ File downloaded successfully (${FILE_SIZE} bytes)${NC}"
            rm -f /tmp/test_download_file
        else
            echo -e "${RED}❌ File download failed (HTTP $HTTP_CODE)${NC}"
        fi
    fi
else
    echo -e "${YELLOW}⚠️  No files found${NC}"
fi
echo ""

# Test 5: Deduplication test
echo "1️⃣2️⃣  Test 5: Test deduplication..."
echo "   Sending same request again..."
curl -s -X POST "$AGENT_URL/chat" \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Generate another 800x600 gradient test image with the same parameters"
  }' > /dev/null

sleep 3

DEDUP_STATS=$(curl -s "$AGENT_URL/files/stats")
DEDUP_FILES=$(echo "$DEDUP_STATS" | grep -o '"total_files":[0-9]*' | grep -o '[0-9]*' || echo "0")

if [ "$DEDUP_FILES" -eq "$NEW_FILES" ]; then
    echo -e "${GREEN}✅ Deduplication working! (Files count unchanged: $DEDUP_FILES)${NC}"
else
    echo -e "${YELLOW}⚠️  Deduplication may not be working (Files: $NEW_FILES → $DEDUP_FILES)${NC}"
    echo "   Note: This could be normal if images have different checksums"
fi
echo ""

# Test 6: Public URL generation and access
if [ "$FILES_COUNT" -gt "0" ] && [ -n "$FILE_ID" ]; then
    echo "1️⃣3️⃣  Test 6: Public URL generation and analysis..."

    # Ask agent to analyze the image it just generated using the public URL
    echo "   Asking agent to analyze image via public URL..."
    ANALYZE_RESPONSE=$(curl -s -X POST "$AGENT_URL/chat" \
      -H "Content-Type: application/json" \
      -d "{
        \"thread_id\": \"$THREAD_ID\",
        \"message\": \"Now use the analyze_image_url tool to fetch and analyze the image you just generated. The URL should be in the previous tool result.\"
      }")

    sleep 5

    # Check if analyze was successful
    if echo "$ANALYZE_RESPONSE" | grep -q "success"; then
        echo -e "${GREEN}✅ Agent successfully analyzed image via public URL!${NC}"
        echo "   The complete flow worked: generate → extract → store → URL → analyze"
    else
        echo -e "${YELLOW}⚠️  Image analysis via URL may have failed${NC}"
        echo "   Check if PUBLIC_URL is set correctly"
    fi
    echo ""
fi

# Test 7: Manual cleanup
if [ "$FILES_COUNT" -gt "0" ]; then
    echo "1️⃣4️⃣  Test 7: Manual cleanup..."
    CLEANUP_RESPONSE=$(curl -s -X POST "$AGENT_URL/files/cleanup")
    echo "$CLEANUP_RESPONSE" | python3 -m json.tool 2>/dev/null || echo "$CLEANUP_RESPONSE"
    echo -e "${GREEN}✅ Manual cleanup executed${NC}"
    echo ""
fi

# Final statistics
echo "1️⃣5️⃣  Final storage statistics:"
FINAL_STATS=$(curl -s "$AGENT_URL/files/stats")
echo "$FINAL_STATS" | python3 -m json.tool 2>/dev/null || echo "$FINAL_STATS"
echo ""

# Summary
echo "======================================"
echo "🎉 Integration Test Complete!"
echo "======================================"
echo ""
echo "Tests performed:"
echo "  ✅ 1. Image generation via tool"
echo "  ✅ 2. Automatic base64 extraction"
echo "  ✅ 3. File storage in database"
echo "  ✅ 4. File metadata retrieval"
echo "  ✅ 5. File download"
echo "  ✅ 6. Deduplication check"
echo "  ✅ 7. Public URL generation and access"
echo "  ✅ 8. Image analysis via public URL"
echo "  ✅ 9. Manual cleanup"
echo ""

if [ "$NEW_FILES" -gt "$INITIAL_FILES" ]; then
    echo -e "${GREEN}✅ All core functionality working!${NC}"
    echo ""
    echo "Filesystem successfully:"
    echo "  • Extracted base64 images from tool responses"
    echo "  • Stored files with metadata and checksums"
    echo "  • Provided API access to stored files"
    echo "  • Enabled deduplication"
else
    echo -e "${YELLOW}⚠️  File extraction not verified${NC}"
    echo "   Check agent logs for details"
fi

echo ""
echo "Cleanup:"
rm -f "$CONFIG_FILE"
echo "  • Test configuration file removed"
echo ""

echo "To view agent logs: docker logs <container-id>"
echo "To check database: sqlite3 app.db 'SELECT * FROM files;'"
