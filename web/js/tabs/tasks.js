// Tasks Tab Functions

async function loadTasks() {
    const statusFilter = document.getElementById('taskStatusFilter').value;
    let endpoint = '/tasks';
    if (statusFilter) {
        endpoint += `?status=${statusFilter}`;
    }

    const response = await makeRequest(endpoint);

    if (response.status === 200 && response.data) {
        const tasks = response.data.tasks || [];

        // Update stats
        const stats = {
            total: tasks.length,
            completed: tasks.filter(t => t.status === 'completed').length,
            pending: tasks.filter(t => t.status === 'pending').length,
            running: tasks.filter(t => t.status === 'running').length,
            failed: tasks.filter(t => t.status === 'failed').length
        };

        document.getElementById('tasksTotal').textContent = stats.total;
        document.getElementById('tasksCompleted').textContent = stats.completed;
        document.getElementById('tasksPending').textContent = stats.pending;
        document.getElementById('tasksRunning').textContent = stats.running;
        document.getElementById('tasksFailed').textContent = stats.failed;

        // Render tasks
        const container = document.getElementById('tasksList');

        if (tasks.length === 0) {
            container.innerHTML = `
                <div class="empty-state text-center py-12 text-slate-400 dark:text-slate-500">
                    <svg class="empty-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1"><path stroke-linecap="round" stroke-linejoin="round" d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"/></svg>
                    <p>No tasks found</p>
                </div>`;
            return;
        }

        const statusColors = {
            pending: 'bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300',
            running: 'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300',
            completed: 'bg-emerald-100 dark:bg-emerald-900/50 text-emerald-700 dark:text-emerald-300',
            failed: 'bg-red-100 dark:bg-red-900/50 text-red-700 dark:text-red-300'
        };

        const statusIcons = {
            pending: '',
            running: '',
            completed: '',
            failed: ''
        };

        container.innerHTML = tasks.map(task => `
            <div class="p-4 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors">
                <div class="flex justify-between items-start mb-2">
                    <div class="flex items-center gap-2">
                        <span class="px-2 py-1 text-xs font-medium rounded-full ${statusColors[task.status] || 'bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300'}">
                            ${task.status.toUpperCase()}
                        </span>
                        <span class="font-medium text-slate-800 dark:text-slate-100">${escapeHtml(task.description || task.type)}</span>
                    </div>
                    <div class="flex gap-2">
                        ${task.status === 'pending' ? `
                            <button onclick="executeTask('${task.id}')" class="text-blue-500 hover:text-blue-700 text-sm flex items-center gap-1"><svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg> Run</button>
                        ` : ''}
                        <button onclick="deleteTask('${task.id}')" class="text-red-500 hover:text-red-700 text-sm"><svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg></button>
                    </div>
                </div>
                <div class="text-sm text-slate-500 dark:text-slate-400 flex gap-4">
                    <span>ID: ${task.id.substring(0, 8)}...</span>
                    <span>Created: ${formatRelativeTime(task.created_at)}</span>
                    ${task.scheduled_for ? `<span>Scheduled: ${formatDate(task.scheduled_for)}</span>` : ''}
                </div>
                ${task.result ? `
                    <div class="mt-2 p-2 bg-slate-50 dark:bg-slate-700 rounded text-sm font-mono text-slate-600 dark:text-slate-300 max-h-20 overflow-hidden">
                        ${escapeHtml(truncate(JSON.stringify(task.result), 200))}
                    </div>
                ` : ''}
            </div>
        `).join('');
    }
}

async function executeTask(taskId) {
    const response = await makeRequest(`/tasks/${taskId}/execute`, 'POST');

    if (response.status === 200) {
        showToast('Task execution started', 'success');
        loadTasks();
    } else {
        showToast('Failed to execute task', 'error');
    }
}

async function deleteTask(taskId) {
    if (!confirm('Delete this task?')) return;

    const response = await makeRequest(`/tasks/${taskId}`, 'DELETE');

    if (response.status === 200) {
        showToast('Task deleted', 'success');
        loadTasks();
    } else {
        showToast('Failed to delete task', 'error');
    }
}

function initTasksTab() {
    loadTasks();
}
