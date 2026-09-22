import { ThemeManager, HamburgerMenu } from '@shared';

const themes = new ThemeManager({ module: 'utuber', default: 'dark' });
themes.apply();

const hamburger = new HamburgerMenu({
  title: 'uTuber',
  items: [
    { section: 'Settings' },
    {
      id: 'python-setting',
      render(host: HTMLElement) {
        const field = document.createElement('div');
        field.className = 'field';
        const label = document.createElement('label');
        label.htmlFor = 'python-bin';
        label.textContent = 'Python interpreter';
        const input = document.createElement('input');
        input.type = 'text';
        input.id = 'python-bin';
        input.placeholder = 'python3.12';
        input.autocomplete = 'off';
        input.spellcheck = false;
        const hint = document.createElement('span');
        hint.className = 'optional-hint';
        hint.textContent = 'Used by "Update yt-dlp". Blank resets to the server default.';
        field.append(label, input, hint);
        const submitRow = document.createElement('div');
        submitRow.className = 'submit-row';
        const saveBtn = document.createElement('button');
        saveBtn.className = 'btn btn-ghost btn-sm';
        saveBtn.textContent = 'Save';
        saveBtn.addEventListener('click', saveSettings);
        const status = document.createElement('span');
        status.id = 'settings-status';
        status.style.fontSize = '0.75rem';
        status.style.color = 'var(--color-text-faint)';
        submitRow.append(saveBtn, status);
        host.append(field, submitRow);
        loadSettings();
      },
    },
    { separator: true as const },
    {
      id: 'ytdlp-update',
      render(host: HTMLElement) {
        const btn = document.createElement('button');
        btn.className = 'btn btn-ghost btn-sm';
        btn.id = 'update-btn';
        btn.textContent = 'Update yt-dlp';
        btn.addEventListener('click', runYtdlpUpdate);
        const status = document.createElement('span');
        status.id = 'update-status';
        status.style.fontSize = '0.75rem';
        status.style.color = 'var(--color-text-faint)';
        const log = document.createElement('div');
        log.id = 'update-log';
        log.className = 'update-log';
        host.append(btn, status, log);
      },
    },
  ],
  themePicker: true,
  themes,
  mountTrigger: document.getElementById('settings-btn')!,
});

let currentMode = 'video';
let pendingFormData: FormData | null = null;

function setMode(mode: string): void {
  currentMode = mode;
  (document.getElementById('mode-input') as HTMLInputElement).value = mode;

  document.querySelectorAll('.mode-tab').forEach(t => {
    (t as HTMLElement).classList.toggle('active', (t as HTMLElement).dataset.mode === mode);
  });

  const btn = document.getElementById('submit-btn') as HTMLButtonElement;
  btn.className = mode === 'audio' ? 'btn btn-audio' : 'btn btn-video';
}

function hideDupBanner(): void {
  const b = document.getElementById('dup-banner')!;
  b.style.display = 'none';
  b.innerHTML = '';
  pendingFormData = null;
}

function showDupBanner(data: { output_file?: string; show_name?: string }, formData: FormData): void {
  pendingFormData = formData;
  const b = document.getElementById('dup-banner')!;
  const fname = data.output_file || '(unknown)';
  b.innerHTML = `
    <div>
      <strong>Already downloaded.</strong>
      "${esc(data.show_name)}" was previously saved as
      <a href="/downloads/${encodeURIComponent(fname)}">${esc(fname)}</a>.
    </div>
    <div class="dup-actions">
      <button class="dup-btn" id="dup-force-btn">Download again anyway</button>
      <button class="dup-btn dismiss" id="dup-dismiss-btn">Dismiss</button>
    </div>`;
  b.style.display = 'block';
  document.getElementById('dup-force-btn')!.addEventListener('click', forceEnqueue);
  document.getElementById('dup-dismiss-btn')!.addEventListener('click', hideDupBanner);
}

async function forceEnqueue(): Promise<void> {
  if (!pendingFormData) return;
  pendingFormData.set('force', '1');
  await submitForm(pendingFormData);
  hideDupBanner();
}

function showErrToast(msg: string): void {
  const t = document.getElementById('err-toast')!;
  t.textContent = msg;
  t.style.display = 'block';
}
function hideErrToast(): void {
  const t = document.getElementById('err-toast')!;
  t.style.display = 'none';
  t.textContent = '';
}

async function submitForm(formData: FormData): Promise<void> {
  hideErrToast();
  let res: Response;
  try {
    res = await fetch('/enqueue', { method: 'POST', body: formData });
  } catch (err) {
    showErrToast('Could not reach server: ' + (err as Error).message);
    return;
  }
  if (res.status === 409) {
    const data = await res.json();
    showDupBanner(data, formData);
    return;
  }
  if (res.status === 204) {
    (document.querySelector('input[name="url"]') as HTMLInputElement).value = '';
    hideDupBanner();
    refreshJobs();
    return;
  }
  const body = await res.text().catch(() => '');
  showErrToast(`Server error ${res.status}${body ? ': ' + body.trim() : ''}`);
}

document.getElementById('enqueue-form')!.addEventListener('submit', async (e: Event) => {
  e.preventDefault();
  hideDupBanner();
  await submitForm(new FormData(e.target as HTMLFormElement));
});

interface Job {
  ShowName: string;
  EpisodeTitle: string;
  Season: number;
  Episode: number;
  Mode?: string;
  Progress: string;
  Status: string;
  OutputFile: string;
  Error?: string;
}

async function refreshJobs(): Promise<void> {
  const res = await fetch('/jobs.json');
  const jobs: Job[] = await res.json();

  const list = document.getElementById('jobs-list')!;
  const countEl = document.getElementById('jobs-count')!;

  if (!jobs || jobs.length === 0) {
    list.innerHTML = '<div class="empty-state">No jobs yet — add one above.</div>';
    countEl.textContent = '';
    return;
  }

  countEl.textContent = jobs.length + (jobs.length === 1 ? ' job' : ' jobs');

  list.innerHTML = jobs.map(j => jobRow(j)).join('');
}

function jobRow(j: Job): string {
  const epLabel = `S${String(j.Season).padStart(2, '0')}E${String(j.Episode).padStart(2, '0')}`;
  const mode = j.Mode || 'video';
  const { barClass, pct, label } = parseProgress(j.Progress, j.Status);
  const statusClass = 'status-' + (j.Status || 'queued');
  const isAudio = mode === 'audio';

  const dlLink = j.Status === 'completed'
    ? `<div class="dl-actions">
        <a class="download-link ${isAudio ? 'audio-dl' : ''}" href="/downloads/${encodeURIComponent(j.OutputFile)}" target="_blank" rel="noopener">
          <svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor">
            <path d="M6 4l10 6-10 6V4z"/>
          </svg>
          Play
        </a>
        <a class="download-link ${isAudio ? 'audio-dl' : ''}" href="/downloads/${encodeURIComponent(j.OutputFile)}" download>
          <svg width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <path d="M10 3v10M5 13l5 5 5-5"/><line x1="3" y1="19" x2="17" y2="19"/>
          </svg>
          Download
        </a>
      </div>`
    : '';

  const errLine = j.Error
    ? `<div class="job-error" title="${esc(j.Error)}">⚠ ${esc(j.Error)}</div>`
    : '';

  return `
    <div class="job-item">
      <div class="job-meta">
        <div class="job-name">${esc(j.ShowName)} — ${esc(j.EpisodeTitle)}</div>
        <div class="job-sub">
          <span>${epLabel}</span>
          <span class="mode-badge ${mode}">${mode}</span>
        </div>
        ${errLine}
      </div>
      <div class="job-right">
        <span class="status-pill ${statusClass}">${j.Status}</span>
        ${barClass ? `
        <div class="progress-wrap">
          <div class="progress-bar ${barClass}" style="width:${pct}%"></div>
        </div>
        <span class="progress-label">${label}</span>
        ` : ''}
        ${dlLink}
      </div>
    </div>`;
}

function parseProgress(p: string, status: string): { barClass: string; pct: number; label: string } {
  if (status === 'completed') return { barClass: 'done', pct: 100, label: '' };
  if (!p) return { barClass: '', pct: 0, label: '' };

  if (p.startsWith('download')) {
    const pct = parseFloat(p.split(' ')[1]) || 0;
    return { barClass: 'dl', pct, label: `Downloading ${pct.toFixed(0)}%` };
  }
  if (p.startsWith('convert') || p === 'converting') {
    const pct = parseFloat(p.split(' ')[1]) || 0;
    return { barClass: 'conv', pct: pct || 50, label: 'Converting…' };
  }
  if (p === 'done') return { barClass: 'done', pct: 100, label: '' };

  return { barClass: '', pct: 0, label: p };
}

function esc(s: string | undefined): string {
  return String(s || '')
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function runYtdlpUpdate(): void {
  const btn = document.getElementById('update-btn') as HTMLButtonElement;
  const status = document.getElementById('update-status')!;
  const log = document.getElementById('update-log')!;

  btn.disabled = true;
  btn.textContent = 'Updating…';
  status.textContent = '';
  log.textContent = '';
  log.style.display = 'block';

  const es = new EventSource('/ytdlp-update');
  es.onmessage = (e: MessageEvent) => {
    if (e.data === '__done__') {
      es.close();
      btn.disabled = false;
      btn.textContent = 'Update yt-dlp';
      status.textContent = 'Done.';
      return;
    }
    if (e.data.startsWith('ERROR:')) {
      log.textContent += e.data + '\n';
      es.close();
      btn.disabled = false;
      btn.textContent = 'Update yt-dlp';
      status.style.color = 'var(--color-danger)';
      status.textContent = 'Update failed.';
      return;
    }
    log.textContent += e.data + '\n';
    log.scrollTop = log.scrollHeight;
  };
  es.onerror = () => {
    es.close();
    btn.disabled = false;
    btn.textContent = 'Update yt-dlp';
    status.style.color = 'var(--color-danger)';
    status.textContent = 'Connection error.';
  };
}

async function loadSettings(): Promise<void> {
  const status = document.getElementById('settings-status')!;
  try {
    const res = await fetch('/settings.json');
    const data = await res.json();
    (document.getElementById('python-bin') as HTMLInputElement).value = data.python_bin || '';
    status.textContent = '';
  } catch {
    status.style.color = 'var(--color-danger)';
    status.textContent = 'Connection error.';
  }
}

async function saveSettings(): Promise<void> {
  const status = document.getElementById('settings-status')!;
  const input = document.getElementById('python-bin') as HTMLInputElement;
  try {
    const res = await fetch('/settings.json', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ python_bin: input.value }),
    });
    if (res.ok) {
      const data = await res.json();
      input.value = data.python_bin || '';
      status.style.color = 'var(--color-success)';
      status.textContent = 'Saved.';
    } else {
      status.style.color = 'var(--color-danger)';
      status.textContent = await res.text();
    }
  } catch {
    status.style.color = 'var(--color-danger)';
    status.textContent = 'Connection error.';
  }
}

// Wire up inline onclick handlers that were in the HTML
document.querySelectorAll<HTMLButtonElement>('.mode-tab').forEach(tab => {
  tab.addEventListener('click', () => setMode(tab.dataset.mode!));
});
setInterval(refreshJobs, 2000);
refreshJobs();
