// Memories Tab Functions

async function loadMemories() {
    const threadFilter = document.getElementById('memoryThreadFilter').value;
    const categoryFilter = document.getElementById('memoryCategoryFilter').value;
    const sourceFilter = document.getElementById('memorySourceFilter').value;

    let endpoint = '/memories?';
    const params = [];
    if (threadFilter) params.push(`thread_id=${threadFilter}`);
    if (categoryFilter) params.push(`category=${categoryFilter}`);
    if (sourceFilter) params.push(`source=${sourceFilter}`);
    endpoint += params.join('&');

    const response = await makeRequest(endpoint);

    if (response.status === 200 && response.data) {
        const memories = response.data.memories || [];
        const enabled = response.data.enabled || false;
        const configEnabled = response.data.config_enabled || false;
        const errorMsg = response.data.error || '';

        // Update stats
        const stats = {
            total: memories.length,
            fact: 0,
            preference: 0,
            instruction: 0,
            context: 0,
            document: 0
        };

        memories.forEach(m => {
            switch (m.category) {
                case 'fact': stats.fact++; break;
                case 'preference': stats.preference++; break;
                case 'instruction': stats.instruction++; break;
                case 'context': stats.context++; break;
                case 'document': stats.document++; break;
            }
        });

        document.getElementById('memTotal').textContent = stats.total;
        document.getElementById('memFacts').textContent = stats.fact;
        document.getElementById('memPrefs').textContent = stats.preference;
        document.getElementById('memInstrs').textContent = stats.instruction;
        document.getElementById('memContext').textContent = stats.context;
        document.getElementById('memDocs').textContent = stats.document;

        // Update status text
        const statusEl = document.getElementById('memoryStatusText');
        if (errorMsg) {
            statusEl.innerHTML = `<span class="text-red-500">⚠️ ${errorMsg}</span>`;
        } else if (!configEnabled) {
            statusEl.textContent = 'Memory disabled in config';
        } else if (enabled) {
            statusEl.textContent = `${memories.length} memories loaded`;
        }

        // Update thread filter options
        if (!threadFilter) {
            const threads = [...new Set(memories.map(m => m.thread_id).filter(Boolean))];
            const select = document.getElementById('memoryThreadFilter');
            const current = select.value;
            select.innerHTML = '<option value="">All Threads</option>' +
                threads.map(t => `<option value="${t}">${t.substring(0, 12)}...</option>`).join('');
            select.value = current;
        }

        // Render memories
        const container = document.getElementById('memoriesList');

        if (memories.length === 0) {
            let message = '';
            if (errorMsg) {
                message = `<span class="text-red-500">⚠️ ${errorMsg}</span>`;
            } else if (!configEnabled) {
                message = 'Memory system is disabled in config';
            } else if (!enabled) {
                message = 'Memory system failed to initialize';
            } else {
                message = 'No memories stored yet. Start chatting to build memories!';
            }
            container.innerHTML = `
                <div class="text-center py-12 text-slate-400 dark:text-slate-500">
                    <div class="text-4xl mb-3">🧠</div>
                    <p>${message}</p>
                </div>`;
            return;
        }

        const categoryColors = {
            fact: 'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300',
            preference: 'bg-purple-100 dark:bg-purple-900/50 text-purple-700 dark:text-purple-300',
            instruction: 'bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300',
            context: 'bg-emerald-100 dark:bg-emerald-900/50 text-emerald-700 dark:text-emerald-300',
            document: 'bg-cyan-100 dark:bg-cyan-900/50 text-cyan-700 dark:text-cyan-300'
        };

        const sourceIcons = {
            chat: '💬',
            file: '📄',
            manual: '✏️'
        };

        container.innerHTML = memories.map(memory => {
            const importance = Math.round((memory.importance || 0) * 100);
            const source = memory.source || 'chat';
            const sourceIcon = sourceIcons[source] || '💬';

            return `
                <div class="p-4 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors data-item">
                    <div class="flex justify-between items-start mb-2">
                        <div class="flex items-center gap-2 flex-wrap">
                            <span class="px-2 py-1 text-xs font-medium rounded-full ${categoryColors[memory.category] || 'bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300'}">
                                ${(memory.category || 'unknown').toUpperCase()}
                            </span>
                            <span title="Source: ${source}">${sourceIcon}</span>
                            ${memory.source_name ? `
                                <span class="text-xs text-slate-500 dark:text-slate-400 max-w-[150px] truncate" title="${escapeHtml(memory.source_name)}">
                                    ${escapeHtml(memory.source_name)}
                                </span>
                            ` : ''}
                        </div>
                        <button onclick="deleteMemory('${memory.id}')" class="text-red-500 hover:text-red-700">
                            🗑️
                        </button>
                    </div>
                    <div class="text-slate-700 dark:text-slate-200 mb-2 whitespace-pre-wrap text-sm">
                        ${escapeHtml(memory.content || '')}
                    </div>
                    ${memory.summary ? `
                        <div class="text-sm text-slate-500 dark:text-slate-400 mb-2 italic">
                            ${escapeHtml(memory.summary)}
                        </div>
                    ` : ''}
                    <div class="flex gap-4 text-xs text-slate-400 dark:text-slate-500 flex-wrap">
                        <span>📊 ${importance}%</span>
                        <span>👁️ ${memory.access_count || 0}x</span>
                        <span>📅 ${formatRelativeTime(memory.created_at)}</span>
                        ${memory.total_chunks > 1 ? `<span>📑 ${(memory.chunk_index || 0) + 1}/${memory.total_chunks}</span>` : ''}
                    </div>
                </div>
            `;
        }).join('');
    }
}

async function deleteMemory(memoryId) {
    if (!confirm('Delete this memory?')) return;

    const response = await makeRequest(`/memories/${memoryId}`, 'DELETE');

    if (response.status === 200) {
        showToast('Memory deleted', 'success');
        loadMemories();
    } else {
        showToast('Failed to delete memory', 'error');
    }
}

async function clearAllMemories() {
    if (!confirm('Delete ALL memories? This cannot be undone!')) return;

    const response = await makeRequest('/memories', 'DELETE');

    if (response.status === 200) {
        showToast('All memories cleared', 'success');
        loadMemories();
    } else {
        showToast('Failed to clear memories', 'error');
    }
}

function initMemoriesTab() {
    loadMemories();
}
