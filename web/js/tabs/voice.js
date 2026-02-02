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

// Voice options for each provider
const OPENAI_VOICES = [
    { value: 'alloy', label: 'Alloy' },
    { value: 'ash', label: 'Ash' },
    { value: 'ballad', label: 'Ballad' },
    { value: 'cedar', label: 'Cedar' },
    { value: 'coral', label: 'Coral' },
    { value: 'echo', label: 'Echo' },
    { value: 'marin', label: 'Marin' },
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

function updateVoiceOptions(selectedVoice) {
    const providerSelect = document.getElementById('voiceProviderSelect');
    const voiceSelect = document.getElementById('voiceSelect');
    if (!providerSelect || !voiceSelect) return;

    const provider = providerSelect.value;
    const voices = provider === 'gemini' ? GEMINI_VOICES : OPENAI_VOICES;

    // Clear and repopulate
    voiceSelect.innerHTML = '';
    voices.forEach(v => {
        const option = document.createElement('option');
        option.value = v.value;
        option.textContent = v.label;
        voiceSelect.appendChild(option);
    });

    // Set voice value if provided
    if (selectedVoice) {
        voiceSelect.value = selectedVoice;
    } else if (voiceConfig) {
        // Use config values
        if (provider === 'gemini' && voiceConfig.gemini_voice) {
            voiceSelect.value = voiceConfig.gemini_voice;
        } else if (voiceConfig.voice) {
            voiceSelect.value = voiceConfig.voice;
        }
    }
}

async function initVoiceTab() {
    // Load config and set voice tab values from realtime config
    try {
        const response = await makeRequest('/config');
        if (response.status === 200 && response.data) {
            const rt = response.data.realtime || {};
            voiceConfig = rt;

            const provider = rt.provider || 'openai';

            // Set provider dropdown
            const providerSelect = document.getElementById('voiceProviderSelect');
            if (providerSelect) {
                providerSelect.value = provider;
            }

            // Update status panel provider display
            const providerDisplay = document.getElementById('voiceProvider');
            if (providerDisplay) {
                providerDisplay.textContent = provider === 'gemini' ? 'Gemini' : 'OpenAI';
            }

            // Update voice options based on provider, then set voice
            const voice = provider === 'gemini' ? rt.gemini_voice : rt.voice;
            updateVoiceOptions(voice);
        }
    } catch (e) {
        console.error('Failed to load voice config:', e);
    }
}

// Audio conversion helpers
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

async function startVoiceSession() {
    const provider = document.getElementById('voiceProviderSelect').value;
    const voice = document.getElementById('voiceSelect').value;

    document.getElementById('voiceStatus').textContent = 'Connecting...';

    try {
        // Build WebSocket URL with API key if configured
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        let wsUrl = `${protocol}//${window.location.host}/voice`;
        if (getApiKey()) {
            wsUrl += `?api_key=${encodeURIComponent(getApiKey())}`;
        }

        voiceWs = new WebSocket(wsUrl);

        voiceWs.onopen = () => {
            document.getElementById('voiceStatus').textContent = 'Connected';
            document.getElementById('voiceProvider').textContent = provider === 'openai' ? 'OpenAI' : 'Gemini';

            // Send initial message to trigger session creation
            // Server waits for first message to detect format (JSON vs binary)
            voiceWs.send(JSON.stringify({
                type: 'start',
                data: {
                    provider: provider,
                    voice: voice
                }
            }));
            console.log('Sent start message to voice server');
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
            showToast('WebSocket connection failed', 'error');
        };

        voiceWs.onclose = () => {
            document.getElementById('voiceStatus').textContent = 'Disconnected';
            stopVoiceSessionInternal();
        };

    } catch (error) {
        document.getElementById('voiceStatus').textContent = 'Error';
        showToast('Failed to connect: ' + error.message, 'error');
    }
}

async function handleVoiceMessage(msg) {
    console.log('Voice message:', msg.type);

    switch (msg.type) {
        case 'session_created':
            document.getElementById('voiceStatus').textContent = 'Active';
            voiceStartTime = Date.now();
            startDurationTimer();
            voiceIsRecording = true;
            showToast('Voice session started', 'success');

            // Start audio capture
            await startAudioCapture();
            break;

        case 'audio_delta':
            if (msg.data && msg.data.chunk) {
                playAudioChunk(msg.data.chunk);
            }
            break;

        case 'transcript':
            if (msg.data) {
                addVoiceLogEntry(msg.data.role, msg.data.content);
            }
            break;

        case 'tool_call':
            if (msg.data) {
                addVoiceLogEntry('system', `Calling tool: ${msg.data.name}`);
            }
            break;

        case 'tool_result':
            if (msg.data) {
                const status = msg.data.error ? 'failed' : 'completed';
                addVoiceLogEntry('system', `Tool ${status}: ${msg.data.call_id}`);
            }
            break;

        case 'error':
            console.error('Voice error:', msg.data);
            document.getElementById('voiceStatus').textContent = 'Error';
            showToast('Voice error: ' + (msg.data?.message || 'Unknown'), 'error');
            break;
    }
}

// Resample audio from source rate to target rate
function resampleAudio(inputData, inputSampleRate, outputSampleRate) {
    if (inputSampleRate === outputSampleRate) {
        return inputData;
    }
    const ratio = inputSampleRate / outputSampleRate;
    const outputLength = Math.floor(inputData.length / ratio);
    const output = new Float32Array(outputLength);
    for (let i = 0; i < outputLength; i++) {
        const srcIndex = i * ratio;
        const srcIndexFloor = Math.floor(srcIndex);
        const srcIndexCeil = Math.min(srcIndexFloor + 1, inputData.length - 1);
        const t = srcIndex - srcIndexFloor;
        output[i] = inputData[srcIndexFloor] * (1 - t) + inputData[srcIndexCeil] * t;
    }
    return output;
}

async function startAudioCapture() {
    try {
        // Capture at native rate (browser default), we'll resample to 16kHz for Gemini
        voiceAudioContext = new AudioContext();
        const nativeSampleRate = voiceAudioContext.sampleRate;
        console.log('Native sample rate:', nativeSampleRate);

        voiceMediaStream = await navigator.mediaDevices.getUserMedia({ audio: true });

        const source = voiceAudioContext.createMediaStreamSource(voiceMediaStream);

        // Create processor for audio chunks (smaller buffer = lower latency)
        voiceProcessor = voiceAudioContext.createScriptProcessor(2048, 1, 1);
        voiceProcessor.onaudioprocess = (e) => {
            if (!voiceWs || voiceWs.readyState !== WebSocket.OPEN) return;

            const inputData = e.inputBuffer.getChannelData(0);

            // Resample to 16kHz for Gemini
            const resampledData = resampleAudio(inputData, nativeSampleRate, 16000);
            const int16Data = float32ToInt16(resampledData);
            const base64Data = int16ToBase64(int16Data);

            // Send to server
            voiceWs.send(JSON.stringify({
                type: 'audio',
                data: {
                    chunk: base64Data
                }
            }));
        };

        source.connect(voiceProcessor);
        voiceProcessor.connect(voiceAudioContext.destination);

        console.log('Audio capture started, resampling from', nativeSampleRate, 'to 16000');
    } catch (e) {
        console.error('Failed to start audio capture:', e);
        showToast('Microphone access denied', 'error');
    }
}

function playAudioChunk(base64Audio) {
    // Use separate playback context at 24kHz (Gemini output rate)
    if (!voicePlaybackContext) {
        voicePlaybackContext = new AudioContext({ sampleRate: 24000 });
    }

    // Resume if suspended (browser autoplay policy)
    if (voicePlaybackContext.state === 'suspended') {
        voicePlaybackContext.resume();
    }

    const float32Data = base64ToFloat32(base64Audio);
    const audioBuffer = voicePlaybackContext.createBuffer(1, float32Data.length, 24000);
    audioBuffer.getChannelData(0).set(float32Data);

    const source = voicePlaybackContext.createBufferSource();
    source.buffer = audioBuffer;
    source.connect(voicePlaybackContext.destination);

    // Schedule for gapless playback
    const currentTime = voicePlaybackContext.currentTime;
    const startTime = Math.max(currentTime, voiceNextPlayTime);

    source.start(startTime);
    voiceNextPlayTime = startTime + audioBuffer.duration;

    voiceIsPlaying = true;
}

function resetAudioPlayback() {
    voiceNextPlayTime = 0;
    voiceIsPlaying = false;
}

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
    resetAudioPlayback();
}

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

    // Send text message
    voiceWs.send(JSON.stringify({
        type: 'text',
        data: {
            content: text
        }
    }));

    // Add to log as user message
    addVoiceLogEntry('user', text);

    // Clear input
    input.value = '';
    console.log('Sent text message:', text);
}

function addVoiceLogEntry(role, text) {
    const log = document.getElementById('voiceLog');
    if (!log) return;

    // Remove empty state message if present
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
