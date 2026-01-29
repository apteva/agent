// MCP Tab Functions

let mcpServersData = [];
let mcpToolsData = [];
let mcpResourcesData = {};
let mcpConfig = null;

// Initialize MCP tab (called by switchTab via init{TabName}Tab pattern)
async function initMcpTab() {
    await loadMCPData();
}

// Load all MCP data
async function loadMCPData() {
    await Promise.all([
        loadMCPStatus(),
        loadMCPServers(),
        loadMCPTools(),
        loadMCPResources(),
        loadMCPConfig()
    ]);
    renderMCPUI();
}

// Refresh MCP data (called by refresh button)
async function refreshMCPData() {
    const badge = document.getElementById('mcpStatusBadge');
    badge.textContent = 'Refreshing...';
    badge.className = 'px-2 py-1 rounded text-xs bg-blue-100 text-blue-600';

    try {
        // Trigger server-side refresh
        await makeRequest('/mcp/refresh', 'POST');
        await loadMCPData();
        showToast('MCP data refreshed', 'success');
    } catch (e) {
        console.error('Failed to refresh MCP:', e);
        showToast('Failed to refresh MCP', 'error');
    }
}

// Load MCP status
async function loadMCPStatus() {
    try {
        const response = await makeRequest('/mcp/status');
        if (response.status === 200 && response.data) {
            const { enabled, servers_count, tools_count } = response.data;
            const badge = document.getElementById('mcpStatusBadge');

            if (enabled) {
                badge.textContent = 'Connected';
                badge.className = 'px-2 py-1 rounded text-xs bg-emerald-100 text-emerald-600';
            } else {
                badge.textContent = 'Disabled';
                badge.className = 'px-2 py-1 rounded text-xs bg-slate-100 text-slate-600';
            }

            document.getElementById('mcpServerCount').textContent = servers_count || 0;
            document.getElementById('mcpToolCount').textContent = tools_count || 0;
        }
    } catch (e) {
        console.error('Failed to load MCP status:', e);
    }
}

// Load MCP servers
async function loadMCPServers() {
    try {
        const response = await makeRequest('/mcp/servers');
        if (response.status === 200 && response.data) {
            mcpServersData = response.data.servers || [];
            document.getElementById('mcpServerCount').textContent = mcpServersData.length;
        }
    } catch (e) {
        console.error('Failed to load MCP servers:', e);
    }
}

// Load MCP tools
async function loadMCPTools() {
    try {
        const response = await makeRequest('/mcp/tools');
        if (response.status === 200 && response.data) {
            mcpToolsData = response.data.tools || [];
            document.getElementById('mcpToolCount').textContent = mcpToolsData.length;
        }
    } catch (e) {
        console.error('Failed to load MCP tools:', e);
    }
}

// Load available resources
async function loadMCPResources() {
    try {
        const response = await makeRequest('/mcp/resources/available');
        if (response.status === 200 && response.data) {
            mcpResourcesData = response.data;
            document.getElementById('mcpResourceCount').textContent = response.data.total || 0;
        }
    } catch (e) {
        console.error('Failed to load MCP resources:', e);
    }
}

// Load current config
async function loadMCPConfig() {
    try {
        const response = await makeRequest('/config');
        if (response.status === 200 && response.data) {
            mcpConfig = response.data.mcp || {};
        }
    } catch (e) {
        console.error('Failed to load config:', e);
    }
}

// Render all MCP UI components
function renderMCPUI() {
    renderMCPServers();
    renderMCPTools();
    renderMCPResources();
}

// Render servers list
function renderMCPServers() {
    const container = document.getElementById('mcpServersList');
    if (!container) return;

    if (mcpServersData.length === 0) {
        container.innerHTML = '<span class="text-slate-400">No MCP servers available</span>';
        return;
    }

    container.innerHTML = mcpServersData.map(server => `
        <div class="flex items-center justify-between p-2 bg-slate-50 dark:bg-slate-700 rounded">
            <div class="flex items-center gap-2">
                ${server.icon_url ? `<img src="${server.icon_url}" class="w-5 h-5 rounded">` : '<span class="w-5 h-5 bg-slate-300 rounded flex items-center justify-center text-xs">🔌</span>'}
                <span class="font-medium text-slate-700 dark:text-slate-200">${server.display_name || server.name}</span>
                <span class="text-xs text-slate-400">${server.name}</span>
            </div>
            <span class="px-2 py-0.5 rounded text-xs ${server.status === 'active' ? 'bg-emerald-100 text-emerald-600' : 'bg-slate-100 text-slate-500'}">
                ${server.status || 'unknown'}
            </span>
        </div>
    `).join('');
}

// Render tools list
function renderMCPTools() {
    const enabledContainer = document.getElementById('mcpEnabledToolsList');
    const availableContainer = document.getElementById('mcpAvailableToolsList');
    if (!enabledContainer || !availableContainer) return;

    const enabledTools = mcpConfig?.tools || [];
    document.getElementById('mcpEnabledToolCount').textContent = enabledTools.length;

    // Enabled tools
    if (enabledTools.length === 0) {
        enabledContainer.innerHTML = '<span class="text-slate-400 text-sm">No tools enabled (all tools available)</span>';
    } else {
        enabledContainer.innerHTML = enabledTools.map(tool => `
            <span class="inline-flex items-center gap-1 px-2 py-1 bg-emerald-100 dark:bg-emerald-900 text-emerald-700 dark:text-emerald-300 rounded text-sm">
                ${tool}
                <button onclick="removeMCPTool('${tool}')" class="ml-1 text-emerald-500 hover:text-red-500">×</button>
            </span>
        `).join('');
    }

    // Available tools (grouped by server)
    const toolsByServer = {};
    mcpToolsData.forEach(tool => {
        const serverName = tool.server_name || 'unknown';
        if (!toolsByServer[serverName]) {
            toolsByServer[serverName] = [];
        }
        toolsByServer[serverName].push(tool);
    });

    const searchTerm = document.getElementById('mcpToolSearch')?.value?.toLowerCase() || '';

    let html = '';
    for (const [serverName, tools] of Object.entries(toolsByServer)) {
        const filteredTools = tools.filter(t =>
            t.name.toLowerCase().includes(searchTerm) ||
            (t.description || '').toLowerCase().includes(searchTerm)
        );

        if (filteredTools.length === 0) continue;

        html += `
            <div class="mb-3">
                <div class="text-xs font-medium text-slate-500 mb-1">${serverName}</div>
                <div class="space-y-1">
                    ${filteredTools.map(tool => {
                        const isEnabled = enabledTools.includes(tool.name);
                        return `
                            <div class="flex items-center justify-between p-2 bg-slate-50 dark:bg-slate-700 rounded text-sm">
                                <div>
                                    <span class="font-medium text-slate-700 dark:text-slate-200">${tool.display_name || tool.name}</span>
                                    <span class="text-xs text-slate-400 ml-1">${tool.name}</span>
                                    ${tool.description ? `<p class="text-xs text-slate-500 mt-0.5">${tool.description.slice(0, 80)}${tool.description.length > 80 ? '...' : ''}</p>` : ''}
                                </div>
                                ${isEnabled
                                    ? '<span class="px-2 py-0.5 bg-emerald-100 text-emerald-600 rounded text-xs">Enabled</span>'
                                    : `<button onclick="addMCPTool('${tool.name}')" class="px-2 py-0.5 bg-blue-100 hover:bg-blue-200 text-blue-600 rounded text-xs">+ Add</button>`
                                }
                            </div>
                        `;
                    }).join('')}
                </div>
            </div>
        `;
    }

    availableContainer.innerHTML = html || '<span class="text-slate-400 text-sm">No tools available</span>';
}

// Render resources list
function renderMCPResources() {
    const enabledContainer = document.getElementById('mcpEnabledResourcesList');
    const availableContainer = document.getElementById('mcpAvailableResourcesList');
    const serverSelect = document.getElementById('mcpResourceServerSelect');
    if (!enabledContainer || !availableContainer) return;

    const enabledResources = mcpConfig?.resources || [];
    document.getElementById('mcpEnabledResourceCount').textContent = enabledResources.length;

    // Enabled resources (by URI)
    if (enabledResources.length === 0) {
        enabledContainer.innerHTML = '<span class="text-slate-400 text-sm">No resources enabled</span>';
    } else {
        enabledContainer.innerHTML = enabledResources.map(uri => {
            const shortUri = uri.length > 40 ? uri.slice(0, 37) + '...' : uri;
            return `
                <span class="inline-flex items-center gap-1 px-2 py-1 bg-emerald-100 dark:bg-emerald-900 text-emerald-700 dark:text-emerald-300 rounded text-sm" title="${uri}">
                    ${shortUri}
                    <button onclick="removeMCPResource('${encodeURIComponent(uri)}')" class="ml-1 text-emerald-500 hover:text-red-500">×</button>
                </span>
            `;
        }).join('');
    }

    // Populate server filter dropdown
    const servers = mcpResourcesData.servers || [];
    serverSelect.innerHTML = '<option value="">All Servers</option>' +
        servers.map(s => `<option value="${s}">${s}</option>`).join('');

    // Available resources by server - show individual resources
    const resources = mcpResourcesData.resources || {};
    const selectedServer = serverSelect.value;

    let html = '';
    for (const [serverName, serverResources] of Object.entries(resources)) {
        if (selectedServer && serverName !== selectedServer) continue;
        if (!serverResources || serverResources.length === 0) continue;

        html += `
            <div class="mb-3">
                <div class="text-xs font-medium text-slate-500 mb-1">${serverName}</div>
                <div class="space-y-1">
                    ${serverResources.map(r => {
                        const uri = r.uri || '';
                        const isEnabled = enabledResources.includes(uri);
                        const name = r.name || r.uri || 'Unnamed';
                        return `
                            <div class="flex items-center justify-between p-2 bg-slate-50 dark:bg-slate-700 rounded text-sm">
                                <div class="flex-1 min-w-0">
                                    <span class="font-medium text-slate-700 dark:text-slate-200">${name}</span>
                                    <p class="text-xs text-slate-400 truncate" title="${uri}">${uri}</p>
                                    ${r.description ? `<p class="text-xs text-slate-500 mt-0.5">${r.description.slice(0, 60)}${r.description.length > 60 ? '...' : ''}</p>` : ''}
                                </div>
                                ${isEnabled
                                    ? '<span class="px-2 py-0.5 bg-emerald-100 text-emerald-600 rounded text-xs ml-2">Enabled</span>'
                                    : `<button onclick="addMCPResource('${encodeURIComponent(uri)}')" class="px-2 py-0.5 bg-blue-100 hover:bg-blue-200 text-blue-600 rounded text-xs ml-2">+ Add</button>`
                                }
                            </div>
                        `;
                    }).join('')}
                </div>
            </div>
        `;
    }

    availableContainer.innerHTML = html || '<span class="text-slate-400 text-sm">No resources discovered. Click Refresh to load.</span>';
}

// Filter tools by search
function filterMCPTools() {
    renderMCPTools();
}

// Add an MCP tool
async function addMCPTool(toolName) {
    const tools = mcpConfig?.tools || [];
    if (tools.includes(toolName)) return;

    const newTools = [...tools, toolName];

    const response = await makeRequest('/config', 'POST', {
        mcp: { ...mcpConfig, tools: newTools }
    });

    if (response.status === 200) {
        showToast(`Added ${toolName}`, 'success');
        await loadMCPConfig();
        renderMCPTools();
    } else {
        showToast('Failed to add tool', 'error');
    }
}

// Remove an MCP tool
async function removeMCPTool(toolName) {
    const tools = mcpConfig?.tools || [];
    const newTools = tools.filter(t => t !== toolName);

    const response = await makeRequest('/config', 'POST', {
        mcp: { ...mcpConfig, tools: newTools }
    });

    if (response.status === 200) {
        showToast(`Removed ${toolName}`, 'success');
        await loadMCPConfig();
        renderMCPTools();
    } else {
        showToast('Failed to remove tool', 'error');
    }
}

// Add a resource by URI
async function addMCPResource(encodedUri) {
    const uri = decodeURIComponent(encodedUri);
    const resources = mcpConfig?.resources || [];
    if (resources.includes(uri)) return;

    const newResources = [...resources, uri];

    const response = await makeRequest('/config', 'POST', {
        mcp: { ...mcpConfig, resources: newResources }
    });

    if (response.status === 200) {
        showToast(`Added resource`, 'success');
        await loadMCPConfig();
        renderMCPResources();
    } else {
        showToast('Failed to add resource', 'error');
    }
}

// Remove a resource by URI
async function removeMCPResource(encodedUri) {
    const uri = decodeURIComponent(encodedUri);
    const resources = mcpConfig?.resources || [];
    const newResources = resources.filter(r => r !== uri);

    const response = await makeRequest('/config', 'POST', {
        mcp: { ...mcpConfig, resources: newResources }
    });

    if (response.status === 200) {
        showToast(`Removed resource`, 'success');
        await loadMCPConfig();
        renderMCPResources();
    } else {
        showToast('Failed to remove resource', 'error');
    }
}

// Handle server select change
document.getElementById('mcpResourceServerSelect')?.addEventListener('change', renderMCPResources);

// ============ MCP Credentials ============

let mcpCredentialsData = [];
let agentCredentials = [];

// Load credentials directly from backend API
async function loadCredentials() {
    try {
        // Get MCP base URL from config
        const configResp = await makeRequest('/config');
        if (configResp.status !== 200 || !configResp.data?.mcp?.base_url) {
            console.log('MCP not configured, skipping credentials load');
            mcpCredentialsData = [];
            renderCredentialsList('MCP not configured');
            return;
        }

        // Strip /mcp suffix to get API base
        let baseURL = configResp.data.mcp.base_url;
        if (baseURL.endsWith('/mcp')) {
            baseURL = baseURL.slice(0, -4);
        }

        // Check for localhost - won't work from browser on remote machine
        if (baseURL.includes('localhost') || baseURL.includes('127.0.0.1')) {
            // Replace localhost with current page's hostname
            const currentHost = window.location.hostname;
            baseURL = baseURL.replace(/localhost|127\.0\.0\.1/, currentHost);
            console.log('Replaced localhost with:', currentHost);
        }

        // Call backend API directly using API key from config (if available)
        const apiKey = configResp.data.mcp?.api_key || '';
        const url = `${baseURL}/omnikit/data/credentials?filter={"status":"active"}`;

        const resp = await fetch(url, {
            headers: apiKey ? { 'X-API-Key': apiKey } : {}
        });

        if (resp.ok) {
            const result = await resp.json();
            // Extract credentials from data array
            mcpCredentialsData = (result.data || []).map(c => ({
                id: c.id,
                provider: c.provider,
                name: c.name,
                project_id: c.project_id
            }));
            renderCredentialsList();
        } else {
            console.error('Failed to load credentials:', resp.status);
            mcpCredentialsData = [];
            renderCredentialsList('Failed to load: ' + resp.status);
        }

        populateCredentialSelect();
    } catch (e) {
        console.error('Failed to load credentials:', e);
        mcpCredentialsData = [];
        renderCredentialsList('Error: ' + e.message);
    }
}

// Refresh credentials
async function refreshCredentials() {
    const container = document.getElementById('mcpCredentialsList');
    if (container) container.innerHTML = '<span class="text-blue-500 text-sm">Loading...</span>';
    await loadCredentials();
    showToast('Credentials refreshed', 'success');
}

// Render available credentials list
function renderCredentialsList(errorMsg = null) {
    const container = document.getElementById('mcpCredentialsList');
    if (!container) return;

    if (errorMsg) {
        container.innerHTML = `<span class="text-amber-500 text-sm">${escapeHtml(errorMsg)}</span>`;
        return;
    }

    if (mcpCredentialsData.length === 0) {
        container.innerHTML = '<span class="text-slate-400 text-sm">No credentials found in backend</span>';
        return;
    }

    container.innerHTML = mcpCredentialsData.map(cred => `
        <div class="flex items-center justify-between p-2 bg-white dark:bg-slate-800 rounded mb-1 border border-slate-200 dark:border-slate-600">
            <div class="flex items-center gap-2">
                <span class="text-lg">${getProviderIcon(cred.provider)}</span>
                <div>
                    <span class="font-medium text-slate-700 dark:text-slate-200 text-sm">${escapeHtml(cred.name || cred.provider)}</span>
                    <span class="text-xs text-slate-400 ml-2">${cred.provider}</span>
                </div>
            </div>
            <span class="text-xs text-slate-400">ID: ${cred.id}</span>
        </div>
    `).join('');
}

// Get icon for provider
function getProviderIcon(provider) {
    const icons = {
        'stripe': '💳',
        'github': '🐙',
        'slack': '💬',
        'notion': '📝',
        'google': '🔵',
        'facebook': '📘',
        'twitter': '🐦',
        'discord': '🎮'
    };
    return icons[provider?.toLowerCase()] || '🔑';
}

// Populate credential select dropdown
function populateCredentialSelect() {
    const select = document.getElementById('mcpAddCredential');
    if (!select) return;

    // Filter out already selected credentials
    const selectedIds = agentCredentials.map(c => c.credential_id);
    const available = mcpCredentialsData.filter(c => !selectedIds.includes(String(c.id)));

    select.innerHTML = '<option value="">Select a credential...</option>' +
        available.map(c => `<option value="${c.id}" data-provider="${c.provider}" data-name="${escapeHtml(c.name || c.provider)}">${c.name || c.provider} (${c.provider})</option>`).join('');
}

// Add credential to agent config
function addAgentCredential() {
    const select = document.getElementById('mcpAddCredential');
    if (!select || !select.value) return;

    const option = select.options[select.selectedIndex];
    const credential = {
        credential_id: select.value,
        provider: option.dataset.provider,
        name: option.dataset.name
    };

    agentCredentials.push(credential);
    renderSelectedCredentials();
    populateCredentialSelect();
}

// Remove credential from agent config
function removeAgentCredential(credentialId) {
    agentCredentials = agentCredentials.filter(c => c.credential_id !== credentialId);
    renderSelectedCredentials();
    populateCredentialSelect();
}

// Render selected credentials
function renderSelectedCredentials() {
    const container = document.getElementById('mcpSelectedCredentials');
    if (!container) return;

    if (agentCredentials.length === 0) {
        container.innerHTML = '<span class="text-slate-400 text-sm">No credentials selected</span>';
        return;
    }

    container.innerHTML = agentCredentials.map(cred => `
        <span class="inline-flex items-center gap-1 px-2 py-1 bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 rounded text-sm mr-1 mb-1">
            ${getProviderIcon(cred.provider)} ${escapeHtml(cred.name)}
            <button onclick="removeAgentCredential('${cred.credential_id}')" class="ml-1 text-blue-500 hover:text-red-500">×</button>
        </span>
    `).join('');
}

// Load agent credentials from config (credentials is a map keyed by provider)
function loadAgentCredentials() {
    if (mcpConfig?.credentials && typeof mcpConfig.credentials === 'object') {
        agentCredentials = Object.values(mcpConfig.credentials).map(c => ({
            credential_id: c.credential_id || c.CredentialID || '',
            provider: c.provider || c.Provider || '',
            name: c.name || c.Name || c.provider || c.Provider || ''
        }));
        renderSelectedCredentials();
        populateCredentialSelect();
    }
}

// Get agent credentials for saving (as map keyed by provider)
function getAgentCredentials() {
    const credMap = {};
    for (const cred of agentCredentials) {
        credMap[cred.provider] = {
            credential_id: cred.credential_id,
            provider: cred.provider,
            name: cred.name
        };
    }
    return credMap;
}

// Initialize credentials when MCP config loads
const originalLoadMCPConfig = loadMCPConfig;
loadMCPConfig = async function() {
    await originalLoadMCPConfig();
    await loadCredentials();
    loadAgentCredentials();
};
