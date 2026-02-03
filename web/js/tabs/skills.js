// Skills Tab Functions

let skillsData = [];
let editingSkillName = null;

// Initialize Skills tab (called by switchTab via init{TabName}Tab pattern)
async function initSkillsTab() {
    await loadSkillsData();
}

// Load all skills data
async function loadSkillsData() {
    try {
        const response = await makeRequest('/skills');
        if (response.status === 200 && response.data) {
            skillsData = response.data.skills || [];
            const enabled = response.data.enabled || false;

            // Update toggle
            document.getElementById('skillsEnabledToggle').checked = enabled;

            // Update stats
            const enabledCount = skillsData.filter(s => s.enabled).length;

            document.getElementById('skillsCount').textContent = skillsData.length;
            document.getElementById('skillsEnabledCount').textContent = enabledCount;

            renderSkillsList();
        }
    } catch (e) {
        console.error('Failed to load skills:', e);
        showToast('Failed to load skills', 'error');
    }
}

// Render skills list
function renderSkillsList() {
    const container = document.getElementById('skillsList');

    if (skillsData.length === 0) {
        container.innerHTML = `
            <div class="text-center py-8 text-slate-400">
                <div class="text-4xl mb-2">🎯</div>
                <p>No skills defined yet</p>
                <p class="text-sm mt-1">Click "Add Skill" to create your first skill</p>
            </div>
        `;
        return;
    }

    container.innerHTML = skillsData.map(skill => `
        <div class="border border-slate-200 dark:border-slate-600 rounded-lg p-4 ${skill.enabled ? '' : 'opacity-60'}">
            <div class="flex items-start justify-between">
                <div class="flex-1">
                    <div class="flex items-center gap-2 mb-1">
                        ${skill.icon ? `<span class="text-xl">${skill.icon}</span>` : ''}
                        <span class="font-semibold text-slate-800 dark:text-slate-100">${escapeHtml(skill.name)}</span>
                        ${skill.category ? `<span class="text-xs px-2 py-0.5 bg-slate-100 dark:bg-slate-700 rounded">${escapeHtml(skill.category)}</span>` : ''}
                        ${skill.enabled ?
                            '<span class="text-xs px-2 py-0.5 bg-green-100 text-green-700 rounded">enabled</span>' :
                            '<span class="text-xs px-2 py-0.5 bg-slate-100 text-slate-500 rounded">disabled</span>'
                        }
                    </div>
                    <p class="text-sm text-slate-600 dark:text-slate-300 mb-2">${escapeHtml(skill.description)}</p>
                    ${skill.tools && skill.tools.length > 0 ? `
                        <div class="flex flex-wrap gap-1">
                            <span class="text-xs text-slate-500">Tools:</span>
                            ${skill.tools.map(t => `<span class="text-xs px-2 py-0.5 bg-slate-100 dark:bg-slate-700 rounded">${escapeHtml(t)}</span>`).join('')}
                        </div>
                    ` : ''}
                </div>
                <div class="flex items-center gap-2 ml-4">
                    <button onclick="editSkill('${escapeHtml(skill.name)}')" class="p-1.5 text-slate-400 hover:text-blue-600" title="Edit">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>
                        </svg>
                    </button>
                    <button onclick="deleteSkill('${escapeHtml(skill.name)}')" class="p-1.5 text-slate-400 hover:text-red-600" title="Delete">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
                        </svg>
                    </button>
                </div>
            </div>
        </div>
    `).join('');
}

// Toggle skills enabled globally
async function toggleSkillsEnabled() {
    const enabled = document.getElementById('skillsEnabledToggle').checked;
    try {
        console.log('Toggling skills enabled:', enabled);
        const response = await makeRequest('/skills/status', 'POST', { enabled });
        console.log('Toggle response:', response);
        if (response.status === 200 && response.data?.success) {
            showToast(`Skills ${enabled ? 'enabled' : 'disabled'}`, 'success');
        } else {
            showToast(response.data?.error || 'Failed to toggle skills', 'error');
            document.getElementById('skillsEnabledToggle').checked = !enabled;
        }
    } catch (e) {
        console.error('Failed to toggle skills:', e);
        showToast('Failed to toggle skills', 'error');
        // Revert toggle
        document.getElementById('skillsEnabledToggle').checked = !enabled;
    }
}

// Show add skill modal
function showAddSkillModal() {
    editingSkillName = null;
    document.getElementById('skillModalTitle').textContent = 'Add Skill';

    // Clear form
    document.getElementById('skillName').value = '';
    document.getElementById('skillDescription').value = '';
    document.getElementById('skillIcon').value = '';
    document.getElementById('skillCategory').value = '';
    document.getElementById('skillTools').value = '';
    document.getElementById('skillInstructions').value = '';
    document.getElementById('skillEnabled').checked = true;

    // Enable name field for new skills
    document.getElementById('skillName').disabled = false;

    // Show modal
    document.getElementById('skillModal').classList.remove('hidden');
    document.getElementById('skillModal').classList.add('flex');
}

// Hide skill modal
function hideSkillModal() {
    document.getElementById('skillModal').classList.add('hidden');
    document.getElementById('skillModal').classList.remove('flex');
    editingSkillName = null;
}

// Edit skill
function editSkill(name) {
    const skill = skillsData.find(s => s.name === name);
    if (!skill) return;

    editingSkillName = name;
    document.getElementById('skillModalTitle').textContent = 'Edit Skill';

    // Fill form
    document.getElementById('skillName').value = skill.name;
    document.getElementById('skillDescription').value = skill.description || '';
    document.getElementById('skillIcon').value = skill.icon || '';
    document.getElementById('skillCategory').value = skill.category || '';
    document.getElementById('skillTools').value = (skill.tools || []).join(', ');
    document.getElementById('skillInstructions').value = skill.instructions || '';
    document.getElementById('skillEnabled').checked = skill.enabled !== false;

    // Disable name field for existing skills
    document.getElementById('skillName').disabled = true;

    // Show modal
    document.getElementById('skillModal').classList.remove('hidden');
    document.getElementById('skillModal').classList.add('flex');
}

// Save skill
async function saveSkill() {
    const name = document.getElementById('skillName').value.trim();
    const description = document.getElementById('skillDescription').value.trim();
    const instructions = document.getElementById('skillInstructions').value.trim();

    if (!name || !description || !instructions) {
        showToast('Name, description, and instructions are required', 'error');
        return;
    }

    const skill = {
        name,
        description,
        instructions,
        icon: document.getElementById('skillIcon').value.trim(),
        category: document.getElementById('skillCategory').value.trim(),
        tools: document.getElementById('skillTools').value.split(',').map(t => t.trim()).filter(t => t),
        enabled: document.getElementById('skillEnabled').checked
    };

    try {
        const method = editingSkillName ? 'PUT' : 'POST';
        const response = await makeRequest('/skills', method, skill);

        if (response.data && response.data.success) {
            showToast(`Skill ${editingSkillName ? 'updated' : 'created'} successfully`, 'success');
            hideSkillModal();
            await loadSkillsData();
        } else {
            showToast(response.data?.error || 'Failed to save skill', 'error');
        }
    } catch (e) {
        console.error('Failed to save skill:', e);
        showToast('Failed to save skill', 'error');
    }
}

// Delete skill
async function deleteSkill(name) {
    if (!confirm(`Are you sure you want to delete the skill "${name}"?`)) {
        return;
    }

    try {
        const response = await makeRequest(`/skills?name=${encodeURIComponent(name)}`, 'DELETE');

        if (response.data && response.data.success) {
            showToast('Skill deleted', 'success');
            await loadSkillsData();
        } else {
            showToast(response.data?.error || 'Failed to delete skill', 'error');
        }
    } catch (e) {
        console.error('Failed to delete skill:', e);
        showToast('Failed to delete skill', 'error');
    }
}

// Test trigger matching
async function testSkillMatching() {
    const input = document.getElementById('skillsTestInput').value.trim();
    if (!input) {
        showToast('Enter a test message', 'error');
        return;
    }

    try {
        const response = await makeRequest('/skills/match', 'POST', { input });

        const resultDiv = document.getElementById('skillsTestResult');

        if (response.data && response.data.success) {
            const matched = response.data.matched || [];
            if (matched.length === 0) {
                resultDiv.innerHTML = `<span class="text-slate-500">No skills matched for: "${escapeHtml(input)}"</span>`;
            } else {
                resultDiv.innerHTML = `
                    <div class="text-green-600 dark:text-green-400 mb-2">
                        Matched ${matched.length} skill(s):
                    </div>
                    <div class="flex flex-wrap gap-2">
                        ${matched.map(name => {
                            const skill = skillsData.find(s => s.name === name);
                            return `<span class="px-3 py-1 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400 rounded-lg">
                                ${skill?.icon || '🎯'} ${escapeHtml(name)}
                            </span>`;
                        }).join('')}
                    </div>
                `;
            }
        } else {
            resultDiv.innerHTML = `<span class="text-red-500">Error: ${escapeHtml(response.data?.error || 'Unknown error')}</span>`;
        }
    } catch (e) {
        console.error('Failed to test triggers:', e);
        document.getElementById('skillsTestResult').innerHTML = `<span class="text-red-500">Error testing triggers</span>`;
    }
}

// Utility function to escape HTML
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
