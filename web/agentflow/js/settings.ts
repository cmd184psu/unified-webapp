// settings.ts — Settings panel: model profiles, Ollama hosts, timeouts, prompt directory.

import { api } from './api.js';
import { alertDialog, confirmDialog } from '@shared';

interface ProfileConfig {
  name: string;
  ollama_host: string;
  worker_model: string;
  planner_model: string;
  review_model: string;
  timeout_secs: number;
  prompt_dir: string;
}

let settings: any = null;
let editingProfile: string | null = null;

async function loadSettings(): Promise<void> {
  try {
    const resp = await fetch('/settings');
    if (resp.ok) {
      settings = await resp.json();
    }
  } catch {
    settings = {
      default_profile: 'm5',
      profiles: [],
      prompt_dir: './prompts',
      timeout_secs: 300,
    };
  }
}

function renderSettings(): void {
  const host = document.getElementById('settings-host');
  if (!host) return;

  if (!settings) {
    host.innerHTML = '<p>Loading settings...</p>';
    return;
  }

  host.innerHTML = `
    <div class="settings-section">
      <h4>Default Profile</h4>
      <select id="default-profile" class="settings-select">
        ${settings.profiles.map((p: ProfileConfig) => `
          <option value="${p.name}" ${p.name === settings.default_profile ? 'selected' : ''}>${p.name}</option>
        `).join('')}
      </select>
    </div>

    <div class="settings-section">
      <div class="section-header">
        <h4>Model Profiles</h4>
        <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="add-profile">Add Profile</button>
      </div>
      <div id="profile-list" class="profile-list">
        ${settings.profiles.map((p: ProfileConfig) => renderProfileCard(p)).join('')}
      </div>
    </div>

    <div class="settings-section">
      <h4>Prompt Directory</h4>
      <input type="text" id="prompt-dir" class="settings-input" value="${settings.prompt_dir || './prompts'}">
    </div>

    <div class="settings-section">
      <h4>Default Timeout (seconds)</h4>
      <input type="number" id="timeout-secs" class="settings-input" value="${settings.timeout_secs || 300}" min="30" max="3600">
    </div>

    <div class="settings-actions">
      <button type="button" class="ui-modal-btn" id="reset-settings">Reset to Defaults</button>
      <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="save-settings">Save Settings</button>
    </div>
  `;

  // Event listeners
  const profileSelect = document.getElementById('default-profile') as HTMLSelectElement;
  profileSelect?.addEventListener('change', () => {
    settings.default_profile = profileSelect.value;
  });

  const promptDir = document.getElementById('prompt-dir') as HTMLInputElement;
  promptDir?.addEventListener('input', () => {
    settings.prompt_dir = promptDir.value;
  });

  const timeoutSecs = document.getElementById('timeout-secs') as HTMLInputElement;
  timeoutSecs?.addEventListener('input', () => {
    settings.timeout_secs = parseInt(timeoutSecs.value, 10) || 300;
  });

  document.getElementById('add-profile')?.addEventListener('click', () => openProfileEditor(null));
  document.getElementById('save-settings')?.addEventListener('click', saveSettings);
  document.getElementById('reset-settings')?.addEventListener('click', () => confirmReset());

  renderProfileList();
}

function renderProfileCard(p: any): string {
  return `
    <div class="profile-card" data-name="${p.name}">
      <div class="profile-header">
        <strong>${p.name}</strong>
        <div class="profile-actions">
          <button type="button" class="icon-btn edit-profile" data-name="${p.name}" title="Edit">✎</button>
          <button type="button" class="icon-btn delete-profile" data-name="${p.name}" title="Delete">✕</button>
        </div>
      </div>
      <div class="profile-fields">
        <div><label>Ollama Host</label><input type="text" class="field-ollama" value="${p.ollama_host}" disabled></div>
        <div><label>Worker Model</label><input type="text" class="field-worker" value="${p.worker_model}" disabled></div>
        <div><label>Planner Model</label><input type="text" class="field-planner" value="${p.planner_model}" disabled></div>
        <div><label>Review Model</label><input type="text" class="field-review" value="${p.review_model}" disabled></div>
        <div><label>Timeout (s)</label><input type="number" class="field-timeout" value="${p.timeout_secs}" disabled></div>
        <div><label>Prompt Dir</label><input type="text" class="field-promptdir" value="${p.prompt_dir || ''}" disabled></div>
      </div>
    </div>
  `;
}

function renderProfileList(): void {
  const host = document.getElementById('profile-list');
  if (!host || !settings) return;

  host.innerHTML = settings.profiles.map((p: any) => renderProfileCard(p)).join('');

  host.querySelectorAll('.edit-profile').forEach(btn => {
    btn.addEventListener('click', () => openProfileEditor(btn.getAttribute('data-name')!));
  });

  host.querySelectorAll('.delete-profile').forEach(btn => {
    btn.addEventListener('click', async () => {
      const name = btn.getAttribute('data-name')!;
      if (await confirmDialog(`Delete profile "${name}"?`)) {
        settings.profiles = settings.profiles.filter((p: any) => p.name !== name);
        if (settings.default_profile === name) {
          settings.default_profile = settings.profiles[0]?.name || 'm5';
        }
        renderSettings();
      }
    });
  });
}

function openProfileEditor(name: string | null): void {
  const profile = name ? settings.profiles.find((p: any) => p.name === name) : null;
  const isNew = !profile;

  const form = document.createElement('div');
  form.className = 'profile-editor-modal';
  form.innerHTML = `
    <div class="modal-overlay"></div>
    <div class="modal-content">
      <h3>${isNew ? 'Add' : 'Edit'} Profile</h3>
      <form id="profile-form">
        <div class="form-field">
          <label>Profile Name</label>
          <input type="text" name="name" value="${profile?.name || ''}" ${!isNew ? 'readonly' : ''} required pattern="^[a-z0-9][a-z0-9_-]{0,31}$">
        </div>
        <div class="form-field">
          <label>Ollama Host URL</label>
          <input type="url" name="ollama_host" value="${profile?.ollama_host || 'http://'}" required>
        </div>
        <div class="form-field">
          <label>Worker Model</label>
          <input type="text" name="worker_model" value="${profile?.worker_model || ''}" required>
        </div>
        <div class="form-field">
          <label>Planner Model</label>
          <input type="text" name="planner_model" value="${profile?.planner_model || ''}" required>
        </div>
        <div class="form-field">
          <label>Review Model</label>
          <input type="text" name="review_model" value="${profile?.review_model || ''}" required>
        </div>
        <div class="form-field">
          <label>Timeout (seconds)</label>
          <input type="number" name="timeout_secs" value="${profile?.timeout_secs || 300}" min="30" max="3600">
        </div>
        <div class="form-field">
          <label>Prompt Directory (optional)</label>
          <input type="text" name="prompt_dir" value="${profile?.prompt_dir || ''}">
        </div>
        <div class="modal-actions">
          <button type="button" class="ui-modal-btn" id="cancel-profile">Cancel</button>
          <button type="submit" class="ui-modal-btn ui-modal-btn-primary">${isNew ? 'Add' : 'Save'}</button>
        </div>
      </form>
    </div>
  `;

  document.body.appendChild(form);

  form.querySelector('#cancel-profile')?.addEventListener('click', () => form.remove());
  form.querySelector('#profile-form')?.addEventListener('submit', (e) => {
    e.preventDefault();
    const fd = new FormData(e.target as HTMLFormElement);
    const newProfile = {
      name: fd.get('name'),
      ollama_host: fd.get('ollama_host'),
      worker_model: fd.get('worker_model'),
      planner_model: fd.get('planner_model'),
      review_model: fd.get('review_model'),
      timeout_secs: parseInt(fd.get('timeout_secs') as string, 10) || 300,
      prompt_dir: fd.get('prompt_dir') as string || '',
    };

    if (isNew) {
      if (settings.profiles.some((p: any) => p.name === newProfile.name)) {
        alert('Profile name already exists');
        return;
      }
      settings.profiles.push(newProfile);
    } else {
      const idx = settings.profiles.findIndex((p: any) => p.name === profile!.name);
      if (idx >= 0) settings.profiles[idx] = newProfile;
    }

    form.remove();
    renderSettings();
  });
}

async function saveSettings(): Promise<void> {
  try {
    const resp = await fetch('/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(settings),
    });
    if (!resp.ok) throw new Error('Failed to save settings');
    await alertDialog('Settings saved');
  } catch (err) {
    await alertDialog('Failed to save settings: ' + (err as Error).message);
  }
}

async function confirmReset(): Promise<void> {
  if (await confirmDialog('Reset all settings to defaults? This cannot be undone.')) {
    settings = {
      default_profile: 'm5',
      profiles: [
        {
          name: 'm5',
          ollama_host: 'http://192.168.1.239:11434',
          worker_model: 'qwen3.6:35b-a3b-mxfp8',
          planner_model: 'qwen3.6:35b-a3b-mxfp8',
          review_model: 'mistral-small3.2',
          timeout_secs: 300,
        },
        {
          name: 'm4',
          ollama_host: 'http://192.168.1.11:11434',
          worker_model: 'qwen3.6:27b-q4_K_M',
          planner_model: 'qwen3.6:27b-q4_K_M',
          review_model: 'qwen2.5-coder:14b',
          timeout_secs: 300,
        },
        {
          name: 'windows',
          ollama_host: 'http://windows.cmdhome.net:11434',
          worker_model: 'qwen3-coder:30b',
          planner_model: 'qwen3-coder:30b',
          review_model: 'mistral-small3.2',
          timeout_secs: 300,
        },
      ],
      prompt_dir: './prompts',
      timeout_secs: 300,
    };
    renderSettings();
  }
}

export async function initSettings(): Promise<void> {
  const host = document.getElementById('settings-host');
  if (!host) return;

  await loadSettings();
  renderSettings();
}