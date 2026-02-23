// Tracing Tab Functions - Enhanced for Runs/Traces hierarchy

async function loadTraces() {
    const search = document.getElementById('traceSearch')?.value || '';
    const topLevel = document.getElementById('traceTopLevel')?.checked || false;
    const source = document.getElementById('traceSource')?.value || '';
    const status = document.getElementById('traceStatus')?.value || '';

    let params = new URLSearchParams();
    if (search) params.append('search', search);
    if (topLevel) params.append('top_level', 'true');
    if (source) params.append('source', source);
    if (status) params.append('status', status);
    params.append('limit', '50');

    const endpoint = '/traces' + (params.toString() ? '?' + params.toString() : '');
    const response = await makeRequest(endpoint);

    if (response.status === 200 && response.data) {
        const traces = response.data.traces || [];
        const total = response.data.total || traces.length;

        // Calculate stats
        let totalSpans = 0;
        let totalDuration = 0;
        let totalInputTokens = 0;
        let totalOutputTokens = 0;
        let errors = 0;

        traces.forEach(t => {
            totalSpans += t.span_count || 0;
            totalDuration += t.duration_ms || 0;
            if (t.usage) {
                totalInputTokens += t.usage.input_tokens || 0;
                totalOutputTokens += t.usage.output_tokens || 0;
            }
            if (t.status === 'error') errors++;
        });

        const avgDuration = traces.length > 0 ? Math.round(totalDuration / traces.length) : 0;

        document.getElementById('tracesTotal').textContent = total;
        document.getElementById('tracesSpans').textContent = totalSpans;
        document.getElementById('tracesAvgDuration').textContent = `${avgDuration}ms`;
        document.getElementById('tracesErrors').textContent = errors;

        // Update token stats if elements exist
        const inputEl = document.getElementById('tracesInputTokens');
        const outputEl = document.getElementById('tracesOutputTokens');
        if (inputEl) inputEl.textContent = formatNumber(totalInputTokens);
        if (outputEl) outputEl.textContent = formatNumber(totalOutputTokens);

        // Render traces
        const container = document.getElementById('tracesList');

        if (traces.length === 0) {
            container.innerHTML = `
                <div class="text-center py-12 text-slate-400 dark:text-slate-500">
                    <div class="empty-icon mx-auto"><svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/></svg></div>
                    <p>No traces found</p>
                </div>`;
            return;
        }

        container.innerHTML = traces.map(trace => renderTraceRow(trace)).join('');
    }
}

function renderTraceRow(trace, indent = 0) {
    const hasError = trace.status === 'error';
    const hasChildren = trace.children && trace.children.length > 0;
    const isChild = trace.parent_trace_id != null;

    // Source badge colors
    const sourceColors = {
        'chat': 'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300',
        'webhook': 'bg-purple-100 dark:bg-purple-900/50 text-purple-700 dark:text-purple-300',
        'task': 'bg-green-100 dark:bg-green-900/50 text-green-700 dark:text-green-300',
        'subtask': 'bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300',
        'agent_call': 'bg-cyan-100 dark:bg-cyan-900/50 text-cyan-700 dark:text-cyan-300'
    };
    const sourceBadge = trace.source ? `<span class="px-2 py-0.5 text-xs ${sourceColors[trace.source] || sourceColors['chat']} rounded-full">${trace.source}</span>` : '';

    // Token usage
    const usage = trace.usage || {};
    const tokenInfo = usage.total_tokens ? `<span class="text-xs text-slate-400">${formatNumber(usage.input_tokens || 0)}↓ ${formatNumber(usage.output_tokens || 0)}↑</span>` : '';

    const paddingLeft = indent * 24;

    return `
        <div class="p-4 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors cursor-pointer border-l-2 ${isChild ? 'border-slate-300 dark:border-slate-600' : 'border-transparent'}"
             style="padding-left: ${16 + paddingLeft}px"
             onclick="viewTrace('${trace.id}')">
            <div class="flex justify-between items-start mb-2">
                <div class="flex items-center gap-2 flex-wrap">
                    ${isChild ? '<span class="text-slate-400">↳</span>' : ''}
                    <span class="font-medium text-slate-800 dark:text-slate-100">${escapeHtml(trace.name || 'Request')}</span>
                    ${sourceBadge}
                    ${hasError ? '<span class="px-2 py-0.5 text-xs bg-red-100 dark:bg-red-900/50 text-red-700 dark:text-red-300 rounded-full">Error</span>' : ''}
                    ${hasChildren ? `<span class="px-2 py-0.5 text-xs bg-slate-100 dark:bg-slate-600 text-slate-600 dark:text-slate-300 rounded-full">${trace.children.length} sub-runs</span>` : ''}
                </div>
                <div class="flex items-center gap-3">
                    ${tokenInfo}
                    <span class="text-sm ${trace.duration_ms > 5000 ? 'text-red-600 dark:text-red-400' : trace.duration_ms > 1000 ? 'text-amber-600 dark:text-amber-400' : 'text-slate-500 dark:text-slate-400'}">
                        ${formatDuration(trace.duration_ms || 0)}
                    </span>
                </div>
            </div>
            <div class="text-sm text-slate-500 dark:text-slate-400 flex gap-4 flex-wrap">
                <span class="font-mono text-xs">${trace.id?.substring(0, 16) || '-'}...</span>
                <span>${trace.span_count || 0} spans</span>
                ${usage.llm_calls ? `<span>${usage.llm_calls} LLM calls</span>` : ''}
                ${usage.tool_calls ? `<span>${usage.tool_calls} tools</span>` : ''}
                <span>${formatRelativeTime(trace.started_at)}</span>
            </div>
        </div>
    `;
}

async function viewTrace(traceId) {
    const response = await makeRequest(`/traces/${traceId}`);

    if (response.status === 200 && response.data) {
        const data = response.data;
        const trace = data.trace || data;
        const spans = data.spans || trace.spans || [];
        const children = data.children || [];
        const usage = trace.usage || {};

        // Create modal content
        const content = `
            <div class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onclick="this.remove()">
                <div class="bg-white dark:bg-slate-800 rounded-xl shadow-xl max-w-5xl w-full mx-4 max-h-[85vh] overflow-hidden" onclick="event.stopPropagation()">
                    <div class="px-5 py-4 border-b border-slate-200 dark:border-slate-700 flex justify-between items-center">
                        <div>
                            <h3 class="font-semibold text-lg text-slate-900 dark:text-slate-100">${escapeHtml(trace.name || trace.id)}</h3>
                            <p class="text-sm text-slate-500">${trace.id}</p>
                        </div>
                        <button onclick="this.closest('.fixed').remove()" class="text-slate-400 hover:text-slate-600 dark:hover:text-slate-300 text-xl">✕</button>
                    </div>
                    <div class="p-5 overflow-y-auto max-h-[70vh]">
                        <!-- Summary Stats -->
                        <div class="mb-6 grid grid-cols-6 gap-3 text-sm">
                            <div class="bg-slate-50 dark:bg-slate-700 rounded-lg p-3">
                                <div class="text-slate-500 dark:text-slate-400 text-xs">Duration</div>
                                <div class="font-semibold text-slate-800 dark:text-slate-200">${formatDuration(trace.duration_ms || 0)}</div>
                            </div>
                            <div class="bg-slate-50 dark:bg-slate-700 rounded-lg p-3">
                                <div class="text-slate-500 dark:text-slate-400 text-xs">Spans</div>
                                <div class="font-semibold text-slate-800 dark:text-slate-200">${spans.length}</div>
                            </div>
                            <div class="bg-slate-50 dark:bg-slate-700 rounded-lg p-3">
                                <div class="text-slate-500 dark:text-slate-400 text-xs">Input Tokens</div>
                                <div class="font-semibold text-slate-800 dark:text-slate-200">${formatNumber(usage.input_tokens || 0)}</div>
                            </div>
                            <div class="bg-slate-50 dark:bg-slate-700 rounded-lg p-3">
                                <div class="text-slate-500 dark:text-slate-400 text-xs">Output Tokens</div>
                                <div class="font-semibold text-slate-800 dark:text-slate-200">${formatNumber(usage.output_tokens || 0)}</div>
                            </div>
                            <div class="bg-slate-50 dark:bg-slate-700 rounded-lg p-3">
                                <div class="text-slate-500 dark:text-slate-400 text-xs">Source</div>
                                <div class="font-semibold text-slate-800 dark:text-slate-200">${trace.source || 'chat'}</div>
                            </div>
                            <div class="bg-slate-50 dark:bg-slate-700 rounded-lg p-3">
                                <div class="text-slate-500 dark:text-slate-400 text-xs">Status</div>
                                <div class="font-semibold ${trace.status === 'error' ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400'}">${trace.status || 'ok'}</div>
                            </div>
                        </div>

                        ${trace.parent_trace_id ? `
                            <div class="mb-4 p-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-sm">
                                <span class="text-amber-700 dark:text-amber-300">↳ Child of: </span>
                                <a href="#" onclick="event.preventDefault(); this.closest('.fixed').remove(); viewTrace('${trace.parent_trace_id}')" class="text-amber-700 dark:text-amber-300 underline font-mono">${trace.parent_trace_id}</a>
                            </div>
                        ` : ''}

                        ${children.length > 0 ? `
                            <div class="mb-6">
                                <h4 class="font-semibold text-slate-800 dark:text-slate-200 mb-3">Child Runs (${children.length})</h4>
                                <div class="space-y-2">
                                    ${children.map(child => `
                                        <div class="p-3 bg-slate-50 dark:bg-slate-700 rounded-lg flex justify-between items-center cursor-pointer hover:bg-slate-100 dark:hover:bg-slate-600" onclick="event.stopPropagation(); this.closest('.fixed').remove(); viewTrace('${child.id}')">
                                            <div>
                                                <span class="font-medium text-slate-800 dark:text-slate-100">${escapeHtml(child.name || 'Sub-run')}</span>
                                                <span class="ml-2 text-xs px-2 py-0.5 bg-slate-200 dark:bg-slate-600 rounded">${child.source || 'subtask'}</span>
                                            </div>
                                            <span class="text-sm text-slate-500">${formatDuration(child.duration_ms || 0)}</span>
                                        </div>
                                    `).join('')}
                                </div>
                            </div>
                        ` : ''}

                        <h4 class="font-semibold text-slate-800 dark:text-slate-200 mb-3">Spans Timeline</h4>
                        <div class="space-y-2">
                            ${spans.map((span, i) => {
                                const kindColors = {
                                    'llm': 'border-l-blue-500 bg-blue-50 dark:bg-blue-900/20',
                                    'tool': 'border-l-green-500 bg-green-50 dark:bg-green-900/20',
                                    'server': 'border-l-purple-500 bg-purple-50 dark:bg-purple-900/20',
                                    'internal': 'border-l-slate-400 bg-slate-50 dark:bg-slate-700'
                                };
                                const colorClass = kindColors[span.kind] || kindColors['internal'];

                                return `
                                    <div class="p-3 rounded-lg border-l-4 ${colorClass}">
                                        <div class="flex justify-between items-start mb-1">
                                            <div class="flex items-center gap-2">
                                                <span class="font-medium text-slate-800 dark:text-slate-100">${escapeHtml(span.name || 'Span ' + (i + 1))}</span>
                                                <span class="text-xs px-2 py-0.5 bg-slate-200 dark:bg-slate-600 rounded">${span.kind || 'internal'}</span>
                                                ${span.status === 'error' ? '<span class="text-xs px-2 py-0.5 bg-red-100 dark:bg-red-900 text-red-700 dark:text-red-300 rounded">error</span>' : ''}
                                            </div>
                                            <span class="text-sm text-slate-500 dark:text-slate-400">${formatDuration(span.duration_ms || 0)}</span>
                                        </div>
                                        ${span.input_tokens || span.output_tokens ? `
                                            <div class="text-xs text-slate-500 dark:text-slate-400 mt-1">
                                                Tokens: ${formatNumber(span.input_tokens || 0)} in / ${formatNumber(span.output_tokens || 0)} out
                                            </div>
                                        ` : ''}
                                        ${span.attributes && Object.keys(span.attributes).length > 0 ? `
                                            <details class="mt-2">
                                                <summary class="text-xs text-slate-500 cursor-pointer">Attributes</summary>
                                                <pre class="text-xs text-slate-600 dark:text-slate-300 overflow-x-auto mt-1 p-2 bg-white dark:bg-slate-800 rounded">${escapeHtml(JSON.stringify(span.attributes, null, 2))}</pre>
                                            </details>
                                        ` : ''}
                                    </div>
                                `;
                            }).join('')}
                        </div>

                        <!-- Usage Rollup Button -->
                        <div class="mt-6 pt-4 border-t border-slate-200 dark:border-slate-700">
                            <button onclick="loadTraceUsage('${trace.id}')" class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm">
                                Load Full Usage Rollup (including children)
                            </button>
                            <div id="usageRollup-${trace.id}" class="mt-3"></div>
                        </div>
                    </div>
                </div>
            </div>
        `;

        document.body.insertAdjacentHTML('beforeend', content);
    }
}

async function loadTraceUsage(traceId) {
    const container = document.getElementById(`usageRollup-${traceId}`);
    if (!container) return;

    container.innerHTML = '<div class="text-slate-500">Loading...</div>';

    const response = await makeRequest(`/traces/${traceId}/usage`);

    if (response.status === 200 && response.data) {
        const usage = response.data;
        container.innerHTML = `
            <div class="grid grid-cols-3 gap-4 text-sm">
                <div class="bg-blue-50 dark:bg-blue-900/20 rounded-lg p-3">
                    <div class="text-blue-600 dark:text-blue-400 font-medium mb-2">Direct Usage</div>
                    <div class="text-slate-700 dark:text-slate-300">
                        <div>Input: ${formatNumber(usage.direct?.input_tokens || 0)}</div>
                        <div>Output: ${formatNumber(usage.direct?.output_tokens || 0)}</div>
                        <div>LLM Calls: ${usage.direct?.llm_calls || 0}</div>
                        <div>Tool Calls: ${usage.direct?.tool_calls || 0}</div>
                    </div>
                </div>
                <div class="bg-amber-50 dark:bg-amber-900/20 rounded-lg p-3">
                    <div class="text-amber-600 dark:text-amber-400 font-medium mb-2">Children Usage</div>
                    <div class="text-slate-700 dark:text-slate-300">
                        <div>Input: ${formatNumber(usage.children?.input_tokens || 0)}</div>
                        <div>Output: ${formatNumber(usage.children?.output_tokens || 0)}</div>
                        <div>LLM Calls: ${usage.children?.llm_calls || 0}</div>
                        <div>Tool Calls: ${usage.children?.tool_calls || 0}</div>
                    </div>
                </div>
                <div class="bg-emerald-50 dark:bg-emerald-900/20 rounded-lg p-3">
                    <div class="text-emerald-600 dark:text-emerald-400 font-medium mb-2">Total (Recursive)</div>
                    <div class="text-slate-700 dark:text-slate-300 font-semibold">
                        <div>Input: ${formatNumber(usage.total?.input_tokens || 0)}</div>
                        <div>Output: ${formatNumber(usage.total?.output_tokens || 0)}</div>
                        <div>LLM Calls: ${usage.total?.llm_calls || 0}</div>
                        <div>Tool Calls: ${usage.total?.tool_calls || 0}</div>
                    </div>
                </div>
            </div>
        `;
    } else {
        container.innerHTML = '<div class="text-red-500">Failed to load usage data</div>';
    }
}

function formatDuration(ms) {
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
    return `${(ms / 60000).toFixed(1)}m`;
}

function formatNumber(n) {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
    return n.toString();
}

function initTracingTab() {
    // Add event listeners for filter controls
    const topLevelCheckbox = document.getElementById('traceTopLevel');
    const sourceSelect = document.getElementById('traceSource');
    const statusSelect = document.getElementById('traceStatus');
    const searchInput = document.getElementById('traceSearch');

    if (topLevelCheckbox) {
        topLevelCheckbox.addEventListener('change', loadTraces);
    }
    if (sourceSelect) {
        sourceSelect.addEventListener('change', loadTraces);
    }
    if (statusSelect) {
        statusSelect.addEventListener('change', loadTraces);
    }
    if (searchInput) {
        // Debounce search input
        let searchTimeout;
        searchInput.addEventListener('input', () => {
            clearTimeout(searchTimeout);
            searchTimeout = setTimeout(loadTraces, 300);
        });
    }

    loadTraces();
}
