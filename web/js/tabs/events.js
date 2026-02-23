// Events Tab Functions

let eventsList = [];
const MAX_EVENTS = 500;

function addEventToList(event) {
    eventsList.unshift(event);
    if (eventsList.length > MAX_EVENTS) {
        eventsList = eventsList.slice(0, MAX_EVENTS);
    }

    renderEvents();
    updateEventCount();

    // Auto-scroll if enabled
    const autoScroll = document.getElementById('eventAutoScroll')?.checked;
    if (autoScroll) {
        const container = document.getElementById('eventsList');
        container.scrollTop = 0;
    }
}

function renderEvents() {
    const categoryFilter = document.getElementById('eventCategoryFilter')?.value || '';
    const levelFilter = document.getElementById('eventLevelFilter')?.value || '';

    let filtered = eventsList;
    if (categoryFilter) {
        filtered = filtered.filter(e => e.category === categoryFilter);
    }
    if (levelFilter) {
        filtered = filtered.filter(e => e.level === levelFilter);
    }

    const container = document.getElementById('eventsList');

    if (filtered.length === 0) {
        container.innerHTML = `
            <div class="empty-state text-center py-12 text-slate-400 dark:text-slate-500">
                <svg class="empty-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1"><path stroke-linecap="round" stroke-linejoin="round" d="M9.348 14.651a3.75 3.75 0 010-5.303m5.304 0a3.75 3.75 0 010 5.303m-7.425 2.122a6.75 6.75 0 010-9.546m9.546 0a6.75 6.75 0 010 9.546M5.106 18.894c-3.808-3.808-3.808-9.98 0-13.789m13.788 0c3.808 3.808 3.808 9.981 0 13.79M12 12h.008v.007H12V12zm.375 0a.375.375 0 11-.75 0 .375.375 0 01.75 0z"/></svg>
                <p>No events to display</p>
            </div>`;
        return;
    }

    const levelColors = {
        debug: 'text-slate-500 dark:text-slate-400',
        info: 'text-blue-600 dark:text-blue-400',
        warn: 'text-amber-600 dark:text-amber-400',
        error: 'text-red-600 dark:text-red-400'
    };

    const categoryColors = {
        SYSTEM: 'bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300',
        CHAT: 'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300',
        TOOL: 'bg-purple-100 dark:bg-purple-900/50 text-purple-700 dark:text-purple-300',
        LLM: 'bg-emerald-100 dark:bg-emerald-900/50 text-emerald-700 dark:text-emerald-300',
        TASK: 'bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300',
        FILE: 'bg-indigo-100 dark:bg-indigo-900/50 text-indigo-700 dark:text-indigo-300',
        MEMORY: 'bg-cyan-100 dark:bg-cyan-900/50 text-cyan-700 dark:text-cyan-300',
        MCP: 'bg-orange-100 dark:bg-orange-900/50 text-orange-700 dark:text-orange-300',
        ERROR: 'bg-red-100 dark:bg-red-900/50 text-red-700 dark:text-red-300'
    };

    container.innerHTML = filtered.slice(0, 100).map(event => {
        const time = new Date(event.timestamp).toLocaleTimeString();
        const levelClass = levelColors[event.level] || 'text-slate-600 dark:text-slate-400';
        const categoryClass = categoryColors[event.category] || 'bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300';

        // Build context info (thread_id, task_id)
        const contextParts = [];
        if (event.thread_id) contextParts.push(`thread: ${event.thread_id.substring(0, 12)}...`);
        if (event.task_id) contextParts.push(`task: ${event.task_id}`);
        const contextInfo = contextParts.length > 0 ? contextParts.join(' | ') : '';

        return `
            <div class="event-item p-3 border-b border-slate-100 dark:border-slate-700 ${levelClass}">
                <div class="flex items-center gap-2 mb-1">
                    <span class="text-xs text-slate-400 dark:text-slate-500">${time}</span>
                    <span class="px-1.5 py-0.5 text-xs rounded ${categoryClass}">${event.category || 'UNKNOWN'}</span>
                    <span class="text-xs font-medium uppercase">${event.type || ''}</span>
                    ${contextInfo ? `<span class="text-xs text-slate-400 dark:text-slate-500">${contextInfo}</span>` : ''}
                </div>
                <div class="text-sm">
                    ${event.message || ''}
                    ${event.data ? `
                        <details class="mt-1">
                            <summary class="text-xs text-slate-400 dark:text-slate-500 cursor-pointer hover:text-slate-600 dark:hover:text-slate-300">View data</summary>
                            <pre class="mt-1 p-2 bg-slate-50 dark:bg-slate-700 rounded text-xs overflow-x-auto">${escapeHtml(JSON.stringify(event.data, null, 2))}</pre>
                        </details>
                    ` : ''}
                </div>
            </div>
        `;
    }).join('');
}

function filterEvents() {
    renderEvents();
}

function clearEventLog() {
    eventsList = [];
    renderEvents();
    updateEventCount();
    showToast('Event log cleared', 'info');
}

function updateEventCount() {
    document.getElementById('eventCount').textContent = `${eventsList.length} events`;
}

function initEventsTab() {
    renderEvents();
}
