// Usage Tab Functions

async function loadUsage() {
    const startDate = document.getElementById('usageStartDate')?.value || '';
    const endDate = document.getElementById('usageEndDate')?.value || '';
    const groupBy = document.getElementById('usageGroupBy')?.value || '';

    let endpoint = '/usage?';
    const params = [];
    if (startDate) params.push(`start_date=${startDate}`);
    if (endDate) params.push(`end_date=${endDate}`);
    if (groupBy) params.push(`group_by=${groupBy}`);
    endpoint += params.join('&');

    const response = await makeRequest(endpoint);

    if (response.status === 200 && response.data) {
        const data = response.data;
        const summary = data.summary || data.usage || {};

        // Update summary cards
        document.getElementById('usageTotalTokens').textContent = formatNumber(summary.total_tokens || 0);
        document.getElementById('usageInputTokens').textContent = formatNumber(summary.total_input_tokens || 0);
        document.getElementById('usageOutputTokens').textContent = formatNumber(summary.total_output_tokens || 0);
        document.getElementById('usageCallCount').textContent = formatNumber(summary.call_count || 0);

        // Render details based on groupBy
        const container = document.getElementById('usageDetails');

        if (groupBy === 'day' && data.usage_by_day) {
            renderUsageByDay(container, data.usage_by_day);
        } else if (groupBy === 'thread' && data.usage_by_thread) {
            renderUsageByThread(container, data.usage_by_thread);
        } else {
            renderUsageSummary(container, summary);
        }
    }
}

function renderUsageSummary(container, summary) {
    if (!summary || Object.keys(summary).length === 0) {
        container.innerHTML = `
            <div class="empty-state text-center py-12 text-slate-400 dark:text-slate-500">
                <svg class="empty-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1"><path stroke-linecap="round" stroke-linejoin="round" d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V4.125z"/></svg>
                <p>No usage data available</p>
            </div>`;
        return;
    }

    container.innerHTML = `
        <div class="space-y-6">
            <div>
                <h4 class="font-medium text-slate-800 dark:text-slate-100 mb-3">Token Breakdown</h4>
                <div class="space-y-3">
                    <div class="flex justify-between items-center">
                        <span class="text-slate-600 dark:text-slate-400">Input Tokens</span>
                        <span class="font-medium text-slate-800 dark:text-slate-200">${formatNumber(summary.total_input_tokens || 0)}</span>
                    </div>
                    <div class="flex justify-between items-center">
                        <span class="text-slate-600 dark:text-slate-400">Output Tokens</span>
                        <span class="font-medium text-slate-800 dark:text-slate-200">${formatNumber(summary.total_output_tokens || 0)}</span>
                    </div>
                    <div class="flex justify-between items-center border-t border-slate-200 dark:border-slate-700 pt-3">
                        <span class="text-slate-800 dark:text-slate-100 font-medium">Total Tokens</span>
                        <span class="font-bold text-slate-900 dark:text-slate-100">${formatNumber(summary.total_tokens || 0)}</span>
                    </div>
                </div>
            </div>
            ${summary.call_count ? `
                <div class="pt-6 border-t border-slate-200 dark:border-slate-700">
                    <div class="flex gap-8">
                        <div>
                            <span class="text-slate-600 dark:text-slate-400">Total LLM Calls:</span>
                            <span class="font-medium text-slate-800 dark:text-slate-200 ml-2">${formatNumber(summary.call_count)}</span>
                        </div>
                        <div>
                            <span class="text-slate-600 dark:text-slate-400">Avg Tokens/Call:</span>
                            <span class="font-medium text-slate-800 dark:text-slate-200 ml-2">${formatNumber(Math.round((summary.total_tokens || 0) / summary.call_count))}</span>
                        </div>
                    </div>
                </div>
            ` : ''}
        </div>
    `;
}

function renderUsageByDay(container, usageByDay) {
    if (!usageByDay || usageByDay.length === 0) {
        container.innerHTML = `
            <div class="text-center py-12 text-slate-400 dark:text-slate-500">
                <p>No daily usage data available</p>
            </div>`;
        return;
    }

    container.innerHTML = `
        <table class="data-table">
            <thead>
                <tr>
                    <th>Date</th>
                    <th>LLM Calls</th>
                    <th>Input Tokens</th>
                    <th>Output Tokens</th>
                    <th>Total Tokens</th>
                </tr>
            </thead>
            <tbody>
                ${usageByDay.map(day => `
                    <tr>
                        <td class="font-medium">${day.date}</td>
                        <td>${formatNumber(day.call_count || 0)}</td>
                        <td>${formatNumber(day.input_tokens || 0)}</td>
                        <td>${formatNumber(day.output_tokens || 0)}</td>
                        <td>${formatNumber(day.total_tokens || 0)}</td>
                    </tr>
                `).join('')}
            </tbody>
        </table>
    `;
}

function renderUsageByThread(container, usageByThread) {
    if (!usageByThread || usageByThread.length === 0) {
        container.innerHTML = `
            <div class="text-center py-12 text-slate-400 dark:text-slate-500">
                <p>No thread usage data available</p>
            </div>`;
        return;
    }

    container.innerHTML = `
        <table class="data-table">
            <thead>
                <tr>
                    <th>Thread ID</th>
                    <th>LLM Calls</th>
                    <th>Input Tokens</th>
                    <th>Output Tokens</th>
                    <th>Total Tokens</th>
                </tr>
            </thead>
            <tbody>
                ${usageByThread.map(thread => `
                    <tr>
                        <td class="font-mono text-sm">${thread.thread_id?.substring(0, 12) || '-'}...</td>
                        <td>${formatNumber(thread.call_count || 0)}</td>
                        <td>${formatNumber(thread.input_tokens || 0)}</td>
                        <td>${formatNumber(thread.output_tokens || 0)}</td>
                        <td>${formatNumber(thread.total_tokens || 0)}</td>
                    </tr>
                `).join('')}
            </tbody>
        </table>
    `;
}

function initUsageTab() {
    // Set default date range to last 30 days
    const endDate = new Date();
    const startDate = new Date();
    startDate.setDate(startDate.getDate() - 30);

    document.getElementById('usageStartDate').value = startDate.toISOString().split('T')[0];
    document.getElementById('usageEndDate').value = endDate.toISOString().split('T')[0];

    loadUsage();
}
