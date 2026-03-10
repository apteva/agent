// Chat Tab Functions

let chatThreadId = null;
let isChatStreaming = false;
let chatRequestId = null; // Current request ID for cancellation
let chatAttachedFiles = []; // Files attached to current message
let rawChunksCount = 0;
let rawChunksData = []; // Store full chunk data for copy/export
let currentChunkDetail = null; // Currently viewed chunk in modal

// Test mode functions — per-request only (toggle state is local, sent via X-Test-Mode header)
function toggleTestMode(enabled) {
    updateTestModeBanner(enabled);
    console.log('Test mode (per-request):', enabled ? 'enabled' : 'disabled');
    showToast(enabled ? 'Test Mode enabled — tools will be simulated for chat requests.' : 'Test Mode disabled.', enabled ? 'info' : 'success');
}

async function loadTestModeState() {
    // Test mode is per-request only — default to off on page load
    const toggle = document.getElementById('testModeToggle');
    if (toggle) {
        toggle.checked = false;
        updateTestModeBanner(false);
    }
}

function updateTestModeBanner(enabled) {
    const banner = document.getElementById('testModeBanner');
    if (banner) {
        banner.classList.toggle('hidden', !enabled);
    }
}

// Setup mode functions
async function toggleSetupMode(enabled) {
    try {
        const headers = { 'Content-Type': 'application/json' };
        if (getApiKey()) headers['X-API-Key'] = getApiKey();
        const response = await fetch('/config', {
            method: 'POST',
            headers,
            body: JSON.stringify({ setup_mode: enabled })
        });

        if (!response.ok) {
            throw new Error('Failed to toggle setup mode');
        }

        const result = await response.json();
        console.log('Setup mode:', enabled ? 'enabled' : 'disabled', result);

        // Show notification
        const msg = enabled ?
            'Setup Mode enabled. Agent can now configure itself.' :
            'Setup Mode disabled. Configuration applied.';
        showToast(msg, enabled ? 'info' : 'success');

        // If we're enabling setup mode, add a system message to the chat
        if (enabled) {
            addSystemMessage('Setup mode enabled. You can now ask the agent to configure itself (e.g., "Configure yourself to be a code reviewer with file access")');
        }
    } catch (error) {
        console.error('Error toggling setup mode:', error);
        // Revert toggle
        document.getElementById('setupModeToggle').checked = !enabled;
        showToast('Failed to toggle setup mode', 'error');
    }
}

// Load initial setup mode state
async function loadSetupModeState() {
    try {
        const headers = {};
        if (getApiKey()) headers['X-API-Key'] = getApiKey();
        const response = await fetch('/config', { headers });
        const config = await response.json();
        const toggle = document.getElementById('setupModeToggle');
        if (toggle && config.setup_mode !== undefined) {
            toggle.checked = config.setup_mode;
        }
    } catch (error) {
        console.error('Error loading setup mode state:', error);
    }
}

// Helper to show toast notification
function showToast(message, type = 'info') {
    // Create toast element if it doesn't exist
    let toast = document.getElementById('chatToast');
    if (!toast) {
        toast = document.createElement('div');
        toast.id = 'chatToast';
        toast.className = 'fixed bottom-4 right-4 px-4 py-2 rounded-lg shadow-lg transition-opacity duration-300 z-50';
        document.body.appendChild(toast);
    }

    // Set color based on type
    const colors = {
        info: 'bg-blue-600 text-white',
        success: 'bg-green-600 text-white',
        error: 'bg-red-600 text-white'
    };
    toast.className = `fixed bottom-4 right-4 px-4 py-2 rounded-lg shadow-lg transition-opacity duration-300 z-50 ${colors[type] || colors.info}`;
    toast.textContent = message;
    toast.style.opacity = '1';

    // Auto-hide after 3 seconds
    setTimeout(() => {
        toast.style.opacity = '0';
    }, 3000);
}

// Add system message to chat
function addSystemMessage(text) {
    const container = document.getElementById('chatMessagesContainer');
    if (!container) return;

    const msgDiv = document.createElement('div');
    msgDiv.className = 'text-center py-2';
    msgDiv.innerHTML = `<span class="inline-block px-3 py-1 bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 rounded-full text-sm">${text}</span>`;
    container.appendChild(msgDiv);
    container.scrollTop = container.scrollHeight;
}

// Initialize toggle states when chat tab loads
document.addEventListener('DOMContentLoaded', () => {
    loadSetupModeState();
    loadTestModeState();
});

// Raw chunks viewer functions
function addRawChunk(rawLine, parsedData) {
    const container = document.getElementById('rawChunksList');
    if (!container) return;

    // Clear placeholder on first chunk
    if (rawChunksCount === 0) {
        container.innerHTML = '';
    }

    const chunkIndex = rawChunksCount;
    rawChunksCount++;

    // Store full data for copy/export
    const timestamp = new Date().toISOString();
    const chunkData = {
        index: chunkIndex,
        timestamp: timestamp,
        rawLine: rawLine,
        parsedData: parsedData,
        type: parsedData?.type || 'raw'
    };
    rawChunksData.push(chunkData);

    const chunkDiv = document.createElement('div');
    chunkDiv.className = 'chunk-item border-b border-slate-700 pb-2 cursor-pointer hover:bg-slate-800/50 rounded px-1 -mx-1 transition-colors';
    chunkDiv.dataset.index = chunkIndex;
    chunkDiv.dataset.type = chunkData.type;
    chunkDiv.onclick = () => showChunkDetail(chunkIndex);

    // Determine color based on type
    let typeColor = 'text-slate-400';
    if (parsedData) {
        switch (parsedData.type) {
            case 'tool_use':
            case 'tool_call':
                typeColor = 'text-purple-400';
                break;
            case 'tool_result':
                typeColor = 'text-emerald-400';
                break;
            case 'content':
                typeColor = 'text-blue-400';
                break;
            case 'thinking':
                typeColor = 'text-pink-400';
                break;
            case 'error':
                typeColor = 'text-red-400';
                break;
            case 'start':
            case 'stop':
                typeColor = 'text-yellow-400';
                break;
            case 'tool_input_delta':
                typeColor = 'text-cyan-400';
                break;
            default:
                typeColor = 'text-slate-400';
        }
    }

    // Truncate only base64 data for display (full data available in modal)
    let displayData = rawLine;
    if (displayData.includes('"data":"') && displayData.includes('base64')) {
        displayData = displayData.replace(/"data":"[A-Za-z0-9+/=]{100,}"/, '"data":"[BASE64_TRUNCATED]"');
    }

    const displayTime = new Date().toLocaleTimeString();
    chunkDiv.innerHTML = `
        <div class="flex items-center gap-2 mb-1">
            <span class="text-slate-600 text-[10px]">#${chunkIndex}</span>
            <span class="text-slate-500">${displayTime}</span>
            <span class="${typeColor} font-semibold">${parsedData?.type || 'raw'}</span>
            ${parsedData?.tool_name ? `<span class="text-orange-400">${parsedData.tool_name}</span>` : ''}
            ${parsedData?.tool_id ? `<span class="text-slate-600">${parsedData.tool_id.substring(0, 8)}...</span>` : ''}
            <button onclick="event.stopPropagation(); copyChunk(${chunkIndex})" class="ml-auto text-slate-500 hover:text-blue-400 text-[10px]" title="Copy this chunk">Copy</button>
        </div>
        <pre class="text-green-400 whitespace-pre-wrap break-all text-[10px]">${escapeHtml(displayData)}</pre>
    `;

    // Prepend to show newest first
    container.insertBefore(chunkDiv, container.firstChild);

    // Update count
    document.getElementById('rawChunksCount').textContent = `${rawChunksCount} chunks`;
}

function clearRawChunks() {
    const container = document.getElementById('rawChunksList');
    if (container) {
        container.innerHTML = '<div class="text-slate-500 text-center py-4">Chunks will appear here...</div>';
    }
    rawChunksCount = 0;
    rawChunksData = [];
    document.getElementById('rawChunksCount').textContent = '0 chunks';
    // Clear filters
    const filterInput = document.getElementById('chunkFilter');
    const typeFilter = document.getElementById('chunkTypeFilter');
    if (filterInput) filterInput.value = '';
    if (typeFilter) typeFilter.value = '';
}

// Fallback copy function for non-HTTPS environments
function copyToClipboard(text) {
    // Try modern clipboard API first
    if (navigator.clipboard && window.isSecureContext) {
        return navigator.clipboard.writeText(text);
    }

    // Fallback for non-HTTPS
    return new Promise((resolve, reject) => {
        const textArea = document.createElement('textarea');
        textArea.value = text;
        textArea.style.position = 'fixed';
        textArea.style.left = '-999999px';
        textArea.style.top = '-999999px';
        document.body.appendChild(textArea);
        textArea.focus();
        textArea.select();

        try {
            const successful = document.execCommand('copy');
            document.body.removeChild(textArea);
            if (successful) {
                resolve();
            } else {
                reject(new Error('execCommand copy failed'));
            }
        } catch (err) {
            document.body.removeChild(textArea);
            reject(err);
        }
    });
}

// Copy single chunk
function copyChunk(index) {
    const chunk = rawChunksData[index];
    if (!chunk) return;

    const text = chunk.rawLine;
    copyToClipboard(text).then(() => {
        showToast('Chunk copied to clipboard', 'success');
    }).catch(err => {
        console.error('Copy failed:', err);
        showToast('Failed to copy: ' + err.message, 'error');
    });
}

// Copy all chunks - opens modal with selectable textarea
function copyAllChunks() {
    if (rawChunksData.length === 0) {
        showToast('No chunks to copy', 'info');
        return;
    }

    const text = rawChunksData.map(c => `[${c.timestamp}] ${c.rawLine}`).join('\n\n');

    const modal = document.getElementById('copyAllModal');
    const content = document.getElementById('copyAllContent');
    const meta = document.getElementById('copyAllMeta');

    content.value = text;
    meta.textContent = `${rawChunksData.length} chunks | ${text.length} characters`;

    modal.classList.remove('hidden');
    modal.classList.add('flex');

    // Auto-select the content
    setTimeout(() => {
        content.focus();
        content.select();
    }, 100);
}

// Close copy all modal
function closeCopyAllModal() {
    const modal = document.getElementById('copyAllModal');
    modal.classList.add('hidden');
    modal.classList.remove('flex');
}

// Select all text in copy all modal
function selectAllInCopyModal() {
    const content = document.getElementById('copyAllContent');
    content.focus();
    content.select();
}

// Export chunks as JSON file
function exportChunksJSON() {
    if (rawChunksData.length === 0) {
        showToast('No chunks to export', 'info');
        return;
    }

    const exportData = {
        exported_at: new Date().toISOString(),
        total_chunks: rawChunksData.length,
        chunks: rawChunksData.map(c => ({
            index: c.index,
            timestamp: c.timestamp,
            type: c.type,
            raw: c.rawLine,
            parsed: c.parsedData
        }))
    };

    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `chunks_${new Date().toISOString().replace(/[:.]/g, '-')}.json`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
    showToast(`Exported ${rawChunksData.length} chunks`, 'success');
}

// Filter chunks by text and type
function filterChunks() {
    const filterText = (document.getElementById('chunkFilter')?.value || '').toLowerCase();
    const typeFilter = document.getElementById('chunkTypeFilter')?.value || '';

    const container = document.getElementById('rawChunksList');
    if (!container) return;

    const items = container.querySelectorAll('.chunk-item');
    let visibleCount = 0;

    items.forEach(item => {
        const index = parseInt(item.dataset.index);
        const chunk = rawChunksData[index];
        if (!chunk) return;

        const matchesText = !filterText || chunk.rawLine.toLowerCase().includes(filterText);
        const matchesType = !typeFilter || chunk.type === typeFilter;

        if (matchesText && matchesType) {
            item.classList.remove('hidden');
            visibleCount++;
        } else {
            item.classList.add('hidden');
        }
    });

    document.getElementById('rawChunksCount').textContent =
        filterText || typeFilter
            ? `${visibleCount}/${rawChunksCount} chunks`
            : `${rawChunksCount} chunks`;
}

// Show chunk detail in modal
function showChunkDetail(index) {
    const chunk = rawChunksData[index];
    if (!chunk) return;

    currentChunkDetail = chunk;

    const modal = document.getElementById('chunkDetailModal');
    const content = document.getElementById('chunkDetailContent');
    const meta = document.getElementById('chunkDetailMeta');

    // Pretty print if it's JSON
    let displayContent = chunk.rawLine;
    try {
        if (chunk.rawLine.startsWith('data: ')) {
            const jsonPart = chunk.rawLine.slice(6);
            const parsed = JSON.parse(jsonPart);
            displayContent = 'data: ' + JSON.stringify(parsed, null, 2);
        }
    } catch (e) {
        // Keep original
    }

    content.value = displayContent;
    meta.innerHTML = `
        <span>Index: #${chunk.index}</span>
        <span class="mx-2">|</span>
        <span>Type: ${chunk.type}</span>
        <span class="mx-2">|</span>
        <span>Time: ${new Date(chunk.timestamp).toLocaleString()}</span>
        <span class="mx-2">|</span>
        <span>Size: ${chunk.rawLine.length} bytes</span>
    `;

    modal.classList.remove('hidden');
    modal.classList.add('flex');

    // Auto-select the content
    setTimeout(() => {
        content.focus();
        content.select();
    }, 100);
}

// Select all text in chunk detail modal
function selectAllChunkDetail() {
    const content = document.getElementById('chunkDetailContent');
    content.focus();
    content.select();
}

// Close chunk modal
function closeChunkModal() {
    const modal = document.getElementById('chunkDetailModal');
    modal.classList.add('hidden');
    modal.classList.remove('flex');
    currentChunkDetail = null;
}

// Copy chunk from modal
function copyChunkDetail() {
    if (!currentChunkDetail) return;

    copyToClipboard(currentChunkDetail.rawLine).then(() => {
        showToast('Chunk copied to clipboard', 'success');
    }).catch(err => {
        console.error('Copy detail failed:', err);
        showToast('Failed to copy: ' + err.message, 'error');
    });
}

// Close modals on escape key
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        closeChunkModal();
        closeCopyAllModal();
    }
});

function addChatMessage(role, content, isStreaming = false, attachments = []) {
    const container = document.getElementById('chatMessagesContainer');

    // Remove welcome message if present
    const welcome = container.querySelector('.text-center');
    if (welcome) {
        container.innerHTML = '';
    }

    const isUser = role === 'user';
    const div = document.createElement('div');
    div.className = `flex ${isUser ? 'justify-end' : 'justify-start'}`;

    const bgColor = isUser ? 'bg-blue-600 text-white' : 'bg-slate-100 dark:bg-slate-700 text-slate-800 dark:text-slate-100';
    const rounded = isUser ? 'rounded-br-md' : 'rounded-bl-md';

    // Build attachments HTML
    let attachmentsHtml = '';
    if (attachments && attachments.length > 0) {
        attachmentsHtml = '<div class="flex flex-wrap gap-2 mb-2">';
        for (const att of attachments) {
            if (att.type === 'image' && att.preview) {
                attachmentsHtml += `<img src="${att.preview}" class="max-w-[150px] max-h-[100px] rounded-lg object-cover" alt="${escapeHtml(att.name)}">`;
            } else {
                const icon = `<svg class="w-3.5 h-3.5 inline-block" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="${att.mediaType === 'application/pdf' ? 'M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z' : 'M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13'}"/></svg>`;
                attachmentsHtml += `<div class="flex items-center gap-1 px-2 py-1 bg-white/20 rounded-lg text-xs">${icon} ${escapeHtml(att.name)}</div>`;
            }
        }
        attachmentsHtml += '</div>';
    }

    div.innerHTML = `
        <div class="max-w-[80%] ${bgColor} rounded-2xl px-4 py-3 ${rounded}">
            ${attachmentsHtml}
            <div class="message-content text-sm">${isStreaming ? '' : (isUser ? escapeHtml(content) : marked.parse(content))}</div>
            ${isStreaming ? '<div class="typing-indicator text-slate-400"><span>.</span><span>.</span><span>.</span></div>' : ''}
        </div>
    `;

    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
    return div;
}

// Track thinking content
let currentThinkingDiv = null;
let thinkingContent = '';

function addChatThinking(content) {
    const container = document.getElementById('chatMessagesContainer');

    // Accumulate thinking content
    thinkingContent += content;

    // Create or update thinking div
    if (!currentThinkingDiv) {
        const div = document.createElement('div');
        div.className = 'flex justify-start thinking-container';
        div.innerHTML = `
            <div class="max-w-[90%] w-full bg-pink-50 dark:bg-pink-900/20 border border-pink-200 dark:border-pink-800 rounded-xl overflow-hidden">
                <button onclick="toggleThinking(this)" class="w-full flex items-center justify-between px-4 py-2 bg-pink-100 dark:bg-pink-900/40 hover:bg-pink-200 dark:hover:bg-pink-900/60 transition-colors">
                    <div class="flex items-center gap-2 text-sm text-pink-700 dark:text-pink-300">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z"/></svg>
                        <span class="font-medium">Thinking</span>
                        <span class="thinking-status text-pink-500 dark:text-pink-400 text-xs">(streaming...)</span>
                    </div>
                    <svg class="thinking-chevron w-4 h-4 text-pink-500 transition-transform" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
                    </svg>
                </button>
                <div class="thinking-content px-4 py-3 text-sm text-pink-800 dark:text-pink-200 font-mono whitespace-pre-wrap max-h-64 overflow-y-auto"></div>
            </div>
        `;
        container.appendChild(div);
        currentThinkingDiv = div;
    }

    // Update content
    const contentDiv = currentThinkingDiv.querySelector('.thinking-content');
    contentDiv.textContent = thinkingContent;
    contentDiv.scrollTop = contentDiv.scrollHeight;

    // Scroll chat to bottom
    container.scrollTop = container.scrollHeight;
}

function finalizeThinking() {
    if (currentThinkingDiv) {
        const status = currentThinkingDiv.querySelector('.thinking-status');
        if (status) {
            status.textContent = `(${thinkingContent.length} chars)`;
        }
    }
    currentThinkingDiv = null;
    thinkingContent = '';
}

function toggleThinking(btn) {
    const content = btn.parentElement.querySelector('.thinking-content');
    const chevron = btn.querySelector('.thinking-chevron');
    content.classList.toggle('hidden');
    chevron.classList.toggle('rotate-180');
}

// Track accumulated tool input for each tool
const toolInputAccumulator = {};

function addChatToolCall(toolName, toolId, displayName) {
    const container = document.getElementById('chatMessagesContainer');
    const div = document.createElement('div');
    div.className = 'flex justify-start';
    div.id = 'chat-tool-' + toolId;
    // Reset accumulator for this tool
    toolInputAccumulator[toolId] = '';
    // Use display name if provided, otherwise fall back to tool name
    const shownName = displayName || toolName;
    div.innerHTML = `
        <div class="max-w-[80%] bg-purple-100 dark:bg-purple-900/50 border border-purple-200 dark:border-purple-800 rounded-xl px-4 py-2">
            <div class="flex items-center gap-2 text-sm text-purple-700 dark:text-purple-300">
                <svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
                </svg>
                <span>Calling <strong>${escapeHtml(shownName)}</strong>...</span>
            </div>
            <div id="chat-tool-input-${toolId}" class="hidden mt-2 text-xs text-purple-600 dark:text-purple-400 font-mono bg-purple-50 dark:bg-purple-950 rounded p-2 max-h-32 overflow-y-auto whitespace-pre-wrap break-all"></div>
        </div>
    `;
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
}

function updateChatToolInput(toolId, inputDelta) {
    // Accumulate the input
    if (!toolInputAccumulator[toolId]) {
        toolInputAccumulator[toolId] = '';
    }
    toolInputAccumulator[toolId] += inputDelta;

    const inputDiv = document.getElementById('chat-tool-input-' + toolId);
    if (inputDiv) {
        // Show the input container
        inputDiv.classList.remove('hidden');
        // Format and display - truncate if very long
        let displayText = toolInputAccumulator[toolId];
        if (displayText.length > 500) {
            displayText = displayText.slice(0, 500) + '...';
        }
        inputDiv.textContent = displayText;
        // Auto-scroll
        inputDiv.scrollTop = inputDiv.scrollHeight;
        // Also scroll the container
        const container = document.getElementById('chatMessagesContainer');
        container.scrollTop = container.scrollHeight;
    }
}

function updateChatToolResult(toolId, result) {
    const toolDiv = document.getElementById('chat-tool-' + toolId);
    if (toolDiv) {
        let resultPreview = result;
        let isTestMode = false;
        try {
            const parsed = JSON.parse(result);
            isTestMode = parsed.test_mode === true;
            if (parsed.message) resultPreview = parsed.message;
            else if (parsed.status) resultPreview = parsed.status;
            else if (parsed.success !== undefined) resultPreview = parsed.success ? 'Success' : 'Failed';
            else resultPreview = 'Completed';
        } catch (e) {
            resultPreview = result.length > 50 ? result.slice(0, 50) + '...' : result;
        }
        const testBadge = isTestMode ? '<span class="px-1.5 py-0.5 bg-amber-200 dark:bg-amber-800 text-amber-800 dark:text-amber-200 text-xs font-medium rounded">TEST</span>' : '';
        const bgClass = isTestMode
            ? 'bg-amber-50 dark:bg-amber-900/30 border-amber-200 dark:border-amber-800'
            : 'bg-emerald-100 dark:bg-emerald-900/50 border-emerald-200 dark:border-emerald-800';
        const textClass = isTestMode
            ? 'text-amber-700 dark:text-amber-300'
            : 'text-emerald-700 dark:text-emerald-300';
        toolDiv.innerHTML = `
            <div class="max-w-[80%] ${bgClass} border rounded-xl px-4 py-2">
                <div class="flex items-center gap-2 text-sm ${textClass}">
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
                    </svg>
                    ${testBadge}
                    <span>Tool completed: <strong>${escapeHtml(resultPreview)}</strong></span>
                </div>
            </div>
        `;
    }
}

// Streaming tool output handling
function addStreamingToolContainer(toolId, toolName) {
    const container = document.getElementById('chatMessagesContainer');
    const existingDiv = document.getElementById('chat-tool-stream-' + toolId);
    if (existingDiv) return existingDiv;

    const div = document.createElement('div');
    div.className = 'flex justify-start';
    div.id = 'chat-tool-stream-' + toolId;
    div.innerHTML = `
        <div class="max-w-[90%] w-full bg-slate-50 dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-xl overflow-hidden">
            <div class="flex items-center justify-between px-4 py-2 bg-slate-100 dark:bg-slate-700 border-b border-slate-200 dark:border-slate-600">
                <div class="flex items-center gap-2 text-sm font-medium text-slate-700 dark:text-slate-300">
                    <svg class="w-4 h-4 animate-pulse text-blue-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z" />
                    </svg>
                    <span>${escapeHtml(toolName)}</span>
                </div>
                <div class="tool-stream-progress flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                    <div class="w-24 h-1.5 bg-slate-200 dark:bg-slate-600 rounded-full overflow-hidden">
                        <div class="tool-progress-bar h-full bg-blue-500 transition-all duration-300" style="width: 0%"></div>
                    </div>
                    <span class="tool-progress-text">Starting...</span>
                </div>
            </div>
            <div class="tool-stream-output p-3 max-h-60 overflow-y-auto font-mono text-xs bg-slate-900 text-green-400">
                <div class="tool-stream-content"></div>
            </div>
            <div class="tool-stream-logs px-3 py-2 border-t border-slate-200 dark:border-slate-700 text-xs text-slate-500 dark:text-slate-400 max-h-20 overflow-y-auto hidden">
            </div>
        </div>
    `;
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
    return div;
}

function handleToolStreamEvent(data) {
    const container = addStreamingToolContainer(data.tool_id, data.tool_display_name || data.tool_name);
    if (!container) return;

    const progressBar = container.querySelector('.tool-progress-bar');
    const progressText = container.querySelector('.tool-progress-text');
    const contentArea = container.querySelector('.tool-stream-content');
    const logsArea = container.querySelector('.tool-stream-logs');

    switch (data.event) {
        case 'progress':
            if (data.progress !== undefined) {
                const percent = Math.round(data.progress * 100);
                progressBar.style.width = percent + '%';
            }
            if (data.content) {
                progressText.textContent = data.content;
            }
            break;

        case 'chunk':
        case 'output':
            // Append streaming content
            if (data.content) {
                contentArea.innerHTML += escapeHtml(data.content);
                const outputArea = container.querySelector('.tool-stream-output');
                outputArea.scrollTop = outputArea.scrollHeight;
            }
            break;

        case 'log':
            // Show log message
            logsArea.classList.remove('hidden');
            const logEntry = document.createElement('div');
            logEntry.className = 'py-0.5';
            logEntry.innerHTML = `<span class="text-slate-400">[${new Date().toLocaleTimeString()}]</span> ${escapeHtml(data.content)}`;
            logsArea.appendChild(logEntry);
            logsArea.scrollTop = logsArea.scrollHeight;
            break;

        case 'error':
            // Show error
            const errorEntry = document.createElement('div');
            errorEntry.className = 'text-red-500 py-0.5';
            errorEntry.innerHTML = escapeHtml(data.content);
            logsArea.classList.remove('hidden');
            logsArea.appendChild(errorEntry);
            break;
    }

    // Scroll chat container
    document.getElementById('chatMessagesContainer').scrollTop = document.getElementById('chatMessagesContainer').scrollHeight;
}

function finalizeStreamingTool(toolId) {
    const container = document.getElementById('chat-tool-stream-' + toolId);
    if (!container) return;

    const progressBar = container.querySelector('.tool-progress-bar');
    const progressText = container.querySelector('.tool-progress-text');
    const icon = container.querySelector('svg');

    // Update to completed state
    progressBar.style.width = '100%';
    progressBar.classList.remove('bg-blue-500');
    progressBar.classList.add('bg-emerald-500');
    progressText.textContent = 'Complete';

    // Replace spinning icon with checkmark
    icon.classList.remove('animate-pulse', 'text-blue-500');
    icon.classList.add('text-emerald-500');
    icon.innerHTML = '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>';
}

function updateChatStreamingMessage(div, content) {
    const contentEl = div.querySelector('.message-content');
    contentEl.innerHTML = marked.parse(content);

    // Remove typing indicator
    const indicator = div.querySelector('.typing-indicator');
    if (indicator) indicator.remove();

    document.getElementById('chatMessagesContainer').scrollTop = document.getElementById('chatMessagesContainer').scrollHeight;
}

async function sendChatMessage(e) {
    e.preventDefault();

    const input = document.getElementById('chatInput');
    const sendBtn = document.getElementById('chatSendBtn');
    const message = input.value.trim();
    const hasFiles = chatAttachedFiles.length > 0;

    if (!message && !hasFiles) return;
    if (isChatStreaming) return;

    // Build message content (text or multimodal array)
    let messageContent;
    let displayAttachments = [];

    if (hasFiles) {
        // Multimodal message with content blocks
        messageContent = [];

        // Add text block if there's text
        if (message) {
            messageContent.push({
                type: "text",
                text: message
            });
        }

        // Add file blocks
        for (const file of chatAttachedFiles) {
            // Determine block type: "document" for PDFs, "image" for images
            const blockType = file.mediaType === 'application/pdf' ? 'document' : 'image';

            messageContent.push({
                type: blockType,
                source: {
                    type: "base64",
                    media_type: file.mediaType,
                    data: file.base64
                }
            });

            displayAttachments.push({
                name: file.name,
                type: blockType,
                mediaType: file.mediaType,
                preview: file.preview
            });
        }
    } else {
        messageContent = message;
    }

    // Add user message with attachments
    addChatMessage('user', message || '[Attached files]', false, displayAttachments);
    input.value = '';

    // Clear attached files and preview
    chatAttachedFiles = [];
    updateChatFilePreview();

    // Disable input, show stop button
    isChatStreaming = true;
    chatRequestId = null;
    sendBtn.disabled = true;
    sendBtn.classList.add('hidden');
    const stopBtn = document.getElementById('chatStopBtn');
    if (stopBtn) {
        stopBtn.classList.remove('hidden');
    }

    // Track current assistant message and content
    let assistantDiv = null;
    let fullContent = '';

    try {
        const headers = { 'Content-Type': 'application/json' };
        if (getApiKey()) headers['X-API-Key'] = getApiKey();
        const testModeToggle = document.getElementById('testModeToggle');
        if (testModeToggle && testModeToggle.checked) {
            headers['X-Test-Mode'] = 'true';
        }
        const response = await fetch('/chat', {
            method: 'POST',
            headers,
            body: JSON.stringify({
                message: messageContent,
                thread_id: chatThreadId
            })
        });

        const reader = response.body.getReader();
        const decoder = new TextDecoder();

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            const chunk = decoder.decode(value);
            const lines = chunk.split('\n');

            for (const line of lines) {
                if (!line.startsWith('data: ')) continue;

                try {
                    const data = JSON.parse(line.slice(6));

                    // Log raw chunk to viewer
                    addRawChunk(line, data);

                    switch (data.type) {
                        case 'thread_id':
                            chatThreadId = data.thread_id;
                            document.getElementById('chatThreadId').textContent = chatThreadId.substring(0, 12) + '...';
                            break;

                        case 'request_id':
                            chatRequestId = data.request_id;
                            break;

                        case 'start':
                            // New turn starting - reset state for fresh message
                            finalizeThinking();
                            assistantDiv = null;
                            fullContent = '';
                            break;

                        case 'thinking':
                            // Display thinking in a collapsible section
                            addChatThinking(data.content);
                            break;

                        case 'content':
                            // Finalize thinking if we were thinking
                            finalizeThinking();
                            // Create assistant message if needed
                            if (!assistantDiv) {
                                assistantDiv = addChatMessage('assistant', '', true);
                            }
                            fullContent += data.content;
                            updateChatStreamingMessage(assistantDiv, fullContent);
                            break;

                        case 'tool_call':
                            // Finalize thinking if we were thinking
                            finalizeThinking();
                            // Finalize current assistant message if any
                            if (assistantDiv && fullContent) {
                                updateChatStreamingMessage(assistantDiv, fullContent);
                                assistantDiv = null;
                                fullContent = '';
                            }
                            addChatToolCall(data.tool_name, data.tool_id, data.tool_display_name);
                            break;

                        case 'tool_input_delta':
                            // Stream tool input arguments as they come in
                            if (data.tool_id && data.content) {
                                updateChatToolInput(data.tool_id, data.content);
                            }
                            break;

                        case 'tool_stream':
                            // Handle streaming tool output
                            handleToolStreamEvent(data);
                            break;

                        case 'tool_result':
                            // Finalize streaming tool if it was a streaming tool
                            finalizeStreamingTool(data.tool_id);
                            // Also update the regular tool result
                            updateChatToolResult(data.tool_id, data.content);
                            // Clean up input accumulator
                            delete toolInputAccumulator[data.tool_id];
                            // Reset for next assistant message after tool
                            assistantDiv = null;
                            fullContent = '';
                            break;

                        case 'request_cancelled':
                            // Request was cancelled
                            if (assistantDiv && fullContent) {
                                updateChatStreamingMessage(assistantDiv, fullContent + '\n\n*[Cancelled]*');
                            } else {
                                addSystemMessage('Request cancelled');
                            }
                            break;

                        case 'error':
                            throw new Error(data.content || 'Unknown error');
                    }
                } catch (parseErr) {
                    if (parseErr.message && !parseErr.message.includes('JSON')) {
                        throw parseErr;
                    }
                }
            }
        }

        // Finalize last message
        if (assistantDiv && fullContent) {
            updateChatStreamingMessage(assistantDiv, fullContent);
        }

    } catch (err) {
        console.error('Error:', err);
        if (!assistantDiv) {
            assistantDiv = addChatMessage('assistant', '', false);
        }
        updateChatStreamingMessage(assistantDiv, 'Error: ' + err.message);
        showToast('Chat error: ' + err.message, 'error');
    } finally {
        isChatStreaming = false;
        chatRequestId = null;
        sendBtn.disabled = false;
        sendBtn.classList.remove('hidden');
        const stopBtnFinal = document.getElementById('chatStopBtn');
        if (stopBtnFinal) {
            stopBtnFinal.classList.add('hidden');
        }
        input.focus();
    }
}

async function stopChatRequest() {
    if (!chatRequestId) {
        showToast('No active request to stop', 'info');
        return;
    }

    try {
        const headers = { 'Content-Type': 'application/json' };
        if (getApiKey()) headers['X-API-Key'] = getApiKey();
        const response = await fetch(`/requests/${chatRequestId}/cancel`, {
            method: 'POST',
            headers
        });

        if (response.ok) {
            showToast('Request cancelled', 'info');
        } else {
            const data = await response.json();
            showToast(data.error || 'Failed to cancel', 'error');
        }
    } catch (err) {
        console.error('Error cancelling request:', err);
        showToast('Failed to cancel request', 'error');
    }
}

function clearChatThread() {
    chatThreadId = null;
    document.getElementById('chatMessagesContainer').innerHTML = `
        <div class="text-center text-slate-400 dark:text-slate-500 py-12">
            <div class="empty-icon mx-auto"><svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"/></svg></div>
            <p>Start a conversation with the agent</p>
        </div>
    `;
    document.getElementById('chatThreadId').textContent = 'New conversation';
    showToast('Chat cleared', 'info');
}

// File upload handling for chat
function handleChatFileSelect(event) {
    const files = event.target.files;
    if (!files || files.length === 0) return;

    for (const file of files) {
        // Validate file size (max 10MB)
        if (file.size > 10 * 1024 * 1024) {
            showToast(`File ${file.name} is too large (max 10MB)`, 'error');
            continue;
        }

        // Validate file type
        const allowedTypes = ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'application/pdf'];
        if (!allowedTypes.includes(file.type)) {
            showToast(`File type ${file.type} is not supported`, 'error');
            continue;
        }

        // Read file as base64
        const reader = new FileReader();
        reader.onload = (e) => {
            const dataUrl = e.target.result;
            const base64 = dataUrl.split(',')[1];

            chatAttachedFiles.push({
                name: file.name,
                mediaType: file.type,
                base64: base64,
                preview: file.type.startsWith('image/') ? dataUrl : null
            });

            updateChatFilePreview();
        };
        reader.readAsDataURL(file);
    }

    // Reset input
    event.target.value = '';
}

function updateChatFilePreview() {
    const previewContainer = document.getElementById('chatFilePreview');
    if (!previewContainer) return;

    if (chatAttachedFiles.length === 0) {
        previewContainer.classList.add('hidden');
        previewContainer.innerHTML = '';
        return;
    }

    previewContainer.classList.remove('hidden');
    previewContainer.innerHTML = chatAttachedFiles.map((file, index) => {
        if (file.preview) {
            return `
                <div class="relative group">
                    <img src="${file.preview}" class="w-16 h-16 rounded-lg object-cover border border-slate-200 dark:border-slate-600">
                    <button onclick="removeChatFile(${index})" class="absolute -top-2 -right-2 w-5 h-5 bg-red-500 text-white rounded-full text-xs flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">×</button>
                </div>
            `;
        } else {
            const icon = `<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="${file.mediaType === 'application/pdf' ? 'M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z' : 'M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13'}"/></svg>`;
            return `
                <div class="relative group flex items-center gap-2 px-3 py-2 bg-slate-100 dark:bg-slate-700 rounded-lg">
                    <span>${icon}</span>
                    <span class="text-xs text-slate-600 dark:text-slate-300 max-w-[100px] truncate">${escapeHtml(file.name)}</span>
                    <button onclick="removeChatFile(${index})" class="ml-1 w-4 h-4 bg-red-500 text-white rounded-full text-xs flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">×</button>
                </div>
            `;
        }
    }).join('');
}

function removeChatFile(index) {
    chatAttachedFiles.splice(index, 1);
    updateChatFilePreview();
}

function initChatTab() {
    document.getElementById('chatInput').focus();
    updateChatFilePreview();
}
