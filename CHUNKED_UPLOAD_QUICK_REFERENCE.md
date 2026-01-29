# Chunked Upload - Quick Reference

## How Clients Upload Parts (Chunks)

### Process Overview

```
1. Initialize → 2. Upload Chunks → 3. Complete
```

### Step-by-Step Example

#### **Step 1: Initialize Upload**

Client tells server: "I want to upload a 500MB file in 5MB chunks"

```bash
curl -X POST http://localhost:8080/files/uploads/init \
  -H "Content-Type: application/json" \
  -d '{
    "filename": "video.mp4",
    "mime_type": "video/mp4",
    "total_size": 524288000,
    "chunk_size": 5242880,
    "thread_id": "thread_123"
  }'
```

**Response:**
```json
{
  "success": true,
  "upload_id": "upload_abc123",
  "chunk_size": 5242880,
  "total_chunks": 100,
  "expires_at": "2024-11-12T10:00:00Z"
}
```

#### **Step 2: Upload Chunks (100 times)**

Client splits file into chunks and uploads each one:

```javascript
// JavaScript example
const file = document.getElementById('fileInput').files[0];
const chunkSize = 5 * 1024 * 1024; // 5MB
const totalChunks = Math.ceil(file.size / chunkSize);

for (let i = 0; i < totalChunks; i++) {
  const start = i * chunkSize;
  const end = Math.min(start + chunkSize, file.size);
  const chunk = file.slice(start, end);

  // Upload this chunk
  const formData = new FormData();
  formData.append('chunk', chunk);

  await fetch(`http://localhost:8080/files/uploads/${uploadId}/chunks`, {
    method: 'POST',
    headers: {
      'X-Chunk-Index': i,
      'X-Chunk-Checksum': await calculateChecksum(chunk)
    },
    body: chunk // Binary data
  });

  console.log(`Uploaded chunk ${i + 1}/${totalChunks}`);
}
```

**cURL example (chunk 0):**
```bash
# Extract chunk 0 (first 5MB)
dd if=video.mp4 of=chunk_0.bin bs=5242880 count=1 skip=0

# Upload chunk 0
curl -X POST http://localhost:8080/files/uploads/upload_abc123/chunks \
  -H "Content-Type: application/octet-stream" \
  -H "X-Chunk-Index: 0" \
  -H "X-Chunk-Checksum: sha256:abc123..." \
  --data-binary @chunk_0.bin
```

**Response for each chunk:**
```json
{
  "success": true,
  "upload_id": "upload_abc123",
  "chunk_index": 0,
  "uploaded_chunks": 1,
  "total_chunks": 100,
  "progress_percent": 1.0
}
```

#### **Step 3: Complete Upload**

After all chunks uploaded, tell server to assemble them:

```bash
curl -X POST http://localhost:8080/files/uploads/upload_abc123/complete \
  -H "Content-Type: application/json" \
  -d '{"verify_checksums": true}'
```

**Response:**
```json
{
  "success": true,
  "upload_id": "upload_abc123",
  "file_id": "file_xyz789",
  "filename": "video.mp4",
  "size": 524288000,
  "message": "File assembled and stored successfully"
}
```

---

## Complete JavaScript Implementation

```javascript
class ChunkedUploader {
  constructor(baseURL = 'http://localhost:8080') {
    this.baseURL = baseURL;
  }

  async uploadFile(file, options = {}) {
    const {
      chunkSize = 5 * 1024 * 1024, // 5MB default
      threadId = null,
      onProgress = null,
      onChunkUploaded = null
    } = options;

    try {
      // Step 1: Initialize
      const initResponse = await fetch(`${this.baseURL}/files/uploads/init`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          filename: file.name,
          mime_type: file.type,
          total_size: file.size,
          chunk_size: chunkSize,
          thread_id: threadId
        })
      });

      const { upload_id, total_chunks } = await initResponse.json();
      console.log(`Upload initialized: ${upload_id}, ${total_chunks} chunks`);

      // Step 2: Upload chunks
      for (let i = 0; i < total_chunks; i++) {
        const start = i * chunkSize;
        const end = Math.min(start + chunkSize, file.size);
        const chunk = file.slice(start, end);

        // Calculate checksum
        const checksum = await this.calculateChecksum(chunk);

        // Upload chunk
        const chunkResponse = await fetch(
          `${this.baseURL}/files/uploads/${upload_id}/chunks`,
          {
            method: 'POST',
            headers: {
              'X-Chunk-Index': i.toString(),
              'X-Chunk-Checksum': checksum
            },
            body: chunk
          }
        );

        const chunkResult = await chunkResponse.json();

        if (onProgress) {
          onProgress(chunkResult.progress_percent, i + 1, total_chunks);
        }

        if (onChunkUploaded) {
          onChunkUploaded(i, chunk.size);
        }

        console.log(`Chunk ${i + 1}/${total_chunks} uploaded (${chunkResult.progress_percent.toFixed(1)}%)`);
      }

      // Step 3: Complete upload
      const completeResponse = await fetch(
        `${this.baseURL}/files/uploads/${upload_id}/complete`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ verify_checksums: true })
        }
      );

      const result = await completeResponse.json();
      console.log('Upload complete:', result.file_id);

      return result;

    } catch (error) {
      console.error('Upload failed:', error);
      throw error;
    }
  }

  async calculateChecksum(chunk) {
    const buffer = await chunk.arrayBuffer();
    const hashBuffer = await crypto.subtle.digest('SHA-256', buffer);
    const hashArray = Array.from(new Uint8Array(hashBuffer));
    const hashHex = hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
    return `sha256:${hashHex}`;
  }

  async resumeUpload(uploadId, file, options = {}) {
    // Get current status
    const statusResponse = await fetch(
      `${this.baseURL}/files/uploads/${uploadId}/status`
    );
    const status = await statusResponse.json();

    console.log(`Resuming upload: ${status.uploaded_chunks}/${status.total_chunks} chunks complete`);

    // Upload missing chunks only
    const chunkSize = Math.ceil(status.total_size / status.total_chunks);

    for (const chunkIndex of status.missing_chunks) {
      const start = chunkIndex * chunkSize;
      const end = Math.min(start + chunkSize, file.size);
      const chunk = file.slice(start, end);

      const checksum = await this.calculateChecksum(chunk);

      await fetch(`${this.baseURL}/files/uploads/${uploadId}/chunks`, {
        method: 'POST',
        headers: {
          'X-Chunk-Index': chunkIndex.toString(),
          'X-Chunk-Checksum': checksum
        },
        body: chunk
      });

      console.log(`Resumed chunk ${chunkIndex + 1}/${status.total_chunks}`);
    }

    // Complete
    const completeResponse = await fetch(
      `${this.baseURL}/files/uploads/${uploadId}/complete`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ verify_checksums: true })
      }
    );

    return await completeResponse.json();
  }

  async cancelUpload(uploadId) {
    await fetch(`${this.baseURL}/files/uploads/${uploadId}`, {
      method: 'DELETE'
    });
    console.log('Upload cancelled:', uploadId);
  }
}

// Usage example
const uploader = new ChunkedUploader();

const fileInput = document.getElementById('fileInput');
fileInput.addEventListener('change', async (e) => {
  const file = e.target.files[0];

  try {
    const result = await uploader.uploadFile(file, {
      chunkSize: 5 * 1024 * 1024,
      threadId: 'thread_123',
      onProgress: (percent, current, total) => {
        console.log(`Progress: ${percent.toFixed(1)}% (${current}/${total} chunks)`);
        document.getElementById('progress').style.width = percent + '%';
      }
    });

    console.log('File uploaded successfully:', result.file_id);
  } catch (error) {
    console.error('Upload failed:', error);
  }
});
```

---

## Python Example

```python
import requests
import hashlib
from pathlib import Path

class ChunkedUploader:
    def __init__(self, base_url='http://localhost:8080'):
        self.base_url = base_url

    def upload_file(self, file_path, chunk_size=5*1024*1024, thread_id=None):
        """Upload file in chunks"""
        file_path = Path(file_path)
        file_size = file_path.stat().st_size

        # Step 1: Initialize
        init_response = requests.post(
            f'{self.base_url}/files/uploads/init',
            json={
                'filename': file_path.name,
                'mime_type': self._guess_mime_type(file_path),
                'total_size': file_size,
                'chunk_size': chunk_size,
                'thread_id': thread_id
            }
        )

        data = init_response.json()
        upload_id = data['upload_id']
        total_chunks = data['total_chunks']

        print(f"Upload initialized: {upload_id}, {total_chunks} chunks")

        # Step 2: Upload chunks
        with open(file_path, 'rb') as f:
            for chunk_index in range(total_chunks):
                # Read chunk
                chunk_data = f.read(chunk_size)

                # Calculate checksum
                checksum = f"sha256:{hashlib.sha256(chunk_data).hexdigest()}"

                # Upload chunk
                chunk_response = requests.post(
                    f'{self.base_url}/files/uploads/{upload_id}/chunks',
                    headers={
                        'X-Chunk-Index': str(chunk_index),
                        'X-Chunk-Checksum': checksum,
                        'Content-Type': 'application/octet-stream'
                    },
                    data=chunk_data
                )

                result = chunk_response.json()
                print(f"Chunk {chunk_index + 1}/{total_chunks} uploaded ({result['progress_percent']:.1f}%)")

        # Step 3: Complete
        complete_response = requests.post(
            f'{self.base_url}/files/uploads/{upload_id}/complete',
            json={'verify_checksums': True}
        )

        result = complete_response.json()
        print(f"Upload complete: {result['file_id']}")

        return result

    def resume_upload(self, upload_id, file_path):
        """Resume interrupted upload"""
        # Get status
        status_response = requests.get(
            f'{self.base_url}/files/uploads/{upload_id}/status'
        )
        status = status_response.json()

        print(f"Resuming: {status['uploaded_chunks']}/{status['total_chunks']} chunks")

        # Upload missing chunks
        chunk_size = status['total_size'] // status['total_chunks']

        with open(file_path, 'rb') as f:
            for chunk_index in status['missing_chunks']:
                # Seek to chunk position
                f.seek(chunk_index * chunk_size)

                # Read chunk
                chunk_data = f.read(chunk_size)

                # Calculate checksum
                checksum = f"sha256:{hashlib.sha256(chunk_data).hexdigest()}"

                # Upload
                requests.post(
                    f'{self.base_url}/files/uploads/{upload_id}/chunks',
                    headers={
                        'X-Chunk-Index': str(chunk_index),
                        'X-Chunk-Checksum': checksum
                    },
                    data=chunk_data
                )

                print(f"Resumed chunk {chunk_index + 1}")

        # Complete
        complete_response = requests.post(
            f'{self.base_url}/files/uploads/{upload_id}/complete',
            json={'verify_checksums': True}
        )

        return complete_response.json()

    def _guess_mime_type(self, file_path):
        import mimetypes
        mime_type, _ = mimetypes.guess_type(str(file_path))
        return mime_type or 'application/octet-stream'

# Usage
uploader = ChunkedUploader()
result = uploader.upload_file('large_video.mp4', thread_id='thread_123')
print(f"File ID: {result['file_id']}")
```

---

## Bash Script Example

```bash
#!/bin/bash

# Configuration
BASE_URL="http://localhost:8080"
FILE_PATH="$1"
CHUNK_SIZE=5242880  # 5MB
THREAD_ID="thread_123"

if [ -z "$FILE_PATH" ]; then
    echo "Usage: $0 <file_path>"
    exit 1
fi

FILENAME=$(basename "$FILE_PATH")
FILE_SIZE=$(stat -f%z "$FILE_PATH")
MIME_TYPE=$(file -b --mime-type "$FILE_PATH")

echo "Uploading: $FILENAME ($FILE_SIZE bytes)"

# Step 1: Initialize
INIT_RESPONSE=$(curl -s -X POST "$BASE_URL/files/uploads/init" \
  -H "Content-Type: application/json" \
  -d "{
    \"filename\": \"$FILENAME\",
    \"mime_type\": \"$MIME_TYPE\",
    \"total_size\": $FILE_SIZE,
    \"chunk_size\": $CHUNK_SIZE,
    \"thread_id\": \"$THREAD_ID\"
  }")

UPLOAD_ID=$(echo "$INIT_RESPONSE" | jq -r '.upload_id')
TOTAL_CHUNKS=$(echo "$INIT_RESPONSE" | jq -r '.total_chunks')

echo "Upload ID: $UPLOAD_ID"
echo "Total chunks: $TOTAL_CHUNKS"

# Step 2: Upload chunks
for ((i=0; i<$TOTAL_CHUNKS; i++)); do
    SKIP=$((i * CHUNK_SIZE))

    # Extract chunk
    dd if="$FILE_PATH" bs=1 skip=$SKIP count=$CHUNK_SIZE 2>/dev/null | \
    curl -s -X POST "$BASE_URL/files/uploads/$UPLOAD_ID/chunks" \
      -H "Content-Type: application/octet-stream" \
      -H "X-Chunk-Index: $i" \
      --data-binary @- > /dev/null

    echo "Uploaded chunk $((i + 1))/$TOTAL_CHUNKS"
done

# Step 3: Complete
COMPLETE_RESPONSE=$(curl -s -X POST "$BASE_URL/files/uploads/$UPLOAD_ID/complete" \
  -H "Content-Type: application/json" \
  -d '{"verify_checksums": true}')

FILE_ID=$(echo "$COMPLETE_RESPONSE" | jq -r '.file_id')

echo "Upload complete!"
echo "File ID: $FILE_ID"
```

---

## Key Differences: Standard vs Chunked

| Feature | Standard Multipart | Chunked Upload |
|---------|-------------------|----------------|
| **Requests** | 1 | Many (N+2) |
| **Resume** | ❌ No | ✅ Yes |
| **Max Size** | ~50MB | Unlimited |
| **Network Failure** | Start over | Resume from last chunk |
| **Progress** | Basic | Granular (per chunk) |
| **Implementation** | Simple | Complex |
| **Use Case** | Images, documents | Videos, large files |

---

## When to Use Which

**Use Standard Multipart when:**
- File < 50MB
- Don't need resume
- Simple implementation preferred

**Use Chunked Upload when:**
- File > 50MB
- Need resume capability
- Want detailed progress
- Unreliable network

---

## Summary

**Chunked upload works by:**
1. Client splits file into chunks (e.g., 5MB each)
2. Client uploads each chunk separately with index
3. Server stores chunks temporarily
4. Client tells server to assemble chunks
5. Server combines chunks into final file
6. Server deletes temporary chunks

**Key benefit:** If network fails, client can resume from last successful chunk instead of starting over!
