// Common utilities for debug interface

const API_BASE = '';

// API Key for authentication - loaded from localStorage or can be set via setApiKey()
let API_KEY = localStorage.getItem('agent_api_key') || '';

function setApiKey(key) {
    API_KEY = key;
    localStorage.setItem('agent_api_key', key);
}

function getApiKey() {
    return API_KEY;
}

function clearApiKey() {
    API_KEY = '';
    localStorage.removeItem('agent_api_key');
}

// ========== SVG Icon Helpers ==========

const ICONS = {
    sun: '<svg width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M12 3v2.25m6.364.386-1.591 1.591M21 12h-2.25m-.386 6.364-1.591-1.591M12 18.75V21m-4.773-4.227-1.591 1.591M5.25 12H3m4.227-4.773L5.636 5.636M15.75 12a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0Z"/></svg>',
    moon: '<svg width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M21.752 15.002A9.72 9.72 0 0 1 18 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 0 0 3 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 0 0 9.002-5.998Z"/></svg>',
    empty: '<svg width="48" height="48" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1"><path stroke-linecap="round" stroke-linejoin="round" d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m6 4.125l2.25 2.25m0 0l2.25 2.25M12 13.875l2.25-2.25M12 13.875l-2.25 2.25M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"/></svg>',
};

// ========== Dark Mode ==========

function toggleDarkMode() {
    document.body.style.transition = 'background-color 0.2s ease, color 0.2s ease';
    document.documentElement.classList.toggle('dark');
    const isDark = document.documentElement.classList.contains('dark');
    localStorage.setItem('darkMode', isDark ? 'true' : 'false');
    const btn = document.getElementById('darkModeToggle');
    if (btn) btn.innerHTML = isDark ? ICONS.sun : ICONS.moon;
}

function initDarkMode() {
    const isDark = document.documentElement.classList.contains('dark');
    const btn = document.getElementById('darkModeToggle');
    if (btn && isDark) btn.innerHTML = ICONS.sun;
}

// ========== API Functions ==========

async function makeRequest(endpoint, method = 'GET', body = null) {
    try {
        const headers = { 'Content-Type': 'application/json' };

        if (API_KEY) {
            headers['X-API-Key'] = API_KEY;
        }

        const options = {
            method,
            headers
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
    toast.className = `fixed top-4 right-4 px-4 py-3 ${colors[type] || colors.info} text-white rounded-lg shadow-lg z-50 animate-fade-in text-sm font-medium`;
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
            ? 'bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-800/50'
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
        btn.classList.add('text-slate-400', 'hover:bg-white/5', 'hover:text-slate-200');
    });

    if (event && event.currentTarget) {
        event.currentTarget.classList.add('active', 'bg-slate-800', 'text-white');
        event.currentTarget.classList.remove('text-slate-400');
    }

    // Update tab panes
    document.querySelectorAll('.tab-pane').forEach(pane => {
        pane.classList.add('hidden');
        pane.classList.remove('animate');
    });

    const activePane = document.getElementById(tabName);
    if (activePane) {
        activePane.classList.remove('hidden');
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

const STAT_VARIANTS = {
    primary: 'stat-card-primary',
    success: 'stat-card-success',
    warning: 'stat-card-warning',
    danger:  'stat-card-danger',
    purple:  'stat-card-purple',
    teal:    'stat-card-teal',
};

function createStatCard(label, value, variant = 'primary', icon = '') {
    const cls = STAT_VARIANTS[variant] || STAT_VARIANTS.primary;
    return `
        <div class="stat-card ${cls}">
            <div class="stat-label">${icon ? icon + ' ' : ''}${label}</div>
            <div class="stat-value">${value}</div>
        </div>
    `;
}

function createCard(title, content, actions = '') {
    return `
        <div class="bg-white dark:bg-slate-800 border border-slate-200/75 dark:border-slate-700/50 rounded-xl shadow-sm overflow-hidden">
            <div class="px-5 py-3.5 border-b border-slate-100 dark:border-slate-700/50 flex justify-between items-center">
                <h3 class="text-sm font-semibold text-slate-800 dark:text-slate-100">${title}</h3>
                ${actions}
            </div>
            <div class="p-5">${content}</div>
        </div>
    `;
}

function createBadge(text, color = 'blue') {
    const colors = {
        blue:   'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-300 ring-1 ring-blue-200/50 dark:ring-blue-800/50',
        green:  'bg-emerald-50 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-300 ring-1 ring-emerald-200/50 dark:ring-emerald-800/50',
        red:    'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-300 ring-1 ring-red-200/50 dark:ring-red-800/50',
        yellow: 'bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-300 ring-1 ring-amber-200/50 dark:ring-amber-800/50',
        purple: 'bg-purple-50 dark:bg-purple-900/30 text-purple-600 dark:text-purple-300 ring-1 ring-purple-200/50 dark:ring-purple-800/50',
        cyan:   'bg-cyan-50 dark:bg-cyan-900/30 text-cyan-600 dark:text-cyan-300 ring-1 ring-cyan-200/50 dark:ring-cyan-800/50',
        slate:  'bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300 ring-1 ring-slate-200/50 dark:ring-slate-600/50'
    };
    return `<span class="px-2 py-0.5 text-xs font-medium rounded-md ${colors[color] || colors.blue}">${text}</span>`;
}

function createButton(text, onclick, variant = 'primary', size = 'md') {
    const variants = {
        primary:   'bg-blue-600 hover:bg-blue-700 text-white shadow-sm',
        secondary: 'bg-slate-100 hover:bg-slate-200 dark:bg-slate-700 dark:hover:bg-slate-600 text-slate-700 dark:text-slate-200',
        danger:    'bg-red-600 hover:bg-red-700 text-white shadow-sm',
        success:   'bg-emerald-600 hover:bg-emerald-700 text-white shadow-sm'
    };
    const sizes = {
        sm: 'px-2.5 py-1 text-xs',
        md: 'px-3.5 py-2 text-sm',
        lg: 'px-5 py-2.5 text-sm'
    };
    return `<button onclick="${onclick}" class="${variants[variant]} ${sizes[size]} font-medium rounded-lg transition-all">${text}</button>`;
}

function createInput(id, placeholder = '', type = 'text', className = '') {
    return `<input type="${type}" id="${id}" placeholder="${placeholder}"
        class="px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 text-sm outline-none transition-shadow ${className}">`;
}

function createSelect(id, options, className = '') {
    const opts = options.map(o =>
        typeof o === 'string'
            ? `<option value="${o}">${o}</option>`
            : `<option value="${o.value}">${o.label}</option>`
    ).join('');
    return `<select id="${id}" class="px-3 py-2 border border-slate-200 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 text-sm outline-none transition-shadow ${className}">${opts}</select>`;
}

function createEmptyState(message, icon = '') {
    const svg = icon || ICONS.empty;
    return `
        <div class="text-center py-12 text-slate-400 dark:text-slate-500">
            <div class="flex justify-center mb-3 opacity-40">${svg}</div>
            <p class="text-sm">${message}</p>
        </div>
    `;
}

function createLoadingState() {
    return `
        <div class="flex items-center justify-center py-12">
            <div class="h-6 w-6 border-2 border-blue-500/30 border-t-blue-500 rounded-full" style="animation: spin 0.7s linear infinite;"></div>
        </div>
    `;
}

// ========== Event Source (SSE) ==========

let eventSource = null;

// All known event types from the server
const EVENT_TYPES = [
    // System
    'system_startup', 'system_shutdown', 'config_change', 'sse_connected', 'heartbeat',
    'webhook_received', 'subscription_created', 'subscription_deleted',
    'realtime_session_created', 'realtime_session_closed', 'realtime_event', 'realtime_telephony_event',
    // Chat
    'message_received', 'response_started', 'stream_chunk', 'response_complete', 'chat_error',
    'thread_created', 'thread_activity',
    // Tool
    'tool_invocation', 'tool_result', 'tool_error',
    // Database
    'db_query', 'db_insert', 'db_update', 'db_delete', 'db_error', 'db_connection', 'db_schema',
    // MCP
    'mcp_server_connect', 'mcp_tool_discovery', 'mcp_tool_execution', 'mcp_error',
    // Scheduler
    'scheduler_started', 'scheduler_stopped', 'scheduler_tick', 'scheduler_manual_tick',
    'scheduler_task_start', 'scheduler_task_complete', 'scheduler_task_failed', 'scheduler_agent_called',
    // LLM
    'llm_request', 'llm_response', 'llm_tokens', 'llm_error',
    // Task
    'task_created', 'task_updated', 'task_executed', 'task_deleted', 'task_failed', 'task_listed',
    // File
    'file_uploaded', 'file_deleted', 'file_expired', 'file_ingested',
    // Memory
    'memory_stored', 'memory_retrieved', 'memories_injected',
    // Agent
    'agent_comm_initialized', 'agent_call_started', 'agent_call_completed', 'agent_call_failed',
    'agent_call_retry', 'agent_call_blocked',
    // Reflection
    'reflection_started', 'reflection_stopped', 'reflection_session_started', 'reflection_session_completed',
    // Browser/Operator
    'operator_enabled', 'session_created', 'session_cleanup'
];

function connectEventStream(onEvent) {
    if (eventSource) {
        eventSource.close();
    }

    const url = API_KEY ? `${API_BASE}/events?api_key=${encodeURIComponent(API_KEY)}` : `${API_BASE}/events`;
    eventSource = new EventSource(url);

    const handleEvent = (e) => {
        try {
            const event = JSON.parse(e.data);
            onEvent(event);
        } catch (err) {
            console.error('Failed to parse event:', err);
        }
    };

    EVENT_TYPES.forEach(type => {
        eventSource.addEventListener(type, handleEvent);
    });

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
