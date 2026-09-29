import { ThemeManager, HamburgerMenu, confirmDialog, showToast, QueuePanel, openOutputModal, createToggle } from '@shared';
import type { QueuePanelAdapter, QueueActionKind, QueueSection, QueueProgress } from '@shared';

const themes = new ThemeManager({ module: 'utuber', default: 'dark' });
themes.apply();

const hamburger = new HamburgerMenu({
  title: 'uTuber',
  items: [
    { section: 'Queue' },
    {
      id: 'queue-setting',
      render(host: HTMLElement) {
        host.appendChild(numberField('age-out-days', 'Age-out (days)', 1, 365,
          v => saveQueueSetting({ age_out_days: v })));
        host.appendChild(numberField('concurrent-downloads', 'Concurrent downloads', 1, 8,
          v => saveQueueSetting({ concurrent_downloads: v })));

        host.appendChild(createToggle({
          id: 'show-in-taskmaster',
          checked: false,
          label: 'Show in taskmaster',
          onChange: checked => void saveQueueSetting({ show_in_taskmaster: checked }),
        }));

        const status = document.createElement('span');
        status.id = 'queue-settings-status';
        status.style.fontSize = '0.75rem';
        status.style.color = 'var(--color-text-faint)';
        host.appendChild(status);

        void loadQueueSettings();
      },
    },
    { separator: true as const },
    { section: 'Testing' },
    {
      id: 'simulate-download',
      render(host: HTMLElement) {
        const hint = document.createElement('p');
        hint.className = 'optional-hint';
        hint.textContent =
          'Runs a ~30s fake download through the real queue — no network, ' +
          'no YouTube — for testing progress bars, cancel, rerun, and the ' +
          'output modal without touching real videos.';
        hint.style.whiteSpace = 'pre-wrap';

        const btn = document.createElement('button');
        btn.className = 'btn btn-ghost btn-sm';
        btn.textContent = 'Simulate download';
        btn.addEventListener('click', () => void submitSimulatedDownload());

        host.append(hint, btn);
      },
    },
    { separator: true as const },
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
      id: 'cookies-setting',
      render(host: HTMLElement) {
        // Collapsed by default (owner request): this is a rarely-needed,
        // easy-to-misuse escape hatch (D5/Phase 8: a stale cookie file
        // actively breaks downloads that would otherwise succeed anonymously)
        // — it should take a deliberate click to even see, not sit open and
        // inviting by default in a menu used for routine settings.
        const details = document.createElement('details');
        details.className = 'cookies-card';

        const summary = document.createElement('summary');
        summary.textContent = 'Cookie';
        details.appendChild(summary);

        const hint = document.createElement('p');
        hint.className = 'optional-hint';
        hint.textContent =
          'yt-dlp cannot sign in on its own. Export a Netscape-format cookie ' +
          'file from a browser that is already signed into YouTube — either ' +
          'the "Get cookies.txt LOCALLY" extension, or on that machine: ' +
          'yt-dlp --cookies-from-browser chrome --cookies cookies.txt --skip-download <any-url> ' +
          '— then paste its contents below.';
        hint.style.whiteSpace = 'pre-wrap';

        const status = document.createElement('span');
        status.id = 'cookies-status';
        status.style.display = 'block';
        status.style.fontSize = '0.75rem';
        status.style.color = 'var(--color-text-faint)';
        status.style.marginBottom = '0.5rem';

        const textarea = document.createElement('textarea');
        textarea.id = 'cookies-txt';
        textarea.placeholder = '# Netscape HTTP Cookie File\n...';
        textarea.rows = 4;
        textarea.spellcheck = false;
        textarea.style.width = '100%';
        textarea.style.fontFamily = 'monospace';
        textarea.style.fontSize = '0.75rem';

        const submitRow = document.createElement('div');
        submitRow.className = 'submit-row';
        const saveBtn = document.createElement('button');
        saveBtn.className = 'btn btn-ghost btn-sm';
        saveBtn.textContent = 'Save';
        saveBtn.addEventListener('click', saveCookies);
        const clearBtn = document.createElement('button');
        clearBtn.className = 'btn btn-ghost btn-sm';
        clearBtn.textContent = 'Clear';
        clearBtn.addEventListener('click', clearCookies);
        const saveStatus = document.createElement('span');
        saveStatus.id = 'cookies-save-status';
        saveStatus.style.fontSize = '0.75rem';
        saveStatus.style.color = 'var(--color-text-faint)';
        submitRow.append(saveBtn, clearBtn, saveStatus);

        details.append(hint, status, textarea, submitRow);
        host.appendChild(details);
        loadCookiesStatus();
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
  // The trigger sits at the right end of the topbar; the drawer opens beside it.
  side: 'right',
});

function numberField(
  id: string,
  label: string,
  min: number,
  max: number,
  onSave: (value: number) => void | Promise<void>,
): HTMLElement {
  const field = document.createElement('div');
  field.className = 'field field-num';
  const lbl = document.createElement('label');
  lbl.htmlFor = id;
  lbl.textContent = label;
  const input = document.createElement('input');
  input.type = 'number';
  input.id = id;
  input.min = String(min);
  input.max = String(max);
  input.addEventListener('change', () => {
    const v = parseInt(input.value, 10);
    if (Number.isFinite(v) && v >= min && v <= max) void onSave(v);
  });
  field.append(lbl, input);
  return field;
}

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
    void refreshJobs();
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

// submitSimulatedDownload posts a "test://simulate/..." job (see
// processor.runSimulated in internal/utuber/handler.go) — a unique URL per
// click so repeated clicks each go straight through instead of tripping the
// real duplicate-download banner. Not crypto.randomUUID(): that throws
// outside a browser "secure context", which a plain-HTTP custom hostname
// (e.g. utuber.test over LAN, as opposed to literal localhost) does not
// qualify for — silently, since an uncaught exception in a click handler
// produces no visible UI feedback at all. Date.now() + Math.random() needs
// no such context and is more than sufficient uniqueness for a manual test
// button a human is clicking one at a time.
async function submitSimulatedDownload(): Promise<void> {
  const formData = new FormData();
  const uniqueID = Date.now().toString(36) + '-' + Math.random().toString(36).slice(2);
  formData.set('url', 'test://simulate/' + uniqueID);
  formData.set('show_name', 'Simulated');
  formData.set('episode_title', 'Download Test');
  formData.set('mode', 'video');
  await submitForm(formData);
}

// ── Jobs queue (shared QueuePanel) ──────────────────────────────────────────

// UJob mirrors utuber's /jobs.json 15-key shape.
interface UJob {
  id: string;
  status: string; // queued | running | success | failed | canceled
  label: string;
  url: string;
  show_name: string;
  episode_title: string;
  season: number;
  episode: number;
  mode: string;
  progress: { pct: number | null; label: string } | null;
  output_file: string;
  error: string;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
}

function jobSection(j: UJob): QueueSection {
  if (j.status === 'queued') return 'upnext';
  if (j.status === 'running') return 'running';
  return 'recent';
}

function jobMeta(j: UJob): string {
  const ep =
    (j.season > 0 ? 'S' + String(j.season).padStart(2, '0') : '') +
    (j.episode > 0 ? 'E' + String(j.episode).padStart(2, '0') : '');
  const mode = j.mode || 'video';
  return [ep, mode].filter(Boolean).join(' · ');
}

function jobProgress(j: UJob): QueueProgress | null {
  return j.status === 'running' ? j.progress : null;
}

function jobActions(j: UJob): QueueActionKind[] {
  switch (j.status) {
    case 'queued': return ['cancel', 'remove'];
    case 'running': return ['cancel'];
    case 'failed':
    case 'canceled': return ['rerun', 'remove'];
    default: return ['remove']; // success
  }
}

async function onJobAction(action: QueueActionKind, j: UJob): Promise<void> {
  if (action === 'cancel') {
    const ok = await confirmDialog(
      `Cancel "${j.label}"?`,
      { title: 'Cancel download', confirmLabel: 'Cancel download' },
    );
    if (!ok) return;
    await postAction('/jobs/cancel?id=' + encodeURIComponent(j.id), 'cancel');
  } else if (action === 'rerun') {
    await postAction('/jobs/rerun?id=' + encodeURIComponent(j.id), 'rerun');
  } else if (action === 'remove') {
    const queued = j.status === 'queued';
    const ok = await confirmDialog(
      queued
        ? `Remove "${j.label}" from the queue? It won't be downloaded.`
        : `Remove "${j.label}" from the list? Any downloaded file is kept.`,
      { title: queued ? 'Remove queued download' : 'Remove from list', confirmLabel: 'Remove' },
    );
    if (!ok) return;
    await postAction('/jobs/delete?id=' + encodeURIComponent(j.id), 'remove');
  }
  void refreshJobs();
}

async function postAction(url: string, kind: 'cancel' | 'rerun' | 'remove'): Promise<void> {
  let res: Response;
  try {
    res = await fetch(url, { method: 'POST' });
  } catch {
    showToast('Could not reach the server.', 'error');
    return;
  }
  if (res.status === 409) {
    const msg = (await res.text().catch(() => '')).trim();
    showToast(msg || 'That download has already changed state.', 'notice');
  } else if (!res.ok && res.status !== 404) {
    showToast(`Could not ${kind} it.`, 'error');
  }
}

// decorateJobRow adds the success download links and the failure reason as
// module extras. Both use DOM APIs / textContent — never innerHTML — so
// yt-dlp's error text can never inject markup.
function decorateJobRow(row: HTMLElement, j: UJob): void {
  row.querySelector('.utuber-links')?.remove();
  row.querySelector('.utuber-error')?.remove();

  if (j.status === 'success' && j.output_file) {
    const href = '/downloads/' + encodeURIComponent(j.output_file);
    const wrap = document.createElement('div');
    wrap.className = 'utuber-links';

    const play = document.createElement('a');
    play.className = 'download-link';
    play.href = href;
    play.target = '_blank';
    play.rel = 'noopener';
    play.textContent = 'Play';

    const dl = document.createElement('a');
    dl.className = 'download-link';
    dl.href = href;
    dl.setAttribute('download', '');
    dl.textContent = 'Download';

    wrap.append(play, dl);
    row.appendChild(wrap);
  } else if (j.status === 'failed' && j.error) {
    const err = document.createElement('button');
    err.type = 'button';
    err.className = 'utuber-error-icon';
    err.textContent = '⚠';
    err.title = j.error;
    err.setAttribute('aria-label', 'Failure reason: ' + j.error);
    // Hover still shows the short reason (title); a click opens the full
    // "View output" modal streaming this job's complete stdout+stderr, backed
    // by taskmaster's shared streaming path via /jobs/output.
    err.addEventListener('click', () => {
      openOutputModal(new EventSource('/jobs/output?id=' + encodeURIComponent(j.id)), j.label);
    });
    row.appendChild(err);
  }
}

const jobsAdapter: QueuePanelAdapter<UJob> = {
  key: j => j.id,
  section: jobSection,
  title: j => j.label,
  status: j => j.status,
  meta: jobMeta,
  progress: jobProgress,
  actions: jobActions,
  onAction: onJobAction,
  decorate: decorateJobRow,
};

const jobsHost = document.getElementById('jobs-list')!;
jobsHost.innerHTML = '';
// No `header` option here -- the pause toggle lives next to the "Queue"
// heading in .jobs-header instead (wired by refreshQueueState/togglePause
// below), not inside the panel itself.
const jobsPanel = new QueuePanel<UJob>(jobsHost, jobsAdapter, {
  // Open by default (recentCollapsible only controls whether it CAN
  // collapse, not its initial state -- that's recentOpen): a finished
  // download moving into "Recent" behind a closed disclosure looked like
  // it had vanished. Still collapsible by the user; just starts open.
  // Scrollable once open (#jobs-list .ui-queue-list-recent in style.css)
  // rather than letting the card grow without bound.
  recentOpen: true,
});

// sortForDisplay puts "Recent" newest-finished-first (matching
// web/taskmaster/js/board.ts's own ran-execs sort) while leaving queued/
// running jobs in their existing FIFO order, which reflects actual run
// sequence and shouldn't be reordered.
function sortForDisplay(jobs: UJob[]): UJob[] {
  const active = jobs.filter(j => j.status === 'queued' || j.status === 'running');
  const recent = jobs.filter(j => j.status !== 'queued' && j.status !== 'running');
  recent.sort((a, b) => (b.finished_at ?? b.created_at).localeCompare(a.finished_at ?? a.created_at));
  return [...active, ...recent];
}

async function refreshJobs(): Promise<void> {
  let jobs: UJob[];
  try {
    const res = await fetch('/jobs.json');
    jobs = await res.json();
  } catch {
    return;
  }
  jobs = jobs || [];
  jobsPanel.update(sortForDisplay(jobs));

  const countEl = document.getElementById('jobs-count')!;
  countEl.textContent = jobs.length ? jobs.length + (jobs.length === 1 ? ' job' : ' jobs') : '';
}

// ── Queue pause toggle, next to the "Queue" heading ─────────────────────────

const pauseToggleBtn = document.getElementById('queue-pause-toggle') as HTMLButtonElement;
const pauseNoteEl = document.getElementById('queue-pause-note')!;

function applyPauseState(paused: boolean, note?: string): void {
  pauseToggleBtn.textContent = paused ? '▶' : '⏸';
  pauseToggleBtn.title = note || (paused ? 'Resume queue' : 'Pause queue');
  pauseToggleBtn.setAttribute('aria-label', pauseToggleBtn.title);
  pauseToggleBtn.disabled = !!note;
  pauseNoteEl.textContent = note ?? '';
  pauseNoteEl.hidden = !note;
}

async function refreshQueueState(): Promise<void> {
  let s: SettingsShape;
  try {
    const res = await fetch('/settings.json');
    s = await res.json();
  } catch {
    return;
  }
  const braked = s.brake_engaged === true || s.queue_paused_by === 'brake';
  const note = braked ? 'Paused by the taskmaster hand brake' : undefined;
  applyPauseState(!!s.queue_paused, note);
  syncQueueMenu(s);
}

async function togglePause(): Promise<void> {
  let s: SettingsShape;
  try {
    const res = await fetch('/settings.json');
    s = await res.json();
  } catch {
    return;
  }
  // The hand brake owns the pause state; the toggle is disabled in that case,
  // but guard here too.
  if (s.brake_engaged === true || s.queue_paused_by === 'brake') return;
  await saveQueueSetting({ queue_paused: !s.queue_paused });
  void refreshQueueState();
}

interface SettingsShape {
  python_bin?: string;
  age_out_days?: number;
  concurrent_downloads?: number;
  show_in_taskmaster?: boolean;
  queue_paused?: boolean;
  queue_paused_by?: string;
  brake_engaged?: boolean;
  cookies_configured?: boolean;
  cookies_updated_at?: string | null;
}

function syncQueueMenu(s: SettingsShape): void {
  const age = document.getElementById('age-out-days') as HTMLInputElement | null;
  if (age && document.activeElement !== age && s.age_out_days !== undefined) age.value = String(s.age_out_days);
  const conc = document.getElementById('concurrent-downloads') as HTMLInputElement | null;
  if (conc && document.activeElement !== conc && s.concurrent_downloads !== undefined) conc.value = String(s.concurrent_downloads);
  const show = document.getElementById('show-in-taskmaster') as HTMLInputElement | null;
  if (show && s.show_in_taskmaster !== undefined) show.checked = s.show_in_taskmaster;
}

async function loadQueueSettings(): Promise<void> {
  try {
    const res = await fetch('/settings.json');
    syncQueueMenu(await res.json());
  } catch {
    /* ignore — the poll will retry */
  }
}

async function saveQueueSetting(patch: Partial<SettingsShape>): Promise<void> {
  const status = document.getElementById('queue-settings-status');
  try {
    const res = await fetch('/settings.json', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    });
    if (res.ok) {
      if (status) { status.style.color = 'var(--color-success)'; status.textContent = 'Saved.'; }
      void refreshQueueState();
    } else if (status) {
      status.style.color = 'var(--color-danger)';
      status.textContent = (await res.text().catch(() => '')).trim() || 'Save failed.';
    }
  } catch {
    if (status) { status.style.color = 'var(--color-danger)'; status.textContent = 'Connection error.'; }
  }
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

// ── Cookie jar (age-restricted downloads, D5 Fix 2) ─────────────────────────

// cookieAgeText renders the jar's staleness: YouTube rotates these cookies,
// and a stale jar fails identically to no jar at all, so age has to be
// visible rather than just "configured: yes".
function cookieAgeText(s: SettingsShape): string {
  if (!s.cookies_configured) return 'No cookie file configured.';
  if (!s.cookies_updated_at) return 'Cookie file configured.';
  const updated = new Date(s.cookies_updated_at);
  if (Number.isNaN(updated.getTime())) return 'Cookie file configured.';
  const ageDays = Math.floor((Date.now() - updated.getTime()) / 86400000);
  const ageText = ageDays <= 0 ? 'today' : ageDays === 1 ? '1 day ago' : `${ageDays} days ago`;
  const stale = ageDays >= 14 ? ' — likely stale; YouTube rotates these, re-export if downloads start failing.' : '';
  return `Cookie file saved ${ageText}.${stale}`;
}

async function loadCookiesStatus(): Promise<void> {
  const status = document.getElementById('cookies-status');
  if (!status) return;
  try {
    const res = await fetch('/settings.json');
    const data: SettingsShape = await res.json();
    status.textContent = cookieAgeText(data);
  } catch {
    status.textContent = 'Connection error.';
  }
}

async function saveCookies(): Promise<void> {
  const textarea = document.getElementById('cookies-txt') as HTMLTextAreaElement;
  const saveStatus = document.getElementById('cookies-save-status');
  try {
    const res = await fetch('/settings.json', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cookies_txt: textarea.value }),
    });
    if (res.ok) {
      const data: SettingsShape = await res.json();
      textarea.value = '';
      if (saveStatus) { saveStatus.style.color = 'var(--color-success)'; saveStatus.textContent = 'Saved.'; }
      const status = document.getElementById('cookies-status');
      if (status) status.textContent = cookieAgeText(data);
    } else if (saveStatus) {
      saveStatus.style.color = 'var(--color-danger)';
      saveStatus.textContent = (await res.text().catch(() => '')).trim() || 'Save failed.';
    }
  } catch {
    if (saveStatus) { saveStatus.style.color = 'var(--color-danger)'; saveStatus.textContent = 'Connection error.'; }
  }
}

async function clearCookies(): Promise<void> {
  const textarea = document.getElementById('cookies-txt') as HTMLTextAreaElement;
  textarea.value = '';
  await saveCookies();
}

// Wire up the mode tabs.
document.querySelectorAll<HTMLButtonElement>('.mode-tab').forEach(tab => {
  tab.addEventListener('click', () => setMode(tab.dataset.mode!));
});

pauseToggleBtn.addEventListener('click', () => void togglePause());

// clearRecents removes every finished (success/failed/canceled) job from
// the list in one go. Reuses the existing single-job /jobs/delete route --
// no new backend endpoint -- since a running/queued job is never eligible
// (mirrors handleJobDelete's own 409-on-running guard, so this never
// touches anything in progress).
document.getElementById('clear-recents-btn')!.addEventListener('click', () => void clearRecents());

async function clearRecents(): Promise<void> {
  let jobs: UJob[];
  try {
    const res = await fetch('/jobs.json');
    jobs = await res.json();
  } catch {
    showToast('Could not reach the server.', 'error');
    return;
  }
  const recents = jobs.filter(j => j.status !== 'queued' && j.status !== 'running');
  if (recents.length === 0) {
    showToast('Nothing to clear.', 'notice');
    return;
  }
  const ok = await confirmDialog(
    `Remove all ${recents.length} finished download${recents.length === 1 ? '' : 's'} from the list? ` +
    'Downloaded files on disk are kept -- this only clears the list.',
    { title: 'Clear recent downloads', confirmLabel: 'Clear' },
  );
  if (!ok) return;
  await Promise.all(recents.map(j => fetch('/jobs/delete?id=' + encodeURIComponent(j.id), { method: 'POST' })));
  void refreshJobs();
}

// Poll at the 2000ms floor (Q3).
setInterval(() => { void refreshJobs(); void refreshQueueState(); }, 2000);
void refreshJobs();
void refreshQueueState();
