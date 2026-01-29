// Scheduler Tab Functions

async function loadSchedulerStatus() {
    const response = await makeRequest('/scheduler/status');

    if (response.status === 200 && response.data) {
        const status = response.data;

        document.getElementById('schedulerStatus').textContent = status.running ? 'Running' : 'Stopped';
        document.getElementById('schedulerJobs').textContent = status.job_count || 0;
        document.getElementById('schedulerExecs').textContent = status.executions_today || 0;
        document.getElementById('schedulerNext').textContent = status.next_run ? formatRelativeTime(status.next_run) : '-';

        // Load jobs list
        loadSchedulerJobs();
    }
}

async function loadSchedulerJobs() {
    const response = await makeRequest('/scheduler/jobs');

    if (response.status === 200 && response.data) {
        const jobs = response.data.jobs || [];
        const container = document.getElementById('schedulerJobsList');

        if (jobs.length === 0) {
            container.innerHTML = `
                <div class="text-center py-8 text-slate-400 dark:text-slate-500">
                    <p>No scheduled jobs</p>
                </div>`;
            return;
        }

        container.innerHTML = jobs.map(job => `
            <div class="p-3 bg-slate-50 dark:bg-slate-700 rounded-lg mb-2">
                <div class="flex justify-between items-start">
                    <div>
                        <div class="font-medium text-slate-800 dark:text-slate-100">${escapeHtml(job.name || job.id)}</div>
                        <div class="text-sm text-slate-500 dark:text-slate-400">${escapeHtml(job.schedule || job.cron)}</div>
                    </div>
                    <span class="px-2 py-1 text-xs rounded-full ${job.enabled ? 'bg-emerald-100 dark:bg-emerald-900/50 text-emerald-700 dark:text-emerald-300' : 'bg-slate-100 dark:bg-slate-600 text-slate-600 dark:text-slate-300'}">
                        ${job.enabled ? 'Active' : 'Paused'}
                    </span>
                </div>
                ${job.next_run ? `<div class="text-xs text-slate-400 dark:text-slate-500 mt-1">Next: ${formatDate(job.next_run)}</div>` : ''}
            </div>
        `).join('');
    }
}

async function startScheduler() {
    const response = await makeRequest('/scheduler/start', 'POST');

    if (response.status === 200) {
        showToast('Scheduler started', 'success');
        loadSchedulerStatus();
    } else {
        showToast('Failed to start scheduler', 'error');
    }
}

async function stopScheduler() {
    const response = await makeRequest('/scheduler/stop', 'POST');

    if (response.status === 200) {
        showToast('Scheduler stopped', 'success');
        loadSchedulerStatus();
    } else {
        showToast('Failed to stop scheduler', 'error');
    }
}

async function triggerScheduler() {
    const response = await makeRequest('/scheduler/trigger', 'POST');

    if (response.status === 200) {
        showToast('Scheduler triggered', 'success');
        loadSchedulerStatus();
    } else {
        showToast('Failed to trigger scheduler', 'error');
    }
}

function initSchedulerTab() {
    loadSchedulerStatus();
}
