// Config Tab Functions

let currentConfig = null;
let providersData = null;
let embeddingProvidersData = null;

// Show/hide browser provider-specific settings
function updateBrowserProviderUI() {
    const provider = document.getElementById('operatorBrowserProvider').value;
    document.getElementById('providerBrowserEngineSettings').classList.toggle('hidden', provider !== 'browserengine');
    document.getElementById('providerBrowserbaseSettings').classList.toggle('hidden', provider !== 'browserbase');
    document.getElementById('providerSteelSettings').classList.toggle('hidden', provider !== 'steel');
    document.getElementById('providerCDPSettings').classList.toggle('hidden', provider !== 'cdp');
}

// Fetch and display operator/browser status
async function refreshOperatorStatus() {
    try {
        const resp = await fetch('/operator/status');
        const status = await resp.json();
        const dot = document.getElementById('operatorStatusDot');
        const text = document.getElementById('operatorStatusText');
        const sessionInfo = document.getElementById('operatorSessionInfo');

        if (!status.enabled) {
            dot.className = 'w-2 h-2 rounded-full bg-slate-400';
            text.textContent = 'Disabled';
            sessionInfo.classList.add('hidden');
        } else if (!status.provider_ready) {
            dot.className = 'w-2 h-2 rounded-full bg-yellow-400';
            text.textContent = `${status.browser_provider || 'unknown'} — not configured (missing credentials)`;
            sessionInfo.classList.add('hidden');
        } else {
            const session = status.active_session;
            if (session) {
                dot.className = 'w-2 h-2 rounded-full bg-green-400';
                text.textContent = `${status.provider_name} — active session`;
                let info = `Session: ${session.id}`;
                if (session.stream_url) info += ` | <a href="${session.stream_url}" target="_blank" class="underline">Stream</a>`;
                if (session.view_url) info += ` | <a href="${session.view_url}" target="_blank" class="underline">View</a>`;
                if (session.cdp_connected) info += ' | CDP connected';
                sessionInfo.innerHTML = info;
                sessionInfo.classList.remove('hidden');
            } else {
                dot.className = 'w-2 h-2 rounded-full bg-blue-400';
                text.textContent = `${status.provider_name} — ready (no active session)`;
                sessionInfo.classList.add('hidden');
            }
        }
    } catch (e) {
        document.getElementById('operatorStatusDot').className = 'w-2 h-2 rounded-full bg-red-400';
        document.getElementById('operatorStatusText').textContent = 'Error fetching status';
    }
}

// Show/hide realtime provider-specific settings
function updateRealtimeProviderUI() {
    const provider = document.getElementById('realtimeProvider').value;
    const isStandard = provider === 'standard';

    // Toggle native (OpenAI/Gemini) vs standard (STT+LLM+TTS) sections
    const nativeSettings = document.getElementById('realtimeNativeSettings');
    const standardSettings = document.getElementById('realtimeStandardSettings');
    const vadRow = document.getElementById('realtimeVadRow');

    if (nativeSettings) nativeSettings.classList.toggle('hidden', isStandard);
    if (standardSettings) standardSettings.classList.toggle('hidden', !isStandard);
    if (vadRow) vadRow.classList.toggle('hidden', isStandard);

    // Update hint text
    const hint = document.getElementById('realtimeProviderHint');
    if (hint) {
        if (isStandard) {
            hint.textContent = 'Uses your configured LLM + ElevenLabs for STT/TTS. Requires ELEVENLABS_API_KEY.';
        } else if (provider === 'gemini') {
            hint.textContent = 'Requires GEMINI_API_KEY environment variable.';
        } else {
            hint.textContent = 'Requires OPENAI_API_KEY environment variable.';
        }
    }

    // Handle custom voice ID visibility
    const ttsVoiceSelect = document.getElementById('realtimeTTSVoice');
    if (ttsVoiceSelect) {
        ttsVoiceSelect.onchange = () => {
            const customRow = document.getElementById('realtimeTTSCustomVoiceRow');
            if (customRow) customRow.classList.toggle('hidden', ttsVoiceSelect.value !== 'custom');
        };
    }

    // Update STT model options based on STT provider
    const sttProviderSelect = document.getElementById('realtimeSTTProvider');
    if (sttProviderSelect) {
        sttProviderSelect.onchange = () => {
            const sttModel = document.getElementById('realtimeSTTModel');
            if (!sttModel) return;
            if (sttProviderSelect.value === 'whisper') {
                sttModel.innerHTML = '<option value="whisper-1">Whisper-1</option>';
            } else {
                sttModel.innerHTML = '<option value="scribe_v2">Scribe v2 (recommended)</option><option value="scribe_v1">Scribe v1</option>';
            }
        };
    }
}

// Load available providers and models from API
async function loadProviders() {
    try {
        const response = await makeRequest('/providers');
        if (response.status === 200 && response.data) {
            // Handle both old format (array) and new format (object with llm/embedding)
            if (Array.isArray(response.data)) {
                providersData = response.data;
            } else {
                providersData = response.data.llm || [];
                embeddingProvidersData = response.data.embedding || [];
            }
            populateProviderSelect();
            populateEmbeddingProviderSelect();
            return;
        }
    } catch (e) {
        console.error('Failed to load providers:', e);
    }

    // Fallback if API fails
    const providerSelect = document.getElementById('configProvider');
    providerSelect.innerHTML = '<option value="">Failed to load providers</option>';
}

// Populate provider dropdown
function populateProviderSelect() {
    const providerSelect = document.getElementById('configProvider');
    providerSelect.innerHTML = '';

    if (!providersData || providersData.length === 0) {
        providerSelect.innerHTML = '<option value="">No providers available</option>';
        return;
    }

    providersData.forEach(provider => {
        const option = document.createElement('option');
        option.value = provider.id;
        option.textContent = provider.name;
        providerSelect.appendChild(option);
    });

    // Set current value if config is loaded
    if (currentConfig?.llm?.provider) {
        providerSelect.value = currentConfig.llm.provider;
    }

    // Update model options based on selected provider
    updateModelOptions();
}

// Update model dropdown based on selected provider
function updateModelOptions() {
    const providerSelect = document.getElementById('configProvider');
    const modelSelect = document.getElementById('configModel');
    const baseUrlContainer = document.getElementById('baseUrlContainer');
    const baseUrlInput = document.getElementById('configBaseUrl');
    const selectedProvider = providerSelect.value;

    modelSelect.innerHTML = '';

    if (!providersData || !selectedProvider) {
        modelSelect.innerHTML = '<option value="">Select provider first</option>';
        if (baseUrlContainer) baseUrlContainer.classList.add('hidden');
        return;
    }

    const provider = providersData.find(p => p.id === selectedProvider);
    if (!provider || !provider.models || provider.models.length === 0) {
        modelSelect.innerHTML = '<option value="">No models available</option>';
        if (baseUrlContainer) baseUrlContainer.classList.add('hidden');
        return;
    }

    // Show/hide base URL field based on provider
    if (baseUrlContainer) {
        if (provider.custom_url) {
            baseUrlContainer.classList.remove('hidden');
            if (baseUrlInput) {
                baseUrlInput.placeholder = provider.default_url || 'http://localhost:11434';
                // Auto-fetch models when base URL changes
                baseUrlInput.onchange = () => fetchOllamaModels();
            }
        } else {
            baseUrlContainer.classList.add('hidden');
        }
    }

    // For Ollama, auto-fetch available models from the instance
    if (provider.custom_url && selectedProvider === 'ollama') {
        modelSelect.innerHTML = '<option value="">Fetching models from Ollama...</option>';
        fetchOllamaModels();
        return;
    }

    populateModelDropdown(provider.models, provider.custom_url);
}

// Store current models list for info panel lookups
let currentModelsList = [];

// Populate model dropdown with given models
function populateModelDropdown(models, allowCustom) {
    const modelSelect = document.getElementById('configModel');
    modelSelect.innerHTML = '';
    currentModelsList = models || [];

    models.forEach(model => {
        const option = document.createElement('option');
        option.value = model.value;
        // Build display text: label + caps/tags/context inline
        let text = model.label;
        if (model.recommended) text += ' ⭐';
        const capIcons = { vision: '👁', reasoning: '🧠', tools: '🔧', code: '💻', web: '🌐' };
        const caps = (model.capabilities || []).map(c => capIcons[c] || c).join('');
        if (caps) text += '  ' + caps;
        const tags = (model.tags || []).filter(t => t.includes('uncensored')).map(() => '🔓');
        if (tags.length) text += ' ' + tags.join('');
        if (model.context_size) {
            const ctx = model.context_size;
            if (ctx >= 1000000) text += ` [${Math.round(ctx/1000000)}M]`;
            else if (ctx >= 1000) text += ` [${Math.round(ctx/1000)}K]`;
        }
        option.textContent = text;
        modelSelect.appendChild(option);
    });

    // Set current model value if it matches
    if (currentConfig?.llm?.model) {
        const modelExists = models.some(m => m.value === currentConfig.llm.model);
        if (modelExists) {
            modelSelect.value = currentConfig.llm.model;
        }
    }

    showModelInfo();
}

// Show model info panel with capabilities, tags, description
function showModelInfo() {
    const panel = document.getElementById('modelInfoPanel');
    const badges = document.getElementById('modelInfoBadges');
    const desc = document.getElementById('modelInfoDesc');
    if (!panel || !badges || !desc) return;

    const selectedValue = document.getElementById('configModel')?.value;
    const model = currentModelsList.find(m => m.value === selectedValue);

    if (!model || (!model.capabilities?.length && !model.tags?.length && !model.description && !model.context_size)) {
        panel.classList.add('hidden');
        return;
    }

    panel.classList.remove('hidden');
    badges.innerHTML = '';

    // Capability badges
    const capColors = {
        vision: 'bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300',
        reasoning: 'bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300',
        tools: 'bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300',
        code: 'bg-amber-100 text-amber-700 dark:bg-amber-900 dark:text-amber-300',
        web: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900 dark:text-cyan-300',
    };
    const capIcons = { vision: '👁', reasoning: '🧠', tools: '🔧', code: '💻', web: '🌐' };

    (model.capabilities || []).forEach(cap => {
        const span = document.createElement('span');
        span.className = `inline-flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[10px] font-medium ${capColors[cap] || 'bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-300'}`;
        span.textContent = `${capIcons[cap] || '•'} ${cap}`;
        badges.appendChild(span);
    });

    // Tag badges
    (model.tags || []).forEach(tag => {
        const span = document.createElement('span');
        const isUncensored = tag.includes('uncensored');
        const isDefault = tag.includes('default');
        const isFastest = tag.includes('fastest');
        let color = 'bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-300';
        if (isUncensored) color = 'bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300';
        if (isDefault) color = 'bg-indigo-100 text-indigo-700 dark:bg-indigo-900 dark:text-indigo-300';
        if (isFastest) color = 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300';
        span.className = `inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium ${color}`;
        span.textContent = tag;
        badges.appendChild(span);
    });

    // Context size badge
    if (model.context_size) {
        const span = document.createElement('span');
        span.className = 'inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-300';
        const ctx = model.context_size;
        if (ctx >= 1000000) span.textContent = `📏 ${Math.round(ctx/1000000)}M ctx`;
        else if (ctx >= 1000) span.textContent = `📏 ${Math.round(ctx/1000)}K ctx`;
        else span.textContent = `📏 ${ctx} ctx`;
        badges.appendChild(span);
    }

    // Description
    if (model.description) {
        desc.textContent = model.description;
        desc.classList.remove('hidden');
    } else {
        desc.textContent = '';
        desc.classList.add('hidden');
    }
}

// Fetch available models from a running Ollama instance
async function fetchOllamaModels() {
    const modelSelect = document.getElementById('configModel');
    const baseUrlInput = document.getElementById('configBaseUrl');
    const ollamaUrl = baseUrlInput?.value || '';

    try {
        const queryParam = ollamaUrl ? `?url=${encodeURIComponent(ollamaUrl)}` : '';
        const response = await makeRequest(`/providers/ollama/models${queryParam}`);

        if (response.status === 200 && response.data?.models?.length > 0) {
            const models = response.data.models;
            modelSelect.innerHTML = '';

            models.forEach(model => {
                const option = document.createElement('option');
                option.value = model.value;
                option.textContent = model.label;
                modelSelect.appendChild(option);
            });

            // Set current model if it exists in the list
            if (currentConfig?.llm?.model) {
                const exists = models.some(m => m.value === currentConfig.llm.model);
                if (exists) {
                    modelSelect.value = currentConfig.llm.model;
                }
            }

            showToast(`Found ${models.length} Ollama models`, 'success');
        } else {
            // Ollama not reachable — fall back to static list
            const provider = providersData?.find(p => p.id === 'ollama');
            if (provider) {
                populateModelDropdown(provider.models, true);
            }
            const errorMsg = response.data?.error || 'Ollama not reachable';
            modelSelect.insertBefore(
                Object.assign(document.createElement('option'), { value: '', textContent: `⚠ ${errorMsg} — showing defaults` }),
                modelSelect.firstChild
            );
        }
    } catch (e) {
        console.error('Failed to fetch Ollama models:', e);
        // Fall back to static list
        const provider = providersData?.find(p => p.id === 'ollama');
        if (provider) {
            populateModelDropdown(provider.models, true);
        }
    }
}

// Populate embedding provider dropdown from API
function populateEmbeddingProviderSelect() {
    const providerSelect = document.getElementById('memoryEmbeddingProvider');
    if (!providerSelect) return;

    providerSelect.innerHTML = '<option value="">-- Select Provider --</option>';

    if (!embeddingProvidersData || embeddingProvidersData.length === 0) {
        return;
    }

    embeddingProvidersData.forEach(provider => {
        const option = document.createElement('option');
        option.value = provider.id;
        // Show API key status
        let label = provider.name;
        if (provider.has_api_key) {
            label += ' ✓';
        } else if (provider.env_var) {
            label += ` (needs ${provider.env_var})`;
        }
        option.textContent = label;
        providerSelect.appendChild(option);
    });

    // Set current value if config is loaded
    if (currentConfig?.memory?.embedding_provider) {
        providerSelect.value = currentConfig.memory.embedding_provider;
        updateEmbeddingModelOptions();
    }
}

// Update embedding model dropdown based on selected embedding provider
function updateEmbeddingModelOptions() {
    const providerSelect = document.getElementById('memoryEmbeddingProvider');
    const modelSelect = document.getElementById('memoryEmbeddingModel');
    const selectedProvider = providerSelect?.value;

    if (!modelSelect) return;

    modelSelect.innerHTML = '';

    if (!selectedProvider) {
        modelSelect.innerHTML = '<option value="">Select provider first</option>';
        return;
    }

    const provider = embeddingProvidersData?.find(p => p.id === selectedProvider);
    if (!provider) {
        modelSelect.innerHTML = '<option value="">No models available</option>';
        return;
    }

    // Add default model option (recommended)
    const defaultOption = document.createElement('option');
    defaultOption.value = provider.default_model;
    defaultOption.textContent = `${provider.default_model} (default, ${provider.dimensions}d)`;
    modelSelect.appendChild(defaultOption);

    // Add empty option to allow custom/override
    const customOption = document.createElement('option');
    customOption.value = '';
    customOption.textContent = '-- Use default --';
    modelSelect.insertBefore(customOption, modelSelect.firstChild);

    // Set current model value
    if (currentConfig?.memory?.embedding_model) {
        modelSelect.value = currentConfig.memory.embedding_model;
    } else {
        // Select the default model
        modelSelect.value = provider.default_model;
    }
}

async function loadConfig() {
    const response = await makeRequest('/config');

    if (response.status === 200 && response.data) {
        currentConfig = response.data;

        // Update basic form fields
        document.getElementById('configAgentId').value = currentConfig.id || '';
        document.getElementById('configAgentName').value = currentConfig.name || '';
        document.getElementById('configAgentDesc').value = currentConfig.description || '';
        document.getElementById('configPublicUrl').value = currentConfig.public_url || '';
        document.getElementById('configSystemPrompt').value = currentConfig.llm?.system_prompt || '';

        // Set provider and model if providers are loaded
        if (providersData) {
            document.getElementById('configProvider').value = currentConfig.llm?.provider || 'anthropic';
            updateModelOptions();
        }

        // Set base URL if configured
        document.getElementById('configBaseUrl').value = currentConfig.llm?.base_url || '';

        // Update feature toggles
        document.getElementById('toggleMemory').checked = currentConfig.memory?.enabled || false;
        document.getElementById('toggleTasks').checked = currentConfig.tasks?.enabled || false;
        document.getElementById('toggleFiles').checked = currentConfig.filesystem?.enabled || false;
        document.getElementById('toggleMcp').checked = currentConfig.mcp?.enabled || false;
        document.getElementById('toggleScheduler').checked = currentConfig.scheduler?.enabled || false;
        document.getElementById('toggleOperator').checked = currentConfig.operator?.enabled || false;
        document.getElementById('toggleAgents').checked = currentConfig.agents?.enabled || false;
        document.getElementById('toggleReflection').checked = currentConfig.reflection?.enabled || false;

        // Update version display
        document.getElementById('agentVersion').textContent = currentConfig.version || '1.0.0';

        // Load Memory settings
        const mem = currentConfig.memory || {};
        document.getElementById('memoryDecisionModel').value = mem.decision_model || '';
        // Set embedding provider first, then update model options
        if (embeddingProvidersData) {
            document.getElementById('memoryEmbeddingProvider').value = mem.embedding_provider || '';
            updateEmbeddingModelOptions();
            // Now set the model value if specified
            if (mem.embedding_model) {
                document.getElementById('memoryEmbeddingModel').value = mem.embedding_model;
            }
        } else {
            document.getElementById('memoryEmbeddingProvider').value = mem.embedding_provider || '';
        }
        // Differential memory settings
        document.getElementById('memoryDecisionMode').value = mem.decision_mode || 'differential';
        document.getElementById('memoryNoveltyThreshold').value = mem.novelty_threshold != null ? mem.novelty_threshold : '0.80';
        document.getElementById('memoryMinSentenceWords').value = mem.min_sentence_words || '4';
        document.getElementById('memoryMaxPerQuery').value = mem.max_memories_per_query || '';
        document.getElementById('memoryMinImportance').value = mem.min_importance != null ? mem.min_importance : '';
        document.getElementById('memoryMinSimilarity').value = mem.min_similarity != null ? mem.min_similarity : '';
        document.getElementById('memoryMaxMemories').value = mem.max_memories || '';
        document.getElementById('memoryChunkSize').value = mem.chunk_size || '';
        document.getElementById('memoryChunkOverlap').value = mem.chunk_overlap || '';
        document.getElementById('memoryDocImportance').value = mem.document_importance != null ? mem.document_importance : '';
        document.getElementById('memoryAutoPrune').checked = mem.auto_prune || false;
        document.getElementById('memoryAutoIngest').checked = mem.auto_ingest_files || false;
        // auto_extract_memories defaults to true when null/undefined
        document.getElementById('memoryAutoExtract').checked = mem.auto_extract_memories !== false;
        // skip_questions defaults to true
        document.getElementById('memorySkipQuestions').checked = mem.skip_questions !== false;
        document.getElementById('memoryOllamaUrl').value = mem.ollama_url || '';
        document.getElementById('memoryIngestTypes').value = (mem.ingest_types || []).join(', ');

        // Load Filesystem settings
        const fs = currentConfig.filesystem || {};
        document.getElementById('fsStoragePath').value = fs.storage_path || '';
        document.getElementById('fsMaxFileSize').value = fs.max_file_size ? Math.round(fs.max_file_size / 1024 / 1024) : '';
        document.getElementById('fsRetentionDays').value = fs.retention_days || '';
        document.getElementById('fsMaxStorage').value = fs.max_storage ? Math.round(fs.max_storage / 1024 / 1024) : '';
        document.getElementById('fsPublicUrl').value = fs.public_url_prefix || '';
        document.getElementById('fsCleanupOrphans').checked = fs.cleanup_orphans || false;
        document.getElementById('fsAllowedTypes').value = (fs.allowed_types || []).join(', ');

        // Load Tasks settings
        const tasks = currentConfig.tasks || {};
        document.getElementById('tasksMaxConcurrent').value = tasks.max_concurrent || '';
        document.getElementById('tasksTimeout').value = tasks.default_timeout || '';
        document.getElementById('tasksMaxRetries').value = tasks.max_retries || '';
        document.getElementById('tasksRetryDelay').value = tasks.retry_delay || '';
        document.getElementById('tasksCleanupAge').value = tasks.cleanup_age || '';
        document.getElementById('tasksPersist').checked = tasks.persist || false;

        // Load Scheduler settings
        const sched = currentConfig.scheduler || {};
        document.getElementById('schedulerTimezone').value = sched.timezone || '';
        document.getElementById('schedulerMaxConcurrent').value = sched.max_concurrent || '';
        document.getElementById('schedulerCatchUp').checked = sched.catch_up_missed || false;

        // Load Reflection settings
        const refl = currentConfig.reflection || {};
        document.getElementById('reflectionInterval').value = refl.interval || '24h';
        document.getElementById('reflectionLookbackHours').value = refl.lookback_hours || '24';
        document.getElementById('reflectionConversationMin').value = refl.conversation_min || '0';
        document.getElementById('reflectionAfterTask').checked = refl.after_task || false;
        document.getElementById('reflectionPrompt').value = refl.prompt || '';

        // Load MCP settings
        const mcp = currentConfig.mcp || {};
        document.getElementById('mcpTimeout').value = mcp.timeout || '';
        document.getElementById('mcpAutoReconnect').checked = mcp.auto_reconnect || false;

        // Load external MCP servers
        loadExternalServers();

        // Load MCP Resources settings
        const resourceConfig = mcp.resource_config || {};
        document.getElementById('mcpResourcesEnabled').checked = resourceConfig.enabled || false;
        document.getElementById('mcpResourcesAutoSync').checked = resourceConfig.auto_sync || false;
        document.getElementById('mcpResourcesSyncInterval').value = resourceConfig.sync_interval || '';
        document.getElementById('mcpResourcesMaxSize').value = resourceConfig.max_size ? Math.round(resourceConfig.max_size / 1024) : '';
        document.getElementById('mcpResourcesSubscribe').checked = resourceConfig.subscribe || false;
        document.getElementById('mcpResourcesFilters').value = (resourceConfig.filters || []).join('\n');

        // Load enabled resource servers
        loadEnabledResources();

        // Load MCP resources status
        loadMCPResourcesStatus();

        // Load enabled webhook servers
        loadEnabledWebhooks();

        // Load Operator settings
        const op = currentConfig.operator || {};
        document.getElementById('operatorBrowserProvider').value = op.browser_provider || 'browserengine';
        document.getElementById('operatorDisplayWidth').value = op.display_width || '';
        document.getElementById('operatorDisplayHeight').value = op.display_height || '';
        document.getElementById('operatorAllowedDomains').value = (op.allowed_domains || []).join(', ');
        document.getElementById('operatorBlockedDomains').value = (op.blocked_domains || []).join(', ');
        // Load provider-specific settings
        const be = op.browserengine || {};
        document.getElementById('operatorBrowserEngineApiKey').value = be.api_key || '';
        document.getElementById('operatorBrowserEngineBaseUrl').value = be.base_url || '';
        const bb = op.browserbase || {};
        document.getElementById('operatorBrowserbaseApiKey').value = bb.api_key || '';
        document.getElementById('operatorBrowserbaseProjectId').value = bb.project_id || '';
        const steel = op.steel || {};
        document.getElementById('operatorSteelApiKey').value = steel.api_key || '';
        document.getElementById('operatorSteelBaseUrl').value = steel.base_url || '';
        document.getElementById('operatorCDPUrl').value = (op.cdp || {}).url || '';
        updateBrowserProviderUI();
        refreshOperatorStatus();

        // Load Agents settings
        const agents = currentConfig.agents || {};
        document.getElementById('agentsMode').value = agents.mode || 'peer';
        document.getElementById('agentsGroup').value = agents.group || '';
        document.getElementById('agentsDiscoveryMethod').value = agents.discovery_method || 'file';
        document.getElementById('agentsFileRegistryPath').value = agents.file_registry_path || '/tmp/apteva-agents';
        updateAgentsModeStatus(agents.mode || 'peer');
        updateDiscoveryMethodUI();

        // Load Telemetry settings
        document.getElementById('toggleTelemetry').checked = currentConfig.telemetry?.enabled || false;
        const tel = currentConfig.telemetry || {};
        document.getElementById('telemetryEndpoint').value = tel.endpoint || '';
        document.getElementById('telemetryApiKey').value = tel.api_key || '';
        document.getElementById('telemetryBatchSize').value = tel.batch_size || 10;
        document.getElementById('telemetryFlushInterval').value = tel.flush_interval || 30;
        document.getElementById('telemetryCategories').value = (tel.categories || []).join(', ');

        // Load Realtime settings
        document.getElementById('toggleRealtime').checked = currentConfig.realtime?.enabled || false;
        const rt = currentConfig.realtime || {};
        document.getElementById('realtimeProvider').value = rt.provider || 'openai';
        document.getElementById('realtimeModel').value = rt.model || 'gpt-realtime';
        document.getElementById('realtimeGeminiModel').value = rt.gemini_model || 'gemini-2.5-flash-native-audio-preview-12-2025';
        document.getElementById('realtimeVoice').value = rt.voice || 'alloy';
        document.getElementById('realtimeGeminiVoice').value = rt.gemini_voice || 'Kore';
        document.getElementById('realtimeVadType').value = rt.vad_type || 'semantic_vad';
        document.getElementById('realtimeGoogleSearch').checked = rt.google_search || false;

        // Load Standard voice STT/TTS settings
        const stt = rt.stt || {};
        const tts = rt.tts || {};
        document.getElementById('realtimeSTTProvider').value = stt.provider || 'elevenlabs';
        document.getElementById('realtimeSTTModel').value = stt.model || 'scribe_v2';
        document.getElementById('realtimeSTTLanguage').value = stt.language || '';
        document.getElementById('realtimeTTSProvider').value = tts.provider || 'elevenlabs';
        document.getElementById('realtimeTTSModel').value = tts.model || 'eleven_turbo_v2_5';

        // Load TTS voice - check if it matches a preset or is custom
        const ttsVoice = tts.voice || '21m00Tcm4TlvDq8ikWAM';
        const ttsVoiceSelect = document.getElementById('realtimeTTSVoice');
        const presetMatch = Array.from(ttsVoiceSelect.options).find(o => o.value === ttsVoice);
        if (presetMatch) {
            ttsVoiceSelect.value = ttsVoice;
        } else {
            ttsVoiceSelect.value = 'custom';
            document.getElementById('realtimeTTSCustomVoice').value = ttsVoice;
            document.getElementById('realtimeTTSCustomVoiceRow').classList.remove('hidden');
        }

        // Show/hide provider-specific sections
        updateRealtimeProviderUI();

        // Load Context & Summarization settings
        const ctx = currentConfig.context || {};
        const compaction = ctx.compaction || {};
        document.getElementById('contextMaxMessages').value = ctx.max_messages || '';
        document.getElementById('contextMaxTokens').value = ctx.max_tokens || '';
        document.getElementById('contextCompactionModel').value = compaction.model || '';
        document.getElementById('contextSummaryModel').value = ctx.summary_model || '';
        document.getElementById('contextCompactionEnabled').checked = compaction.enabled || false;
        document.getElementById('contextKeepRecent').value = compaction.keep_recent || '';

        // Show raw config
        document.getElementById('configRaw').textContent = JSON.stringify(currentConfig, null, 2);

        // Load tools configuration
        loadToolsConfig();

        // Load tool loading configuration
        loadToolLoadingConfig();
    }
}

async function saveConfig() {
    const model = document.getElementById('configModel').value;

    const updates = {
        id: document.getElementById('configAgentId').value,
        name: document.getElementById('configAgentName').value,
        description: document.getElementById('configAgentDesc').value,
        public_url: document.getElementById('configPublicUrl').value || undefined,
        llm: {
            ...currentConfig?.llm,
            provider: document.getElementById('configProvider').value,
            model: model,
            base_url: document.getElementById('configBaseUrl').value || undefined,
            system_prompt: document.getElementById('configSystemPrompt').value
        }
    };

    const response = await makeRequest('/config', 'POST', updates);

    if (response.status === 200) {
        showToast('Configuration saved', 'success');
        loadConfig();
    } else {
        showToast('Failed to save configuration', 'error');
    }
}

async function toggleFeature(feature) {
    const toggleMap = {
        memory: 'toggleMemory',
        tasks: 'toggleTasks',
        filesystem: 'toggleFiles',
        mcp: 'toggleMcp',
        scheduler: 'toggleScheduler',
        operator: 'toggleOperator',
        agents: 'toggleAgents',
        telemetry: 'toggleTelemetry',
        realtime: 'toggleRealtime',
        reflection: 'toggleReflection'
    };

    const enabled = document.getElementById(toggleMap[feature]).checked;

    const updates = {
        [feature]: {
            ...currentConfig?.[feature],
            enabled
        }
    };

    const response = await makeRequest('/config', 'POST', updates);

    if (response.status === 200) {
        showToast(`${feature} ${enabled ? 'enabled' : 'disabled'}`, 'success');
        loadConfig();
    } else {
        showToast(`Failed to toggle ${feature}`, 'error');
        // Revert toggle
        document.getElementById(toggleMap[feature]).checked = !enabled;
    }
}

function toggleConfigSection(section) {
    const settingsEl = document.getElementById(`${section}Settings`);
    const chevronEl = document.getElementById(`${section}Chevron`);

    if (settingsEl) {
        settingsEl.classList.toggle('hidden');
    }
    if (chevronEl) {
        chevronEl.style.transform = settingsEl?.classList.contains('hidden') ? '' : 'rotate(180deg)';
    }
}

async function saveFeatureConfig(feature) {
    let updates = {};

    switch (feature) {
        case 'memory':
            // Parse ingest types from comma-separated list
            const ingestTypes = document.getElementById('memoryIngestTypes').value
                .split(',')
                .map(t => t.trim())
                .filter(t => t);
            updates = {
                memory: {
                    ...currentConfig?.memory,
                    enabled: document.getElementById('toggleMemory').checked,
                    embedding_model: document.getElementById('memoryEmbeddingModel').value || undefined,
                    decision_model: document.getElementById('memoryDecisionModel').value || undefined,
                    embedding_provider: document.getElementById('memoryEmbeddingProvider').value || undefined,
                    // Differential memory settings
                    decision_mode: document.getElementById('memoryDecisionMode').value || 'differential',
                    novelty_threshold: document.getElementById('memoryNoveltyThreshold').value ? parseFloat(document.getElementById('memoryNoveltyThreshold').value) : undefined,
                    min_sentence_words: parseInt(document.getElementById('memoryMinSentenceWords').value) || undefined,
                    skip_questions: document.getElementById('memorySkipQuestions').checked,
                    max_memories_per_query: parseInt(document.getElementById('memoryMaxPerQuery').value) || undefined,
                    min_importance: document.getElementById('memoryMinImportance').value ? parseFloat(document.getElementById('memoryMinImportance').value) : undefined,
                    min_similarity: document.getElementById('memoryMinSimilarity').value ? parseFloat(document.getElementById('memoryMinSimilarity').value) : undefined,
                    max_memories: parseInt(document.getElementById('memoryMaxMemories').value) || undefined,
                    chunk_size: parseInt(document.getElementById('memoryChunkSize').value) || undefined,
                    chunk_overlap: parseInt(document.getElementById('memoryChunkOverlap').value) || undefined,
                    document_importance: document.getElementById('memoryDocImportance').value ? parseFloat(document.getElementById('memoryDocImportance').value) : undefined,
                    auto_prune: document.getElementById('memoryAutoPrune').checked,
                    auto_ingest_files: document.getElementById('memoryAutoIngest').checked,
                    auto_extract_memories: document.getElementById('memoryAutoExtract').checked,
                    ollama_url: document.getElementById('memoryOllamaUrl').value || undefined,
                    ingest_types: ingestTypes.length > 0 ? ingestTypes : undefined
                }
            };
            break;

        case 'filesystem':
            const allowedTypes = document.getElementById('fsAllowedTypes').value
                .split(',')
                .map(t => t.trim())
                .filter(t => t);
            updates = {
                filesystem: {
                    ...currentConfig?.filesystem,
                    enabled: document.getElementById('toggleFiles').checked,
                    storage_path: document.getElementById('fsStoragePath').value || undefined,
                    max_file_size: (parseInt(document.getElementById('fsMaxFileSize').value) || 0) * 1024 * 1024 || undefined,
                    retention_days: parseInt(document.getElementById('fsRetentionDays').value) || undefined,
                    max_storage: (parseInt(document.getElementById('fsMaxStorage').value) || 0) * 1024 * 1024 || undefined,
                    public_url_prefix: document.getElementById('fsPublicUrl').value || undefined,
                    cleanup_orphans: document.getElementById('fsCleanupOrphans').checked,
                    allowed_types: allowedTypes.length > 0 ? allowedTypes : undefined
                }
            };
            break;

        case 'tasks':
            updates = {
                tasks: {
                    ...currentConfig?.tasks,
                    enabled: document.getElementById('toggleTasks').checked,
                    max_concurrent: parseInt(document.getElementById('tasksMaxConcurrent').value) || undefined,
                    default_timeout: parseInt(document.getElementById('tasksTimeout').value) || undefined,
                    max_retries: parseInt(document.getElementById('tasksMaxRetries').value) || undefined,
                    retry_delay: parseInt(document.getElementById('tasksRetryDelay').value) || undefined,
                    cleanup_age: parseInt(document.getElementById('tasksCleanupAge').value) || undefined,
                    persist: document.getElementById('tasksPersist').checked
                }
            };
            break;

        case 'scheduler':
            updates = {
                scheduler: {
                    ...currentConfig?.scheduler,
                    enabled: document.getElementById('toggleScheduler').checked,
                    timezone: document.getElementById('schedulerTimezone').value || undefined,
                    max_concurrent: parseInt(document.getElementById('schedulerMaxConcurrent').value) || undefined,
                    catch_up_missed: document.getElementById('schedulerCatchUp').checked
                }
            };
            break;

        case 'mcp':
            // Parse resource filters
            const resourceFilters = document.getElementById('mcpResourcesFilters').value
                .split('\n')
                .map(f => f.trim())
                .filter(f => f);
            // Get selected credentials from mcp.js (returns map keyed by provider)
            const credentials = typeof getAgentCredentials === 'function' ? getAgentCredentials() : {};
            updates = {
                mcp: {
                    ...currentConfig?.mcp,
                    enabled: document.getElementById('toggleMcp').checked,
                    timeout: parseInt(document.getElementById('mcpTimeout').value) || undefined,
                    auto_reconnect: document.getElementById('mcpAutoReconnect').checked,
                    // webhooks is an array of server names (managed via add/remove functions)
                    // resources is an array of server names (managed separately via add/remove)
                    // external servers managed via add/edit/delete UI (stored in externalServers array)
                    servers: externalServers.length > 0 ? externalServers : undefined,
                    // credentials for MCP tools (selected from backend)
                    credentials: Object.keys(credentials).length > 0 ? credentials : undefined,
                    // resource_config is the sync settings
                    resource_config: {
                        enabled: document.getElementById('mcpResourcesEnabled').checked,
                        auto_sync: document.getElementById('mcpResourcesAutoSync').checked,
                        sync_interval: document.getElementById('mcpResourcesSyncInterval').value || undefined,
                        max_size: (parseInt(document.getElementById('mcpResourcesMaxSize').value) || 0) * 1024 || undefined,
                        subscribe: document.getElementById('mcpResourcesSubscribe').checked,
                        filters: resourceFilters.length > 0 ? resourceFilters : undefined
                    }
                }
            };
            break;

        case 'operator':
            const allowedDomains = document.getElementById('operatorAllowedDomains').value
                .split(',')
                .map(d => d.trim())
                .filter(d => d);
            const blockedDomains = document.getElementById('operatorBlockedDomains').value
                .split(',')
                .map(d => d.trim())
                .filter(d => d);
            const browserProvider = document.getElementById('operatorBrowserProvider').value;
            const operatorUpdate = {
                enabled: document.getElementById('toggleOperator').checked,
                browser_provider: browserProvider,
                display_width: parseInt(document.getElementById('operatorDisplayWidth').value) || undefined,
                display_height: parseInt(document.getElementById('operatorDisplayHeight').value) || undefined,
                allowed_domains: allowedDomains.length > 0 ? allowedDomains : undefined,
                blocked_domains: blockedDomains.length > 0 ? blockedDomains : undefined
            };
            // Only include the active provider's config
            if (browserProvider === 'browserengine') {
                const apiKey = document.getElementById('operatorBrowserEngineApiKey').value;
                const baseUrl = document.getElementById('operatorBrowserEngineBaseUrl').value;
                if (apiKey) operatorUpdate.browserengine = { api_key: apiKey, base_url: baseUrl || undefined };
            } else if (browserProvider === 'browserbase') {
                const apiKey = document.getElementById('operatorBrowserbaseApiKey').value;
                const projectId = document.getElementById('operatorBrowserbaseProjectId').value;
                if (apiKey) operatorUpdate.browserbase = { api_key: apiKey, project_id: projectId || undefined };
            } else if (browserProvider === 'steel') {
                const apiKey = document.getElementById('operatorSteelApiKey').value;
                const baseUrl = document.getElementById('operatorSteelBaseUrl').value;
                if (apiKey) operatorUpdate.steel = { api_key: apiKey, base_url: baseUrl || undefined };
            } else if (browserProvider === 'cdp') {
                const cdpUrl = document.getElementById('operatorCDPUrl').value;
                if (cdpUrl) operatorUpdate.cdp = { url: cdpUrl };
            }
            updates = { operator: operatorUpdate };
            break;

        case 'agents':
            const agentsMode = document.getElementById('agentsMode').value;
            const discoveryMethod = document.getElementById('agentsDiscoveryMethod').value;
            updates = {
                agents: {
                    ...currentConfig?.agents,
                    enabled: document.getElementById('toggleAgents').checked,
                    mode: agentsMode,
                    group: document.getElementById('agentsGroup').value || undefined,
                    discovery_method: discoveryMethod || 'file',
                    file_registry_path: discoveryMethod === 'file' ? (document.getElementById('agentsFileRegistryPath').value || undefined) : undefined
                }
            };
            updateAgentsModeStatus(agentsMode);
            break;

        case 'telemetry':
            const telCategories = document.getElementById('telemetryCategories').value
                .split(',')
                .map(c => c.trim())
                .filter(c => c);
            updates = {
                telemetry: {
                    ...currentConfig?.telemetry,
                    enabled: document.getElementById('toggleTelemetry').checked,
                    endpoint: document.getElementById('telemetryEndpoint').value || undefined,
                    api_key: document.getElementById('telemetryApiKey').value || undefined,
                    batch_size: parseInt(document.getElementById('telemetryBatchSize').value) || undefined,
                    flush_interval: parseInt(document.getElementById('telemetryFlushInterval').value) || undefined,
                    categories: telCategories.length > 0 ? telCategories : undefined
                }
            };
            break;

        case 'realtime':
            const realtimeProvider = document.getElementById('realtimeProvider').value || 'openai';
            const realtimeUpdate = {
                ...currentConfig?.realtime,
                enabled: document.getElementById('toggleRealtime').checked,
                provider: realtimeProvider,
                model: document.getElementById('realtimeModel').value || 'gpt-realtime',
                gemini_model: document.getElementById('realtimeGeminiModel').value || undefined,
                voice: document.getElementById('realtimeVoice').value || 'alloy',
                gemini_voice: document.getElementById('realtimeGeminiVoice').value || 'Kore',
                vad_type: document.getElementById('realtimeVadType').value || 'semantic_vad',
                google_search: document.getElementById('realtimeGoogleSearch').checked
            };

            // Include STT/TTS config when standard provider is selected
            if (realtimeProvider === 'standard') {
                let ttsVoice = document.getElementById('realtimeTTSVoice').value;
                if (ttsVoice === 'custom') {
                    ttsVoice = document.getElementById('realtimeTTSCustomVoice').value || '21m00Tcm4TlvDq8ikWAM';
                }
                realtimeUpdate.stt = {
                    provider: document.getElementById('realtimeSTTProvider').value || 'elevenlabs',
                    model: document.getElementById('realtimeSTTModel').value || 'scribe_v2',
                    language: document.getElementById('realtimeSTTLanguage').value || undefined
                };
                realtimeUpdate.tts = {
                    provider: document.getElementById('realtimeTTSProvider').value || 'elevenlabs',
                    voice: ttsVoice,
                    model: document.getElementById('realtimeTTSModel').value || 'eleven_turbo_v2_5'
                };
            }

            updates = { realtime: realtimeUpdate };
            break;

        case 'reflection':
            updates = {
                reflection: {
                    ...currentConfig?.reflection,
                    enabled: document.getElementById('toggleReflection').checked,
                    interval: document.getElementById('reflectionInterval').value || '24h',
                    lookback_hours: parseInt(document.getElementById('reflectionLookbackHours').value) || 24,
                    conversation_min: parseInt(document.getElementById('reflectionConversationMin').value) || 0,
                    after_task: document.getElementById('reflectionAfterTask').checked,
                    prompt: document.getElementById('reflectionPrompt').value || undefined
                }
            };
            break;

        case 'context':
            updates = {
                context: {
                    ...currentConfig?.context,
                    max_messages: parseInt(document.getElementById('contextMaxMessages').value) || undefined,
                    max_tokens: parseInt(document.getElementById('contextMaxTokens').value) || undefined,
                    summary_model: document.getElementById('contextSummaryModel').value || undefined,
                    compaction: {
                        ...currentConfig?.context?.compaction,
                        enabled: document.getElementById('contextCompactionEnabled').checked,
                        model: document.getElementById('contextCompactionModel').value || undefined,
                        keep_recent: parseInt(document.getElementById('contextKeepRecent').value) || undefined
                    }
                }
            };
            break;
    }

    const response = await makeRequest('/config', 'POST', updates);

    if (response.status === 200) {
        showToast(`${feature} settings saved`, 'success');
        loadConfig();
    } else {
        showToast(`Failed to save ${feature} settings: ${response.data?.error || 'Unknown error'}`, 'error');
    }
}

async function triggerReflection() {
    const response = await makeRequest('/reflection/trigger', 'POST');

    if (response.status === 200) {
        showToast('Reflection triggered', 'success');
    } else {
        showToast(response.data?.error || 'Failed to trigger reflection', 'error');
    }
}

async function initConfigTab() {
    // Load providers first, then config
    await loadProviders();
    await loadConfig();
}

// Load tools configuration from current config
function loadToolsConfig() {
    if (!currentConfig) return;

    const llm = currentConfig.llm || {};
    const tools = llm.tools || [];
    const builtinTools = llm.builtin_tools || [];

    // Check built-in tools
    const hasWebSearch = builtinTools.some(t => t.type === 'web_search_20250305' || t.name === 'web_search');
    const hasWebFetch = builtinTools.some(t => t.type === 'web_fetch_20250910' || t.name === 'web_fetch');
    const hasComputer = builtinTools.some(t => t.type?.startsWith('computer_') || t.name === 'computer');

    document.getElementById('toolWebSearch').checked = hasWebSearch;
    document.getElementById('toolWebFetch').checked = hasWebFetch;
    document.getElementById('toolComputer').checked = hasComputer;

    // Check agent tools
    document.getElementById('toolSendNotification').checked = tools.includes('send_notification');
    document.getElementById('toolGetTime').checked = tools.includes('get_time');
    document.getElementById('toolPing').checked = tools.includes('ping');
    document.getElementById('toolWait').checked = tools.includes('wait');
    document.getElementById('toolGenerateTestImage').checked = tools.includes('generate_test_image');
    document.getElementById('toolAnalyzeImageUrl').checked = tools.includes('analyze_image_url');
    document.getElementById('toolDocumentSearch').checked = tools.includes('document_search');

    // Update MCP tools list
    updateMCPToolsList();
}

function updateMCPToolsList() {
    const mcpToolsEl = document.getElementById('mcpToolsList');
    if (!mcpToolsEl) return;

    const mcp = currentConfig?.mcp || {};
    const mcpTools = mcp.tools || [];

    if (mcpTools.length > 0) {
        mcpToolsEl.innerHTML = mcpTools.map(t =>
            `<span class="inline-block px-2 py-1 bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 rounded text-xs mr-1 mb-1">${t}</span>`
        ).join('');
    } else if (mcp.enabled) {
        mcpToolsEl.innerHTML = '<span class="text-slate-400">MCP enabled - all tools available</span>';
    } else {
        mcpToolsEl.innerHTML = '<span class="text-slate-400">MCP disabled</span>';
    }
}

function updateToolsConfig() {
    // This is called on checkbox change - visual feedback only
    // Actual save happens on button click
}

async function saveToolsConfig() {
    // Build tools array
    const tools = [];
    if (document.getElementById('toolSendNotification').checked) tools.push('send_notification');
    if (document.getElementById('toolGetTime').checked) tools.push('get_time');
    if (document.getElementById('toolPing').checked) tools.push('ping');
    if (document.getElementById('toolWait').checked) tools.push('wait');
    if (document.getElementById('toolGenerateTestImage').checked) tools.push('generate_test_image');
    if (document.getElementById('toolAnalyzeImageUrl').checked) tools.push('analyze_image_url');
    if (document.getElementById('toolDocumentSearch').checked) tools.push('document_search');

    // Build builtin_tools array
    const builtinTools = [];
    if (document.getElementById('toolWebSearch').checked) {
        builtinTools.push({ type: 'web_search_20250305', name: 'web_search' });
    }
    if (document.getElementById('toolWebFetch').checked) {
        builtinTools.push({ type: 'web_fetch_20250910', name: 'web_fetch' });
    }
    if (document.getElementById('toolComputer').checked) {
        // Get display dimensions from operator config if available
        const op = currentConfig?.operator || {};
        // Determine correct computer tool version based on model
        const model = currentConfig?.llm?.model || '';
        const isNewVersion = model.includes('opus-4-6') || model.includes('sonnet-4-6') || model.includes('opus-4-5');
        builtinTools.push({
            type: isNewVersion ? 'computer_20251124' : 'computer_20250124',
            name: 'computer',
            display_width_px: op.display_width || 1024,
            display_height_px: op.display_height || 768,
            display_number: 1
        });
    }

    const updates = {
        llm: {
            ...currentConfig?.llm,
            tools: tools,
            builtin_tools: builtinTools
        }
    };

    const response = await makeRequest('/config', 'POST', updates);

    if (response.status === 200) {
        showToast('Tools configuration saved', 'success');
        loadConfig();
    } else {
        showToast('Failed to save tools configuration', 'error');
    }
}

// Helper function to update the agents mode status display
function updateAgentsModeStatus(mode) {
    const iconEl = document.getElementById('agentsModeIcon');
    const statusEl = document.getElementById('agentsModeStatus');

    if (!iconEl || !statusEl) return;

    if (mode === 'worker') {
        iconEl.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z"/></svg>';
        statusEl.textContent = 'Mode: Worker (receive-only, no call_agent tool)';
    } else if (mode === 'coordinator') {
        iconEl.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" stroke-width="2"/><circle cx="12" cy="12" r="6" stroke-width="2"/><circle cx="12" cy="12" r="2" fill="currentColor"/></svg>';
        statusEl.textContent = 'Mode: Coordinator (discovers & calls other agents)';
    } else {
        iconEl.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0z"/></svg>';
        statusEl.textContent = 'Mode: Peer (all agents can collaborate freely)';
    }
}

// Helper function to show/hide discovery method specific settings
function updateDiscoveryMethodUI() {
    const method = document.getElementById('agentsDiscoveryMethod')?.value || 'file';
    const fileSettings = document.getElementById('agentsFileSettings');
    const mdnsSettings = document.getElementById('agentsMdnsSettings');

    if (fileSettings) {
        fileSettings.classList.toggle('hidden', method !== 'file');
    }
    if (mdnsSettings) {
        mdnsSettings.classList.toggle('hidden', method !== 'mdns' && method !== 'ssdp');
    }
}

// Track available resources data
let availableResourcesData = null;

// Load and display enabled resource servers
function loadEnabledResources() {
    const container = document.getElementById('mcpEnabledResources');
    if (!container) return;

    const mcp = currentConfig?.mcp || {};
    const resources = mcp.resources || [];

    if (resources.length === 0) {
        container.innerHTML = '<span class="text-slate-400 text-sm">No resource servers enabled</span>';
        return;
    }

    container.innerHTML = resources.map(server => `
        <span class="inline-flex items-center gap-1 px-2 py-1 bg-emerald-100 dark:bg-emerald-900 text-emerald-700 dark:text-emerald-300 rounded text-sm mr-2 mb-1">
            ${server}
            <button onclick="removeResourceServer('${server}')" class="ml-1 text-emerald-500 hover:text-red-500">×</button>
        </span>
    `).join('');
}

// Add a resource server
async function addResourceServer() {
    const input = document.getElementById('mcpAddResourceServer');
    const serverName = input.value.trim();

    if (!serverName) {
        showToast('Please enter a server name', 'error');
        return;
    }

    const mcp = currentConfig?.mcp || {};
    const resources = mcp.resources || [];

    if (resources.includes(serverName)) {
        showToast(`${serverName} is already enabled`, 'error');
        return;
    }

    const newResources = [...resources, serverName];

    const response = await makeRequest('/config', 'POST', {
        mcp: {
            ...mcp,
            resources: newResources
        }
    });

    if (response.status === 200) {
        showToast(`Added ${serverName} to resource servers`, 'success');
        input.value = '';
        loadConfig();
    } else {
        showToast('Failed to add resource server', 'error');
    }
}

// Remove a resource server
async function removeResourceServer(serverName) {
    const mcp = currentConfig?.mcp || {};
    const resources = mcp.resources || [];

    const newResources = resources.filter(s => s !== serverName);

    const response = await makeRequest('/config', 'POST', {
        mcp: {
            ...mcp,
            resources: newResources
        }
    });

    if (response.status === 200) {
        showToast(`Removed ${serverName} from resource servers`, 'success');
        loadConfig();
    } else {
        showToast('Failed to remove resource server', 'error');
    }
}

// Show available resources panel
async function showAvailableResources() {
    const panel = document.getElementById('mcpAvailableResourcesPanel');
    const list = document.getElementById('mcpAvailableResourcesList');
    const filter = document.getElementById('mcpResourceServerFilter');

    panel.classList.remove('hidden');
    list.innerHTML = '<span class="text-slate-400">Loading available resources...</span>';

    try {
        const response = await makeRequest('/mcp/resources/available');
        if (response.status === 200 && response.data) {
            availableResourcesData = response.data;

            // Populate server filter dropdown
            const servers = response.data.servers || [];
            filter.innerHTML = '<option value="">All Servers</option>' +
                servers.map(s => `<option value="${s}">${s}</option>`).join('');

            // Display resources
            renderAvailableResources();
        } else {
            list.innerHTML = '<span class="text-red-500">Failed to load resources</span>';
        }
    } catch (e) {
        console.error('Failed to load available resources:', e);
        list.innerHTML = '<span class="text-red-500">Error loading resources</span>';
    }
}

// Hide available resources panel
function hideAvailableResources() {
    document.getElementById('mcpAvailableResourcesPanel').classList.add('hidden');
}

// Filter available resources by server
function filterAvailableResources() {
    renderAvailableResources();
}

// Render available resources list
function renderAvailableResources() {
    const list = document.getElementById('mcpAvailableResourcesList');
    const filter = document.getElementById('mcpResourceServerFilter');
    const selectedServer = filter.value;

    if (!availableResourcesData) {
        list.innerHTML = '<span class="text-slate-400">No data</span>';
        return;
    }

    const resources = availableResourcesData.resources || {};
    const enabledServers = currentConfig?.mcp?.resources || [];

    let html = '';
    let totalShown = 0;

    for (const [serverName, serverResources] of Object.entries(resources)) {
        if (selectedServer && serverName !== selectedServer) continue;

        const isEnabled = enabledServers.includes(serverName);
        const resourceCount = serverResources?.length || 0;

        html += `
            <div class="mb-3">
                <div class="flex items-center justify-between mb-1">
                    <span class="font-medium text-slate-700 dark:text-slate-300">${serverName}</span>
                    <div class="flex items-center gap-2">
                        <span class="text-slate-400">${resourceCount} resources</span>
                        ${isEnabled
                            ? '<span class="px-1.5 py-0.5 bg-emerald-100 dark:bg-emerald-900 text-emerald-600 dark:text-emerald-400 rounded text-xs">Enabled</span>'
                            : `<button onclick="quickAddResourceServer('${serverName}')" class="px-1.5 py-0.5 bg-blue-100 hover:bg-blue-200 dark:bg-blue-900 dark:hover:bg-blue-800 text-blue-600 dark:text-blue-400 rounded text-xs">+ Enable</button>`
                        }
                    </div>
                </div>
                <div class="pl-2 border-l-2 border-slate-200 dark:border-slate-600">
                    ${(serverResources || []).slice(0, 5).map(r => `
                        <div class="py-0.5 text-slate-500 dark:text-slate-400 truncate" title="${r.uri || r.name}">
                            ${r.name || r.uri || 'Unnamed resource'}
                        </div>
                    `).join('')}
                    ${resourceCount > 5 ? `<div class="text-slate-400 italic">...and ${resourceCount - 5} more</div>` : ''}
                </div>
            </div>
        `;
        totalShown++;
    }

    if (totalShown === 0) {
        html = '<span class="text-slate-400">No resources discovered. Make sure MCP is enabled and refresh.</span>';
    }

    list.innerHTML = html;
}

// Quick add from available resources panel
async function quickAddResourceServer(serverName) {
    const mcp = currentConfig?.mcp || {};
    const resources = mcp.resources || [];

    if (resources.includes(serverName)) {
        showToast(`${serverName} is already enabled`, 'info');
        return;
    }

    const newResources = [...resources, serverName];

    const response = await makeRequest('/config', 'POST', {
        mcp: {
            ...mcp,
            resources: newResources
        }
    });

    if (response.status === 200) {
        showToast(`Enabled ${serverName} for resource sync`, 'success');
        await loadConfig();
        renderAvailableResources(); // Re-render to update button state
    } else {
        showToast('Failed to enable resource server', 'error');
    }
}

// Load MCP resources sync status
async function loadMCPResourcesStatus() {
    const statusEl = document.getElementById('mcpResourcesStatus');
    if (!statusEl) return;

    try {
        const response = await makeRequest('/mcp/resources/status');
        if (response.status === 200 && response.data) {
            const { enabled, status } = response.data;

            if (!enabled) {
                statusEl.innerHTML = '<span class="text-slate-400">Resource sync not enabled</span>';
                return;
            }

            if (!status) {
                statusEl.innerHTML = '<span class="text-slate-400">No sync status available</span>';
                return;
            }

            const lastSync = status.last_sync ? new Date(status.last_sync).toLocaleString() : 'Never';
            const resourceCount = status.synced_resources || 0;
            const serverCount = status.synced_servers || 0;

            statusEl.innerHTML = `
                <div class="grid grid-cols-3 gap-2">
                    <div>
                        <span class="text-slate-400">Last sync:</span>
                        <span class="ml-1">${lastSync}</span>
                    </div>
                    <div>
                        <span class="text-slate-400">Resources:</span>
                        <span class="ml-1 font-medium">${resourceCount}</span>
                    </div>
                    <div>
                        <span class="text-slate-400">Servers:</span>
                        <span class="ml-1 font-medium">${serverCount}</span>
                    </div>
                </div>
            `;
        } else {
            statusEl.innerHTML = '<span class="text-slate-400">Failed to load status</span>';
        }
    } catch (e) {
        console.error('Failed to load MCP resources status:', e);
        statusEl.innerHTML = '<span class="text-red-500">Error loading status</span>';
    }
}

// Trigger MCP resources sync
async function syncMCPResources() {
    const statusEl = document.getElementById('mcpResourcesStatus');
    if (statusEl) {
        statusEl.innerHTML = '<span class="text-blue-500">Syncing resources...</span>';
    }

    try {
        const response = await makeRequest('/mcp/resources/sync', 'POST');
        if (response.status === 200 && response.data?.success) {
            const stats = response.data.stats || {};
            showToast(`Synced ${stats.synced || 0} resources from ${stats.servers || 0} servers`, 'success');
            // Refresh status display
            loadMCPResourcesStatus();
        } else {
            showToast(`Sync failed: ${response.data?.error || 'Unknown error'}`, 'error');
            loadMCPResourcesStatus();
        }
    } catch (e) {
        console.error('MCP resources sync failed:', e);
        showToast('Sync failed: ' + e.message, 'error');
        loadMCPResourcesStatus();
    }
}

// ============= Tool Loading Functions =============

// Load tool loading config from current config
function loadToolLoadingConfig() {
    if (!currentConfig) return;

    console.log('Loading tool_loading config:', currentConfig.llm?.tool_loading);
    const toolLoading = currentConfig.llm?.tool_loading || {};

    // Set mode and strategy
    document.getElementById('toolLoadingMode').value = toolLoading.mode || 'full';
    document.getElementById('toolLoadingStrategy').value = toolLoading.strategy || 'bm25';
    document.getElementById('toolLoadingMaxTools').value = toolLoading.max_tools || '';
    document.getElementById('toolLoadingAlwaysLoad').value = (toolLoading.always_load || []).join(', ');

    // BM25 settings
    const bm25 = toolLoading.bm25 || {};
    document.getElementById('toolLoadingBM25TopK').value = bm25.top_k || '';
    document.getElementById('toolLoadingBM25Threshold').value = bm25.threshold != null ? bm25.threshold : '';

    // Load keyword rules
    loadKeywordRules(toolLoading.keyword?.rules || []);

    // Load regex rules
    loadRegexRules(toolLoading.regex?.rules || []);

    // Update UI visibility
    updateToolLoadingUI();
}

// Update tool loading UI based on mode and strategy
function updateToolLoadingUI() {
    const mode = document.getElementById('toolLoadingMode').value;
    const strategy = document.getElementById('toolLoadingStrategy').value;

    // Show/hide strategy settings based on mode
    const strategyEnabled = mode === 'on_demand';
    document.getElementById('toolLoadingStrategy').disabled = !strategyEnabled;

    // Show/hide strategy-specific settings
    document.getElementById('toolLoadingBM25Settings').classList.toggle('hidden', strategy !== 'bm25' || !strategyEnabled);
    document.getElementById('toolLoadingKeywordSettings').classList.toggle('hidden', strategy !== 'keyword' || !strategyEnabled);
    document.getElementById('toolLoadingRegexSettings').classList.toggle('hidden', strategy !== 'regex' || !strategyEnabled);
}

// Keyword rules management
let keywordRules = [];

function loadKeywordRules(rules) {
    keywordRules = rules || [];
    renderKeywordRules();
}

function renderKeywordRules() {
    const container = document.getElementById('keywordRulesList');
    if (!container) return;

    if (keywordRules.length === 0) {
        container.innerHTML = '<p class="text-sm text-slate-400">No keyword rules defined.</p>';
        return;
    }

    container.innerHTML = keywordRules.map((rule, idx) => `
        <div class="flex items-center gap-2 p-2 bg-slate-50 dark:bg-slate-700 rounded">
            <div class="flex-1">
                <input type="text" value="${(rule.keywords || []).join(', ')}" placeholder="Keywords (comma-separated)"
                    class="w-full px-2 py-1 text-sm border border-slate-200 dark:border-slate-600 rounded dark:bg-slate-600 dark:text-slate-100 mb-1"
                    onchange="updateKeywordRule(${idx}, 'keywords', this.value)">
                <input type="text" value="${(rule.tools || []).join(', ')}" placeholder="Tools to load (comma-separated)"
                    class="w-full px-2 py-1 text-sm border border-slate-200 dark:border-slate-600 rounded dark:bg-slate-600 dark:text-slate-100"
                    onchange="updateKeywordRule(${idx}, 'tools', this.value)">
            </div>
            <button onclick="removeKeywordRule(${idx})" class="text-red-500 hover:text-red-700 px-2">×</button>
        </div>
    `).join('');
}

function addKeywordRule() {
    keywordRules.push({ keywords: [], tools: [] });
    renderKeywordRules();
}

function updateKeywordRule(idx, field, value) {
    const arr = value.split(',').map(s => s.trim()).filter(s => s);
    keywordRules[idx][field] = arr;
}

function removeKeywordRule(idx) {
    keywordRules.splice(idx, 1);
    renderKeywordRules();
}

// Regex rules management
let regexRules = [];

function loadRegexRules(rules) {
    regexRules = rules || [];
    renderRegexRules();
}

function renderRegexRules() {
    const container = document.getElementById('regexRulesList');
    if (!container) return;

    if (regexRules.length === 0) {
        container.innerHTML = '<p class="text-sm text-slate-400">No regex rules defined.</p>';
        return;
    }

    container.innerHTML = regexRules.map((rule, idx) => `
        <div class="flex items-center gap-2 p-2 bg-slate-50 dark:bg-slate-700 rounded">
            <div class="flex-1">
                <input type="text" value="${rule.pattern || ''}" placeholder="Regex pattern"
                    class="w-full px-2 py-1 text-sm border border-slate-200 dark:border-slate-600 rounded dark:bg-slate-600 dark:text-slate-100 font-mono mb-1"
                    onchange="updateRegexRule(${idx}, 'pattern', this.value)">
                <input type="text" value="${(rule.tools || []).join(', ')}" placeholder="Tools to load (comma-separated)"
                    class="w-full px-2 py-1 text-sm border border-slate-200 dark:border-slate-600 rounded dark:bg-slate-600 dark:text-slate-100"
                    onchange="updateRegexRule(${idx}, 'tools', this.value)">
            </div>
            <button onclick="removeRegexRule(${idx})" class="text-red-500 hover:text-red-700 px-2">×</button>
        </div>
    `).join('');
}

function addRegexRule() {
    regexRules.push({ pattern: '', tools: [] });
    renderRegexRules();
}

function updateRegexRule(idx, field, value) {
    if (field === 'tools') {
        regexRules[idx].tools = value.split(',').map(s => s.trim()).filter(s => s);
    } else {
        regexRules[idx][field] = value;
    }
}

function removeRegexRule(idx) {
    regexRules.splice(idx, 1);
    renderRegexRules();
}

// Save tool loading configuration
async function saveToolLoadingConfig() {
    const mode = document.getElementById('toolLoadingMode').value;
    const strategy = document.getElementById('toolLoadingStrategy').value;
    const maxTools = parseInt(document.getElementById('toolLoadingMaxTools').value) || undefined;
    const alwaysLoad = document.getElementById('toolLoadingAlwaysLoad').value
        .split(',').map(s => s.trim()).filter(s => s);

    const toolLoading = {
        mode: mode,
        strategy: strategy,
        max_tools: maxTools,
        always_load: alwaysLoad.length > 0 ? alwaysLoad : undefined
    };

    // Add strategy-specific config
    if (strategy === 'bm25') {
        const topK = parseInt(document.getElementById('toolLoadingBM25TopK').value);
        const threshold = parseFloat(document.getElementById('toolLoadingBM25Threshold').value);
        if (topK || threshold) {
            toolLoading.bm25 = {};
            if (topK) toolLoading.bm25.top_k = topK;
            if (!isNaN(threshold)) toolLoading.bm25.threshold = threshold;
        }
    } else if (strategy === 'keyword' && keywordRules.length > 0) {
        toolLoading.keyword = { rules: keywordRules };
    } else if (strategy === 'regex' && regexRules.length > 0) {
        toolLoading.regex = { rules: regexRules };
    }

    const updates = {
        llm: {
            ...currentConfig?.llm,
            tool_loading: toolLoading
        }
    };

    console.log('Saving tool loading config:', JSON.stringify(updates, null, 2));

    const response = await makeRequest('/config', 'POST', updates);
    console.log('Save response:', response.status, response.data);

    if (response.status === 200) {
        console.log('Response config llm.tool_loading:', response.data?.config?.llm?.tool_loading);
        showToast('Tool loading configuration saved', 'success');
        loadConfig();
    } else {
        showToast('Failed to save tool loading configuration: ' + (response.data?.error || 'Unknown'), 'error');
    }
}

// ============= MCP Webhooks Functions =============

// Load and display enabled webhook servers from config
function loadEnabledWebhooks() {
    const container = document.getElementById('mcpEnabledWebhooks');
    if (!container) return;

    const webhooks = currentConfig?.mcp?.webhooks || [];

    if (webhooks.length === 0) {
        container.innerHTML = '<span class="text-slate-400 text-sm">No webhook servers enabled</span>';
    } else {
        container.innerHTML = webhooks.map(server => `
            <span class="inline-flex items-center gap-1 px-2 py-1 bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 rounded text-sm">
                ${server}
                <button onclick="removeWebhookServer('${server}')" class="ml-1 text-blue-500 hover:text-red-500">&times;</button>
            </span>
        `).join(' ');
    }

    // Also load available servers for the dropdown
    refreshAvailableWebhookServers();
}

// Fetch available webhook servers from /mcp/webhooks and populate dropdown
async function refreshAvailableWebhookServers() {
    const select = document.getElementById('mcpAddWebhookServer');
    if (!select) return;

    try {
        const response = await makeRequest('/mcp/webhooks');
        if (response.status === 200 && response.data) {
            const availableServers = response.data.available_servers || [];
            const enabledServers = response.data.enabled_servers || [];

            // Filter out already enabled servers
            const notEnabled = availableServers.filter(s => !enabledServers.includes(s));

            select.innerHTML = '<option value="">Select a server...</option>';
            notEnabled.forEach(server => {
                const option = document.createElement('option');
                option.value = server;
                option.textContent = server;
                select.appendChild(option);
            });

            if (notEnabled.length === 0 && availableServers.length > 0) {
                select.innerHTML = '<option value="">All servers already enabled</option>';
            } else if (availableServers.length === 0) {
                select.innerHTML = '<option value="">No webhook servers available</option>';
            }
        }
    } catch (e) {
        console.error('Failed to load available webhook servers:', e);
        select.innerHTML = '<option value="">Failed to load servers</option>';
    }
}

// Add a webhook server to the enabled list
async function addWebhookServer() {
    const select = document.getElementById('mcpAddWebhookServer');
    const server = select?.value;

    if (!server) {
        showToast('Please select a server', 'error');
        return;
    }

    // Get current webhooks array
    const currentWebhooks = currentConfig?.mcp?.webhooks || [];

    // Check if already enabled
    if (currentWebhooks.includes(server)) {
        showToast('Server already enabled', 'error');
        return;
    }

    // Add to array and save
    const newWebhooks = [...currentWebhooks, server];

    try {
        const response = await makeRequest('/config', 'POST', {
            mcp: {
                ...currentConfig?.mcp,
                webhooks: newWebhooks
            }
        });

        if (response.status === 200) {
            currentConfig = response.data.config || currentConfig;
            if (currentConfig.mcp) {
                currentConfig.mcp.webhooks = newWebhooks;
            }
            loadEnabledWebhooks();
            showToast(`Webhook server "${server}" enabled`, 'success');
        } else {
            showToast('Failed to add webhook server', 'error');
        }
    } catch (e) {
        console.error('Failed to add webhook server:', e);
        showToast('Failed to add webhook server: ' + e.message, 'error');
    }
}

// Remove a webhook server from the enabled list
async function removeWebhookServer(server) {
    const currentWebhooks = currentConfig?.mcp?.webhooks || [];
    const newWebhooks = currentWebhooks.filter(s => s !== server);

    try {
        const response = await makeRequest('/config', 'POST', {
            mcp: {
                ...currentConfig?.mcp,
                webhooks: newWebhooks
            }
        });

        if (response.status === 200) {
            currentConfig = response.data.config || currentConfig;
            if (currentConfig.mcp) {
                currentConfig.mcp.webhooks = newWebhooks;
            }
            loadEnabledWebhooks();
            showToast(`Webhook server "${server}" disabled`, 'success');
        } else {
            showToast('Failed to remove webhook server', 'error');
        }
    } catch (e) {
        console.error('Failed to remove webhook server:', e);
        showToast('Failed to remove webhook server: ' + e.message, 'error');
    }
}

// ============ External MCP Servers ============

let externalServers = [];

// Toggle the add/edit server form
function toggleAddExternalServer() {
    const form = document.getElementById('externalServerForm');
    const isHidden = form.classList.contains('hidden');

    if (isHidden) {
        // Show form - reset for new server
        form.classList.remove('hidden');
        document.getElementById('externalServerFormTitle').textContent = 'Add External MCP Server';
        document.getElementById('externalServerEditIndex').value = '-1';
        document.getElementById('externalServerName').value = '';
        document.getElementById('externalServerProvider').value = 'custom';
        document.getElementById('externalServerUrl').value = '';
        document.getElementById('externalServerApiKey').value = '';
        document.getElementById('externalServerHeaders').value = '';
        document.getElementById('externalServerEnabled').checked = true;
    } else {
        // Hide form
        form.classList.add('hidden');
    }
}

// Handle provider preset selection
function onExternalServerProviderChange() {
    const provider = document.getElementById('externalServerProvider').value;
    const urlField = document.getElementById('externalServerUrl');
    const nameField = document.getElementById('externalServerName');

    switch (provider) {
        case 'composio':
            urlField.placeholder = 'https://backend.composio.dev/v3/mcp/YOUR_SERVER_ID/mcp?user_id=YOUR_USER_ID';
            if (!nameField.value) nameField.value = 'composio';
            break;
        case 'smithery':
            urlField.placeholder = 'https://server.smithery.ai/v1/mcp';
            if (!nameField.value) nameField.value = 'smithery';
            break;
        default:
            urlField.placeholder = 'https://your-mcp-server.com/mcp';
    }
}

// Save external server (add or update)
async function saveExternalServer() {
    const name = document.getElementById('externalServerName').value.trim();
    const url = document.getElementById('externalServerUrl').value.trim();
    const apiKey = document.getElementById('externalServerApiKey').value.trim();
    const headersStr = document.getElementById('externalServerHeaders').value.trim();
    const enabled = document.getElementById('externalServerEnabled').checked;
    const editIndex = parseInt(document.getElementById('externalServerEditIndex').value);

    // Validation
    if (!name) {
        showToast('Server name is required', 'error');
        return;
    }
    if (!url) {
        showToast('Server URL is required', 'error');
        return;
    }

    // Build headers object
    let headers = {};
    if (headersStr) {
        try {
            headers = JSON.parse(headersStr);
        } catch (e) {
            showToast('Invalid JSON for custom headers', 'error');
            return;
        }
    }

    // Add API key header based on provider
    if (apiKey) {
        const provider = document.getElementById('externalServerProvider').value;
        if (provider === 'composio') {
            headers['x-api-key'] = apiKey;
        } else if (provider === 'smithery') {
            headers['Authorization'] = `Bearer ${apiKey}`;
        } else {
            // Default to x-api-key
            headers['x-api-key'] = apiKey;
        }
    }

    const server = { name, url, headers, enabled };

    // Update or add
    if (editIndex >= 0) {
        externalServers[editIndex] = server;
    } else {
        // Check for duplicate name
        if (externalServers.some(s => s.name === name)) {
            showToast('A server with this name already exists', 'error');
            return;
        }
        externalServers.push(server);
    }

    // Save to config
    await saveExternalServersToConfig();

    // Hide form and refresh list
    toggleAddExternalServer();
    renderExternalServersList();
}

// Edit an existing external server
function editExternalServer(index) {
    const server = externalServers[index];
    if (!server) return;

    const form = document.getElementById('externalServerForm');
    form.classList.remove('hidden');

    document.getElementById('externalServerFormTitle').textContent = 'Edit External MCP Server';
    document.getElementById('externalServerEditIndex').value = index;
    document.getElementById('externalServerName').value = server.name;
    document.getElementById('externalServerUrl').value = server.url;
    document.getElementById('externalServerEnabled').checked = server.enabled !== false;

    // Try to detect provider and extract API key
    const headers = server.headers || {};
    let apiKey = '';
    let provider = 'custom';

    if (server.url.includes('composio.dev')) {
        provider = 'composio';
        apiKey = headers['x-api-key'] || '';
    } else if (server.url.includes('smithery.ai')) {
        provider = 'smithery';
        const auth = headers['Authorization'] || '';
        apiKey = auth.replace('Bearer ', '');
    } else {
        apiKey = headers['x-api-key'] || headers['Authorization'] || '';
    }

    document.getElementById('externalServerProvider').value = provider;
    document.getElementById('externalServerApiKey').value = apiKey;

    // Show remaining headers (excluding api key headers)
    const remainingHeaders = { ...headers };
    delete remainingHeaders['x-api-key'];
    delete remainingHeaders['Authorization'];
    if (Object.keys(remainingHeaders).length > 0) {
        document.getElementById('externalServerHeaders').value = JSON.stringify(remainingHeaders, null, 2);
    } else {
        document.getElementById('externalServerHeaders').value = '';
    }
}

// Delete an external server
async function deleteExternalServer(index) {
    const server = externalServers[index];
    if (!server) return;

    if (!confirm(`Delete server "${server.name}"?`)) return;

    externalServers.splice(index, 1);
    await saveExternalServersToConfig();
    renderExternalServersList();
}

// Toggle server enabled state
async function toggleExternalServerEnabled(index) {
    const server = externalServers[index];
    if (!server) return;

    server.enabled = !server.enabled;
    await saveExternalServersToConfig();
    renderExternalServersList();
}

// Save external servers to config
async function saveExternalServersToConfig() {
    try {
        const response = await makeRequest('/config', 'POST', {
            mcp: {
                ...currentConfig?.mcp,
                servers: externalServers
            }
        });

        if (response.status === 200) {
            currentConfig = response.data.config || currentConfig;
            showToast('External MCP servers updated', 'success');
        } else {
            showToast('Failed to save servers', 'error');
        }
    } catch (e) {
        console.error('Failed to save external servers:', e);
        showToast('Failed to save servers: ' + e.message, 'error');
    }
}

// Render the list of configured external servers
function renderExternalServersList() {
    const container = document.getElementById('externalServersList');
    if (!container) return;

    // Also update hidden field for form compatibility
    document.getElementById('mcpServers').value = JSON.stringify(externalServers);

    if (externalServers.length === 0) {
        container.innerHTML = '<div class="text-sm text-slate-400 italic p-3 bg-slate-50 dark:bg-slate-700 rounded-lg">No external MCP servers configured</div>';
        return;
    }

    container.innerHTML = externalServers.map((server, index) => {
        const statusClass = server.enabled !== false
            ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900 dark:text-emerald-300'
            : 'bg-slate-100 text-slate-500 dark:bg-slate-600 dark:text-slate-400';
        const statusText = server.enabled !== false ? 'Enabled' : 'Disabled';

        // Detect provider icon (SVG)
        let icon = '<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1"/></svg>';
        if (server.url.includes('composio.dev')) icon = '<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>';
        else if (server.url.includes('smithery.ai')) icon = '<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/></svg>';

        // Mask URL for display
        const displayUrl = server.url.length > 60 ? server.url.substring(0, 57) + '...' : server.url;

        return `
            <div class="flex items-center justify-between p-3 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 rounded-lg">
                <div class="flex items-center gap-3 flex-1 min-w-0">
                    <span class="text-slate-500 dark:text-slate-400">${icon}</span>
                    <div class="min-w-0 flex-1">
                        <div class="flex items-center gap-2">
                            <span class="font-medium text-slate-700 dark:text-slate-200">${escapeHtml(server.name)}</span>
                            <span class="px-2 py-0.5 rounded text-xs ${statusClass}">${statusText}</span>
                        </div>
                        <p class="text-xs text-slate-400 truncate" title="${escapeHtml(server.url)}">${escapeHtml(displayUrl)}</p>
                    </div>
                </div>
                <div class="flex items-center gap-1 ml-2">
                    <button onclick="toggleExternalServerEnabled(${index})" class="p-1.5 hover:bg-slate-100 dark:hover:bg-slate-700 rounded text-slate-500 hover:text-slate-700" title="${server.enabled !== false ? 'Disable' : 'Enable'}">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">${server.enabled !== false ? '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z"/>' : '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>'}</svg>
                    </button>
                    <button onclick="editExternalServer(${index})" class="p-1.5 hover:bg-slate-100 dark:hover:bg-slate-700 rounded text-slate-500 hover:text-blue-600" title="Edit">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg>
                    </button>
                    <button onclick="deleteExternalServer(${index})" class="p-1.5 hover:bg-slate-100 dark:hover:bg-slate-700 rounded text-slate-500 hover:text-red-600" title="Delete">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                    </button>
                </div>
            </div>
        `;
    }).join('');
}

// Load external servers from config
function loadExternalServers() {
    externalServers = currentConfig?.mcp?.servers || [];
    renderExternalServersList();
}
