// backends.ts — the Backends tab: add, edit, retire and re-enable backends at
// runtime (BK1), the ordered model candidates per role with the paid badge and
// explicit opt-in (MB4, MB5), per-project limit overrides where blank means
// inherit (CF2) and the per-model pass rates (MB8). With no backend the tab
// prompts to add one (CI1). A 400 shows next to the field it names, and the
// warnings a retire returns are shown in full (SV3). Every server- or
// user-supplied string reaches the page through h(), so it is text.

import { autosave, type Autosave } from './autosave.js';
import { confirmDialog, showToast } from '@shared';
import {
  api, type Backend, type BackendInput, type BackendResult, type Candidate, type CheckView, type EvalView, type EvalSummary, type ModelCheck, type ModelRest, type ProjectView,
} from './api.js';
import { h, replace } from './dom.js';
import { subscribe, type AgentflowEvent } from './events.js';
import { defaultInstallDeps, InstallPanel, type InstallDeps } from './installs.js';
import { FormErrors, badge, button, checkbox, type ToggleEl, fieldRow, section, selectInput, textInput } from './forms.js';
import { ROLES, buildRoleEditor, isPaid, unconsented, type CandRow } from './candidates.js';
import { ConfigPanel, defaultConfigDeps } from './configfile.js';
import { LIMIT_FIELDS } from './settings.js';
import { formatTokens } from './effort-settings.js';

/** isPrivateHost is true when a backend's host (a URL or host:port) is a loopback or private-network address. */
export function isPrivateHost(host: string): boolean {
  const m = /^(?:[a-z][a-z0-9+.-]*:\/\/)?(?:[^@/]*@)?(\[[^\]]*\]|[^:/?#]*)/i.exec(host.trim());
  const name = (m?.[1] ?? '').toLowerCase();
  if (name === 'localhost' || name === '[::1]') return true;
  const ip = /^(\d{1,3})\.(\d{1,3})\.\d{1,3}\.\d{1,3}$/.exec(name);
  if (!ip) return false;
  const [a, b] = [Number(ip[1]), Number(ip[2])];
  return a === 10 || a === 127 || (a === 192 && b === 168) || (a === 172 && b >= 16 && b <= 31);
}

export { ROLES, isPaid, unconsented };
export type { CandRow };

export /** TIERS are the ratings a model can have; unrated models count as tier 2 where it matters. */
const TIERS: Array<[string, string]> = [['0', 'not rated'], ['1', '1 strong'], ['2', '2 capable'], ['3', '3 light']];

const API_TYPES: Array<[string, string]> = [
  ['ollama', 'ollama'],
  ['openai_compatible', 'openai compatible'],
  ['anthropic', 'anthropic'],
  ['opencode', 'opencode built-in'],
  ['grok', 'Grok CLI (your Grok plan)'],
];

/** How often a running model check is looked at again. */
export let checkPollMs = 3000;
export const setCheckPollMs = (ms: number): void => { checkPollMs = ms; };

export type BackendsDeps = Pick<
  typeof api,
  'backends' | 'createBackend' | 'probeBackend' | 'patchBackend' | 'retireBackend' | 'enableBackend' | 'deleteBackend' | 'rests' | 'wakeModel' | 'checkModels' | 'checkStatus' | 'checkReviewers' | 'evalStatus' | 'extendWindows' | 'candidates' | 'putCandidates' |
  'projects' | 'limits' | 'putLimits' | 'settings'
> & { notify(message: string): void; confirm(message: string, confirmLabel?: string): Promise<boolean> };

export const defaultBackendsDeps = (): BackendsDeps => ({
  backends: api.backends,
  createBackend: api.createBackend,
  probeBackend: api.probeBackend,
  patchBackend: api.patchBackend,
  retireBackend: api.retireBackend,
  enableBackend: api.enableBackend,
  deleteBackend: api.deleteBackend,
  rests: api.rests,
  wakeModel: api.wakeModel,
  checkModels: api.checkModels,
  checkStatus: api.checkStatus,
  checkReviewers: api.checkReviewers,
  evalStatus: api.evalStatus,
  extendWindows: api.extendWindows,
  candidates: api.candidates,
  putCandidates: api.putCandidates,
  projects: api.projects,
  limits: api.limits,
  putLimits: api.putLimits,
  settings: api.settings,
  notify: (m) => void showToast(m, 'success'),
  confirm: (m, label = 'Confirm') => confirmDialog(m, { confirmLabel: label }),
});

/** usedBy names the candidate lists that still hold the backend, as "global worker" or "project p reviewer". */
export function usedBy(cands: Candidate[], name: string): string[] {
  const out = new Set<string>();
  for (const c of cands) {
    if (c.backend !== name) continue;
    out.add(c.scope === 'project' ? `project ${c.project ?? '?'} ${c.role}` : `global ${c.role}`);
  }
  return [...out];
}

/** lastForRoles lists the roles whose only candidates are on the named backend. */
export function lastForRoles(cands: Candidate[], name: string): string[] {
  return ROLES.filter((role) => {
    const mine = cands.filter((c) => c.scope === 'global' && c.role === role);
    return mine.length > 0 && mine.every((c) => c.backend === name);
  });
}

export class BackendsView {
  backends: Backend[] = [];
  candidates: Candidate[] = [];
  projects: ProjectView[] = [];
  globals: Record<string, string | number> = {};
  warnings: string[] = [];
  private loaded = false;
  private adding = false;
  private editing = new Set<string>();
  private retiring = new Set<string>();
  /** The last model check of each backend (H27), refreshed while one runs. */
  private checks = new Map<string, CheckView>();
  /** The models that are resting after a rate limit or a stall, keyed backend + NUL + model. */
  private rests = new Map<string, ModelRest>();
  /** The last seeded-defect reviewer check of each backend (H32), refreshed while one runs. */
  private evals = new Map<string, EvalView>();
  /** role -> rows being edited; present only while the role has unsaved edits. */
  private drafts = new Map<string, CandRow[]>();
  private limitsSlug = '';
  private limitsHost = h('div');
  private off: (() => void) | null = null;
  private configPanel: ConfigPanel | null = null;

  /** One install panel per Ollama backend; each keeps its own list and event subscription. */
  private panels = new Map<string, InstallPanel>();

  constructor(private host: HTMLElement, private deps: BackendsDeps, private installDeps: InstallDeps = defaultInstallDeps()) {}

  private panelFor(name: string): InstallPanel {
    let p = this.panels.get(name);
    if (!p) {
      p = new InstallPanel(name, this.installDeps);
      this.panels.set(name, p);
      p.mount();
    }
    return p;
  }

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      if (['backend.update', 'candidates.update', 'state.changed', 'settings.update'].includes(ev.type) && this.idle()) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      this.panels.forEach((p) => p.unmount());
      this.panels.clear();
    };
  }

  private idle(): boolean {
    return !this.adding && this.editing.size === 0 && this.retiring.size === 0 && this.drafts.size === 0;
  }

  async load(): Promise<void> {
    try {
      const [backends, candidates, projects, globals] = await Promise.all([
        this.deps.backends(), this.deps.candidates(), this.deps.projects(), this.deps.settings(),
      ]);
      Object.assign(this, { backends, candidates, projects, globals, loaded: true });
      try {
        this.rests = new Map((await this.deps.rests()).map((r) => [r.backend + '\u0000' + r.model, r]));
      } catch {
        // no list of rests yet: nothing is shown as resting
      }
      await Promise.all(backends.map(async (b) => {
        try {
          this.checks.set(b.name, await this.deps.checkStatus(b.name));
        } catch {
          // a backend with no check yet shows none
        }
        try {
          this.evals.set(b.name, await this.deps.evalStatus(b.name));
        } catch {
          // nor a reviewer check
        }
      }));
    } catch {
      // apiFetch already told the user; keep what is shown.
      if (!this.loaded) return;
    }
    if (!this.projects.some((p) => p.slug === this.limitsSlug)) this.limitsSlug = this.projects[0]?.slug ?? '';
    this.render();
    void this.drawLimits();
  }

  // ─── Backend list ────────────────────────────────────────────────────────

  private showResult(res: BackendResult, what: string): void {
    this.warnings = res.warnings ?? [];
    this.deps.notify(what);
  }

  private backendForm(initial: Backend | null, done: () => void): HTMLElement {
    const errors = new FormErrors();
    // Edits the page makes itself (no input event) still count as a change.
    let auto: Autosave | null = null;
    const bump = (): void => auto?.touch();
    const name = textInput('name', initial?.name ?? '', { readOnly: !!initial });
    const host = textInput('host', initial?.host ?? '', { placeholder: 'http://host:port' });
    const apiType = selectInput('api_type', API_TYPES, initial?.api_type ?? 'ollama');
    const slots = textInput('slots', String(initial?.slots ?? 1), { type: 'number' });
    const budget = textInput('memory_budget_gb', initial?.memory_budget_mb ? String(Math.round(initial.memory_budget_mb / 1024)) : '', { type: 'number', placeholder: 'not set' });
    const cap = textInput('window_cap_tokens', initial?.window_cap_tokens ? String(initial.window_cap_tokens) : '', { type: 'number', placeholder: 'no cap' });
    const hosted = checkbox('hosted', initial?.hosted ?? false);
    const paid = checkbox('paid', initial?.paid ?? false);
    // The API key is write-only: typed text is sent, an untouched field keeps the stored key, Clear key removes it.
    const apiKey = textInput('api_key', '', { type: 'password', placeholder: initial?.has_key ? 'unchanged' : 'API key' });
    apiKey.setAttribute('autocomplete', 'new-password');
    let clearKey = false;
    const keyState = h('span', { class: 'row-actions', attrs: { 'data-role': 'key-state' } });
    const drawKey = (): void => replace(keyState,
      initial?.has_key ? (clearKey ? badge('key cleared on save', 'warn') : badge('key set', 'ok')) : null,
      initial?.has_key ? button(clearKey ? 'Keep key' : 'Clear key', () => { clearKey = !clearKey; drawKey(); bump(); }, { field: 'clear-key', icon: clearKey ? 'cancel' : 'remove', word: true }) : null);
    drawKey();
    const keyField = errors.add('api_key', fieldRow('API key', h('div', { class: 'row-actions' }, apiKey, keyState),
      'Sent to the server as its bearer credential, for a hosted service or a local one that asks for a key. Stored write-only; leave blank to keep the stored key'));
    const hostField = errors.add('host', fieldRow('Host', host, 'An http or https URL'));
    // opencode's own provider has no host and no key: its free models are reached through opencode itself.
    const builtin = (): boolean => apiType.value === 'opencode' || apiType.value === 'grok';
    const syncKey = (): void => {
      if (!builtin()) keyField.el.removeAttribute('hidden');
      else keyField.el.setAttribute('hidden', '');
      if (builtin()) hostField.el.setAttribute('hidden', '');
      else hostField.el.removeAttribute('hidden');
    };
    (hosted.firstChild as HTMLInputElement).addEventListener('change', syncKey);
    apiType.addEventListener('change', syncKey);
    syncKey();
    type ModelControl = HTMLInputElement | HTMLSelectElement;
    type ModelRow = { model: ModelControl; paid: ToggleEl; max: HTMLInputElement; ctx: HTMLInputElement; tier: HTMLSelectElement; auto: string; warn: HTMLElement; el: HTMLElement };
    const models: ModelRow[] = [];
    // Names the host reported when probed; model rows then pick from them instead of typing.
    let probed: string[] = [];
    // The largest context each model supports, as the host reported it; kept and sent back, never typed (H15).
    let contexts: Record<string, number> = {};
    for (const m of initial?.models ?? []) if (m.context_tokens) contexts[m.model] = m.context_tokens;
    // What the host said about tool calls, by model name (H10); a model it says lacks them cannot be saved.
    let tools: Record<string, string> = {};
    const modelControl = (m: string): ModelControl => probed.length > 0
      ? selectInput('model', probed.map((n): [string, string] => [n, tools[n] === 'no' ? `${n} (no tool support)` : n]), m)
      : textInput('model', m, { placeholder: 'model name (or Probe host)' });
    const modelList = h('div', { class: 'wizard-list' });
    // The host's figure is the model's maximum, not what it is served with, so it no longer fills the window field.
    const prefill = (_row: ModelRow): void => {};
    const flag = (row: ModelRow): void => { row.warn.hidden = tools[row.model.value] !== 'no'; };
    const draw = (row: ModelRow): void => {
      row.model.addEventListener('change', () => { prefill(row); flag(row); });
      replace(row.el,
        row.model, row.warn,
        h('span', { class: 'inline-check' }, row.paid, 'paid'),
        h('span', { class: 'inline-check', attrs: { title: 'The context window the host serves this model with, in tokens. Blank keeps what agentflow knows (learned when a run is cut off, otherwise the context floor in Settings).' } }, 'window', row.ctx),
        h('span', { class: 'inline-check', attrs: { title: 'How capable the model is. Light models are kept off planning roles; a reviewer is never weaker than the worker it checks.' } }, 'tier', row.tier),
        h('span', { class: 'inline-check' }, 'at once', row.max),
        button('Remove', () => {
          models.splice(models.indexOf(row), 1);
          row.el.parentNode?.removeChild(row.el);
          bump();
        }, { field: 'remove-model', icon: 'remove' }));
    };
    const addModel = (m = '', p = false, max = 0, ctx = 0, tier = 0): void => {
      const row: ModelRow = {
        model: modelControl(m),
        paid: checkbox('model_paid', p),
        max: textInput('max_concurrent', max > 0 ? String(max) : '', { type: 'number', placeholder: 'no limit' }),
        ctx: textInput('window_tokens', ctx > 0 ? String(ctx) : '', { type: 'number', placeholder: '?' }),
        tier: selectInput('tier', TIERS, String(tier)),
        auto: '',
        warn: h('span', { class: 'badge badge-warn', text: 'no tool support: remove it', attrs: { 'data-role': 'no-tools', hidden: true, title: 'The host says this model cannot make tool calls, which every agentflow role needs. It cannot be saved.' } }),
        el: h('div', { class: 'inline-row', attrs: { 'data-role': 'model-row' } }),
      };
      models.push(row);
      draw(row);
      modelList.appendChild(row.el);
    };
    for (const m of initial?.models ?? []) addModel(m.model, m.paid, m.max_concurrent, m.window_tokens ?? 0, m.tier ?? 0);
    if (!initial) addModel();
    const probe = async (): Promise<void> => {
      errors.clear();
      try {
        const res = await this.deps.probeBackend(builtin() ? '' : host.value.trim(), apiType.value, apiKey.value, initial?.name ?? '');
        probed = res.models;
        contexts = res.contexts ?? {};
        tools = res.tools ?? {};
        if (probed.length === 0) throw new Error('the host reported no models');
        // Swap each row's control for a picker, keeping a name the host still serves.
        for (const row of models) {
          const keep = row.model.value;
          row.model = modelControl(keep);
          draw(row);
          prefill(row);
          flag(row);
        }
        bump();
        this.deps.notify(`${probed.length} model(s) found${builtin() ? '' : ` on ${host.value.trim()}`}`);
      } catch (err) {
        errors.show(err);
      }
    };
    const modelsField = errors.add('models', fieldRow('Models', modelList,
      'Probe lists what the host serves. A paid model is only used when listed for a role. "At once" caps how many stages run it together (blank = the slots limit)'));
    modelsField.el.appendChild(button('Probe host for models', () => void probe(), { field: 'probe-host', icon: 'probe' }));
    modelsField.el.appendChild(button('Add model', () => addModel(), { field: 'add-model', icon: 'add' }));

    const readBody = (): BackendInput => {
      const body: BackendInput = {
        name: name.value.trim(),
        host: builtin() ? '' : host.value.trim(),
        api_type: apiType.value,
        hosted: hosted.checked,
        paid: paid.checked,
        slots: Number(slots.value),
        memory_budget_mb: Math.max(0, Math.round((Number(budget.value) || 0) * 1024)),
        window_cap_tokens: Math.max(0, Math.round(Number(cap.value) || 0)),
        models: models.map((m) => ({ model: m.model.value.trim(), paid: m.paid.checked, max_concurrent: Number(m.max.value) || 0, context_tokens: contexts[m.model.value.trim()] ?? 0, window_tokens: Math.max(0, Math.round(Number(m.ctx.value) || 0)), tier: Number(m.tier.value) || 0 })).filter((m) => m.model !== ''),
      };
      if (!builtin() && apiKey.value !== '') body.api_key = apiKey.value;
      else if (clearKey) body.api_key = '';
      return body;
    };
    // Adding is one deliberate step; editing a card saves itself after a pause.
    const save = async (): Promise<boolean> => {
      errors.clear();
      const body = readBody();
      try {
        const res = initial
          ? await this.deps.patchBackend(initial.name, body, { quiet: true })
          : await this.deps.createBackend(body, { quiet: true });
        this.showResult(res, initial ? `Saved ${body.name}` : `Added ${body.name}`);
      } catch (err) {
        errors.show(err);
        return false;
      }
      if (!initial) done();
      return true;
    };
    auto = initial ? autosave({ snapshot: () => JSON.stringify(readBody()), save }) : null;
    const card = h('div', { class: 'profile-card', attrs: { 'data-form': initial ? 'edit' : 'add' } },
      h('div', { class: 'profile-fields' },
        errors.add('name', fieldRow('Name', name, 'Letters, digits and dashes; fixed once added')).el,
        hostField.el,
        errors.add('api_type', fieldRow('API type', apiType)).el,
        errors.add('slots', fieldRow('Slots', slots, 'Stages this backend runs at once')).el,
        errors.add('memory_budget_mb', fieldRow('Memory budget (GB)', budget, 'What one loaded model may use on the host, weights and context together. Needed to extend windows')).el,
        errors.add('window_cap_tokens', fieldRow('Window cap (tokens)', cap, 'Never extend a window past this, for a host where a long context is too slow')).el,
        errors.add('hosted', fieldRow('Hosted', hosted, 'A remote service rather than your own machine')).el,
        errors.add('paid', fieldRow('Paid', paid, 'Every model on it costs money')).el,
        keyField.el),
      modelsField.el,
      errors.general,
      h('div', { class: 'settings-actions' },
        auto?.status ?? null,
        initial ? null : button('Cancel', done, { field: 'cancel', icon: 'cancel' }),
        initial
          ? button('Done', () => void auto?.flush().then(done), { primary: true, field: 'close', icon: 'approve' })
          : button('Add backend', () => void save(), { primary: true, field: 'save', icon: 'approve' })));
    if (auto) auto.watch(card);
    return card;
  }

  private retirePanel(b: Backend): HTMLElement {
    const cancel = checkbox('cancel_running', true);
    const last = lastForRoles(this.candidates, b.name);
    const onlyActive = this.backends.filter((x) => !x.retired && x.name !== b.name).length === 0;
    const notes = [
      ...(onlyActive ? ['This is the only active backend: while it is disabled nothing can be dispatched until you add one or enable one.'] : []),
      ...(last.length ? [`It is the only backend listed for: ${last.join(', ')}. Those roles will wait with no eligible backend.`] : []),
    ];
    const run = async (): Promise<void> => {
      let res: BackendResult;
      try {
        res = await this.deps.retireBackend(b.name, cancel.checked);
      } catch {
        return;
      }
      this.retiring.delete(b.name);
      this.showResult(res, `${b.name} is disabled`);
      await this.load();
    };
    return h('div', { class: 'warn-box', attrs: { 'data-panel': 'retire' } },
      ...notes.map((n) => h('p', { text: n })),
      h('span', { class: 'inline-check' }, cancel, 'Stop what is running on this backend now'),
      h('div', { class: 'row-actions' },
        button('Disable', () => void run(), { primary: true, field: 'confirm-retire', icon: 'pause', word: true }),
        button('Keep enabled', () => { this.retiring.delete(b.name); this.render(); }, { field: 'cancel-retire', icon: 'cancel', word: true })));
  }

  private card(b: Backend): HTMLElement {
    if (this.editing.has(b.name)) {
      return this.backendForm(b, () => { this.editing.delete(b.name); void this.load(); });
    }
    const checked = new Map((this.checks.get(b.name)?.results ?? []).map((c) => [c.Model, c]));
    const models = b.models.map((m) => h('div', { class: 'model-row' }, h('strong', { class: 'model-name', text: m.model }), m.paid ? badge('paid') : null,
      this.checkBadge(checked.get(m.model), this.checks.get(b.name)?.current === m.model),
      ...this.evalBadges(b.name, m.model),
      m.max_concurrent > 0 ? badge(`max ${m.max_concurrent} at once`, 'muted') : null,
      m.window_tokens ? badge(`window ${formatTokens(m.window_tokens)}${m.window_source === 'observed' ? ' (learned)' : ''}`, m.window_source === 'observed' ? 'warn' : 'muted') : null,
      m.context_tokens ? badge(`max ${formatTokens(m.context_tokens)}`, 'muted') : null,
      m.tier ? badge(`tier ${m.tier}`, m.tier === 3 ? 'warn' : 'muted') : null,
      m.too_big ? badge('too big for host', 'warn') : null,
      ...this.restParts(b.name, m.model),
      m.tools === 'no' ? badge('no tools', 'warn') : null));
    return h('div', { class: 'profile-card', attrs: { 'data-backend': b.name } },
      h('div', { class: 'profile-header' },
        h('span', { class: 'row-actions' },
          h('strong', { text: b.name }),
          b.paid ? badge('paid') : null,
          b.hosted ? badge('hosted', 'muted') : null,
          b.has_key ? badge('key set', 'ok') : null,
          b.hosted && b.api_type !== 'opencode' && b.api_type !== 'grok' && isPrivateHost(b.host) ? h('span', { class: 'badge badge-warn', text: '\u26A0 hosted?', attrs: { title: 'Flagged hosted, but the host is a private or local address: it may be mis-flagged. Edit the backend and untick Hosted if it runs on your own machine or network.', 'data-role': 'hosted-warning' } }) : null,
          b.retired ? badge('disabled', 'warn') : null,
          b.suspended_since ? badge('suspended: not answering', 'warn') : b.down_until ? badge('unreachable', 'warn') : null),
        h('div', { class: 'profile-actions' },
          b.api_type === 'ollama' && !b.retired && (b.memory_budget_mb ?? 0) > 0
            ? button('Extend windows', () => void this.extend(b.name), { field: 'extend-windows', icon: 'approve', word: true })
            : null,
          b.retired ? null : button(this.checks.get(b.name)?.state === 'running' ? 'Checking\u2026' : 'Check models', () => void this.check(b.name), { field: 'check-models', icon: 'approve', word: true }),
          b.retired ? null : button(this.evals.get(b.name)?.state === 'running' ? `Checking reviewers\u2026 ${this.evals.get(b.name)?.done}/${this.evals.get(b.name)?.total}` : 'Check reviewers', () => void this.checkReviewers(b.name), { field: 'check-reviewers', icon: 'approve', word: true }),
          button('Edit', () => { this.editing.add(b.name); this.render(); }, { field: 'edit', icon: 'edit' }),
          b.retired
            ? button('Enable', () => void this.enable(b.name), { field: 'enable', icon: 'approve', word: true })
            : button('Disable', () => { this.retiring.add(b.name); this.render(); }, { field: 'retire', icon: 'pause', word: true }),
          button('Delete', () => void this.remove(b), { field: 'delete', icon: 'trash', unavailable: this.whyNotDelete(b) || undefined }))),
      h('div', { class: 'profile-fields' },
        b.api_type === 'opencode' || b.api_type === 'grok' ? null : h('div', {}, h('label', { text: 'Host' }), h('span', { text: b.host })),
        h('div', {}, h('label', { text: 'API type' }), h('span', { text: b.api_type })),
        h('div', {}, h('label', { text: 'Slots' }), h('span', { text: String(b.slots) })),
        b.retired ? null : h('div', {}, h('label', { text: 'Pace' }), this.paceSelect(b)),
        b.memory_budget_mb ? h('div', {}, h('label', { text: 'Memory budget' }), h('span', { text: `${Math.round(b.memory_budget_mb / 1024)} GB${b.window_cap_tokens ? `, windows capped at ${formatTokens(b.window_cap_tokens)}` : ''}` })) : null,
        h('div', { class: 'profile-wide' }, h('label', { text: 'Models' }), h('div', { class: 'model-list' }, ...models))),
      b.api_type === 'ollama' && !b.retired ? this.panelFor(b.name).el : null,
      this.retiring.has(b.name) ? this.retirePanel(b) : null);
  }

  /** Pace is the duty cap (H20): how much of the time the backend may be kept busy. It applies at once, with no restart. */
  private paceSelect(b: Backend): HTMLElement {
    const now = b.duty_percent && b.duty_percent < 100 ? b.duty_percent : 100;
    const opts: Array<[string, string]> = [['100', 'Full speed'], ['75', 'Busy 75% of the time'], ['50', 'Busy 50% of the time'], ['25', 'Busy 25% of the time']];
    if (!opts.some(([v]) => v === String(now))) opts.push([String(now), `Busy ${now}% of the time`]);
    const sel = selectInput('duty_percent', opts, String(now));
    sel.addEventListener('change', () => {
      void (async () => {
        try {
          await this.deps.patchBackend(b.name, { duty_percent: Number(sel.value) }, { quiet: true });
          this.deps.notify(Number(sel.value) >= 100 ? `${b.name} runs at full speed` : `${b.name} is held to ${sel.value}% busy`);
        } catch {
          // the server's message is already shown
        }
        await this.load();
      })();
    });
    return sel;
  }

  /** One model's last check: answered, or why not. A failed model is passed over until it passes again. */
  private checkBadge(c: ModelCheck | undefined, now: boolean): HTMLElement | null {
    if (now) return badge('checking\u2026', 'muted');
    if (!c) return null;
    const label: Record<string, string> = { failing: 'not answering', rate_limited: 'out of quota', unreachable: 'unreachable', timeout: 'too slow', empty: 'empty reply' };
    const span = c.OK ? badge(`answers (${(c.Millis / 1000).toFixed(1)}s)`, 'ok') : badge(label[c.Class] ?? c.Class, 'warn');
    span.setAttribute('title', `${c.OK ? 'Passed' : c.Message || 'Failed'} \u2014 checked ${new Date(c.Checked).toLocaleString()}`);
    span.setAttribute('data-role', c.OK ? 'check-ok' : 'check-failed');
    return span;
  }

  /**
   * One model's score on the seeded-defect cases (H32), one badge per layer:
   * how many planted defects it caught, and how many fixed versions it
   * wrongly flagged. A reviewer that misses defects, or flags clean code, is
   * not one to trust with a gate.
   */
  private evalBadges(backend: string, mdl: string): HTMLElement[] {
    const ev = this.evals.get(backend);
    if (!ev) return [];
    const rows = ev.rows.filter((r) => r.model === mdl);
    return ev.summary.filter((s: EvalSummary) => s.model === mdl).map((s) => {
      const bad = s.caught < s.defects || s.false_alarms > 0 || s.unread > 0;
      const label = `${s.layer === 'task' ? 'task review' : 'acceptance'} ${s.caught}/${s.defects}${s.false_alarms ? `, ${s.false_alarms} false alarm${s.false_alarms === 1 ? '' : 's'}` : ''}${s.unread ? `, ${s.unread} unread` : ''}`;
      const el = badge(label, bad ? 'warn' : 'ok');
      el.setAttribute('title', rows.filter((r) => r.layer === s.layer).map((r) => `${r.case} (${r.variant}): ${r.outcome}${r.detail ? ` \u2014 ${r.detail}` : ''}`).join('\n'));
      el.setAttribute('data-role', 'eval-score');
      return el;
    });
  }

  /** Run the seeded-defect cases through every model of a backend (H32) and follow the run until it ends. */
  private async checkReviewers(name: string): Promise<void> {
    try {
      this.evals.set(name, await this.deps.checkReviewers(name, { layers: ['task', 'acceptance'] }));
    } catch {
      return;
    }
    this.render();
    while (this.evals.get(name)?.state === 'running') {
      await new Promise((r) => setTimeout(r, checkPollMs));
      try {
        this.evals.set(name, await this.deps.evalStatus(name));
      } catch {
        break;
      }
      this.render();
    }
    const weak = (this.evals.get(name)?.summary ?? []).filter((s) => s.caught < s.defects || s.false_alarms > 0);
    this.deps.notify(weak.length ? `${weak.length} model score${weak.length === 1 ? '' : 's'} on ${name} missed a defect or raised a false alarm` : `Every model on ${name} caught every planted defect`);
  }

  /** Check every model of a backend, one after the other (H27), and follow the run until it ends. */
  private async check(name: string): Promise<void> {
    try {
      this.checks.set(name, await this.deps.checkModels(name));
    } catch {
      return;
    }
    this.render();
    while (this.checks.get(name)?.state === 'running') {
      await new Promise((r) => setTimeout(r, checkPollMs));
      try {
        this.checks.set(name, await this.deps.checkStatus(name));
      } catch {
        break;
      }
      this.render();
    }
    const bad = (this.checks.get(name)?.results ?? []).filter((c) => !c.OK);
    this.deps.notify(bad.length ? `${bad.length} model${bad.length === 1 ? '' : 's'} on ${name} did not answer: ${bad.map((c) => c.Model).join(', ')}` : `Every model on ${name} answered`);
  }

  /** Raise each model's served window as far as the backend's memory budget allows (H15d). */
  private async extend(name: string): Promise<void> {
    try {
      const res = await this.deps.extendWindows(name);
      const done = res.filter((r) => r.applied).length;
      this.deps.notify(done > 0 ? `Extended ${done} of ${res.length} model windows on ${name}` : `No window changed on ${name}: ${res.map((r) => `${r.model}: ${r.note}`).join('; ')}`);
    } catch {
      return;
    }
    await this.load();
  }

  /** Why a backend cannot be deleted yet, or '' when it can. The server enforces the same two rules. */
  private whyNotDelete(b: Backend): string {
    if (!b.retired) return 'Disable this backend first; only a disabled backend can be deleted.';
    const lists = usedBy(this.candidates, b.name);
    return lists.length > 0 ? `Still on these candidate lists: ${lists.join(', ')}. Remove it from them to delete it.` : '';
  }

  /** restParts shows a resting model with the time it comes back and a way to wake it now. */
  private restParts(backend: string, model: string): (HTMLElement | null)[] {
    const r = this.rests.get(backend + '\u0000' + model);
    if (!r || r.until <= Date.now()) return [];
    const at = new Date(r.until).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    return [
      badge(`resting until ${at} (${r.reason}${r.streak > 1 ? `, ${r.streak} in a row` : ''})`, 'warn'),
      button('Wake', () => void this.wake(backend, model), { field: 'wake-model', word: true }),
    ];
  }

  private async wake(backend: string, model: string): Promise<void> {
    try {
      await this.deps.wakeModel(backend, model);
      this.deps.notify(`${model} is back in use`);
    } catch {
      return;
    }
    await this.load();
  }

  private async remove(b: Backend): Promise<void> {
    const sure = await this.deps.confirm(
      `Delete backend ${b.name} and its ${b.models.length} model${b.models.length === 1 ? '' : 's'}? Past stages keep the name, so history stays. This cannot be undone.`, 'Delete');
    if (!sure) return;
    try {
      await this.deps.deleteBackend(b.name);
      this.deps.notify(`${b.name} is deleted`);
    } catch {
      return;
    }
    await this.load();
  }

  private async enable(name: string): Promise<void> {
    try {
      this.showResult(await this.deps.enableBackend(name), `${name} is enabled`);
    } catch {
      return;
    }
    await this.load();
  }

  private backendsSection(): HTMLElement {
    const addForm = this.adding ? this.backendForm(null, () => { this.adding = false; void this.load(); }) : null;
    const addBtn = button('Add backend', () => { this.adding = true; this.render(); }, { primary: true, field: 'add-backend', icon: 'add' });
    if (this.backends.length === 0) {
      return h('div', { class: 'plan-empty', attrs: { 'data-role': 'no-backends' } },
        h('h3', { text: 'No backends configured' }),
        h('p', { text: 'Nothing is dispatched until you add a backend that serves at least one model.' }),
        this.adding ? addForm : addBtn);
    }
    return section('Backends',
      h('div', { class: 'section-header' }, h('span', { class: 'field-hint', text: `${this.backends.length} configured` }), this.adding ? null : addBtn),
      addForm,
      h('div', { class: 'profile-list profile-grid' }, ...this.backends.map((b) => this.card(b))));
  }

  // ─── Role candidates ─────────────────────────────────────────────────────

  private serverRows(role: string): CandRow[] {
    return this.candidates
      .filter((c) => c.scope === 'global' && c.role === role)
      .sort((a, b) => a.position - b.position)
      .map((c) => ({ backend: c.backend, model: c.model, optIn: true }));
  }

  private roleEditor(role: string): HTMLElement {
    return buildRoleEditor({
      role,
      backends: this.backends,
      rows: () => this.drafts.get(role) ?? this.serverRows(role),
      hasDraft: () => this.drafts.has(role),
      setDraft: (rows) => { if (rows) this.drafts.set(role, rows); else this.drafts.delete(role); },
      save: async (body) => {
        this.candidates = await this.deps.putCandidates(role, body, { quiet: true });
        this.deps.notify(`Saved ${role} candidates`);
      },
      emptyNote: 'No candidates: this role has no eligible backend and its stages wait.',
    });
  }

  private candidatesSection(): HTMLElement {
    return section('Role candidates',
      h('p', { class: 'field-hint', text: 'Tried in order; a paid model is used only when you list it here and opt in.' }),
      h('div', { class: 'profile-list' }, ...ROLES.map((r) => this.roleEditor(r))));
  }

  // ─── Per-project limits ──────────────────────────────────────────────────

  private limitsAuto: Autosave | null = null;

  private async drawLimits(): Promise<void> {
    // Leaving a project with a change pending saves it first (to that project).
    void this.limitsAuto?.flush();
    const slug = this.limitsSlug;
    if (!slug) {
      replace(this.limitsHost, h('p', { class: 'field-hint', text: 'No projects yet.' }));
      return;
    }
    let overrides: Record<string, string>;
    try {
      overrides = (await this.deps.limits(slug)).limits ?? {};
    } catch {
      return;
    }
    const errors = new FormErrors();
    const inputs = new Map<string, HTMLInputElement>();
    const rows = LIMIT_FIELDS.map((f) => {
      const input = textInput(f.key, overrides[f.key] ?? '', { placeholder: `inherit (${String(this.globals[f.key] ?? '')})` });
      inputs.set(f.key, input);
      return errors.add(f.key, fieldRow(f.label, input, 'Blank uses the global value')).el;
    });
    const read = (): Record<string, string> => {
      const body: Record<string, string> = {};
      for (const [k, input] of inputs) body[k] = input.value.trim();
      return body;
    };
    const save = async (): Promise<boolean> => {
      errors.clear();
      try {
        await this.deps.putLimits(slug, read(), { quiet: true });
      } catch (err) {
        errors.show(err);
        return false;
      }
      return true;
    };
    const auto = autosave({ snapshot: () => JSON.stringify(read()), save });
    this.limitsAuto = auto;
    const box = h('div', { class: 'profile-fields', attrs: { 'data-role': 'project-limits' } }, ...rows);
    replace(this.limitsHost, box, errors.general, h('div', { class: 'settings-actions' }, auto.status));
    auto.watch(box);
  }

  private limitsSection(): HTMLElement {
    const pick = selectInput('project', this.projects.map((p) => [p.slug, p.slug] as [string, string]), this.limitsSlug);
    pick.addEventListener('change', () => { this.limitsSlug = pick.value; void this.drawLimits(); });
    return section('Project limits', fieldRow('Project', pick).el, this.limitsHost);
  }

  // ─── Whole view ──────────────────────────────────────────────────────────

  /** The configuration file section: export, and a previewed import (H13). The panel keeps its state across renders. */
  private configSection(): HTMLElement {
    if (!this.configPanel) {
      this.configPanel = new ConfigPanel(defaultConfigDeps(), () => void this.load());
      this.configPanel.render();
    }
    return section('Configuration file', this.configPanel.el);
  }

  render(): void {
    if (!this.loaded) {
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading backends…' }));
      return;
    }
    const warn = this.warnings.length === 0 ? null : h('div', { class: 'warn-box', attrs: { 'data-role': 'warnings' } },
      ...this.warnings.map((w) => h('p', { text: w })),
      button('Dismiss', () => { this.warnings = []; this.render(); }, { field: 'dismiss', icon: 'remove' }));
    replace(this.host,
      h('div', { class: 'settings-host' },
        warn,
        this.backendsSection(),
        this.backends.length > 0 ? this.candidatesSection() : null,
        this.projects.length > 0 || this.backends.length > 0 ? this.limitsSection() : null,
        this.configSection()));
  }
}

/** mountBackends is the Backends tab's mount. */
export function mountBackends(host: HTMLElement): () => void {
  return new BackendsView(host, defaultBackendsDeps()).mount();
}
