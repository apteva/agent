// Files Tab Functions

async function loadFiles() {
    const typeFilter = document.getElementById('fileTypeFilter').value;
    let endpoint = '/files';
    if (typeFilter) {
        endpoint += `?type=${typeFilter}`;
    }

    const response = await makeRequest(endpoint);

    if (response.status === 200 && response.data) {
        const files = response.data.files || [];

        // Update stats
        let totalSize = 0;
        let images = 0;
        let docs = 0;

        files.forEach(f => {
            totalSize += f.size_bytes || 0;
            if (f.mime_type?.startsWith('image/')) images++;
            else docs++;
        });

        document.getElementById('filesTotal').textContent = files.length;
        document.getElementById('filesTotalSize').textContent = formatBytes(totalSize);
        document.getElementById('filesImages').textContent = images;
        document.getElementById('filesDocs').textContent = docs;

        // Render files
        const container = document.getElementById('filesList');

        if (files.length === 0) {
            container.innerHTML = `
                <div class="empty-state text-center py-12 text-slate-400 dark:text-slate-500">
                    <svg class="empty-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1"><path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 014.5 9.75h15A2.25 2.25 0 0121.75 12v.75m-8.69-6.44l-2.12-2.12a1.5 1.5 0 00-1.061-.44H4.5A2.25 2.25 0 002.25 6v12a2.25 2.25 0 002.25 2.25h15A2.25 2.25 0 0021.75 18V9a2.25 2.25 0 00-2.25-2.25h-5.379a1.5 1.5 0 01-1.06-.44z"/></svg>
                    <p>No files stored</p>
                </div>`;
            return;
        }

        container.innerHTML = files.map(file => {
            const isImage = file.mime_type?.startsWith('image/');
            const dotColor = isImage ? 'bg-blue-400' : (file.mime_type?.includes('pdf') ? 'bg-red-400' : 'bg-slate-400');

            return `
                <div class="p-4 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors flex items-center gap-4">
                    <div class="w-3 h-3 rounded-full ${dotColor} flex-shrink-0"></div>
                    <div class="flex-1 min-w-0">
                        <div class="font-medium text-slate-800 dark:text-slate-100 truncate">${escapeHtml(file.filename || file.id)}</div>
                        <div class="text-sm text-slate-500 dark:text-slate-400 flex gap-3">
                            <span>${formatBytes(file.size_bytes || 0)}</span>
                            <span>${file.mime_type || 'unknown'}</span>
                            <span>${formatRelativeTime(file.created_at)}</span>
                        </div>
                    </div>
                    <div class="flex gap-2">
                        ${isImage ? `
                            <button onclick="previewFile('${file.id}')" class="px-3 py-1 text-sm bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300 rounded-lg hover:bg-blue-200 dark:hover:bg-blue-800">
                                Preview
                            </button>
                        ` : ''}
                        <button onclick="downloadFile('${file.id}')" class="px-3 py-1 text-sm bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300 rounded-lg hover:bg-slate-200 dark:hover:bg-slate-600">
                            Download
                        </button>
                        <button onclick="deleteFile('${file.id}')" class="px-3 py-1 text-sm bg-red-100 dark:bg-red-900/50 text-red-700 dark:text-red-300 rounded-lg hover:bg-red-200 dark:hover:bg-red-800">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                        </button>
                    </div>
                </div>
            `;
        }).join('');
    }
}

async function previewFile(fileId) {
    window.open(`/files/${fileId}`, '_blank');
}

async function downloadFile(fileId) {
    window.location.href = `/files/${fileId}/download`;
}

async function deleteFile(fileId) {
    if (!confirm('Delete this file?')) return;

    const response = await makeRequest(`/files/${fileId}`, 'DELETE');

    if (response.status === 200) {
        showToast('File deleted', 'success');
        loadFiles();
    } else {
        showToast('Failed to delete file', 'error');
    }
}

async function cleanupFiles() {
    if (!confirm('Run cleanup to remove orphaned and expired files?')) return;

    const response = await makeRequest('/files/cleanup', 'POST');

    if (response.status === 200) {
        const result = response.data;
        showToast(`Cleaned up ${result.removed || 0} files`, 'success');
        loadFiles();
    } else {
        showToast('Failed to run cleanup', 'error');
    }
}

async function uploadFile() {
    const input = document.getElementById('fileUploadInput');
    const files = input.files;

    if (!files || files.length === 0) return;

    let uploaded = 0;
    let failed = 0;

    for (const file of files) {
        try {
            // Read file as base64
            const base64Data = await new Promise((resolve, reject) => {
                const reader = new FileReader();
                reader.onload = () => {
                    // Remove data URL prefix (e.g., "data:image/png;base64,")
                    const result = reader.result;
                    const base64 = result.split(',')[1];
                    resolve(base64);
                };
                reader.onerror = reject;
                reader.readAsDataURL(file);
            });

            // Determine file type category
            let fileType = 'document';
            if (file.type.startsWith('image/')) fileType = 'image';
            else if (file.type.startsWith('audio/')) fileType = 'audio';
            else if (file.type.startsWith('video/')) fileType = 'video';

            // Upload to API
            const response = await makeRequest('/files', 'POST', {
                filename: file.name,
                mime_type: file.type || 'application/octet-stream',
                file_type: fileType,
                size_bytes: file.size,
                data: base64Data,
                source: 'manual_upload',
                metadata: {
                    uploaded_at: new Date().toISOString(),
                    original_name: file.name
                }
            });

            if (response.status === 200 && response.data?.success) {
                uploaded++;
            } else {
                failed++;
                console.error('Upload failed:', response.data?.error || 'Unknown error');
            }
        } catch (err) {
            failed++;
            console.error('Upload error:', err);
        }
    }

    // Clear input
    input.value = '';

    // Show result
    if (uploaded > 0) {
        showToast(`Uploaded ${uploaded} file${uploaded > 1 ? 's' : ''}${failed > 0 ? ` (${failed} failed)` : ''}`, failed > 0 ? 'warning' : 'success');
        loadFiles();
    } else {
        showToast('Failed to upload files', 'error');
    }
}

function initFilesTab() {
    loadFiles();
}
