// Subscriptions Tab Functions

let subscriptionsData = [];
let availableWebhooksData = [];

// Initialize Subscriptions tab
async function initSubscriptionsTab() {
    await loadSubscriptionsData();
}

// Load all subscriptions data
async function loadSubscriptionsData() {
    await Promise.all([
        loadSubscriptions(),
        loadAvailableWebhooks()
    ]);
    renderSubscriptionsUI();
}

// Refresh subscriptions
async function refreshSubscriptions() {
    const badge = document.getElementById('subscriptionsStatusBadge');
    badge.textContent = 'Refreshing...';
    badge.className = 'px-2 py-1 rounded text-xs bg-blue-100 text-blue-600';

    try {
        await loadSubscriptionsData();
        showToast('Subscriptions refreshed', 'success');
    } catch (e) {
        console.error('Failed to refresh subscriptions:', e);
        showToast('Failed to refresh', 'error');
    }
}

// Load active subscriptions
async function loadSubscriptions() {
    try {
        const response = await makeRequest('/subscriptions');
        if (response.status === 200 && response.data) {
            subscriptionsData = response.data.subscriptions || [];

            const badge = document.getElementById('subscriptionsStatusBadge');
            badge.textContent = subscriptionsData.length > 0 ? 'Active' : 'No Subscriptions';
            badge.className = subscriptionsData.length > 0
                ? 'px-2 py-1 rounded text-xs bg-emerald-100 text-emerald-600'
                : 'px-2 py-1 rounded text-xs bg-slate-100 text-slate-600';

            document.getElementById('subscriptionsActiveCount').textContent = subscriptionsData.length;

            // Sum up thread counts
            const totalThreads = subscriptionsData.reduce((sum, s) => sum + (s.thread_count || 0), 0);
            document.getElementById('subscriptionsThreadCount').textContent = totalThreads;
        }
    } catch (e) {
        console.error('Failed to load subscriptions:', e);
    }
}

// Track enabled webhook servers for UI messaging
let enabledWebhookServers = [];

// Load available webhooks from MCP servers
async function loadAvailableWebhooks() {
    try {
        // This calls the MCP webhooks endpoint which filters by enabled servers
        const response = await makeRequest('/mcp/webhooks');
        if (response.status === 200 && response.data) {
            availableWebhooksData = response.data.webhooks || [];
            enabledWebhookServers = response.data.enabled_servers || [];

            // Count total available events
            const totalEvents = availableWebhooksData.reduce((sum, s) => sum + (s.events?.length || 0), 0);
            document.getElementById('subscriptionsAvailableCount').textContent = totalEvents;
        }
    } catch (e) {
        console.error('Failed to load available webhooks:', e);
        // Fallback to empty
        availableWebhooksData = [];
        enabledWebhookServers = [];
        document.getElementById('subscriptionsAvailableCount').textContent = '0';
    }
}

// Render all UI components
function renderSubscriptionsUI() {
    renderSubscriptionsList();
    renderAvailableWebhooks();
    populateTestServerDropdown();
}

// Render active subscriptions
function renderSubscriptionsList() {
    const container = document.getElementById('subscriptionsList');
    if (!container) return;

    if (subscriptionsData.length === 0) {
        container.innerHTML = `
            <div class="text-center py-8 text-slate-400 dark:text-slate-500">
                <div class="text-3xl mb-2">📨</div>
                <p>No active subscriptions</p>
                <p class="text-sm mt-1">Subscribe to webhook events from the Available Webhooks section below</p>
            </div>`;
        return;
    }

    container.innerHTML = subscriptionsData.map(sub => `
        <div class="p-4 bg-slate-50 dark:bg-slate-700 rounded-lg">
            <div class="flex justify-between items-start mb-2">
                <div class="flex items-center gap-2">
                    ${sub.title ? `<span class="font-semibold text-slate-800 dark:text-slate-100">${escapeHtml(sub.title)}</span>` : ''}
                    <span class="${sub.title ? 'text-slate-500 dark:text-slate-400 text-sm' : 'font-medium text-slate-800 dark:text-slate-100'}">${sub.server}</span>
                    <span class="px-2 py-0.5 bg-emerald-100 dark:bg-emerald-900 text-emerald-600 dark:text-emerald-300 rounded text-xs">
                        ${sub.events?.length || 0} events
                    </span>
                </div>
                <div class="flex gap-2">
                    <button onclick="editSubscription('${escapeHtml(sub.title)}')" class="text-blue-500 hover:text-blue-700 text-sm" title="Edit subscription">
                        ✏️
                    </button>
                    <button onclick="unsubscribeByTitle('${escapeHtml(sub.title)}')" class="text-red-500 hover:text-red-700 text-sm" title="Unsubscribe">
                        🗑️
                    </button>
                </div>
            </div>
            <div class="flex flex-wrap gap-1 mb-2">
                ${(sub.events || []).map(e => `
                    <span class="px-2 py-0.5 bg-blue-100 dark:bg-blue-900 text-blue-600 dark:text-blue-300 rounded text-xs">${e}</span>
                `).join('')}
            </div>
            ${sub.prompt ? `
                <div class="text-xs text-slate-500 dark:text-slate-400 mt-2 p-2 bg-white dark:bg-slate-800 rounded border border-slate-200 dark:border-slate-600">
                    <span class="font-medium">Prompt:</span> ${escapeHtml(sub.prompt.slice(0, 100))}${sub.prompt.length > 100 ? '...' : ''}
                </div>
            ` : ''}
            <div class="text-xs text-slate-400 mt-2 flex gap-4">
                <span>Threads: ${sub.thread_count || 0}</span>
                ${sub.last_triggered ? `<span>Last: ${formatRelativeTime(sub.last_triggered)}</span>` : ''}
                <span>Created: ${formatRelativeTime(sub.created_at)}</span>
            </div>
        </div>
    `).join('');
}

// Render available webhooks
function renderAvailableWebhooks() {
    const container = document.getElementById('availableWebhooksList');
    if (!container) return;

    if (availableWebhooksData.length === 0) {
        // Check if no servers are enabled vs no webhooks available
        if (enabledWebhookServers.length === 0) {
            container.innerHTML = `
                <div class="text-center py-4 text-slate-400">
                    <p class="text-amber-500">⚠️ No webhook servers enabled</p>
                    <p class="text-sm mt-1">Enable webhook servers in Config → MCP → Webhooks section</p>
                </div>`;
        } else {
            container.innerHTML = `
                <div class="text-center py-4 text-slate-400">
                    <p>No webhook events available from enabled servers</p>
                    <p class="text-sm mt-1">Enabled servers: ${enabledWebhookServers.join(', ')}</p>
                </div>`;
        }
        return;
    }

    container.innerHTML = availableWebhooksData.map(server => `
        <div class="mb-4">
            <div class="flex items-center justify-between mb-2">
                <div class="flex items-center gap-2">
                    <span class="font-medium text-slate-700 dark:text-slate-200">${server.display_name || server.server}</span>
                    <span class="text-xs text-slate-400">${server.server}</span>
                </div>
                <button onclick="subscribeToAllEvents('${server.server}')" class="px-2 py-1 bg-blue-100 hover:bg-blue-200 text-blue-600 rounded text-xs">
                    + Subscribe All
                </button>
            </div>
            <div class="space-y-1 pl-4">
                ${(server.events || []).map(event => {
                    const isSubscribed = isEventSubscribed(server.server, event.name);
                    return `
                        <div class="flex items-center justify-between p-2 bg-slate-50 dark:bg-slate-700 rounded text-sm">
                            <div>
                                <span class="font-medium text-slate-600 dark:text-slate-300">${event.name}</span>
                                ${event.description ? `<p class="text-xs text-slate-400">${event.description}</p>` : ''}
                            </div>
                            ${isSubscribed
                                ? '<span class="px-2 py-0.5 bg-emerald-100 text-emerald-600 rounded text-xs">Subscribed</span>'
                                : `<button onclick="subscribeToEvent('${server.server}', '${event.name}')" class="px-2 py-0.5 bg-blue-100 hover:bg-blue-200 text-blue-600 rounded text-xs">+ Subscribe</button>`
                            }
                        </div>
                    `;
                }).join('')}
            </div>
        </div>
    `).join('');
}

// Check if an event is subscribed
function isEventSubscribed(server, eventName) {
    const sub = subscriptionsData.find(s => s.server === server);
    return sub && sub.events && sub.events.includes(eventName);
}

// Subscribe to a single event
async function subscribeToEvent(server, eventName) {
    // Get existing subscription for this server
    const existing = subscriptionsData.find(s => s.server === server);
    const events = existing ? [...(existing.events || []), eventName] : [eventName];

    // Remove duplicates
    const uniqueEvents = [...new Set(events)];

    await doSubscribe(server, uniqueEvents);
}

// Subscribe to all events from a server
async function subscribeToAllEvents(server) {
    const serverData = availableWebhooksData.find(s => s.server === server);
    if (!serverData) return;

    const events = (serverData.events || []).map(e => e.name);

    // Check if this is a new subscription (not updating existing)
    const existing = subscriptionsData.find(s => s.server === server);
    if (!existing) {
        // Show modal for new subscription to set title
        showNewSubscriptionModal(server, events);
        return;
    }

    await doSubscribe(server, events);
}

// Show modal for new subscription
function showNewSubscriptionModal(server, events) {
    const serverData = availableWebhooksData.find(s => s.server === server);
    const displayName = serverData?.display_name || server;

    const modalHtml = `
        <div id="newSubscriptionModal" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div class="bg-white dark:bg-slate-800 rounded-lg p-6 max-w-lg w-full mx-4 shadow-xl">
                <h3 class="text-lg font-semibold mb-4 text-slate-800 dark:text-slate-100">Subscribe to ${displayName}</h3>

                <div class="space-y-4">
                    <div>
                        <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1">Title <span class="text-red-500">*</span></label>
                        <input type="text" id="newSubTitle" value="" required
                            placeholder="e.g., Payment notifications, PR alerts"
                            class="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-800 dark:text-slate-100">
                        <p class="text-xs text-slate-400 mt-1">A short description of what this subscription is for</p>
                    </div>

                    <div>
                        <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1">Prompt <span class="text-slate-400">(optional)</span></label>
                        <textarea id="newSubPrompt" rows="3"
                            placeholder="Instructions for handling events..."
                            class="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-800 dark:text-slate-100"></textarea>
                    </div>

                    <div class="text-xs text-slate-500 dark:text-slate-400">
                        <span class="font-medium">Events:</span> ${events.join(', ')}
                    </div>
                </div>

                <div class="flex justify-end gap-2 mt-6">
                    <button onclick="closeNewSubscriptionModal()" class="px-4 py-2 text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700 rounded-lg">
                        Cancel
                    </button>
                    <button onclick="confirmNewSubscription('${server}')" class="px-4 py-2 bg-blue-500 hover:bg-blue-600 text-white rounded-lg">
                        Subscribe
                    </button>
                </div>
            </div>
        </div>
    `;

    // Store events for later
    window._pendingSubscriptionEvents = events;

    document.body.insertAdjacentHTML('beforeend', modalHtml);
}

// Close new subscription modal
function closeNewSubscriptionModal() {
    const modal = document.getElementById('newSubscriptionModal');
    if (modal) modal.remove();
    window._pendingSubscriptionEvents = null;
}

// Confirm new subscription
async function confirmNewSubscription(server) {
    const title = document.getElementById('newSubTitle').value.trim();
    const prompt = document.getElementById('newSubPrompt').value.trim();
    const events = window._pendingSubscriptionEvents || [];

    // Title is required
    if (!title) {
        showToast('Title is required', 'error');
        document.getElementById('newSubTitle').focus();
        return;
    }

    closeNewSubscriptionModal();

    await doSubscribe(server, events, prompt || null, title);
}

// Perform subscription
async function doSubscribe(server, events, prompt = null, title = null) {
    const payload = { server, events };
    if (prompt) payload.prompt = prompt;
    if (title) payload.title = title;

    // Get credential from agent config (set in Config → MCP → Credentials)
    // Try to find matching credential for server (e.g., 'stripe' server uses 'stripe' credential)
    const credentials = typeof getAgentCredentials === 'function' ? getAgentCredentials() : [];
    let credentialId = null;

    // Try exact match first (server name matches provider)
    const matchingCred = credentials.find(c =>
        c.provider?.toLowerCase() === server.toLowerCase() ||
        server.toLowerCase().includes(c.provider?.toLowerCase())
    );

    if (matchingCred) {
        credentialId = matchingCred.credential_id;
    } else if (credentials.length > 0) {
        // Fall back to first available credential
        credentialId = credentials[0].credential_id;
    }

    if (!credentialId) {
        showToast('No credentials configured. Add credentials in Config → MCP → Credentials', 'error');
        return;
    }

    payload.credential_id = parseInt(credentialId);

    // Call the subscribe tool via chat or a dedicated endpoint
    const response = await makeRequest('/chat', 'POST', {
        message: JSON.stringify({
            tool: 'subscribe',
            input: payload
        }),
        stream: false
    });

    if (response.status === 200) {
        showToast(`Subscribed to ${events.length} events from ${server}`, 'success');
        await loadSubscriptionsData();
    } else {
        showToast('Failed to subscribe', 'error');
    }
}

// Unsubscribe by title
async function unsubscribeByTitle(title) {
    if (!confirm(`Unsubscribe from "${title}"?`)) return;

    const response = await makeRequest('/chat', 'POST', {
        message: JSON.stringify({
            tool: 'unsubscribe',
            input: { title }
        }),
        stream: false
    });

    if (response.status === 200) {
        showToast(`Unsubscribed: ${title}`, 'success');
        await loadSubscriptionsData();
    } else {
        showToast('Failed to unsubscribe', 'error');
    }
}

// Populate test server dropdown
function populateTestServerDropdown() {
    const select = document.getElementById('webhookTestServer');
    if (!select) return;

    // Combine subscribed servers and available servers
    const servers = new Set();
    subscriptionsData.forEach(s => servers.add(s.server));
    availableWebhooksData.forEach(s => servers.add(s.server));

    select.innerHTML = '<option value="">Select server...</option>' +
        Array.from(servers).map(s => `<option value="${s}">${s}</option>`).join('');
}

// Update events dropdown when server changes
function updateWebhookTestEvents() {
    const serverSelect = document.getElementById('webhookTestServer');
    const eventSelect = document.getElementById('webhookTestEvent');
    if (!serverSelect || !eventSelect) return;

    const server = serverSelect.value;
    if (!server) {
        eventSelect.innerHTML = '<option value="">Select event...</option>';
        return;
    }

    // Get events from available webhooks or subscriptions
    const serverData = availableWebhooksData.find(s => s.server === server);
    const subData = subscriptionsData.find(s => s.server === server);

    const events = new Set();
    if (serverData?.events) {
        serverData.events.forEach(e => events.add(e.name));
    }
    if (subData?.events) {
        subData.events.forEach(e => events.add(e));
    }

    eventSelect.innerHTML = '<option value="">Select event...</option>' +
        Array.from(events).map(e => `<option value="${e}">${e}</option>`).join('');
}

// Edit subscription - show modal to edit title and prompt
function editSubscription(title) {
    const sub = subscriptionsData.find(s => s.title === title);
    if (!sub) return;

    // Store current title for lookup
    window._editingSubscriptionTitle = title;

    // Create modal content
    const modalHtml = `
        <div id="editSubscriptionModal" class="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div class="bg-white dark:bg-slate-800 rounded-lg p-6 max-w-lg w-full mx-4 shadow-xl">
                <h3 class="text-lg font-semibold mb-4 text-slate-800 dark:text-slate-100">Edit Subscription</h3>

                <div class="space-y-4">
                    <div>
                        <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1">Title <span class="text-red-500">*</span></label>
                        <input type="text" id="editSubTitle" value="${escapeHtml(sub.title || '')}"
                            placeholder="e.g., Payment notifications, PR alerts"
                            class="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-800 dark:text-slate-100">
                    </div>

                    <div>
                        <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1">Prompt (instructions for handling events)</label>
                        <textarea id="editSubPrompt" rows="3"
                            placeholder="e.g., When a payment is received, send a thank you email..."
                            class="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-lg bg-white dark:bg-slate-700 text-slate-800 dark:text-slate-100">${escapeHtml(sub.prompt || '')}</textarea>
                    </div>

                    <div class="text-xs text-slate-500 dark:text-slate-400">
                        <p><span class="font-medium">Server:</span> ${sub.server}</p>
                        <p><span class="font-medium">Events:</span> ${(sub.events || []).join(', ')}</p>
                    </div>
                </div>

                <div class="flex justify-end gap-2 mt-6">
                    <button onclick="closeEditSubscriptionModal()" class="px-4 py-2 text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700 rounded-lg">
                        Cancel
                    </button>
                    <button onclick="saveSubscriptionEdit()" class="px-4 py-2 bg-blue-500 hover:bg-blue-600 text-white rounded-lg">
                        Save Changes
                    </button>
                </div>
            </div>
        </div>
    `;

    // Add modal to page
    document.body.insertAdjacentHTML('beforeend', modalHtml);
}

// Close edit modal
function closeEditSubscriptionModal() {
    const modal = document.getElementById('editSubscriptionModal');
    if (modal) modal.remove();
    window._editingSubscriptionTitle = null;
}

// Save subscription edit
async function saveSubscriptionEdit() {
    const currentTitle = window._editingSubscriptionTitle;
    const newTitle = document.getElementById('editSubTitle').value.trim();
    const prompt = document.getElementById('editSubPrompt').value.trim();

    if (!newTitle) {
        showToast('Title is required', 'error');
        document.getElementById('editSubTitle').focus();
        return;
    }

    // Use current title to identify, new_title if renaming
    const payload = { title: currentTitle };
    if (newTitle !== currentTitle) {
        payload.new_title = newTitle;
    }
    payload.prompt = prompt || null;

    try {
        const response = await makeRequest('/chat', 'POST', {
            message: JSON.stringify({
                tool: 'update_subscription',
                input: payload
            }),
            stream: false
        });

        if (response.status === 200) {
            showToast('Subscription updated', 'success');
            closeEditSubscriptionModal();
            await loadSubscriptionsData();
        } else {
            showToast('Failed to update subscription', 'error');
        }
    } catch (e) {
        console.error('Failed to update subscription:', e);
        showToast('Failed to update subscription', 'error');
    }
}

// Send test webhook
async function sendTestWebhook() {
    const server = document.getElementById('webhookTestServer').value;
    const event = document.getElementById('webhookTestEvent').value;
    const dataStr = document.getElementById('webhookTestData').value;
    const resultDiv = document.getElementById('webhookTestResult');

    if (!server || !event) {
        resultDiv.innerHTML = '<span class="text-red-500">Please select server and event</span>';
        return;
    }

    let data;
    try {
        data = JSON.parse(dataStr);
    } catch (e) {
        resultDiv.innerHTML = '<span class="text-red-500">Invalid JSON data</span>';
        return;
    }

    resultDiv.innerHTML = '<span class="text-blue-500">Sending...</span>';

    try {
        const response = await makeRequest('/chat', 'POST', {
            type: 'webhook',
            server: server,
            event: event,
            data: data
        });

        if (response.status === 200) {
            if (response.data?.success === false) {
                resultDiv.innerHTML = `<span class="text-amber-500">⚠️ ${response.data.message || 'Not subscribed'}</span>`;
            } else {
                resultDiv.innerHTML = '<span class="text-emerald-500">✅ Webhook sent successfully! Check threads for new conversation.</span>';
            }
        } else {
            resultDiv.innerHTML = `<span class="text-red-500">❌ Error: ${response.data?.error || 'Unknown error'}</span>`;
        }
    } catch (e) {
        resultDiv.innerHTML = `<span class="text-red-500">❌ Error: ${e.message}</span>`;
    }
}
