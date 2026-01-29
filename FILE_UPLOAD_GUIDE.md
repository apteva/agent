# File Upload Guide - Agent System

## Yes! The agent supports file uploads ✅

Your agent has a **complete file storage system** that supports uploading images and documents in multiple ways.

---

## 📋 Table of Contents
1. [Upload Methods](#upload-methods)
2. [Via Chat Endpoint (Inline)](#1-via-chat-endpoint-inline)
3. [Via Files API (Direct)](#2-via-files-api-direct)
4. [Supported File Types](#supported-file-types)
5. [Configuration](#configuration)
6. [Examples](#examples)
7. [File Management](#file-management)

---

## Upload Methods

### Method 1: Via Chat Endpoint (Inline) 🎯 **Recommended**
Upload files as part of your conversation - the agent automatically processes them.

### Method 2: Via Files API (Direct)
Upload files directly to the storage system, then reference them in messages.

---

## 1. Via Chat Endpoint (Inline)

### How It Works
1. Send message with base64-encoded file in content blocks
2. Agent automatically extracts and stores files (if enabled)
3. Replaces large base64 data with lightweight file references
4. LLM can "see" and analyze the files

### Example: Upload Image
```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": [
      {
        "type": "text",
        "text": "What do you see in this image?"
      },
      {
        "type": "image",
        "source": {
          "type": "base64",
          "media_type": "image/jpeg",
          "data": "/9j/4AAQSkZJRgABAQEA..."
        }
      }
    ]
  }'
```

### Example: Upload PDF Document
```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": [
      {
        "type": "text",
        "text": "Summarize this document"
      },
      {
        "type": "document",
        "source": {
          "type": "base64",
          "media_type": "application/pdf",
          "data": "JVBERi0xLjQKJeLjz9..."
        }
      }
    ]
  }'
```

### What Happens Behind the Scenes
```
User Message (with base64)
    ↓
Agent detects base64 > 10KB
    ↓
Extracts file → Stores in database
    ↓
Replaces base64 with file reference
    ↓
Message saved with: {type: "file", file_id: "abc123"}
    ↓
When LLM needs it: Expands file reference back to base64
```

### JavaScript Example (Browser)
```javascript
// Convert file to base64
async function uploadFileToChat(file, question) {
  const reader = new FileReader();

  reader.onload = async (e) => {
    const base64 = e.target.result.split(',')[1]; // Remove data:image/jpeg;base64,

    const response = await fetch('http://localhost:8080/chat', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        message: [
          {
            type: 'text',
            text: question
          },
          {
            type: 'image',
            source: {
              type: 'base64',
              media_type: file.type,
              data: base64
            }
          }
        ]
      })
    });

    // Read streaming response
    const reader = response.body.getReader();
    const decoder = new TextDecoder();

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      const chunk = decoder.decode(value);
      console.log(chunk); // Process streaming events
    }
  };

  reader.readAsDataURL(file);
}

// Usage
const fileInput = document.getElementById('fileInput');
fileInput.addEventListener('change', (e) => {
  const file = e.target.files[0];
  uploadFileToChat(file, 'What is in this image?');
});
```

---

## 2. Via Files API (Direct)

### Upload File Directly
```bash
curl -X POST http://localhost:8080/files \
  -H "Content-Type: application/json" \
  -d '{
    "filename": "screenshot.png",
    "mime_type": "image/png",
    "file_type": "image",
    "data": "iVBORw0KGgoAAAANSUhEUgAA...",
    "thread_id": "thread_abc123",
    "source": "user_upload"
  }'
```

**Response:**
```json
{
  "success": true,
  "file_id": "file_xyz789",
  "message": "File screenshot.png uploaded successfully"
}
```

### Reference File in Chat
```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": [
      {
        "type": "text",
        "text": "Analyze this image"
      },
      {
        "type": "image",
        "source": {
          "type": "file",
          "file_id": "file_xyz789"
        }
      }
    ],
    "thread_id": "thread_abc123"
  }'
```

---

## Supported File Types

### Images
- **JPEG** (`image/jpeg`) - Photos, screenshots
- **PNG** (`image/png`) - Graphics, screenshots with transparency
- **GIF** (`image/gif`) - Animated images
- **WebP** (`image/webp`) - Modern image format

### Documents
- **PDF** (`application/pdf`) - Documents, reports
- **Text** (`text/plain`) - Plain text files
- **Markdown** (`text/markdown`) - Markdown documents

### Configuration
File types are controlled by `allowed_types` in config:
```json
{
  "filesystem": {
    "allowed_types": ["image", "document"]
  }
}
```

---

## Configuration

### Enable File System
**File**: `agent-config.json` or via API

```json
{
  "filesystem": {
    "enabled": true,                     // ⚠️ Disabled by default
    "max_file_size": 10485760,          // 10MB per file
    "max_total_size": 104857600,        // 100MB total storage
    "auto_extract": true,                // Auto-extract base64 from messages
    "min_extract_size": 10240,          // Extract if > 10KB (saves DB space)
    "deduplication": true,               // Avoid storing duplicates (SHA-256)
    "auto_cleanup": true,                // Enable automatic cleanup
    "retention_days": 7,                 // Keep files for 7 days
    "cleanup_orphans": true,             // Remove orphaned files
    "allowed_types": ["image", "document"]
  }
}
```

### Enable via API
```bash
curl -X PUT http://localhost:8080/config \
  -H "Content-Type: application/json" \
  -d '{
    "filesystem": {
      "enabled": true,
      "max_file_size": 10485760
    }
  }'
```

---

## Examples

### Example 1: Image Analysis
```bash
# 1. Take screenshot and convert to base64
base64 -i screenshot.png -o screenshot.b64

# 2. Send to agent
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d @- << 'EOF'
{
  "message": [
    {
      "type": "text",
      "text": "What's shown in this screenshot? Describe in detail."
    },
    {
      "type": "image",
      "source": {
        "type": "base64",
        "media_type": "image/png",
        "data": "'"$(cat screenshot.b64)"'"
      }
    }
  ]
}
EOF
```

### Example 2: Multiple Images
```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": [
      {
        "type": "text",
        "text": "Compare these two images"
      },
      {
        "type": "image",
        "source": {
          "type": "base64",
          "media_type": "image/jpeg",
          "data": "'$(base64 -i image1.jpg)'"
        }
      },
      {
        "type": "image",
        "source": {
          "type": "base64",
          "media_type": "image/jpeg",
          "data": "'$(base64 -i image2.jpg)'"
        }
      }
    ]
  }'
```

### Example 3: PDF Document
```bash
# Convert PDF to base64
PDF_DATA=$(base64 -i document.pdf)

curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": [
      {
        "type": "text",
        "text": "Summarize the key points from this document"
      },
      {
        "type": "document",
        "source": {
          "type": "base64",
          "media_type": "application/pdf",
          "data": "'"$PDF_DATA"'"
        }
      }
    ]
  }'
```

### Example 4: Upload from URL
```bash
# Download image and convert to base64
IMAGE_DATA=$(curl -s https://example.com/image.jpg | base64)

curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": [
      {
        "type": "text",
        "text": "What is in this image?"
      },
      {
        "type": "image",
        "source": {
          "type": "base64",
          "media_type": "image/jpeg",
          "data": "'"$IMAGE_DATA"'"
        }
      }
    ]
  }'
```

---

## File Management

### List All Files
```bash
curl http://localhost:8080/files
```

**Response:**
```json
{
  "success": true,
  "count": 15,
  "files": [
    {
      "id": "file_abc123",
      "filename": "screenshot.png",
      "mime_type": "image/png",
      "file_type": "image",
      "size_bytes": 245678,
      "checksum": "abc123...",
      "thread_id": "thread_xyz",
      "created_at": "2024-11-11T10:30:00Z",
      "access_count": 3
    }
  ]
}
```

### List Files by Thread
```bash
curl http://localhost:8080/files?thread_id=thread_xyz789
```

### Get File Metadata
```bash
curl http://localhost:8080/files/file_abc123
```

### Download File
```bash
curl http://localhost:8080/files/file_abc123/download -o downloaded_file.png
```

### Delete File
```bash
curl -X DELETE http://localhost:8080/files/file_abc123
```

### Storage Statistics
```bash
curl http://localhost:8080/files/stats
```

**Response:**
```json
{
  "success": true,
  "stats": {
    "total_files": 42,
    "total_size_bytes": 15728640,
    "total_size_mb": 15.0,
    "by_type": {
      "image": {
        "count": 35,
        "size_bytes": 12582912
      },
      "document": {
        "count": 7,
        "size_bytes": 3145728
      }
    },
    "oldest_file": "2024-11-04T08:00:00Z",
    "newest_file": "2024-11-11T12:00:00Z"
  }
}
```

### Manual Cleanup
```bash
curl -X POST http://localhost:8080/files/cleanup
```

**Response:**
```json
{
  "success": true,
  "files_cleaned": 8,
  "message": "Cleaned up 8 files"
}
```

---

## Features

### ✅ Automatic Deduplication
- Files are stored once even if sent multiple times
- SHA-256 checksums identify duplicates
- Saves storage space

### ✅ Automatic Extraction
- Base64 data > 10KB automatically extracted
- Stored in database efficiently
- Replaced with lightweight references

### ✅ Automatic Cleanup
- Expired files removed (configurable retention)
- Orphaned files cleaned up
- Runs every hour automatically

### ✅ Thread Integration
- Files linked to conversation threads
- Files deleted when threads are deleted
- Easy filtering by thread

### ✅ Access Tracking
- Track how many times files are accessed
- Monitor file usage patterns
- Identify frequently used files

---

## Database Schema

Files are stored in the `files` table:

```sql
CREATE TABLE files (
    id TEXT PRIMARY KEY,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    file_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    checksum TEXT NOT NULL,
    data BLOB NOT NULL,

    thread_id TEXT,
    message_id TEXT,
    source TEXT,
    source_tool TEXT,

    metadata TEXT DEFAULT '{}',

    expires_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    accessed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    access_count INTEGER DEFAULT 0,

    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_files_checksum ON files(checksum);
CREATE INDEX idx_files_thread ON files(thread_id);
CREATE INDEX idx_files_expires ON files(expires_at);
```

---

## Best Practices

### 1. File Size Limits
- Keep images under 5MB for optimal performance
- PDF documents should be under 10MB
- Configure `max_file_size` based on your needs

### 2. Storage Management
- Enable `auto_cleanup` to prevent storage bloat
- Set reasonable `retention_days` (7-30 days)
- Monitor storage with `/files/stats`

### 3. Deduplication
- Always enable `deduplication: true`
- Saves significant storage space
- No performance impact

### 4. Security
- Files are stored in database (not filesystem)
- Access control via thread ownership
- No direct file path exposure

### 5. Performance
- Files < 10KB stay inline (no extraction)
- Files > 10KB extracted automatically
- Configure `min_extract_size` to tune

---

## Troubleshooting

### Files Not Being Stored?
**Check configuration:**
```bash
curl http://localhost:8080/config | jq .filesystem.enabled
# Should return: true
```

### Enable filesystem:
```bash
curl -X PUT http://localhost:8080/config \
  -H "Content-Type: application/json" \
  -d '{"filesystem": {"enabled": true}}'
```

### Files Too Large?
**Check current limit:**
```bash
curl http://localhost:8080/config | jq .filesystem.max_file_size
```

**Increase limit:**
```bash
curl -X PUT http://localhost:8080/config \
  -H "Content-Type: application/json" \
  -d '{"filesystem": {"max_file_size": 20971520}}'  # 20MB
```

### Storage Full?
**Check stats:**
```bash
curl http://localhost:8080/files/stats
```

**Run cleanup:**
```bash
curl -X POST http://localhost:8080/files/cleanup
```

---

## Summary

✅ **Yes, your agent fully supports file uploads!**

**Two methods:**
1. **Inline with chat** (recommended) - automatic extraction and storage
2. **Direct via Files API** - manual upload, then reference in messages

**Supported file types:**
- Images (JPEG, PNG, GIF, WebP)
- Documents (PDF, text, markdown)

**Key features:**
- Automatic deduplication
- Auto-cleanup with retention
- Thread integration
- Complete file management API

**Configuration:** Disabled by default - enable with `"filesystem": {"enabled": true}`

**Ready to use!** Just enable filesystem in config and start uploading files via chat messages.
