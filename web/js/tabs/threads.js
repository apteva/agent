// Threads Tab Functions

let selectedThreadId = null;

async function loadThreads() {
    const response = await makeRequest('/threads');

    if (response.status === 200 && response.data) {
        const threads = response.data.threads || [];

        // Update stats
        document.getElementById('threadsTotal').textContent = threads.length;

        const today = new Date().toDateString();
        const activeToday = threads.filter(t => new Date(t.updated_at).toDateString() === today).length;
        document.getElementById('threadsToday').textContent = activeToday;

        const totalMessages = threads.reduce((sum, t) => sum + (t.message_count || 0), 0);
        document.getElementById('threadsMessages').textContent = formatNumber(totalMessages);

        const avg = threads.length > 0 ? Math.round(totalMessages / threads.length) : 0;
        document.getElementById('threadsAvg').textContent = avg;

        // Render threads
        const container = document.getElementById('threadsList');

        if (threads.length === 0) {
            container.innerHTML = `
                <div class="empty-state text-center py-12 text-slate-400 dark:text-slate-500">
                    <svg class="empty-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1"><path stroke-linecap="round" stroke-linejoin="round" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"/></svg>
                    <p>No conversation threads yet</p>
                </div>`;
            return;
        }

        container.innerHTML = threads.map(thread => `
            <div class="thread-item p-3 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors cursor-pointer ${selectedThreadId === thread.id ? 'bg-blue-50 dark:bg-blue-900/30 border-l-4 border-blue-500' : ''}" data-thread-id="${thread.id}" data-thread-title="${escapeHtml(thread.title || 'Untitled').replace(/'/g, '&#39;')}">
                <div class="flex justify-between items-start mb-1">
                    <span class="font-medium text-slate-800 dark:text-slate-100 text-sm truncate">${escapeHtml(thread.title || 'Untitled')}</span>
                    <button class="delete-thread-btn text-red-400 hover:text-red-600 text-xs ml-2 opacity-50 hover:opacity-100" data-thread-id="${thread.id}">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                    </button>
                </div>
                ${thread.activity ? `<div class="text-xs text-slate-600 dark:text-slate-300 mb-1 truncate">${escapeHtml(thread.activity)}</div>` : ''}
                <div class="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                    <span class="px-1.5 py-0.5 bg-slate-100 dark:bg-slate-700 rounded">${thread.message_count || 0} msgs</span>
                    <span>${formatRelativeTime(thread.updated_at)}</span>
                </div>
            </div>
        `).join('');

        // Attach event listeners
        container.querySelectorAll('.thread-item').forEach(item => {
            item.addEventListener('click', (e) => {
                if (!e.target.classList.contains('delete-thread-btn')) {
                    viewThread(item.dataset.threadId, item.dataset.threadTitle);
                }
            });
        });

        container.querySelectorAll('.delete-thread-btn').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.stopPropagation();
                deleteThread(btn.dataset.threadId);
            });
        });
    }
}

async function viewThread(threadId, threadTitle) {
    console.log('viewThread called:', threadId, threadTitle);
    selectedThreadId = threadId;

    // Update viewer title immediately
    document.getElementById('threadViewerTitle').textContent = threadTitle || 'Thread Messages';
    document.getElementById('threadViewerActions').classList.remove('hidden');

    const container = document.getElementById('threadMessages');
    container.innerHTML = `
        <div class="text-center py-8 text-slate-400">
            <div class="flex justify-center mb-3"><div class="spinner"></div></div>
            <p>Loading messages...</p>
        </div>`;

    const url = `/threads/${threadId}/messages`;
    console.log('Fetching messages from:', url);
    const response = await makeRequest(url);
    console.log('Response:', response);

    if (response.status === 200 && response.data) {
        const messages = response.data.messages || [];

        if (messages.length === 0) {
            container.innerHTML = `
                <div class="empty-state text-center py-12 text-slate-400 dark:text-slate-500">
                    <svg class="empty-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1"><path stroke-linecap="round" stroke-linejoin="round" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"/></svg>
                    <p>No messages in this thread</p>
                </div>`;
            return;
        }

        container.innerHTML = messages.map(m => {
            const isUser = m.role === 'user';
            const isAssistant = m.role === 'assistant';
            const isTool = m.role === 'tool' || m.role === 'tool_result';

            let contentHtml = '';
            let content = m.content;

            // Handle different content types
            if (typeof content === 'string') {
                contentHtml = `<div class="whitespace-pre-wrap">${escapeHtml(content)}</div>`;
            } else if (typeof content === 'object') {
                // Tool use or tool result
                if (content.type === 'tool_use') {
                    contentHtml = `
                        <div class="text-xs font-mono">
                            <div class="text-purple-600 dark:text-purple-400 font-semibold mb-1">Tool: ${escapeHtml(content.name || 'unknown')}</div>
                            <pre class="bg-slate-100 dark:bg-slate-900 p-2 rounded text-xs overflow-x-auto">${escapeHtml(JSON.stringify(content.input || {}, null, 2))}</pre>
                        </div>`;
                } else if (content.type === 'tool_result') {
                    const resultText = typeof content.content === 'string' ? content.content : JSON.stringify(content.content, null, 2);
                    contentHtml = `
                        <div class="text-xs font-mono">
                            <div class="text-green-600 dark:text-green-400 font-semibold mb-1">Result</div>
                            <pre class="bg-slate-100 dark:bg-slate-900 p-2 rounded text-xs overflow-x-auto max-h-40">${escapeHtml(truncate(resultText, 1000))}</pre>
                        </div>`;
                } else if (Array.isArray(content)) {
                    // Array of content blocks
                    contentHtml = content.map(block => {
                        if (block.type === 'text') {
                            return `<div class="whitespace-pre-wrap">${escapeHtml(block.text || '')}</div>`;
                        } else if (block.type === 'tool_use') {
                            return `
                                <div class="text-xs font-mono mt-2">
                                    <div class="text-purple-600 dark:text-purple-400 font-semibold mb-1">Tool: ${escapeHtml(block.name || 'unknown')}</div>
                                    <pre class="bg-slate-100 dark:bg-slate-900 p-2 rounded text-xs overflow-x-auto">${escapeHtml(JSON.stringify(block.input || {}, null, 2))}</pre>
                                </div>`;
                        }
                        return `<pre class="text-xs">${escapeHtml(JSON.stringify(block, null, 2))}</pre>`;
                    }).join('');
                } else {
                    contentHtml = `<pre class="text-xs overflow-x-auto">${escapeHtml(JSON.stringify(content, null, 2))}</pre>`;
                }
            }

            const bgColor = isUser
                ? 'bg-blue-50 dark:bg-blue-900/20 border-blue-200 dark:border-blue-800'
                : isTool
                    ? 'bg-purple-50 dark:bg-purple-900/20 border-purple-200 dark:border-purple-800'
                    : 'bg-slate-50 dark:bg-slate-700/50 border-slate-200 dark:border-slate-600';

            const roleLabel = isUser ? 'User' : isAssistant ? 'Assistant' : 'Tool';
            const roleColor = isUser ? 'text-blue-600 dark:text-blue-400' : isAssistant ? 'text-slate-700 dark:text-slate-300' : 'text-purple-600 dark:text-purple-400';

            return `
                <div class="mb-4 p-3 rounded-lg border ${bgColor}">
                    <div class="flex justify-between items-center mb-2">
                        <span class="font-semibold text-sm ${roleColor}">${roleLabel}</span>
                        <span class="text-xs text-slate-400">${formatRelativeTime(m.created_at)}</span>
                    </div>
                    <div class="text-sm text-slate-700 dark:text-slate-300">${contentHtml}</div>
                </div>
            `;
        }).join('');
    } else {
        container.innerHTML = `
            <div class="text-center py-12 text-red-400">
                <p class="font-medium">Failed to load messages</p>
            </div>`;
    }
}

function closeThreadViewer() {
    selectedThreadId = null;
    document.getElementById('threadViewerTitle').textContent = 'Select a thread';
    document.getElementById('threadViewerActions').classList.add('hidden');
    document.getElementById('threadMessages').innerHTML = `
        <div class="empty-state text-center py-12 text-slate-400 dark:text-slate-500">
            <svg class="empty-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1"><path stroke-linecap="round" stroke-linejoin="round" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"/></svg>
            <p>Click a thread to view messages</p>
        </div>`;
    loadThreads();
}

async function deleteThread(threadId) {
    if (!confirm('Delete this thread and all its messages?')) return;

    const response = await makeRequest(`/threads/${threadId}`, 'DELETE');

    if (response.status === 200) {
        showToast('Thread deleted', 'success');
        if (selectedThreadId === threadId) {
            closeThreadViewer();
        } else {
            loadThreads();
        }
    } else {
        showToast('Failed to delete thread', 'error');
    }
}

function initThreadsTab() {
    loadThreads();
}
