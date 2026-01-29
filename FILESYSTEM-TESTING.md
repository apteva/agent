# Filesystem Testing Guide

## Overview

Comprehensive testing for the agent file system, including unit tests, integration tests, and an MCP tool for generating test images.

## Test Components

### 1. Test MCP Tool: `generate_test_image`

**Location:** `tools/generate_test_image.go`

Generates test images of various sizes and patterns for testing file storage.

**Features:**
- Multiple image sizes (100x100 to 2000x2000)
- Various patterns: solid, gradient, checkerboard, random
- Returns base64-encoded PNG images
- Useful for testing extraction, deduplication, and storage limits

**Usage:**
```bash
# Via chat API
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Use generate_test_image to create an 800x600 gradient image"
  }'

# Tool parameters
{
  "width": 800,        // 100-2000 pixels
  "height": 600,       // 100-2000 pixels
  "pattern": "gradient", // solid, gradient, checkerboard, random
  "color": "blue"      // For solid pattern: red, green, blue, purple
}
```

**Example response:**
```json
{
  "success": true,
  "message": "Generated 800x600 gradient test image (147.2 KB base64)",
  "image": {
    "type": "image",
    "source": {
      "type": "base64",
      "media_type": "image/png",
      "data": "iVBORw0KGgo..."
    }
  },
  "metadata": {
    "width": 800,
    "height": 600,
    "pattern": "gradient",
    "size_bytes": 98234,
    "size_base64": 150678,
    "size_kb": 147.2
  }
}
```

### 2. Unit Tests

**Location:** `filesystem/filesystem_test.go`

Comprehensive Go unit tests covering all FileManager operations.

**Test Coverage:**
- ✅ File storage (basic, with metadata)
- ✅ Deduplication (same file stored twice)
- ✅ Size limit enforcement
- ✅ File type restrictions
- ✅ File retrieval (Get, GetAsBase64)
- ✅ File deletion
- ✅ Cleanup (expired files, orphans)
- ✅ Storage statistics
- ✅ Content processing (extract base64 → file ref)
- ✅ Content expansion (file ref → base64)
- ✅ Disabled filesystem behavior
- ✅ Benchmarks (Store, Get operations)

**Run tests:**
```bash
# All tests
cd /path/to/agent
go test ./filesystem/... -v

# Specific test
go test ./filesystem/... -run TestFileManager_Store -v

# With coverage
go test ./filesystem/... -cover

# Benchmarks
go test ./filesystem/... -bench=. -benchmem
```

**Expected output:**
```
=== RUN   TestFileManager_Store
=== RUN   TestFileManager_Store/Store_basic_file
=== RUN   TestFileManager_Store/Deduplication
=== RUN   TestFileManager_Store/Size_limit_enforcement
=== RUN   TestFileManager_Store/Disallowed_file_type
--- PASS: TestFileManager_Store (0.02s)
    --- PASS: TestFileManager_Store/Store_basic_file (0.01s)
    --- PASS: TestFileManager_Store/Deduplication (0.00s)
    --- PASS: TestFileManager_Store/Size_limit_enforcement (0.00s)
    --- PASS: TestFileManager_Store/Disallowed_file_type (0.00s)
...
PASS
coverage: 87.5% of statements
```

### 3. Integration Test Script

**Location:** `test-filesystem-integration.sh`

End-to-end integration test that verifies the complete file system workflow.

**What it tests:**
1. ✅ Agent is running
2. ✅ Configuration update (enable filesystem)
3. ✅ Image generation via MCP tool
4. ✅ Automatic base64 extraction
5. ✅ File storage in database
6. ✅ File listing API
7. ✅ File metadata retrieval
8. ✅ File download
9. ✅ Deduplication
10. ✅ Manual cleanup
11. ✅ Storage statistics

**Run integration test:**
```bash
# Start agent first
./agent-go

# In another terminal
./test-filesystem-integration.sh
```

**Expected output:**
```
🧪 Agent Filesystem Integration Test
======================================

1️⃣  Checking if agent is running...
✅ Agent is running

2️⃣  Creating test configuration...
✅ Test configuration created

3️⃣  Updating agent configuration...
✅ Configuration updated

4️⃣  Getting initial storage stats...
   Initial files: 0

5️⃣  Test 1: Generate test image via MCP tool...
✅ Image generation requested (Thread: abc-123)

6️⃣  Waiting for image generation and extraction...

7️⃣  Checking if file was extracted...
✅ File extracted! (0 → 1 files)
   Files added: 1

8️⃣  Storage statistics:
{
  "success": true,
  "stats": {
    "total_files": 1,
    "total_size_bytes": 98234,
    "files_by_type": {
      "image": 1
    }
  }
}

...

🎉 Integration Test Complete!
✅ All core functionality working!
```

## Quick Test Guide

### Option 1: Unit Tests Only (Fast)

```bash
cd /path/to/agent
go test ./filesystem/... -v
```

**Time:** ~1 second
**Best for:** Development, TDD, quick verification

### Option 2: Integration Test (Complete)

```bash
# Terminal 1: Start agent
./agent-go

# Terminal 2: Run integration test
./test-filesystem-integration.sh
```

**Time:** ~15 seconds
**Best for:** Pre-deployment, full verification, Docker testing

### Option 3: Manual Testing

```bash
# 1. Enable filesystem
cat > agent-config.json << 'EOF'
{
  "agent": {
    "filesystem": {
      "enabled": true
    }
  }
}
EOF

# 2. Start agent
./agent-go

# 3. Generate test image
curl -X POST http://localhost:4015/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Use generate_test_image to create a large 1200x900 random pattern image"
  }'

# 4. Check stats
curl http://localhost:4015/files/stats | jq

# 5. List files
curl http://localhost:4015/files | jq

# 6. Get specific file
FILE_ID="<id-from-list>"
curl http://localhost:4015/files/$FILE_ID | jq

# 7. Download file
curl -o downloaded.png http://localhost:4015/files/$FILE_ID/download
```

## Test Scenarios

### Scenario 1: Basic Storage
```bash
# Generate small image
curl -X POST http://localhost:4015/chat -d '{
  "message": "Generate 400x300 gradient image"
}'

# Verify extraction
curl http://localhost:4015/files/stats
# Should show 1 file, ~50KB
```

### Scenario 2: Deduplication
```bash
# Generate same image twice
for i in {1..2}; do
  curl -X POST http://localhost:4015/chat -d '{
    "message": "Generate 800x600 gradient image"
  }'
  sleep 2
done

# Check stats - should still show 1 file (deduplicated)
curl http://localhost:4015/files/stats
```

### Scenario 3: Size Limits
```bash
# Try to generate very large image (should extract if < 10MB)
curl -X POST http://localhost:4015/chat -d '{
  "message": "Generate 2000x2000 random image"
}'

# This will create ~4MB file, should be stored
curl http://localhost:4015/files/stats
```

### Scenario 4: Different Patterns
```bash
# Test all patterns
for pattern in solid gradient checkerboard random; do
  curl -X POST http://localhost:4015/chat -d "{
    \"message\": \"Generate 800x600 $pattern image\"
  }"
  sleep 2
done

# Should show multiple files
curl http://localhost:4015/files
```

### Scenario 5: Cleanup
```bash
# Generate file with short retention (requires config change)
# Wait for expiration
# Trigger cleanup
curl -X POST http://localhost:4015/files/cleanup

# Verify file removed
curl http://localhost:4015/files/stats
```

## Troubleshooting

### Tests Failing?

**Issue:** Unit tests fail to compile
```bash
# Install dependencies
go mod download
go mod tidy
```

**Issue:** Integration test shows "Agent not running"
```bash
# Start agent
./agent-go

# Or with Docker
docker run -p 4015:4015 agent-go
```

**Issue:** Files not being extracted
```bash
# Check filesystem is enabled
curl http://localhost:4015/config | jq '.agent.filesystem'

# Should show: "enabled": true

# Check agent logs
docker logs <container-id> | grep filesystem
# Should see: "📁 File system enabled"
```

**Issue:** Images too small to extract
```bash
# Default min_extract_size is 10KB
# Generate larger images (800x600 or bigger)
# Or lower min_extract_size in config
```

### Verify Database

```bash
# Check files table exists
sqlite3 app.db "SELECT name FROM sqlite_master WHERE type='table' AND name='files';"

# View stored files
sqlite3 app.db "SELECT id, filename, file_type, size_bytes, created_at FROM files;"

# Count files
sqlite3 app.db "SELECT COUNT(*) FROM files;"

# Check total size
sqlite3 app.db "SELECT SUM(size_bytes) FROM files;"
```

## Performance Benchmarks

Expected performance (on typical hardware):

```
BenchmarkFileManager_Store-8     1000    1.2 ms/op    500 KB/op
BenchmarkFileManager_Get-8      10000    0.8 ms/op    250 KB/op
```

- **Store**: ~1-2ms per 50KB file
- **Get**: ~0.5-1ms per retrieval
- **Deduplication**: Adds ~0.2ms (checksum lookup)

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Filesystem Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2

      - name: Set up Go
        uses: actions/setup-go@v2
        with:
          go-version: 1.21

      - name: Run unit tests
        run: |
          cd frontends/apteva/agent
          go test ./filesystem/... -v -cover

      - name: Build agent
        run: |
          cd frontends/apteva/agent
          go build -o agent-go main.go

      - name: Run integration tests
        run: |
          cd frontends/apteva/agent
          ./agent-go &
          sleep 5
          ./test-filesystem-integration.sh
```

## Test Coverage Goals

- ✅ **Unit tests:** 85%+ coverage
- ✅ **Integration:** All API endpoints
- ✅ **End-to-end:** Complete user flows
- ✅ **Performance:** Benchmarks for Store/Get
- ✅ **Edge cases:** Size limits, invalid inputs, disabled mode

## Next Steps

1. ✅ Run unit tests: `go test ./filesystem/... -v`
2. ✅ Run integration test: `./test-filesystem-integration.sh`
3. ✅ Verify in Docker: `docker build . && docker run ...`
4. ✅ Test deduplication: Generate same image twice
5. ✅ Test cleanup: Wait for expiration, trigger cleanup
6. ✅ Check performance: Run benchmarks

## Success Criteria

All tests should pass with:
- ✅ Files extracted from base64 (>10KB)
- ✅ Files stored with correct metadata
- ✅ Deduplication prevents duplicates
- ✅ Size limits enforced
- ✅ Cleanup removes expired files
- ✅ APIs return correct data
- ✅ Performance within benchmarks

**The filesystem is production-ready when all tests pass!** 🚀
