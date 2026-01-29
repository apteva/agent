# Multipart Upload Support - Proposal

## Executive Summary

Add `multipart/form-data` upload support to the agent's file system while maintaining backward compatibility with existing JSON base64 uploads.

**Benefits:**
- 33% smaller uploads (no base64 overhead)
- Native browser support (`<input type="file">`)
- Standard web practice
- Better for large files
- Simpler client code

**Effort:** ~150 lines of code, 2-3 hours implementation

---

## 1. Current State

### ✅ What Works Now
```bash
# JSON with base64
curl -X POST http://localhost:8080/files \
  -H "Content-Type: application/json" \
  -d '{
    "filename": "image.png",
    "mime_type": "image/png",
    "data": "iVBORw0KGgoAAAANSUhEUgAA..."
  }'
```

### ❌ What Doesn't Work
```bash
# Multipart form data (NOT SUPPORTED)
curl -X POST http://localhost:8080/files \
  -F "file=@image.png" \
  -F "thread_id=thread_123"
```

```html
<!-- HTML forms (NOT SUPPORTED) -->
<form action="http://localhost:8080/files" method="POST" enctype="multipart/form-data">
  <input type="file" name="file">
  <input type="submit">
</form>
```

---

## 2. Proposed Solution

### Architecture

**Dual-Mode Upload Handler:**
```
POST /files
    ↓
Check Content-Type header
    ↓
    ├─→ application/json        → Existing handler (base64)
    └─→ multipart/form-data     → NEW handler (binary)
            ↓
    ParseMultipartForm()
            ↓
    Extract file + metadata
            ↓
    Store via FileManager.Store()
```

### Key Design Decisions

#### 1. **Dual Support** (Both JSON and Multipart)
- Detect upload type via `Content-Type` header
- Keep existing JSON method unchanged
- Add new multipart path
- **No breaking changes**

#### 2. **Single Endpoint** (`POST /files`)
- Same endpoint for both methods
- Route based on `Content-Type`
- Unified response format
- Simpler API surface

#### 3. **Metadata Handling**
- Multipart: metadata in form fields
- JSON: metadata in JSON body
- Both map to same internal structure

#### 4. **Backward Compatibility**
- All existing integrations continue working
- JSON method remains primary for chat messages
- Multipart is additive feature

---

## 3. Implementation Details

### 3.1 New Handler Function

**File:** `filesystem/handlers.go`

```go
// HandleFilesCreateOrList - Updated to support both JSON and multipart
func HandleFilesCreateOrList(fm *FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// Check Content-Type to determine upload method
			contentType := r.Header.Get("Content-Type")

			if strings.HasPrefix(contentType, "multipart/form-data") {
				// NEW: Handle multipart upload
				handleMultipartUpload(fm, w, r)
			} else {
				// EXISTING: Handle JSON upload (unchanged)
				handleJSONUpload(fm, w, r)
			}
			return
		}

		if r.Method == http.MethodGet {
			HandleFilesList(fm)(w, r)
			return
		}

		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleJSONUpload - Existing logic extracted into separate function
func handleJSONUpload(fm *FileManager, w http.ResponseWriter, r *http.Request) {
	// ... existing JSON upload code (lines 18-82) ...
	// No changes to existing logic
}

// handleMultipartUpload - NEW function for multipart uploads
func handleMultipartUpload(fm *FileManager, w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 32MB in memory)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "Failed to parse multipart form",
		})
		return
	}
	defer r.MultipartForm.RemoveAll()

	// Get file from form
	file, header, err := r.FormFile("file")
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"error":   "No file provided (use 'file' field name)",
		})
		return
	}
	defer file.Close()

	// Read file data
	fileData, err := io.ReadAll(file)
	if err != nil {
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to read file data",
		})
		return
	}

	// Detect MIME type if not provided
	mimeType := r.FormValue("mime_type")
	if mimeType == "" {
		mimeType = header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = http.DetectContentType(fileData)
		}
	}

	// Determine file type from MIME type
	fileType := determineFileType(mimeType)

	// Parse metadata JSON if provided
	var metadata map[string]interface{}
	metadataStr := r.FormValue("metadata")
	if metadataStr != "" {
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
			log.Printf("Failed to parse metadata JSON: %v", err)
			metadata = make(map[string]interface{})
		}
	} else {
		metadata = make(map[string]interface{})
	}

	// Create file object
	fileObj := &File{
		Filename:   header.Filename,
		MimeType:   mimeType,
		FileType:   fileType,
		SizeBytes:  int64(len(fileData)),
		Data:       fileData,
		ThreadID:   r.FormValue("thread_id"),
		MessageID:  r.FormValue("message_id"),
		Source:     r.FormValue("source"),
		SourceTool: r.FormValue("source_tool"),
		Metadata:   metadata,
	}

	// Check file size limit
	if fm.config.MaxFileSize > 0 && fileObj.SizeBytes > fm.config.MaxFileSize {
		sendJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("File too large (max %d bytes)", fm.config.MaxFileSize),
		})
		return
	}

	// Store file
	fileID, err := fm.Store(r.Context(), fileObj)
	if err != nil {
		log.Printf("Failed to store file: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Failed to store file: %s", err.Error()),
		})
		return
	}

	// Return success response
	sendJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"file_id":  fileID,
		"filename": header.Filename,
		"size":     fileObj.SizeBytes,
		"mime_type": mimeType,
		"message":  fmt.Sprintf("File %s uploaded successfully", header.Filename),
	})
}

// determineFileType - Helper to determine file type from MIME type
func determineFileType(mimeType string) string {
	if strings.HasPrefix(mimeType, "image/") {
		return "image"
	}
	if strings.HasPrefix(mimeType, "video/") {
		return "video"
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return "audio"
	}
	if mimeType == "application/pdf" || strings.HasPrefix(mimeType, "text/") {
		return "document"
	}
	return "other"
}
```

### 3.2 Form Field Mapping

| Form Field | JSON Equivalent | Type | Required | Description |
|------------|-----------------|------|----------|-------------|
| `file` | `data` (base64) | file | ✅ Yes | The actual file binary |
| `thread_id` | `thread_id` | string | ❌ No | Thread association |
| `message_id` | `message_id` | string | ❌ No | Message association |
| `source` | `source` | string | ❌ No | Upload source (e.g., "user_upload") |
| `source_tool` | `source_tool` | string | ❌ No | Tool that created file |
| `metadata` | `metadata` | JSON string | ❌ No | Additional metadata as JSON |
| `mime_type` | `mime_type` | string | ❌ No | Override auto-detection |

**Note:** `filename` and `mime_type` are auto-detected from multipart headers if not provided.

---

## 4. API Examples

### 4.1 Multipart Upload (cURL)

**Basic Upload:**
```bash
curl -X POST http://localhost:8080/files \
  -F "file=@screenshot.png"
```

**With Metadata:**
```bash
curl -X POST http://localhost:8080/files \
  -F "file=@document.pdf" \
  -F "thread_id=thread_abc123" \
  -F "source=user_upload" \
  -F "metadata={\"description\":\"Important document\",\"tags\":[\"invoice\",\"2024\"]}"
```

**Response:**
```json
{
  "success": true,
  "file_id": "file_xyz789",
  "filename": "screenshot.png",
  "size": 245678,
  "mime_type": "image/png",
  "message": "File screenshot.png uploaded successfully"
}
```

### 4.2 HTML Form

**Simple Form:**
```html
<!DOCTYPE html>
<html>
<head>
    <title>Upload File to Agent</title>
</head>
<body>
    <h1>Upload File</h1>
    <form action="http://localhost:8080/files" method="POST" enctype="multipart/form-data">
        <label>File: <input type="file" name="file" required></label><br>
        <label>Thread ID: <input type="text" name="thread_id"></label><br>
        <label>Source: <input type="text" name="source" value="user_upload"></label><br>
        <button type="submit">Upload</button>
    </form>
</body>
</html>
```

**Advanced Form with Preview:**
```html
<!DOCTYPE html>
<html>
<head>
    <title>Agent File Upload</title>
    <style>
        #preview { max-width: 300px; max-height: 300px; margin: 10px 0; }
        .result { margin-top: 20px; padding: 10px; background: #f0f0f0; }
    </style>
</head>
<body>
    <h1>Upload File to Agent</h1>

    <form id="uploadForm">
        <div>
            <label>File: <input type="file" name="file" id="fileInput" required></label>
            <img id="preview" style="display:none;">
        </div>
        <div>
            <label>Thread ID: <input type="text" name="thread_id" id="threadId"></label>
        </div>
        <div>
            <label>Description: <input type="text" id="description"></label>
        </div>
        <div>
            <button type="submit">Upload</button>
        </div>
    </form>

    <div id="result" class="result" style="display:none;"></div>

    <script>
        // Preview image before upload
        document.getElementById('fileInput').addEventListener('change', function(e) {
            const file = e.target.files[0];
            if (file && file.type.startsWith('image/')) {
                const preview = document.getElementById('preview');
                preview.src = URL.createObjectURL(file);
                preview.style.display = 'block';
            }
        });

        // Handle form submission
        document.getElementById('uploadForm').addEventListener('submit', async function(e) {
            e.preventDefault();

            const formData = new FormData();
            const fileInput = document.getElementById('fileInput');
            const file = fileInput.files[0];

            if (!file) {
                alert('Please select a file');
                return;
            }

            // Add file and fields
            formData.append('file', file);

            const threadId = document.getElementById('threadId').value;
            if (threadId) {
                formData.append('thread_id', threadId);
            }

            const description = document.getElementById('description').value;
            if (description) {
                const metadata = JSON.stringify({ description });
                formData.append('metadata', metadata);
            }

            formData.append('source', 'web_form');

            // Upload
            try {
                const response = await fetch('http://localhost:8080/files', {
                    method: 'POST',
                    body: formData
                });

                const result = await response.json();

                const resultDiv = document.getElementById('result');
                resultDiv.style.display = 'block';

                if (result.success) {
                    resultDiv.innerHTML = `
                        <h3>✅ Upload Successful</h3>
                        <p><strong>File ID:</strong> ${result.file_id}</p>
                        <p><strong>Filename:</strong> ${result.filename}</p>
                        <p><strong>Size:</strong> ${(result.size / 1024).toFixed(2)} KB</p>
                        <p><strong>Type:</strong> ${result.mime_type}</p>
                    `;
                    resultDiv.style.background = '#d4edda';
                } else {
                    resultDiv.innerHTML = `
                        <h3>❌ Upload Failed</h3>
                        <p>${result.error}</p>
                    `;
                    resultDiv.style.background = '#f8d7da';
                }
            } catch (error) {
                console.error('Upload error:', error);
                alert('Upload failed: ' + error.message);
            }
        });
    </script>
</body>
</html>
```

### 4.3 JavaScript (Fetch API)

**Simple Upload:**
```javascript
async function uploadFile(file, threadId) {
    const formData = new FormData();
    formData.append('file', file);

    if (threadId) {
        formData.append('thread_id', threadId);
    }

    const response = await fetch('http://localhost:8080/files', {
        method: 'POST',
        body: formData
    });

    return await response.json();
}

// Usage
const fileInput = document.getElementById('fileInput');
const file = fileInput.files[0];
const result = await uploadFile(file, 'thread_123');
console.log('File ID:', result.file_id);
```

**With Progress Tracking:**
```javascript
async function uploadFileWithProgress(file, threadId, onProgress) {
    const formData = new FormData();
    formData.append('file', file);

    if (threadId) {
        formData.append('thread_id', threadId);
    }

    return new Promise((resolve, reject) => {
        const xhr = new XMLHttpRequest();

        // Track upload progress
        xhr.upload.addEventListener('progress', (e) => {
            if (e.lengthComputable) {
                const percentComplete = (e.loaded / e.total) * 100;
                onProgress(percentComplete);
            }
        });

        xhr.addEventListener('load', () => {
            if (xhr.status === 200) {
                resolve(JSON.parse(xhr.responseText));
            } else {
                reject(new Error(`Upload failed: ${xhr.statusText}`));
            }
        });

        xhr.addEventListener('error', () => {
            reject(new Error('Upload failed'));
        });

        xhr.open('POST', 'http://localhost:8080/files');
        xhr.send(formData);
    });
}

// Usage
const result = await uploadFileWithProgress(file, 'thread_123', (percent) => {
    console.log(`Upload: ${percent.toFixed(1)}%`);
    document.getElementById('progress').style.width = percent + '%';
});
```

### 4.4 React Component

```jsx
import React, { useState } from 'react';

function FileUploader({ threadId, onUploadComplete }) {
    const [file, setFile] = useState(null);
    const [uploading, setUploading] = useState(false);
    const [progress, setProgress] = useState(0);
    const [result, setResult] = useState(null);

    const handleFileChange = (e) => {
        setFile(e.target.files[0]);
        setResult(null);
    };

    const handleUpload = async () => {
        if (!file) return;

        setUploading(true);
        setProgress(0);

        const formData = new FormData();
        formData.append('file', file);

        if (threadId) {
            formData.append('thread_id', threadId);
        }

        try {
            const xhr = new XMLHttpRequest();

            xhr.upload.addEventListener('progress', (e) => {
                if (e.lengthComputable) {
                    const percent = (e.loaded / e.total) * 100;
                    setProgress(percent);
                }
            });

            const response = await new Promise((resolve, reject) => {
                xhr.addEventListener('load', () => {
                    if (xhr.status === 200) {
                        resolve(JSON.parse(xhr.responseText));
                    } else {
                        reject(new Error('Upload failed'));
                    }
                });
                xhr.addEventListener('error', () => reject(new Error('Upload failed')));
                xhr.open('POST', 'http://localhost:8080/files');
                xhr.send(formData);
            });

            setResult(response);
            setUploading(false);

            if (onUploadComplete) {
                onUploadComplete(response);
            }
        } catch (error) {
            console.error('Upload error:', error);
            setResult({ success: false, error: error.message });
            setUploading(false);
        }
    };

    return (
        <div className="file-uploader">
            <input
                type="file"
                onChange={handleFileChange}
                disabled={uploading}
            />

            {file && (
                <div>
                    <p>Selected: {file.name} ({(file.size / 1024).toFixed(2)} KB)</p>
                    <button onClick={handleUpload} disabled={uploading}>
                        {uploading ? 'Uploading...' : 'Upload'}
                    </button>
                </div>
            )}

            {uploading && (
                <div className="progress-bar">
                    <div
                        className="progress-fill"
                        style={{ width: `${progress}%` }}
                    >
                        {progress.toFixed(0)}%
                    </div>
                </div>
            )}

            {result && (
                <div className={`result ${result.success ? 'success' : 'error'}`}>
                    {result.success ? (
                        <>
                            <h4>✅ Upload Successful</h4>
                            <p>File ID: {result.file_id}</p>
                        </>
                    ) : (
                        <>
                            <h4>❌ Upload Failed</h4>
                            <p>{result.error}</p>
                        </>
                    )}
                </div>
            )}
        </div>
    );
}

export default FileUploader;
```

### 4.5 Python Example

```python
import requests

def upload_file(file_path, thread_id=None, metadata=None):
    """Upload file using multipart form data"""

    with open(file_path, 'rb') as f:
        files = {'file': f}

        data = {}
        if thread_id:
            data['thread_id'] = thread_id
        if metadata:
            import json
            data['metadata'] = json.dumps(metadata)

        response = requests.post(
            'http://localhost:8080/files',
            files=files,
            data=data
        )

        return response.json()

# Usage
result = upload_file(
    'screenshot.png',
    thread_id='thread_123',
    metadata={'description': 'Bug report screenshot'}
)

print(f"File uploaded: {result['file_id']}")
```

---

## 5. Comparison: JSON vs Multipart

### Size Comparison

**Original File:** 1MB (1,048,576 bytes)

| Method | Upload Size | Overhead | Efficiency |
|--------|-------------|----------|------------|
| **Multipart** | 1.05 MB | +5% (headers) | 95% |
| **JSON base64** | 1.40 MB | +33% (encoding) | 67% |

**Example:**
```bash
# 1MB image file

# Multipart: ~1.05MB upload
curl -X POST http://localhost:8080/files -F "file=@image.png"

# JSON: ~1.40MB upload
curl -X POST http://localhost:8080/files \
  -H "Content-Type: application/json" \
  -d '{"data": "'$(base64 image.png)'"}'
```

### Feature Comparison

| Feature | JSON (Current) | Multipart (Proposed) |
|---------|----------------|----------------------|
| **Upload Size** | +33% overhead | +5% overhead ✅ |
| **Browser Forms** | ❌ Not supported | ✅ Native support |
| **File Input** | ❌ Manual encoding | ✅ Direct upload |
| **Progress Tracking** | ⚠️ Difficult | ✅ XMLHttpRequest.upload |
| **Large Files** | ⚠️ Memory intensive | ✅ Streaming friendly |
| **Client Code** | ⚠️ More complex | ✅ Simpler |
| **Chat Integration** | ✅ Easy | ⚠️ Two-step process |
| **Metadata** | ✅ Structured JSON | ⚠️ Form fields |

### Use Case Recommendations

**Use JSON base64 when:**
- Uploading via `/chat` endpoint (inline with message)
- Small files (< 100KB)
- Programmatic API access
- Need structured metadata

**Use Multipart when:**
- Browser-based uploads
- Large files (> 1MB)
- HTML forms
- User-facing file uploads
- Progress tracking needed

---

## 6. Backward Compatibility

### ✅ Zero Breaking Changes

**Existing JSON uploads continue working exactly as before:**
```bash
# This still works unchanged
curl -X POST http://localhost:8080/files \
  -H "Content-Type: application/json" \
  -d '{"filename": "test.png", "data": "iVBORw..."}'
```

**New multipart uploads work alongside:**
```bash
# This is new addition
curl -X POST http://localhost:8080/files \
  -F "file=@test.png"
```

**Same endpoint, different Content-Type:**
- `Content-Type: application/json` → JSON handler (existing)
- `Content-Type: multipart/form-data` → Multipart handler (new)

**Same response format:**
```json
{
  "success": true,
  "file_id": "file_xyz",
  "message": "File uploaded successfully"
}
```

---

## 7. Implementation Plan

### Phase 1: Core Implementation (2-3 hours)

**Step 1: Extract existing JSON handler (30 min)**
- Move current upload logic to `handleJSONUpload()`
- No functional changes
- Test to ensure no regression

**Step 2: Implement multipart handler (1.5 hours)**
- Create `handleMultipartUpload()` function
- Parse multipart form data
- Extract file and metadata
- Call existing `FileManager.Store()`

**Step 3: Update router (15 min)**
- Modify `HandleFilesCreateOrList()` to route by Content-Type
- Add content type detection

**Step 4: Add helper functions (15 min)**
- `determineFileType()` - MIME type → file type
- Auto-detect MIME type if not provided

### Phase 2: Testing (1 hour)

**Unit Tests:**
- Test multipart parsing
- Test file extraction
- Test metadata parsing
- Test error cases

**Integration Tests:**
- Upload via cURL (multipart)
- Upload via HTML form
- Upload with metadata
- Large file upload
- Verify JSON uploads still work

**Test Cases:**
```bash
# Test 1: Simple multipart upload
curl -X POST http://localhost:8080/files -F "file=@test.png"

# Test 2: With metadata
curl -X POST http://localhost:8080/files \
  -F "file=@test.png" \
  -F "thread_id=thread_123" \
  -F "metadata={\"key\":\"value\"}"

# Test 3: Verify JSON still works
curl -X POST http://localhost:8080/files \
  -H "Content-Type: application/json" \
  -d '{"filename":"test.png","data":"iVBORw..."}'

# Test 4: Large file
curl -X POST http://localhost:8080/files \
  -F "file=@large_file.pdf"

# Test 5: HTML form
# Open test-upload-form.html in browser and upload
```

### Phase 3: Documentation (30 min)

**Update:**
- `FILE_UPLOAD_GUIDE.md` - Add multipart examples
- API documentation - Add multipart endpoint details
- Code comments - Document new functions

**Create:**
- `test-upload-form.html` - Example HTML form
- `examples/multipart-upload.js` - JavaScript examples
- `examples/multipart-upload.py` - Python examples

---

## 8. Files to Modify

| File | Changes | Lines | Complexity |
|------|---------|-------|------------|
| `filesystem/handlers.go` | Add multipart handler | ~120 | Medium |
| `filesystem/handlers_test.go` | Add tests | ~100 | Low |
| `FILE_UPLOAD_GUIDE.md` | Update documentation | ~50 | Low |
| `examples/upload-form.html` | New example | ~50 | Low |
| **Total** | | **~320** | **Low-Medium** |

---

## 9. Testing Strategy

### Manual Testing Checklist

**Multipart Uploads:**
- [ ] Upload PNG image via cURL
- [ ] Upload JPEG image via cURL
- [ ] Upload PDF document via cURL
- [ ] Upload with thread_id
- [ ] Upload with metadata JSON
- [ ] Upload with all optional fields
- [ ] Upload file > 10MB (should fail if limit set)
- [ ] HTML form upload in Chrome
- [ ] HTML form upload in Firefox
- [ ] HTML form upload in Safari
- [ ] Upload with progress tracking (JavaScript)

**JSON Uploads (Regression):**
- [ ] JSON upload still works
- [ ] JSON with base64 data
- [ ] JSON with all metadata fields
- [ ] Via `/chat` endpoint (inline)

**Error Cases:**
- [ ] No file provided
- [ ] File too large
- [ ] Invalid form data
- [ ] Invalid metadata JSON
- [ ] Wrong Content-Type

**Integration:**
- [ ] File stored in database
- [ ] File ID returned correctly
- [ ] File retrievable via GET /files/{id}
- [ ] File downloadable via /files/{id}/download
- [ ] File appears in /files list
- [ ] Deduplication works
- [ ] Stats updated correctly

### Automated Test Cases

```go
// filesystem/handlers_test.go

func TestMultipartUpload(t *testing.T) {
    // Setup
    fm := setupTestFileManager(t)
    handler := HandleFilesCreateOrList(fm)

    // Create multipart request
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)

    // Add file
    part, _ := writer.CreateFormFile("file", "test.png")
    part.Write([]byte("fake image data"))

    // Add metadata
    writer.WriteField("thread_id", "thread_123")
    writer.WriteField("source", "test")

    writer.Close()

    // Make request
    req := httptest.NewRequest("POST", "/files", body)
    req.Header.Set("Content-Type", writer.FormDataContentType())

    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    // Assert
    assert.Equal(t, http.StatusOK, rec.Code)

    var response map[string]interface{}
    json.Unmarshal(rec.Body.Bytes(), &response)

    assert.True(t, response["success"].(bool))
    assert.NotEmpty(t, response["file_id"])
}

func TestMultipartWithMetadata(t *testing.T) {
    // Test multipart upload with metadata JSON
    // ...
}

func TestJSONUploadStillWorks(t *testing.T) {
    // Ensure JSON uploads not broken
    // ...
}

func TestContentTypeRouting(t *testing.T) {
    // Test correct handler called based on Content-Type
    // ...
}
```

---

## 10. Security Considerations

### ✅ Built-in Protections

**1. File Size Limits**
- Configurable `max_file_size`
- Prevents DoS via large uploads
- Checked before storage

**2. MIME Type Validation**
- Validate against `allowed_types` config
- Auto-detect MIME type
- Prevent malicious file types

**3. Memory Limits**
- `ParseMultipartForm(32 << 20)` limits memory usage
- Large files handled efficiently
- Temporary files auto-cleaned

**4. File Name Sanitization**
- Use provided filename but validate
- Strip path traversal attempts
- Generate unique file IDs

**5. Rate Limiting** (Future)
- Consider adding rate limits per IP
- Prevent upload abuse
- Track upload frequency

### ⚠️ Additional Recommendations

**1. File Validation**
```go
// Validate file content matches MIME type
func validateFileContent(data []byte, mimeType string) error {
    detectedType := http.DetectContentType(data)

    if !strings.HasPrefix(detectedType, mimeType) {
        return fmt.Errorf("file content doesn't match declared type")
    }

    return nil
}
```

**2. Virus Scanning** (Optional)
- Integrate ClamAV or similar
- Scan files before storage
- Reject infected files

**3. Content Security**
- Don't execute uploaded files
- Serve with correct Content-Type
- Add Content-Security-Policy headers

---

## 11. Performance Considerations

### Memory Usage

**Multipart Parsing:**
- Files < 32MB: Stored in memory
- Files > 32MB: Written to temp files
- Auto-cleanup via `defer MultipartForm.RemoveAll()`

**Optimization:**
```go
// Current: Load entire file in memory
fileData, _ := io.ReadAll(file)

// Better for large files: Stream to storage
// (Future enhancement if needed)
```

### Benchmarks (Estimated)

| File Size | JSON (base64) | Multipart | Improvement |
|-----------|---------------|-----------|-------------|
| 100 KB | ~140 KB | ~105 KB | 25% smaller |
| 1 MB | ~1.4 MB | ~1.05 MB | 25% smaller |
| 5 MB | ~7 MB | ~5.25 MB | 25% smaller |
| 10 MB | ~14 MB | ~10.5 MB | 25% smaller |

**Upload Time (1 Mbps connection):**
- 1MB file via JSON: ~11 seconds
- 1MB file via multipart: ~8 seconds
- **27% faster**

---

## 12. Migration & Rollout

### Deployment Strategy

**Phase 1: Deploy Backend**
1. Deploy updated handler code
2. Both JSON and multipart work
3. No client changes needed
4. Zero downtime

**Phase 2: Update Documentation**
1. Add multipart examples to docs
2. Update API reference
3. Create example forms

**Phase 3: Update Clients (Optional)**
1. Migrate browser-based uploads to multipart
2. Keep programmatic uploads on JSON
3. Gradual migration, no rush

### Rollback Plan

**If issues found:**
1. Multipart uploads disabled via feature flag
2. JSON uploads continue working
3. No data loss
4. Zero impact on existing users

**Feature Flag Example:**
```go
// config.go
type FileSystemConfig struct {
    // ...
    EnableMultipartUpload bool `json:"enable_multipart_upload"`
}

// handlers.go
if contentType == "multipart/form-data" && fm.config.EnableMultipartUpload {
    handleMultipartUpload(fm, w, r)
} else {
    handleJSONUpload(fm, w, r)
}
```

---

## 13. Future Enhancements

### Post-MVP Improvements

**1. Chunked Uploads**
- For very large files (> 100MB)
- Upload in multiple parts
- Resume on failure

**2. Direct S3 Upload**
- Pre-signed URLs
- Upload directly to S3
- Bypass agent server

**3. Image Processing**
- Auto-generate thumbnails
- Resize large images
- Extract EXIF data

**4. Progress Events via SSE**
- Real-time upload progress
- Server-sent events
- Multiple simultaneous uploads

**5. Drag & Drop UI**
- Drop zone component
- Multiple file selection
- Visual feedback

---

## 14. Success Metrics

### Implementation Complete When:

✅ **Functional Requirements:**
- [ ] Multipart uploads work via cURL
- [ ] HTML forms work in all major browsers
- [ ] JSON uploads still work (no regression)
- [ ] Files stored correctly in database
- [ ] Same response format as JSON uploads
- [ ] Metadata properly parsed and stored

✅ **Performance Requirements:**
- [ ] Upload 25-33% faster than JSON
- [ ] Memory usage under control
- [ ] No memory leaks

✅ **Quality Requirements:**
- [ ] All tests passing
- [ ] Code reviewed
- [ ] Documentation updated
- [ ] Examples provided

✅ **User Experience:**
- [ ] Simpler client code
- [ ] Native browser support
- [ ] Progress tracking works
- [ ] Error messages clear

---

## 15. Decision: Implement or Not?

### ✅ Reasons to Implement

**1. User Experience**
- Native browser file uploads
- Standard web practice
- Simpler for developers

**2. Performance**
- 25-33% smaller uploads
- Faster upload times
- Less memory usage

**3. Compatibility**
- Works with HTML forms
- Standard HTTP protocol
- Wide client library support

**4. Maintainability**
- Clean separation of concerns
- Both methods coexist
- Easy to test

### ⚠️ Reasons to Wait

**1. Current JSON Works**
- Existing integration stable
- No user complaints
- API working fine

**2. Limited Use Case**
- Chat endpoint uses JSON
- Programmatic access prefers JSON
- Multipart mainly for browsers

**3. Development Time**
- 3-4 hours implementation
- Testing and documentation
- Other priorities may exist

---

## 16. Recommendation

### ✅ **Recommended: Implement Multipart Support**

**Why:**
1. **Low effort, high value** - 3-4 hours for significant UX improvement
2. **No breaking changes** - Completely backward compatible
3. **Industry standard** - Expected feature for file uploads
4. **Better performance** - 25-33% bandwidth savings
5. **Future-proof** - Enables browser-based UIs

**When:**
- Now if building browser UI
- Later if only using programmatic access
- Never if JSON is sufficient

**How:**
- Implement dual-mode handler
- Keep JSON as default for chat
- Add multipart for `/files` endpoint
- Test thoroughly
- Document both methods

---

## 17. Next Steps

### If Approved:

**1. Implementation (3 hours)**
- [ ] Extract JSON handler to separate function
- [ ] Implement multipart handler
- [ ] Update router to detect Content-Type
- [ ] Add helper functions

**2. Testing (1 hour)**
- [ ] Write unit tests
- [ ] Manual testing with cURL
- [ ] HTML form testing
- [ ] Regression tests for JSON

**3. Documentation (30 min)**
- [ ] Update FILE_UPLOAD_GUIDE.md
- [ ] Create example HTML form
- [ ] Add JavaScript examples
- [ ] Update API docs

**4. Deployment (15 min)**
- [ ] Build and test
- [ ] Deploy to server
- [ ] Smoke test both methods
- [ ] Monitor for issues

### Total Time: ~4.5 hours

---

## Questions for Discussion

1. **Priority:** High/Medium/Low? (Impacts when to implement)
2. **Feature Flag:** Include feature flag for gradual rollout?
3. **Max Upload Size:** Keep at 10MB or increase for multipart?
4. **Browser UI:** Planning to build browser-based file upload UI?
5. **Other Endpoints:** Should chat endpoint also support multipart?

---

## Appendix: Related Documentation

- **Current Implementation:** `filesystem/handlers.go`
- **File Manager:** `filesystem/manager.go`
- **Configuration:** `config/config.go`
- **Upload Guide:** `FILE_UPLOAD_GUIDE.md`
- **Implementation Docs:** `FILESYSTEM-IMPLEMENTATION.md`

---

**Status:** 📋 Proposal - Awaiting Approval
**Estimated Effort:** 4-5 hours (implementation + testing + docs)
**Risk:** Low (backward compatible, well-tested pattern)
**Impact:** Medium-High (enables browser uploads, better performance)
