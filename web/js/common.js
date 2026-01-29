// Common utilities for debug interface

const API_BASE = '';

// ========== Dark Mode ==========

function toggleDarkMode() {
    // Add transition class for smooth animation
    document.body.style.transition = 'background-color 0.2s ease, color 0.2s ease';

    document.documentElement.classList.toggle('dark');
    const isDark = document.documentElement.classList.contains('dark');
    localStorage.setItem('darkMode', isDark ? 'true' : 'false');
    document.getElementById('darkModeToggle').textContent = isDark ? '☀️' : '🌙';
}

function initDarkMode() {
    // Dark mode class is already applied in head script to prevent flash
    // Just update the toggle button icon
    const isDark = document.documentElement.classList.contains('dark');
    const toggleBtn = document.getElementById('darkModeToggle');
    if (toggleBtn && isDark) {
        toggleBtn.textContent = '☀️';
    }
}

// ========== API Functions ==========

async function makeRequest(endpoint, method = 'GET', body = null) {
    try {
        const options = {
            method,
            headers: { 'Content-Type': 'application/json' }
        };
        if (body) options.body = JSON.stringify(body);

        const response = await fetch(`${API_BASE}${endpoint}`, options);
        const data = await response.text();

        return {
            status: response.status,
            data: data ? JSON.parse(data) : null,
            rawData: data,
            headers: response.headers
        };
    } catch (error) {
        return {
            status: 0,
            data: null,
            rawData: error.message,
            error: true
        };
    }
}

// ========== UI Utilities ==========

function showToast(message, type = 'info') {
    const colors = {
        success: 'bg-emerald-500',
        error: 'bg-red-500',
        info: 'bg-blue-500',
        warning: 'bg-amber-500'
    };

    const toast = document.createElement('div');
    toast.className = `fixed top-4 right-4 px-4 py-3 ${colors[type] || colors.info} text-white rounded-lg shadow-lg z-50 animate-fade-in`;
    toast.textContent = message;
    document.body.appendChild(toast);

    setTimeout(() => {
        toast.classList.add('animate-fade-out');
        setTimeout(() => toast.remove(), 300);
    }, 3000);
}

function showResponse(elementId, response, isError = false) {
    const el = document.getElementById(elementId);
    if (!el) return;

    el.classList.remove('hidden');
    el.className = `mt-4 p-4 rounded-lg text-sm font-mono overflow-auto max-h-64 ${
        isError
            ? 'bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-800'
            : 'bg-slate-50 dark:bg-slate-800 text-slate-700 dark:text-slate-300 border border-slate-200 dark:border-slate-700'
    }`;

    if (typeof response === 'object') {
        el.textContent = JSON.stringify(response, null, 2);
    } else {
        el.textContent = response;
    }
}

// ========== Formatters ==========

function formatNumber(num) {
    if (num >= 1000000) {
        return (num / 1000000).toFixed(2) + 'M';
    } else if (num >= 1000) {
        return (num / 1000).toFixed(1) + 'K';
    }
    return num.toLocaleString();
}

function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatDuration(ms) {
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
    return `${(ms / 60000).toFixed(1)}m`;
}

function formatDate(dateStr) {
    if (!dateStr) return '-';
    const date = new Date(dateStr);
    return date.toLocaleDateString() + ' ' + date.toLocaleTimeString();
}

function formatRelativeTime(dateStr) {
    if (!dateStr) return '-';
    const date = new Date(dateStr);
    const now = new Date();
    const diff = now - date;

    if (diff < 60000) return 'just now';
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
    return `${Math.floor(diff / 86400000)}d ago`;
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function truncate(str, len = 100) {
    if (!str) return '';
    return str.length > len ? str.substring(0, len) + '...' : str;
}

// ========== Tab Management ==========

let currentTab = 'voice';

function switchTab(event, tabName) {
    // Update sidebar buttons
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.classList.remove('active', 'bg-slate-800', 'text-white');
        btn.classList.add('text-slate-300', 'hover:bg-slate-800', 'hover:text-white');
    });

    if (event && event.currentTarget) {
        event.currentTarget.classList.add('active', 'bg-slate-800', 'text-white');
        event.currentTarget.classList.remove('text-slate-300');
    }

    // Update tab panes
    document.querySelectorAll('.tab-pane').forEach(pane => {
        pane.classList.add('hidden');
        pane.classList.remove('animate');
    });

    const activePane = document.getElementById(tabName);
    if (activePane) {
        activePane.classList.remove('hidden');
        // Only animate if user clicked (not initial load)
        if (event) {
            activePane.classList.add('animate');
        }
    }

    currentTab = tabName;

    // Trigger tab-specific initialization
    if (typeof window[`init${capitalize(tabName)}Tab`] === 'function') {
        window[`init${capitalize(tabName)}Tab`]();
    }
}

function capitalize(str) {
    return str.charAt(0).toUpperCase() + str.slice(1);
}

// ========== Component Builders ==========

function createStatCard(label, value, gradient = 'from-blue-500 to-blue-600', icon = '') {
    return `
        <div class="bg-gradient-to-br ${gradient} rounded-xl p-5 text-white shadow-lg">
            <div class="text-sm opacity-90 mb-1">${icon} ${label}</div>
            <div class="text-2xl font-bold">${value}</div>
        </div>
    `;
}

function createCard(title, content, actions = '') {
    return `
        <div class="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-xl shadow-sm overflow-hidden">
            <div class="px-5 py-4 border-b border-slate-100 dark:border-slate-700 flex justify-between items-center">
                <h3 class="font-semibold text-slate-800 dark:text-slate-100">${title}</h3>
                ${actions}
            </div>
            <div class="p-5">${content}</div>
        </div>
    `;
}

function createBadge(text, color = 'blue') {
    const colors = {
        blue: 'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300',
        green: 'bg-emerald-100 dark:bg-emerald-900/50 text-emerald-700 dark:text-emerald-300',
        red: 'bg-red-100 dark:bg-red-900/50 text-red-700 dark:text-red-300',
        yellow: 'bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300',
        purple: 'bg-purple-100 dark:bg-purple-900/50 text-purple-700 dark:text-purple-300',
        cyan: 'bg-cyan-100 dark:bg-cyan-900/50 text-cyan-700 dark:text-cyan-300',
        slate: 'bg-slate-100 dark:bg-slate-700 text-slate-700 dark:text-slate-300'
    };
    return `<span class="px-2 py-1 text-xs font-medium rounded-full ${colors[color] || colors.blue}">${text}</span>`;
}

function createButton(text, onclick, variant = 'primary', size = 'md') {
    const variants = {
        primary: 'bg-blue-600 hover:bg-blue-700 text-white',
        secondary: 'bg-slate-100 hover:bg-slate-200 text-slate-700',
        danger: 'bg-red-600 hover:bg-red-700 text-white',
        success: 'bg-emerald-600 hover:bg-emerald-700 text-white'
    };
    const sizes = {
        sm: 'px-2 py-1 text-xs',
        md: 'px-4 py-2 text-sm',
        lg: 'px-6 py-3 text-base'
    };
    return `<button onclick="${onclick}" class="${variants[variant]} ${sizes[size]} font-medium rounded-lg transition-colors">${text}</button>`;
}

function createInput(id, placeholder = '', type = 'text', className = '') {
    return `<input type="${type}" id="${id}" placeholder="${placeholder}"
        class="px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 outline-none ${className}">`;
}

function createSelect(id, options, className = '') {
    const opts = options.map(o =>
        typeof o === 'string'
            ? `<option value="${o}">${o}</option>`
            : `<option value="${o.value}">${o.label}</option>`
    ).join('');
    return `<select id="${id}" class="px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 focus:ring-2 focus:ring-blue-500 focus:border-blue-500 outline-none ${className}">${opts}</select>`;
}

function createEmptyState(message, icon = '📭') {
    return `
        <div class="text-center py-12 text-slate-400 dark:text-slate-500">
            <div class="text-4xl mb-3">${icon}</div>
            <p>${message}</p>
        </div>
    `;
}

function createLoadingState() {
    return `
        <div class="flex items-center justify-center py-12">
            <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
        </div>
    `;
}

// ========== Event Source (SSE) ==========

let eventSource = null;

// All known event types from the server
const EVENT_TYPES = [
    // System
    'system_startup', 'system_shutdown', 'config_change', 'sse_connected', 'heartbeat',
    // Chat
    'message_received', 'response_started', 'stream_chunk', 'response_complete', 'chat_error', 'thread_created',
    // Tool
    'tool_invocation', 'tool_result', 'tool_error',
    // Database
    'db_query', 'db_insert', 'db_update', 'db_delete', 'db_error',
    // MCP
    'mcp_server_connect', 'mcp_tool_discovery', 'mcp_tool_execution', 'mcp_error',
    // Scheduler
    'scheduler_task_start', 'scheduler_task_complete', 'scheduler_task_failed',
    // LLM
    'llm_request', 'llm_response', 'llm_tokens', 'llm_error',
    // Task
    'task_created', 'task_updated', 'task_executed', 'task_deleted', 'task_failed', 'task_listed',
    // File
    'file_uploaded', 'file_deleted', 'file_expired', 'file_ingested'
];

function connectEventStream(onEvent) {
    if (eventSource) {
        eventSource.close();
    }

    eventSource = new EventSource(`${API_BASE}/events`);

    // Handler for parsing and forwarding events
    const handleEvent = (e) => {
        try {
            const event = JSON.parse(e.data);
            onEvent(event);
        } catch (err) {
            console.error('Failed to parse event:', err);
        }
    };

    // Listen for all known event types
    EVENT_TYPES.forEach(type => {
        eventSource.addEventListener(type, handleEvent);
    });

    // Also listen to generic message events (fallback)
    eventSource.onmessage = handleEvent;

    eventSource.onerror = () => {
        console.log('Event stream disconnected, reconnecting...');
        setTimeout(() => connectEventStream(onEvent), 3000);
    };
}

function disconnectEventStream() {
    if (eventSource) {
        eventSource.close();
        eventSource = null;
    }
}
