package web

// EmbeddedChatHTML is a simple chat interface that's always available
// Uses Tailwind CSS via CDN for styling
const EmbeddedChatHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Agent Chat</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
    <style>
        .message-content pre {
            background: #1e293b;
            padding: 1rem;
            border-radius: 0.5rem;
            overflow-x: auto;
            margin: 0.5rem 0;
        }
        .message-content code {
            font-family: 'Monaco', 'Menlo', monospace;
            font-size: 0.875rem;
        }
        .message-content p { margin: 0.5rem 0; }
        .message-content ul, .message-content ol { margin: 0.5rem 0; padding-left: 1.5rem; }
        .typing-indicator span {
            animation: blink 1.4s infinite both;
        }
        .typing-indicator span:nth-child(2) { animation-delay: 0.2s; }
        .typing-indicator span:nth-child(3) { animation-delay: 0.4s; }
        @keyframes blink {
            0%, 80%, 100% { opacity: 0; }
            40% { opacity: 1; }
        }
        #messages::-webkit-scrollbar { width: 6px; }
        #messages::-webkit-scrollbar-track { background: #1e293b; }
        #messages::-webkit-scrollbar-thumb { background: #475569; border-radius: 3px; }
        .file-preview { max-width: 150px; max-height: 150px; object-fit: cover; border-radius: 0.5rem; }
        .file-preview-container { position: relative; display: inline-block; }
        .file-preview-remove { position: absolute; top: -8px; right: -8px; width: 20px; height: 20px; background: #ef4444; border-radius: 50%; display: flex; align-items: center; justify-content: center; cursor: pointer; font-size: 12px; }
        .pdf-preview { display: flex; align-items: center; gap: 0.5rem; padding: 0.5rem 1rem; background: #334155; border-radius: 0.5rem; font-size: 0.875rem; }
        .message-image { max-width: 300px; max-height: 300px; border-radius: 0.5rem; margin-top: 0.5rem; }
        .message-pdf { display: inline-flex; align-items: center; gap: 0.5rem; padding: 0.5rem 1rem; background: #475569; border-radius: 0.5rem; margin-top: 0.5rem; font-size: 0.875rem; }
        .skeleton {
            background: linear-gradient(90deg, #334155 25%, #475569 50%, #334155 75%);
            background-size: 200% 100%;
            animation: shimmer 1.5s infinite;
            border-radius: 4px;
        }
        @keyframes shimmer {
            0% { background-position: 200% 0; }
            100% { background-position: -200% 0; }
        }
        .loading-overlay {
            transition: opacity 0.3s ease-out;
        }
        .loading-overlay.hidden {
            opacity: 0;
            pointer-events: none;
        }
        /* Voice mode styles */
        .mode-toggle {
            display: flex;
            background: #334155;
            border-radius: 0.5rem;
            padding: 2px;
        }
        .mode-toggle button {
            padding: 0.375rem 0.75rem;
            border-radius: 0.375rem;
            font-size: 0.75rem;
            font-weight: 500;
            transition: all 0.2s;
            border: none;
            cursor: pointer;
        }
        .mode-toggle button.active {
            background: #3b82f6;
            color: white;
        }
        .mode-toggle button:not(.active) {
            background: transparent;
            color: #94a3b8;
        }
        .mode-toggle button:not(.active):hover {
            color: white;
        }
        .voice-visualizer {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 4px;
            height: 60px;
        }
        .voice-bar {
            width: 4px;
            background: #3b82f6;
            border-radius: 2px;
            transition: height 0.1s;
        }
        .voice-bar.active {
            animation: voicePulse 0.5s ease-in-out infinite;
        }
        @keyframes voicePulse {
            0%, 100% { height: 20px; opacity: 0.5; }
            50% { height: 40px; opacity: 1; }
        }
        .voice-bar:nth-child(1) { animation-delay: 0s; }
        .voice-bar:nth-child(2) { animation-delay: 0.1s; }
        .voice-bar:nth-child(3) { animation-delay: 0.2s; }
        .voice-bar:nth-child(4) { animation-delay: 0.3s; }
        .voice-bar:nth-child(5) { animation-delay: 0.4s; }
        .live-indicator {
            display: inline-flex;
            align-items: center;
            gap: 0.375rem;
            padding: 0.25rem 0.5rem;
            background: #dc2626;
            border-radius: 0.375rem;
            font-size: 0.75rem;
            font-weight: 600;
        }
        .live-dot {
            width: 8px;
            height: 8px;
            background: white;
            border-radius: 50%;
            animation: livePulse 1s ease-in-out infinite;
        }
        @keyframes livePulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }
        .transcript-entry {
            padding: 0.5rem 0;
            border-bottom: 1px solid #334155;
        }
        .transcript-entry:last-child {
            border-bottom: none;
        }
        .transcript-role {
            font-size: 0.75rem;
            font-weight: 600;
            margin-bottom: 0.25rem;
        }
        .transcript-role.user { color: #60a5fa; }
        .transcript-role.assistant { color: #34d399; }
        .transcript-role.system { color: #a78bfa; }
    </style>
</head>
<body class="bg-slate-900 text-slate-100 min-h-screen flex justify-center items-center p-4">
    <!-- Loading Skeleton -->
    <div id="loadingSkeleton" class="w-full max-w-3xl flex flex-col h-[80vh] max-h-[700px] bg-slate-800 rounded-xl shadow-2xl overflow-hidden">
        <div class="border-b border-slate-700 px-4 py-3 flex items-center gap-3">
            <div class="skeleton w-8 h-8 rounded-lg"></div>
            <div>
                <div class="skeleton h-5 w-32 mb-1"></div>
                <div class="skeleton h-3 w-24"></div>
            </div>
        </div>
        <div class="flex-1 p-4 space-y-4">
            <div class="flex justify-end"><div class="skeleton h-10 w-48 rounded-2xl"></div></div>
            <div class="flex justify-start"><div class="skeleton h-16 w-64 rounded-2xl"></div></div>
            <div class="flex justify-end"><div class="skeleton h-10 w-36 rounded-2xl"></div></div>
            <div class="flex justify-start"><div class="skeleton h-24 w-72 rounded-2xl"></div></div>
        </div>
        <div class="border-t border-slate-700 p-4 flex gap-3">
            <div class="skeleton flex-1 h-12 rounded-lg"></div>
            <div class="skeleton w-20 h-12 rounded-lg"></div>
        </div>
    </div>

    <!-- Actual Chat UI (hidden until loaded) -->
    <div id="chatUI" class="w-full max-w-3xl flex flex-col h-[80vh] max-h-[700px] bg-slate-800 rounded-xl shadow-2xl overflow-hidden hidden">
    <!-- Header -->
    <header class="bg-slate-800 border-b border-slate-700 px-4 py-3 flex items-center justify-between">
        <div class="flex items-center gap-3">
            <div class="w-8 h-8 bg-gradient-to-br from-blue-500 to-purple-600 rounded-lg flex items-center justify-center">
                <svg class="w-5 h-5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 10h.01M12 10h.01M16 10h.01M9 16H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-5l-5 5v-5z"></path>
                </svg>
            </div>
            <div>
                <h1 class="font-semibold" id="agentName">Agent Chat</h1>
                <p class="text-xs text-slate-400" id="status">Ready</p>
            </div>
        </div>
        <!-- Mode Toggle (hidden if voice not enabled) -->
        <div id="modeToggle" class="mode-toggle hidden">
            <button id="textModeBtn" class="active" onclick="switchToTextMode()">Text</button>
            <button id="liveModeBtn" onclick="switchToLiveMode()">Live</button>
        </div>
    </header>

    <!-- Messages -->
    <main id="messages" class="flex-1 overflow-y-auto p-4 space-y-4">
        <div class="text-center text-slate-500 py-8">
            <p>Start a conversation</p>
        </div>
    </main>

    <!-- Input -->
    <footer class="bg-slate-800 border-t border-slate-700 p-4">
        <!-- File Preview Area -->
        <div id="filePreviewArea" class="hidden mb-3 flex flex-wrap gap-2"></div>
        <form onsubmit="sendMessage(event)" class="flex gap-3">
            <input type="file" id="fileInput" accept="image/*,application/pdf" multiple class="hidden" onchange="handleFileSelect(event)">
            <button
                type="button"
                onclick="document.getElementById('fileInput').click()"
                class="bg-slate-700 hover:bg-slate-600 px-3 py-3 rounded-lg transition"
                title="Attach images or PDFs"
            >
                <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13"></path>
                </svg>
            </button>
            <input
                type="text"
                id="input"
                placeholder="Type a message..."
                class="flex-1 bg-slate-700 border border-slate-600 rounded-lg px-4 py-3 focus:outline-none focus:border-blue-500 transition"
                autocomplete="off"
            >
            <button
                type="submit"
                id="sendBtn"
                class="bg-blue-600 hover:bg-blue-700 px-6 py-3 rounded-lg font-medium transition disabled:opacity-50 disabled:cursor-not-allowed"
            >
                Send
            </button>
            <button
                type="button"
                id="stopBtn"
                onclick="stopGeneration()"
                class="hidden bg-red-600 hover:bg-red-700 px-4 py-3 rounded-lg font-medium transition"
                title="Stop generation"
            >
                <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 10a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1h-4a1 1 0 01-1-1v-4z"></path>
                </svg>
            </button>
        </form>
    </footer>

    <!-- Voice Mode UI (hidden by default) -->
    <div id="voiceModeUI" class="hidden flex-1 flex flex-col">
        <!-- Voice Status -->
        <div class="flex-1 flex flex-col items-center justify-center p-8">
            <div id="voiceLiveIndicator" class="live-indicator mb-6 hidden">
                <div class="live-dot"></div>
                <span>LIVE</span>
            </div>

            <!-- Audio Visualizer -->
            <div class="voice-visualizer mb-6">
                <div class="voice-bar" id="bar1"></div>
                <div class="voice-bar" id="bar2"></div>
                <div class="voice-bar" id="bar3"></div>
                <div class="voice-bar" id="bar4"></div>
                <div class="voice-bar" id="bar5"></div>
            </div>

            <p id="voiceStatusText" class="text-slate-400 text-sm mb-4">Click Start to begin</p>
            <p id="voiceDuration" class="text-2xl font-mono text-slate-300 mb-6">0:00</p>

            <!-- Voice Controls -->
            <div class="flex gap-3">
                <button id="voiceStartBtn" onclick="startVoice()" class="bg-green-600 hover:bg-green-700 px-6 py-3 rounded-full font-medium transition flex items-center gap-2">
                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11a7 7 0 01-7 7m0 0a7 7 0 01-7-7m7 7v4m0 0H8m4 0h4m-4-8a3 3 0 01-3-3V5a3 3 0 116 0v6a3 3 0 01-3 3z"></path>
                    </svg>
                    Start
                </button>
                <button id="voiceStopBtn" onclick="stopVoice()" class="hidden bg-red-600 hover:bg-red-700 px-6 py-3 rounded-full font-medium transition flex items-center gap-2">
                    <svg class="w-5 h-5" fill="currentColor" viewBox="0 0 24 24">
                        <rect x="6" y="6" width="12" height="12" rx="2"></rect>
                    </svg>
                    Stop
                </button>
            </div>
        </div>

        <!-- Transcript Log -->
        <div class="border-t border-slate-700 max-h-48 overflow-y-auto">
            <div class="px-4 py-2 bg-slate-700/50 text-xs text-slate-400 font-medium sticky top-0">Transcript</div>
            <div id="voiceTranscript" class="px-4 py-2 text-sm">
                <p class="text-slate-500 text-center py-4">Conversation will appear here</p>
            </div>
        </div>
    </div>
    </div>

    <script>
        let threadId = null;
        let isStreaming = false;
        let currentRequestId = null;
        let pendingFiles = []; // Array of {data: base64, type: mimeType, preview: dataUrl, name: filename, isImage: bool}

        async function stopGeneration() {
            if (!currentRequestId || !isStreaming) return;

            try {
                const response = await fetch('/requests/' + currentRequestId + '/cancel', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' }
                });

                if (response.ok) {
                    setStatus('Stopped', 'yellow');
                }
            } catch (err) {
                console.error('Failed to cancel request:', err);
            }
        }

        function showStopButton() {
            document.getElementById('sendBtn').classList.add('hidden');
            document.getElementById('stopBtn').classList.remove('hidden');
        }

        function hideStopButton() {
            document.getElementById('stopBtn').classList.add('hidden');
            document.getElementById('sendBtn').classList.remove('hidden');
        }

        function handleFileSelect(event) {
            const files = event.target.files;
            if (!files || files.length === 0) return;

            for (const file of files) {
                const isImage = file.type.startsWith('image/');
                const isPdf = file.type === 'application/pdf';

                if (!isImage && !isPdf) continue;

                const reader = new FileReader();
                reader.onload = (e) => {
                    const dataUrl = e.target.result;
                    const base64 = dataUrl.split(',')[1];
                    pendingFiles.push({
                        data: base64,
                        type: file.type,
                        preview: isImage ? dataUrl : null,
                        name: file.name,
                        isImage: isImage
                    });
                    updateFilePreview();
                };
                reader.readAsDataURL(file);
            }

            // Clear input so same file can be selected again
            event.target.value = '';
        }

        function removeFile(index) {
            pendingFiles.splice(index, 1);
            updateFilePreview();
        }

        function updateFilePreview() {
            const area = document.getElementById('filePreviewArea');
            if (pendingFiles.length === 0) {
                area.classList.add('hidden');
                area.innerHTML = '';
                return;
            }

            area.classList.remove('hidden');
            area.innerHTML = pendingFiles.map((file, i) => {
                if (file.isImage) {
                    return ` + "`" + `
                        <div class="file-preview-container">
                            <img src="${file.preview}" class="file-preview" alt="Preview">
                            <div class="file-preview-remove" onclick="removeFile(${i})">×</div>
                        </div>
                    ` + "`" + `;
                } else {
                    return ` + "`" + `
                        <div class="file-preview-container">
                            <div class="pdf-preview">
                                <svg class="w-4 h-4 inline-block" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/></svg>
                                <span class="truncate max-w-[100px]">${escapeHtml(file.name)}</span>
                            </div>
                            <div class="file-preview-remove" onclick="removeFile(${i})">×</div>
                        </div>
                    ` + "`" + `;
                }
            }).join('');
        }

        function setStatus(text, color = 'slate') {
            const status = document.getElementById('status');
            status.textContent = text;
            status.className = 'text-xs text-' + color + '-400';
        }

        function addMessage(role, content, isStreaming = false, files = []) {
            const messages = document.getElementById('messages');

            // Remove welcome message if present
            if (messages.children.length === 1 && messages.children[0].classList.contains('text-center')) {
                messages.innerHTML = '';
            }

            const isUser = role === 'user';
            const div = document.createElement('div');
            div.className = 'flex ' + (isUser ? 'justify-end' : 'justify-start');

            // Build files HTML (images and PDFs)
            let filesHtml = '';
            if (files && files.length > 0) {
                filesHtml = '<div class="flex flex-wrap gap-2 mt-2">';
                for (const file of files) {
                    if (file.isImage) {
                        filesHtml += ` + "`" + `<img src="${file.preview || ('data:' + file.type + ';base64,' + file.data)}" class="message-image" alt="Attached image">` + "`" + `;
                    } else {
                        filesHtml += ` + "`" + `<div class="message-pdf"><svg class="w-4 h-4 inline-block" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"/></svg><span>${escapeHtml(file.name || 'PDF Document')}</span></div>` + "`" + `;
                    }
                }
                filesHtml += '</div>';
            }

            div.innerHTML = ` + "`" + `
                <div class="max-w-[80%] ${isUser ? 'bg-blue-600' : 'bg-slate-700'} rounded-2xl px-4 py-3 ${isUser ? 'rounded-br-md' : 'rounded-bl-md'}">
                    <div class="message-content text-sm">${isStreaming ? '' : (isUser ? escapeHtml(content) : marked.parse(content))}</div>
                    ${filesHtml}
                    ${isStreaming ? '<div class="typing-indicator text-slate-400"><span>.</span><span>.</span><span>.</span></div>' : ''}
                </div>
            ` + "`" + `;

            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
            return div;
        }

        function addToolCall(toolName, toolId) {
            const messages = document.getElementById('messages');
            const div = document.createElement('div');
            div.className = 'flex justify-start';
            div.id = 'tool-' + toolId;
            div.innerHTML = ` + "`" + `
                <div class="max-w-[80%] bg-purple-900/50 border border-purple-700 rounded-xl px-4 py-2">
                    <div class="flex items-center gap-2 text-sm text-purple-300">
                        <svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
                        </svg>
                        <span>Calling <strong>${escapeHtml(toolName)}</strong>...</span>
                    </div>
                </div>
            ` + "`" + `;
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        }

        function updateToolResult(toolId, result) {
            const toolDiv = document.getElementById('tool-' + toolId);
            if (toolDiv) {
                let resultPreview = result;
                try {
                    const parsed = JSON.parse(result);
                    if (parsed.status) resultPreview = parsed.status;
                    else if (parsed.success !== undefined) resultPreview = parsed.success ? 'Success' : 'Failed';
                    else resultPreview = 'Completed';
                } catch (e) {
                    resultPreview = result.length > 50 ? result.slice(0, 50) + '...' : result;
                }
                toolDiv.innerHTML = ` + "`" + `
                    <div class="max-w-[80%] bg-green-900/50 border border-green-700 rounded-xl px-4 py-2">
                        <div class="flex items-center gap-2 text-sm text-green-300">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
                            </svg>
                            <span>Tool completed: <strong>${escapeHtml(resultPreview)}</strong></span>
                        </div>
                    </div>
                ` + "`" + `;
            }
        }

        function updateStreamingMessage(div, content) {
            const contentEl = div.querySelector('.message-content');
            contentEl.innerHTML = marked.parse(content);

            // Remove typing indicator
            const indicator = div.querySelector('.typing-indicator');
            if (indicator) indicator.remove();

            document.getElementById('messages').scrollTop = document.getElementById('messages').scrollHeight;
        }

        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }

        function clearChat() {
            threadId = null;
            document.getElementById('messages').innerHTML = ` + "`" + `
                <div class="text-center text-slate-500 py-8">
                    <p>Start a conversation</p>
                </div>
            ` + "`" + `;
            setStatus('Ready');
        }

        async function sendMessage(e) {
            e.preventDefault();

            const input = document.getElementById('input');
            const sendBtn = document.getElementById('sendBtn');
            const message = input.value.trim();

            // Allow sending with just files (no text required)
            if ((!message && pendingFiles.length === 0) || isStreaming) return;

            // Capture files for this message
            const messageFiles = [...pendingFiles];

            // Add user message with files
            addMessage('user', message || '(attached file)', false, messageFiles);
            input.value = '';

            // Clear pending files
            pendingFiles = [];
            updateFilePreview();

            // Disable input and show stop button
            isStreaming = true;
            sendBtn.disabled = true;
            showStopButton();
            setStatus('Thinking...', 'blue');

            // Track current assistant message and content
            let assistantDiv = null;
            let fullContent = '';
            let currentToolId = null;
            currentRequestId = null;

            try {
                // Build request body
                let requestBody = {
                    thread_id: threadId
                };

                // If files are present, use content block array format
                if (messageFiles.length > 0) {
                    const contentBlocks = [];

                    // Add text block if message is not empty
                    if (message) {
                        contentBlocks.push({
                            type: 'text',
                            text: message
                        });
                    }

                    // Add file blocks (image or document based on type)
                    for (const file of messageFiles) {
                        // Use "document" for PDFs, "image" for images
                        const blockType = file.type === 'application/pdf' ? 'document' : 'image';
                        contentBlocks.push({
                            type: blockType,
                            source: {
                                type: 'base64',
                                media_type: file.type,
                                data: file.data
                            }
                        });
                    }

                    requestBody.message = contentBlocks;
                } else {
                    requestBody.message = message;
                }

                const response = await fetch('/chat', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(requestBody)
                });

                // Capture request ID from response header
                currentRequestId = response.headers.get('X-Request-ID');

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

                            switch (data.type) {
                                case 'thread_id':
                                    threadId = data.thread_id;
                                    break;

                                case 'request_id':
                                    // Request ID from stream (for clients that can't read headers)
                                    if (!currentRequestId) {
                                        currentRequestId = data.request_id;
                                    }
                                    break;

                                case 'start':
                                    // New turn starting - reset state for fresh message
                                    assistantDiv = null;
                                    fullContent = '';
                                    setStatus('Thinking...', 'blue');
                                    break;

                                case 'content':
                                    // Create assistant message if needed
                                    if (!assistantDiv) {
                                        assistantDiv = addMessage('assistant', '', true);
                                    }
                                    fullContent += data.content;
                                    updateStreamingMessage(assistantDiv, fullContent);
                                    break;

                                case 'tool_call':
                                    // Finalize current assistant message if any
                                    if (assistantDiv && fullContent) {
                                        updateStreamingMessage(assistantDiv, fullContent);
                                        assistantDiv = null;
                                        fullContent = '';
                                    }
                                    currentToolId = data.tool_id;
                                    addToolCall(data.tool_name, data.tool_id);
                                    setStatus('Using ' + data.tool_name + '...', 'purple');
                                    break;

                                case 'tool_result':
                                    updateToolResult(data.tool_id, data.content);
                                    setStatus('Processing...', 'blue');
                                    currentToolId = null;
                                    // Reset for next assistant message after tool
                                    assistantDiv = null;
                                    fullContent = '';
                                    break;

                                case 'cancelled':
                                    // Request was cancelled
                                    if (assistantDiv && fullContent) {
                                        fullContent += '\n\n*[Generation stopped]*';
                                        updateStreamingMessage(assistantDiv, fullContent);
                                    }
                                    setStatus('Stopped', 'yellow');
                                    return; // Exit the processing loop

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
                    updateStreamingMessage(assistantDiv, fullContent);
                }
                setStatus('Ready');

            } catch (err) {
                console.error('Error:', err);
                if (!assistantDiv) {
                    assistantDiv = addMessage('assistant', '', false);
                }
                updateStreamingMessage(assistantDiv, 'Error: ' + err.message);
                setStatus('Error', 'red');
            } finally {
                isStreaming = false;
                currentRequestId = null;
                sendBtn.disabled = false;
                hideStopButton();
                input.focus();
            }
        }

        // Focus input on load
        document.getElementById('input').focus();

        function showChatUI() {
            document.getElementById('loadingSkeleton').classList.add('hidden');
            document.getElementById('chatUI').classList.remove('hidden');
            document.getElementById('input').focus();
        }

        // Voice mode state
        let voiceEnabled = false;
        let voiceConfig = null;
        let voiceWs = null;
        let voiceAudioContext = null;
        let voicePlaybackContext = null;
        let voiceMediaStream = null;
        let voiceProcessor = null;
        let voiceStartTime = null;
        let voiceDurationInterval = null;
        let voiceNextPlayTime = 0;

        // Load config to show agent name and model info
        fetch('/config')
            .then(r => r.json())
            .then(config => {
                if (config.name) {
                    document.getElementById('agentName').textContent = config.name;
                }
                if (config.llm) {
                    setStatus(config.llm.provider + ' / ' + config.llm.model);
                }
                // Check if voice/realtime is enabled
                if (config.realtime && config.realtime.enabled) {
                    voiceEnabled = true;
                    voiceConfig = config.realtime;
                    document.getElementById('modeToggle').classList.remove('hidden');
                }
                showChatUI();
            })
            .catch(() => {
                // On error, still show UI with defaults
                showChatUI();
            });

        // Mode switching
        function switchToTextMode() {
            document.getElementById('textModeBtn').classList.add('active');
            document.getElementById('liveModeBtn').classList.remove('active');
            document.getElementById('messages').classList.remove('hidden');
            document.querySelector('footer').classList.remove('hidden');
            document.getElementById('voiceModeUI').classList.add('hidden');
            // Stop voice if active
            if (voiceWs) stopVoice();
        }

        function switchToLiveMode() {
            document.getElementById('liveModeBtn').classList.add('active');
            document.getElementById('textModeBtn').classList.remove('active');
            document.getElementById('messages').classList.add('hidden');
            document.querySelector('footer').classList.add('hidden');
            document.getElementById('voiceModeUI').classList.remove('hidden');
        }

        // Voice functions
        function float32ToInt16(float32Array) {
            const int16Array = new Int16Array(float32Array.length);
            for (let i = 0; i < float32Array.length; i++) {
                const s = Math.max(-1, Math.min(1, float32Array[i]));
                int16Array[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
            }
            return int16Array;
        }

        function int16ToBase64(int16Array) {
            const uint8Array = new Uint8Array(int16Array.buffer);
            let binary = '';
            for (let i = 0; i < uint8Array.length; i++) {
                binary += String.fromCharCode(uint8Array[i]);
            }
            return btoa(binary);
        }

        function base64ToFloat32(base64) {
            const binaryString = atob(base64);
            const int16Array = new Int16Array(binaryString.length / 2);
            for (let i = 0; i < int16Array.length; i++) {
                int16Array[i] = (binaryString.charCodeAt(i * 2 + 1) << 8) | binaryString.charCodeAt(i * 2);
            }
            const float32Array = new Float32Array(int16Array.length);
            for (let i = 0; i < int16Array.length; i++) {
                float32Array[i] = int16Array[i] / (int16Array[i] < 0 ? 0x8000 : 0x7FFF);
            }
            return float32Array;
        }

        function resampleAudio(inputData, inputSampleRate, outputSampleRate) {
            if (inputSampleRate === outputSampleRate) return inputData;
            const ratio = inputSampleRate / outputSampleRate;
            const outputLength = Math.floor(inputData.length / ratio);
            const output = new Float32Array(outputLength);
            for (let i = 0; i < outputLength; i++) {
                const srcIndex = i * ratio;
                const srcIndexFloor = Math.floor(srcIndex);
                const srcIndexCeil = Math.min(srcIndexFloor + 1, inputData.length - 1);
                const t = srcIndex - srcIndexFloor;
                output[i] = inputData[srcIndexFloor] * (1 - t) + inputData[srcIndexCeil] * t;
            }
            return output;
        }

        async function startVoice() {
            const statusText = document.getElementById('voiceStatusText');
            statusText.textContent = 'Connecting...';

            try {
                const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
                const wsUrl = protocol + '//' + window.location.host + '/voice';
                voiceWs = new WebSocket(wsUrl);

                voiceWs.onopen = () => {
                    statusText.textContent = 'Connected, initializing...';
                    const provider = voiceConfig?.provider || 'openai';
                    const voice = provider === 'gemini' ? (voiceConfig?.gemini_voice || 'Puck') : (voiceConfig?.voice || 'alloy');
                    voiceWs.send(JSON.stringify({
                        type: 'start',
                        data: { provider: provider, voice: voice }
                    }));
                };

                voiceWs.onmessage = (event) => {
                    try {
                        const msg = JSON.parse(event.data);
                        handleVoiceMessage(msg);
                    } catch (e) {
                        console.error('Voice message parse error:', e);
                    }
                };

                voiceWs.onerror = (error) => {
                    console.error('Voice WebSocket error:', error);
                    statusText.textContent = 'Connection error';
                };

                voiceWs.onclose = () => {
                    statusText.textContent = 'Disconnected';
                    stopVoiceInternal();
                };

            } catch (error) {
                statusText.textContent = 'Failed to connect';
                console.error('Voice start error:', error);
            }
        }

        async function handleVoiceMessage(msg) {
            const statusText = document.getElementById('voiceStatusText');

            switch (msg.type) {
                case 'session_created':
                    statusText.textContent = 'Listening...';
                    document.getElementById('voiceLiveIndicator').classList.remove('hidden');
                    document.getElementById('voiceStartBtn').classList.add('hidden');
                    document.getElementById('voiceStopBtn').classList.remove('hidden');
                    setVoiceBarsActive(true);
                    voiceStartTime = Date.now();
                    startVoiceDurationTimer();
                    await startAudioCapture();
                    break;

                case 'audio_delta':
                    if (msg.data && msg.data.chunk) {
                        playAudioChunk(msg.data.chunk);
                    }
                    break;

                case 'transcript':
                    if (msg.data) {
                        addTranscriptEntry(msg.data.role, msg.data.content);
                    }
                    break;

                case 'tool_call':
                    if (msg.data) {
                        addTranscriptEntry('system', 'Using tool: ' + msg.data.name);
                    }
                    break;

                case 'error':
                    console.error('Voice error:', msg.data);
                    statusText.textContent = 'Error: ' + (msg.data?.message || 'Unknown');
                    break;
            }
        }

        async function startAudioCapture() {
            try {
                voiceAudioContext = new AudioContext();
                const nativeSampleRate = voiceAudioContext.sampleRate;
                voiceMediaStream = await navigator.mediaDevices.getUserMedia({ audio: true });
                const source = voiceAudioContext.createMediaStreamSource(voiceMediaStream);

                voiceProcessor = voiceAudioContext.createScriptProcessor(2048, 1, 1);
                voiceProcessor.onaudioprocess = (e) => {
                    if (!voiceWs || voiceWs.readyState !== WebSocket.OPEN) return;
                    const inputData = e.inputBuffer.getChannelData(0);
                    const resampledData = resampleAudio(inputData, nativeSampleRate, 16000);
                    const int16Data = float32ToInt16(resampledData);
                    const base64Data = int16ToBase64(int16Data);
                    voiceWs.send(JSON.stringify({ type: 'audio', data: { chunk: base64Data } }));
                };

                source.connect(voiceProcessor);
                voiceProcessor.connect(voiceAudioContext.destination);
            } catch (e) {
                console.error('Microphone access error:', e);
                document.getElementById('voiceStatusText').textContent = 'Microphone access denied';
            }
        }

        function playAudioChunk(base64Audio) {
            if (!voicePlaybackContext) {
                voicePlaybackContext = new AudioContext({ sampleRate: 24000 });
            }
            if (voicePlaybackContext.state === 'suspended') {
                voicePlaybackContext.resume();
            }

            const float32Data = base64ToFloat32(base64Audio);
            const audioBuffer = voicePlaybackContext.createBuffer(1, float32Data.length, 24000);
            audioBuffer.getChannelData(0).set(float32Data);

            const source = voicePlaybackContext.createBufferSource();
            source.buffer = audioBuffer;
            source.connect(voicePlaybackContext.destination);

            const currentTime = voicePlaybackContext.currentTime;
            const startTime = Math.max(currentTime, voiceNextPlayTime);
            source.start(startTime);
            voiceNextPlayTime = startTime + audioBuffer.duration;
        }

        function stopVoice() {
            if (voiceWs) voiceWs.close();
            stopVoiceInternal();
        }

        function stopVoiceInternal() {
            setVoiceBarsActive(false);
            document.getElementById('voiceLiveIndicator').classList.add('hidden');
            document.getElementById('voiceStartBtn').classList.remove('hidden');
            document.getElementById('voiceStopBtn').classList.add('hidden');
            stopVoiceDurationTimer();

            if (voiceProcessor) { voiceProcessor.disconnect(); voiceProcessor = null; }
            if (voiceMediaStream) { voiceMediaStream.getTracks().forEach(t => t.stop()); voiceMediaStream = null; }
            if (voiceAudioContext) { voiceAudioContext.close(); voiceAudioContext = null; }
            if (voicePlaybackContext) { voicePlaybackContext.close(); voicePlaybackContext = null; }
            voiceWs = null;
            voiceNextPlayTime = 0;
        }

        function setVoiceBarsActive(active) {
            for (let i = 1; i <= 5; i++) {
                const bar = document.getElementById('bar' + i);
                if (active) bar.classList.add('active');
                else bar.classList.remove('active');
            }
        }

        function startVoiceDurationTimer() {
            voiceDurationInterval = setInterval(() => {
                if (voiceStartTime) {
                    const elapsed = Math.floor((Date.now() - voiceStartTime) / 1000);
                    const mins = Math.floor(elapsed / 60);
                    const secs = elapsed % 60;
                    document.getElementById('voiceDuration').textContent = mins + ':' + secs.toString().padStart(2, '0');
                }
            }, 1000);
        }

        function stopVoiceDurationTimer() {
            if (voiceDurationInterval) { clearInterval(voiceDurationInterval); voiceDurationInterval = null; }
            voiceStartTime = null;
            document.getElementById('voiceDuration').textContent = '0:00';
        }

        function addTranscriptEntry(role, text) {
            const transcript = document.getElementById('voiceTranscript');
            // Remove placeholder
            const placeholder = transcript.querySelector('.text-slate-500');
            if (placeholder) placeholder.remove();

            const entry = document.createElement('div');
            entry.className = 'transcript-entry';
            entry.innerHTML = '<div class="transcript-role ' + role + '">' + role + '</div><div class="text-slate-200">' + escapeHtml(text) + '</div>';
            transcript.appendChild(entry);
            transcript.scrollTop = transcript.scrollHeight;
        }
    </script>
</body>
</html>`
