// System Tab Functions

async function loadSystemHealth() {
    const response = await makeRequest('/health');

    if (response.status === 200 && response.data) {
        const health = response.data;

        // Update stats
        document.getElementById('systemStatus').textContent = health.status || 'Unknown';
        document.getElementById('systemUptime').textContent = health.uptime ? formatDuration(health.uptime * 1000) : '-';
        document.getElementById('systemMemory').textContent = health.memory_mb ? `${health.memory_mb} MB` : '-';
        document.getElementById('systemVersion').textContent = health.version || '-';

        // Render health checks
        const container = document.getElementById('healthChecks');
        const checks = health.checks || {};

        if (Object.keys(checks).length === 0) {
            container.innerHTML = `
                <div class="text-center py-8 text-slate-400 dark:text-slate-500">
                    <p>No health checks available</p>
                </div>`;
            return;
        }

        container.innerHTML = Object.entries(checks).map(([name, status]) => {
            const isHealthy = status === true || status === 'ok' || status === 'healthy';
            return `
                <div class="flex items-center justify-between p-3 bg-slate-50 dark:bg-slate-700 rounded-lg">
                    <span class="font-medium text-slate-700 dark:text-slate-200">${escapeHtml(name)}</span>
                    <span class="flex items-center gap-2">
                        <span class="status-dot ${isHealthy ? 'online' : 'offline'}"></span>
                        <span class="text-sm ${isHealthy ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}">
                            ${isHealthy ? 'Healthy' : 'Unhealthy'}
                        </span>
                    </span>
                </div>
            `;
        }).join('');
    }
}

async function resetDatabase() {
    if (!confirm('⚠️ This will DELETE ALL DATA including threads, messages, memories, and tasks. This cannot be undone!\n\nAre you sure?')) {
        return;
    }

    if (!confirm('This is your last chance to cancel. Proceed with database reset?')) {
        return;
    }

    const response = await makeRequest('/reset', 'POST');

    if (response.status === 200) {
        showToast('Database reset complete', 'success');
        loadSystemHealth();
    } else {
        showToast('Failed to reset database', 'error');
    }
}

async function clearEvents() {
    if (!confirm('Clear all stored events?')) return;

    const response = await makeRequest('/events', 'DELETE');

    if (response.status === 200) {
        showToast('Events cleared', 'success');
    } else {
        showToast('Failed to clear events', 'error');
    }
}

async function exportData() {
    showToast('Preparing export...', 'info');

    try {
        const response = await fetch('/admin/export');
        if (response.ok) {
            const blob = await response.blob();
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `agent-export-${new Date().toISOString().split('T')[0]}.json`;
            a.click();
            URL.revokeObjectURL(url);
            showToast('Export complete', 'success');
        } else {
            showToast('Export failed', 'error');
        }
    } catch (error) {
        showToast('Export failed: ' + error.message, 'error');
    }
}

function initSystemTab() {
    loadSystemHealth();
}
