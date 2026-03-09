// Voice Tab Functions

let voiceSession = null;
let voiceDurationInterval = null;
let voiceStartTime = null;
let voiceConfig = null;
let voiceWs = null;
let voiceAudioContext = null;
let voiceMediaStream = null;
let voiceProcessor = null;
let voiceIsRecording = false;
let voiceAudioQueue = [];
let voiceIsPlaying = false;
let voiceNextPlayTime = 0; // For gapless scheduling
let voicePlaybackContext = null; // Separate context for playback at 24kHz
let voiceResponseStartTime = 0; // AudioContext time when first chunk of current response started
let voiceCurrentItemID = null; // item_id from server for truncation on interrupt
let voiceCurrentContentIndex = 0; // content_index from server for truncation
let voiceActiveSources = []; // Track playing AudioBufferSourceNodes for stopping
let voiceAudioDeltaCount = 0; // Count audio deltas for event log batching
let voiceTotalAudioDurationMs = 0; // Total duration of audio received from server (for truncation calc)
let voiceInterrupted = false; // True after interrupt — ignore audio deltas until next response


// Voice options for each provider
const OPENAI_VOICES = [
    { value: 'marin', label: 'Marin (Recommended)' },
    { value: 'cedar', label: 'Cedar (Recommended)' },
    { value: 'alloy', label: 'Alloy' },
    { value: 'ash', label: 'Ash' },
    { value: 'ballad', label: 'Ballad' },
    { value: 'coral', label: 'Coral' },
    { value: 'echo', label: 'Echo' },
    { value: 'sage', label: 'Sage' },
    { value: 'shimmer', label: 'Shimmer' },
    { value: 'verse', label: 'Verse' }
];

const GEMINI_VOICES = [
    { value: 'Aoede', label: 'Aoede' },
    { value: 'Charon', label: 'Charon' },
    { value: 'Fenrir', label: 'Fenrir' },
    { value: 'Kore', label: 'Kore' },
    { value: 'Puck', label: 'Puck' }
];

const ELEVENLABS_VOICES = [
    { value: '21m00Tcm4TlvDq8ikWAM', label: 'Rachel' },
    { value: 'EXAVITQu4vr4xnSDxMaL', label: 'Bella' },
    { value: 'ErXwobaYiN019PkySvjV', label: 'Antoni' },
    { value: 'MF3mGyEYCl7XYWbV9V6O', label: 'Elli' },
    { value: 'TxGEqnHWrfWFTfGW9XjX', label: 'Josh' },
    { value: 'VR6AewLTigWG4xSOukaG', label: 'Arnold' },
    { value: 'pNInz6obpgDQGcFmaJgB', label: 'Adam' },
    { value: 'yoZ06aMxZJJ28mfd3POQ', label: 'Sam' }
];

// ==================== Event Log ====================

function addVoiceEvent(direction, type, detail) {
    const log = document.getElementById('voiceEventLog');
    if (!log) return;

    // Remove placeholder
    const placeholder = log.querySelector('.text-slate-500');
    if (placeholder) placeholder.remove();

    const now = new Date();
    const time = now.toLocaleTimeString('en-US', {
        hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit'
    }) + '.' + String(now.getMilliseconds()).padStart(3, '0');

    const arrow = direction === 'in' ? '\u25C0' : '\u25B6';
    const colorClass = direction === 'in' ? 'text-emerald-400' : 'text-blue-400';

    const entry = document.createElement('div');
    entry.className = colorClass;
    entry.textContent = `${time} ${arrow} ${type}${detail ? ' \u2014 ' + detail : ''}`;
    log.appendChild(entry);

    // Keep max 200 entries
    while (log.children.length > 200) log.removeChild(log.firstChild);
    log.scrollTop = log.scrollHeight;
}

// ==================== Voice Options ====================

function updateVoiceOptions(selectedVoice) {
    const providerSelect = document.getElementById('voiceProviderSelect');
    const voiceSelect = document.getElementById('voiceSelect');
    if (!providerSelect || !voiceSelect) return;

    const provider = providerSelect.value;
    let voices;
    if (provider === 'standard') {
        voices = ELEVENLABS_VOICES;
    } else if (provider === 'gemini') {
        voices = GEMINI_VOICES;
    } else {
        voices = OPENAI_VOICES;
    }

    voiceSelect.innerHTML = '';
    voices.forEach(v => {
        const option = document.createElement('option');
        option.value = v.value;
        option.textContent = v.label;
        voiceSelect.appendChild(option);
    });

    if (selectedVoice) {
        voiceSelect.value = selectedVoice;
    } else if (voiceConfig) {
        if (provider === 'standard' && voiceConfig.tts?.voice) {
            voiceSelect.value = voiceConfig.tts.voice;
        } else if (provider === 'gemini' && voiceConfig.gemini_voice) {
            voiceSelect.value = voiceConfig.gemini_voice;
        } else if (voiceConfig.voice) {
            voiceSelect.value = voiceConfig.voice;
        }
    }
}

async function initVoiceTab() {
    try {
        const response = await makeRequest('/config');
        if (response.status === 200 && response.data) {
            const rt = response.data.realtime || {};
            voiceConfig = rt;
            const provider = rt.provider || 'openai';

            const providerSelect = document.getElementById('voiceProviderSelect');
            if (providerSelect) providerSelect.value = provider;

            const providerDisplay = document.getElementById('voiceProvider');
            if (providerDisplay) {
                const providerLabels = { openai: 'OpenAI', gemini: 'Gemini', standard: 'Standard' };
                providerDisplay.textContent = providerLabels[provider] || provider;
            }

            let voice;
            if (provider === 'standard') voice = rt.tts?.voice;
            else if (provider === 'gemini') voice = rt.gemini_voice;
            else voice = rt.voice;
            updateVoiceOptions(voice);
        }
    } catch (e) {
        console.error('Failed to load voice config:', e);
    }
}

// ==================== Audio Conversion ====================

function float32ToInt16(float32Array) {
    const int16Array = new Int16Array(float32Array.length);
    for (let i = 0; i < float32Array.length; i++) {
        const s = Math.max(-1, Math.min(1, float32Array[i]));
        int16Array[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
    }
    return int16Array;
}

function int16ToBase64(int16Array) {
    const uint8Array = new Uint8Array(int16Array.buffer);
    let binary = '';
    for (let i = 0; i < uint8Array.length; i++) {
        binary += String.fromCharCode(uint8Array[i]);
    }
    return btoa(binary);
}

function base64ToFloat32(base64) {
    const binaryString = atob(base64);
    const int16Array = new Int16Array(binaryString.length / 2);
    for (let i = 0; i < int16Array.length; i++) {
        int16Array[i] = (binaryString.charCodeAt(i * 2 + 1) << 8) | binaryString.charCodeAt(i * 2);
    }
    const float32Array = new Float32Array(int16Array.length);
    for (let i = 0; i < int16Array.length; i++) {
        float32Array[i] = int16Array[i] / (int16Array[i] < 0 ? 0x8000 : 0x7FFF);
    }
    return float32Array;
}

// ==================== Session Management ====================

async function startVoiceSession() {
    // Guard: stop any existing session first
    if (voiceWs && voiceWs.readyState <= WebSocket.OPEN) {
        stopVoiceSession();
    }

    const provider = document.getElementById('voiceProviderSelect').value;
    const voice = document.getElementById('voiceSelect').value;

    document.getElementById('voiceStatus').textContent = 'Connecting...';
    addVoiceEvent('out', 'ws.connect', `provider=${provider}, voice=${voice}`);

    try {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        let wsUrl = `${protocol}//${window.location.host}/voice`;
        if (getApiKey()) {
            wsUrl += `?api_key=${encodeURIComponent(getApiKey())}`;
        }

        voiceWs = new WebSocket(wsUrl);

        // Pre-create playback AudioContext during user gesture (iOS Safari)
        if (!voicePlaybackContext) {
            voicePlaybackContext = new AudioContext({ sampleRate: 24000 });
        }
        if (voicePlaybackContext.state === 'suspended') {
            voicePlaybackContext.resume();
        }

        voiceWs.onopen = () => {
            document.getElementById('voiceStatus').textContent = 'Connected';
            const providerLabels = { openai: 'OpenAI', gemini: 'Gemini', standard: 'Standard' };
            document.getElementById('voiceProvider').textContent = providerLabels[provider] || provider;
            addVoiceEvent('out', 'ws.open', 'connected');

            voiceWs.send(JSON.stringify({
                type: 'start',
                data: { provider: provider, voice: voice }
            }));
            addVoiceEvent('out', 'start', `provider=${provider}`);
        };

        voiceWs.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data);
                handleVoiceMessage(msg);
            } catch (e) {
                console.error('Failed to parse voice message:', e);
            }
        };

        voiceWs.onerror = (error) => {
            console.error('WebSocket error:', error);
            document.getElementById('voiceStatus').textContent = 'Error';
            addVoiceEvent('in', 'ws.error', 'connection failed');
            showToast('WebSocket connection failed', 'error');
        };

        voiceWs.onclose = () => {
            document.getElementById('voiceStatus').textContent = 'Disconnected';
            addVoiceEvent('in', 'ws.close', 'disconnected');
            stopVoiceSessionInternal();
        };

    } catch (error) {
        document.getElementById('voiceStatus').textContent = 'Error';
        showToast('Failed to connect: ' + error.message, 'error');
    }
}

// ==================== Event Handling ====================

async function handleVoiceMessage(msg) {
    switch (msg.type) {
        case 'session_created':
            addVoiceEvent('in', 'session_created', `session=${msg.session_id}`);
            document.getElementById('voiceStatus').textContent = 'Active';
            voiceStartTime = Date.now();
            startDurationTimer();
            voiceIsRecording = true;
            voiceAudioDeltaCount = 0;
            showToast('Voice session started', 'success');
            await startAudioCapture();
            break;

        case 'audio_delta':
            voiceAudioDeltaCount++;
            // After interrupt, ignore remaining audio deltas from cancelled response
            if (voiceInterrupted) {
                if (voiceAudioDeltaCount === 1) {
                    addVoiceEvent('in', 'audio_delta', `DROPPED (interrupted)`);
                }
                break;
            }
            // Log every 10th audio delta to avoid spam
            if (voiceAudioDeltaCount === 1 || voiceAudioDeltaCount % 10 === 0) {
                addVoiceEvent('in', 'audio_delta', `#${voiceAudioDeltaCount}`);
            }
            if (msg.data && msg.data.chunk) {
                playAudioChunk(msg.data.chunk);
            }
            break;

        case 'audio_complete':
            addVoiceEvent('in', 'audio_complete', `total_chunks=${voiceAudioDeltaCount}${voiceInterrupted ? ' (interrupted)' : ''}`);
            voiceAudioDeltaCount = 0;
            voiceInterrupted = false;
            break;

        case 'transcript':
            if (msg.data) {
                const role = msg.data.role || '?';
                if (msg.data.partial) {
                    updatePartialTranscript(msg.data.content);
                    // Don't log every partial — too noisy
                } else {
                    addVoiceEvent('in', 'transcript', `${role}: "${msg.data.content}"`);
                    clearPartialTranscript();
                    addVoiceLogEntry(role, msg.data.content);
                }
            }
            break;

        case 'turn_start':
            addVoiceEvent('in', 'turn_start', `speaker=${msg.data?.speaker || '?'}`);
            break;

        case 'turn_end': {
            const turnStatus = msg.data?.status || 'completed';
            addVoiceEvent('in', 'turn_end', `status=${turnStatus}`);
            // Clear interrupt flag — the response (cancelled or not) is fully done
            voiceInterrupted = false;
            voiceAudioDeltaCount = 0;
            break;
        }

        case 'audio_interrupt': {
            // Only act on interrupt if audio is actually playing
            if (!voiceIsPlaying && voiceActiveSources.length === 0) {
                addVoiceEvent('in', 'audio_interrupt', `ignored (no audio playing)`);
                break;
            }

            // Calculate how much audio was actually played before the interrupt
            // Cap at total received audio duration — can't have played more than what was sent
            let audioEndMs = 0;
            if (voicePlaybackContext && voiceResponseStartTime > 0) {
                const elapsedMs = Math.max(0, Math.floor(
                    (voicePlaybackContext.currentTime - voiceResponseStartTime) * 1000
                ));
                audioEndMs = Math.min(elapsedMs, voiceTotalAudioDurationMs);
            }

            const itemID = msg.data?.item_id;
            const contentIndex = msg.data?.content_index || 0;

            addVoiceEvent('in', 'audio_interrupt', `item=${itemID || 'none'}, played=${audioEndMs}ms`);
            resetAudioPlayback();

            // Only set interrupted flag if we're mid-response (item_id present).
            // This drops late audio deltas from the cancelled response.
            // When item_id is empty, the response already completed — we just stop
            // buffered playback but don't block the next response's audio.
            if (itemID) {
                voiceInterrupted = true;
            }

            // Send truncate back to server
            if (voiceWs && voiceWs.readyState === WebSocket.OPEN && itemID) {
                voiceWs.send(JSON.stringify({
                    type: 'control',
                    data: {
                        action: 'truncate',
                        item_id: itemID,
                        content_index: contentIndex,
                        audio_end_ms: audioEndMs
                    }
                }));
                addVoiceEvent('out', 'control.truncate', `item=${itemID}, audio_end_ms=${audioEndMs}`);
            }
            break;
        }

        case 'tool_call':
            if (msg.data) {
                addVoiceEvent('in', 'tool_call', `${msg.data.name} (${msg.data.id})`);
                addVoiceLogEntry('system', `Calling tool: ${msg.data.name}`);
                resetAudioPlayback();
            }
            break;

        case 'tool_result':
            if (msg.data) {
                const status = msg.data.error ? 'failed' : 'completed';
                addVoiceEvent('in', 'tool_result', `${msg.data.name} ${status}`);
                addVoiceLogEntry('system', `Tool ${status}: ${msg.data.name}`);
            }
            break;

        case 'error':
            addVoiceEvent('in', 'ERROR', `${msg.data?.code}: ${msg.data?.message}`);
            console.error('Voice error:', msg.data);
            document.getElementById('voiceStatus').textContent = 'Error';
            showToast('Voice error: ' + (msg.data?.message || 'Unknown'), 'error');
            break;

        default:
            addVoiceEvent('in', msg.type, JSON.stringify(msg.data).substring(0, 80));
            break;
    }
}

// ==================== Audio Capture ====================

async function startAudioCapture() {
    // Guard: stop existing capture to prevent duplicate audio streams
    if (voiceProcessor) {
        voiceProcessor.disconnect();
        voiceProcessor = null;
    }
    if (voiceMediaStream) {
        voiceMediaStream.getTracks().forEach(track => track.stop());
        voiceMediaStream = null;
    }
    if (voiceAudioContext) {
        voiceAudioContext.close();
        voiceAudioContext = null;
    }

    try {
        // Capture at 24kHz — matches OpenAI Realtime API input rate.
        // Browser handles native->24k resampling internally. No server resampling needed.
        voiceAudioContext = new AudioContext({ sampleRate: 24000 });
        if (voiceAudioContext.state === 'suspended') {
            await voiceAudioContext.resume();
        }

        voiceMediaStream = await navigator.mediaDevices.getUserMedia({
            audio: {
                echoCancellation: true,
                noiseSuppression: true,
                autoGainControl: true,
            },
        });

        const source = voiceAudioContext.createMediaStreamSource(voiceMediaStream);
        voiceProcessor = voiceAudioContext.createScriptProcessor(4096, 1, 1);

        let audioOutCount = 0;
        voiceProcessor.onaudioprocess = (e) => {
            if (!voiceWs || voiceWs.readyState !== WebSocket.OPEN) return;

            const inputData = e.inputBuffer.getChannelData(0);
            const int16Data = float32ToInt16(inputData);
            const base64Data = int16ToBase64(int16Data);

            voiceWs.send(JSON.stringify({
                type: 'audio',
                data: {
                    chunk: base64Data,
                    sample_rate: 24000
                }
            }));

            audioOutCount++;
            if (audioOutCount === 1 || audioOutCount % 50 === 0) {
                addVoiceEvent('out', 'audio', `chunk #${audioOutCount} (${base64Data.length} b64 bytes)`);
            }
        };

        source.connect(voiceProcessor);
        // Route through silent gain — processor must connect to destination to fire,
        // but we don't want capture audio reaching speakers.
        const silentGain = voiceAudioContext.createGain();
        silentGain.gain.value = 0;
        voiceProcessor.connect(silentGain);
        silentGain.connect(voiceAudioContext.destination);

        addVoiceEvent('out', 'capture_started', `rate=${voiceAudioContext.sampleRate}Hz`);
    } catch (e) {
        console.error('Failed to start audio capture:', e);
        addVoiceEvent('out', 'capture_error', e.message);
        showToast('Microphone access denied', 'error');
    }
}

// ==================== Audio Playback ====================

function playAudioChunk(base64Audio) {
    if (!voicePlaybackContext) {
        voicePlaybackContext = new AudioContext({ sampleRate: 24000 });
    }
    if (voicePlaybackContext.state === 'suspended') {
        voicePlaybackContext.resume();
    }

    const float32Data = base64ToFloat32(base64Audio);
    const audioBuffer = voicePlaybackContext.createBuffer(1, float32Data.length, 24000);
    audioBuffer.getChannelData(0).set(float32Data);

    const source = voicePlaybackContext.createBufferSource();
    source.buffer = audioBuffer;
    source.connect(voicePlaybackContext.destination);

    // Track source for stopping on interrupt
    voiceActiveSources.push(source);
    source.onended = () => {
        voiceActiveSources = voiceActiveSources.filter(s => s !== source);
    };

    // Schedule for gapless playback
    const currentTime = voicePlaybackContext.currentTime;
    const startTime = Math.max(currentTime, voiceNextPlayTime);

    source.start(startTime);
    voiceNextPlayTime = startTime + audioBuffer.duration;

    // Track when the first chunk of this response started playing (for interrupt truncation)
    if (voiceResponseStartTime === 0) {
        voiceResponseStartTime = startTime;
    }

    // Track total audio duration received from server
    voiceTotalAudioDurationMs += Math.floor(audioBuffer.duration * 1000);

    voiceIsPlaying = true;
}

function resetAudioPlayback() {
    // Stop all currently playing/scheduled audio sources
    voiceActiveSources.forEach(source => {
        try { source.stop(); } catch (e) { /* already stopped */ }
    });
    voiceActiveSources = [];

    voiceNextPlayTime = 0;
    voiceIsPlaying = false;
    voiceResponseStartTime = 0;
    voiceTotalAudioDurationMs = 0;
    voiceCurrentItemID = null;
    voiceCurrentContentIndex = 0;
}

// ==================== Session Lifecycle ====================

function stopVoiceSession() {
    if (voiceWs) {
        voiceWs.close();
    }
    stopVoiceSessionInternal();
    showToast('Voice session stopped', 'info');
}

function stopVoiceSessionInternal() {
    voiceIsRecording = false;
    stopDurationTimer();

    if (voiceProcessor) {
        voiceProcessor.disconnect();
        voiceProcessor = null;
    }

    if (voiceMediaStream) {
        voiceMediaStream.getTracks().forEach(track => track.stop());
        voiceMediaStream = null;
    }

    if (voiceAudioContext) {
        voiceAudioContext.close();
        voiceAudioContext = null;
    }

    if (voicePlaybackContext) {
        voicePlaybackContext.close();
        voicePlaybackContext = null;
    }

    voiceWs = null;
    voiceAudioQueue = [];
    voiceActiveSources = [];
    resetAudioPlayback();
}

// ==================== UI Helpers ====================

function startDurationTimer() {
    voiceDurationInterval = setInterval(() => {
        if (voiceStartTime) {
            const elapsed = Math.floor((Date.now() - voiceStartTime) / 1000);
            const mins = Math.floor(elapsed / 60);
            const secs = elapsed % 60;
            document.getElementById('voiceDuration').textContent = `${mins}:${secs.toString().padStart(2, '0')}`;
        }
    }, 1000);
}

function stopDurationTimer() {
    if (voiceDurationInterval) {
        clearInterval(voiceDurationInterval);
        voiceDurationInterval = null;
    }
    voiceStartTime = null;
    document.getElementById('voiceDuration').textContent = '0:00';
}

function sendVoiceText() {
    const input = document.getElementById('voiceTextInput');
    const text = input.value.trim();

    if (!text) {
        showToast('Please enter a message', 'error');
        return;
    }

    if (!voiceWs || voiceWs.readyState !== WebSocket.OPEN) {
        showToast('No active voice session. Start a session first.', 'error');
        return;
    }

    voiceWs.send(JSON.stringify({
        type: 'text',
        data: { content: text }
    }));
    addVoiceEvent('out', 'text', `"${text}"`);
    input.value = '';
}

function addVoiceLogEntry(role, text) {
    const log = document.getElementById('voiceLog');
    if (!log) return;

    const emptyMsg = log.querySelector('.text-slate-400');
    if (emptyMsg && emptyMsg.textContent.includes('Start a voice session')) {
        emptyMsg.remove();
    }

    const entry = document.createElement('div');
    const colorClass = role === 'user' ? 'text-blue-600 dark:text-blue-400' :
                       role === 'assistant' ? 'text-emerald-600 dark:text-emerald-400' :
                       'text-slate-500 dark:text-slate-400';
    entry.className = `mb-2 ${colorClass}`;
    entry.innerHTML = `<span class="font-semibold">${role}:</span> ${escapeHtml(text)}`;
    log.appendChild(entry);
    log.scrollTop = log.scrollHeight;

    const countEl = document.getElementById('voiceMessages');
    if (countEl) {
        const count = parseInt(countEl.textContent) + 1;
        countEl.textContent = count;
    }
}

function updatePartialTranscript(text) {
    const log = document.getElementById('voiceLog');
    if (!log) return;

    let partial = log.querySelector('.voice-partial-transcript');
    if (!partial) {
        partial = document.createElement('div');
        partial.className = 'mb-2 text-blue-400 dark:text-blue-300 italic voice-partial-transcript';
        log.appendChild(partial);
    }
    partial.innerHTML = `<span class="font-semibold">user:</span> ${escapeHtml(text)}...`;
    log.scrollTop = log.scrollHeight;
}

function clearPartialTranscript() {
    const log = document.getElementById('voiceLog');
    if (!log) return;
    const partial = log.querySelector('.voice-partial-transcript');
    if (partial) partial.remove();
}
