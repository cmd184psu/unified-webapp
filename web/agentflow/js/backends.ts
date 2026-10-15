// backends.ts — the Backends tab: add, edit, retire and re-enable backends at
// runtime (BK1), the ordered model candidates per role with the paid badge and
// explicit opt-in (MB4, MB5), per-project limit overrides where blank means
// inherit (CF2) and the per-model pass rates (MB8). With no backend the tab
// prompts to add one (CI1). A 400 shows next to the field it names, and the
// warnings a retire returns are shown in full (SV3). Every server- or
// user-supplied string reaches the page through h(), so it is text.

import { autosave, type Autosave } from './autosave.js';
import { showToast } from '@shared';
import {
  api, type Backend, type BackendInput, type BackendResult, type Candidate, type ProjectView,
} from './api.js';
import { h, replace } from './dom.js';
import { subscribe, type AgentflowEvent } from './events.js';
import { FormErrors, badge, button, checkbox, type ToggleEl, fieldRow, section, selectInput, textInput } from './forms.js';
import { ROLES, buildRoleEditor, isPaid, unconsented, type CandRow } from './candidates.js';
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
];

export type BackendsDeps = Pick<
  typeof api,
  'backends' | 'createBackend' | 'probeBackend' | 'patchBackend' | 'retireBackend' | 'enableBackend' | 'candidates' | 'putCandidates' |
  'projects' | 'limits' | 'putLimits' | 'settings'
> & { notify(message: string): void };

export const defaultBackendsDeps = (): BackendsDeps => ({
  backends: api.backends,
  createBackend: api.createBackend,
  probeBackend: api.probeBackend,
  patchBackend: api.patchBackend,
  retireBackend: api.retireBackend,
  enableBackend: api.enableBackend,
  candidates: api.candidates,
  putCandidates: api.putCandidates,
  projects: api.projects,
  limits: api.limits,
  putLimits: api.putLimits,
  settings: api.settings,
  notify: (m) => void showToast(m, 'success'),
});

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
  /** role -> rows being edited; present only while the role has unsaved edits. */
  private drafts = new Map<string, CandRow[]>();
  private limitsSlug = '';
  private limitsHost = h('div');
  private off: (() => void) | null = null;

  constructor(private host: HTMLElement, private deps: BackendsDeps) {}

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      if (['backend.update', 'candidates.update', 'state.changed', 'settings.update'].includes(ev.type) && this.idle()) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
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
      'Sent to the hosted service as the credential. Leave blank to keep the stored key'));
    const hostField = errors.add('host', fieldRow('Host', host, 'An http or https URL'));
    // opencode's own provider has no host and no key: its free models are reached through opencode itself.
    const builtin = (): boolean => apiType.value === 'opencode';
    const syncKey = (): void => {
      if (hosted.checked && !builtin()) keyField.el.removeAttribute('hidden');
      else keyField.el.setAttribute('hidden', '');
      if (builtin()) hostField.el.setAttribute('hidden', '');
      else hostField.el.removeAttribute('hidden');
    };
    (hosted.firstChild as HTMLInputElement).addEventListener('change', syncKey);
    apiType.addEventListener('change', syncKey);
    syncKey();
    type ModelControl = HTMLInputElement | HTMLSelectElement;
    type ModelRow = { model: ModelControl; paid: ToggleEl; max: HTMLInputElement; ctx: HTMLInputElement; tier: HTMLSelectElement; auto: string; el: HTMLElement };
    const models: ModelRow[] = [];
    // Names the host reported when probed; model rows then pick from them instead of typing.
    let probed: string[] = [];
    // Context windows the host reported, by model name; they prefill an empty context field.
    let contexts: Record<string, number> = {};
    const modelControl = (m: string): ModelControl => probed.length > 0
      ? selectInput('model', probed.map((n): [string, string] => [n, n]), m)
      : textInput('model', m, { placeholder: 'model name (or Probe host)' });
    const modelList = h('div', { class: 'wizard-list' });
    const prefill = (row: ModelRow): void => {
      const c = contexts[row.model.value];
      const next = c > 0 ? String(c) : '';
      // Fill a blank field, or swap the value a probe put there for the previously picked model; never overwrite a typed one.
      if (row.ctx.value === '' || row.ctx.value === row.auto) {
        row.ctx.value = next;
        row.auto = next;
      }
    };
    const draw = (row: ModelRow): void => {
      row.model.addEventListener('change', () => prefill(row));
      replace(row.el,
        row.model,
        h('span', { class: 'inline-check' }, row.paid, 'paid'),
        h('span', { class: 'inline-check', attrs: { title: 'Context window in tokens; blank = unknown' } }, 'ctx', row.ctx),
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
        ctx: textInput('context_tokens', ctx > 0 ? String(ctx) : '', { type: 'number', placeholder: '?' }),
        tier: selectInput('tier', TIERS, String(tier)),
        auto: '',
        el: h('div', { class: 'inline-row', attrs: { 'data-role': 'model-row' } }),
      };
      models.push(row);
      draw(row);
      modelList.appendChild(row.el);
    };
    for (const m of initial?.models ?? []) addModel(m.model, m.paid, m.max_concurrent, m.context_tokens ?? 0, m.tier ?? 0);
    if (!initial) addModel();
    const probe = async (): Promise<void> => {
      errors.clear();
      try {
        const res = await this.deps.probeBackend(builtin() ? '' : host.value.trim(), apiType.value);
        probed = res.models;
        contexts = res.contexts ?? {};
        if (probed.length === 0) throw new Error('the host reported no models');
        // Swap each row's control for a picker, keeping a name the host still serves.
        for (const row of models) {
          const keep = row.model.value;
          row.model = modelControl(keep);
          draw(row);
          prefill(row);
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
        models: models.map((m) => ({ model: m.model.value.trim(), paid: m.paid.checked, max_concurrent: Number(m.max.value) || 0, context_tokens: Math.max(0, Math.round(Number(m.ctx.value) || 0)), tier: Number(m.tier.value) || 0 })).filter((m) => m.model !== ''),
      };
      if (hosted.checked && !builtin() && apiKey.value !== '') body.api_key = apiKey.value;
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
    const cancel = checkbox('cancel_running', false);
    const last = lastForRoles(this.candidates, b.name);
    const onlyActive = this.backends.filter((x) => !x.retired && x.name !== b.name).length === 0;
    const notes = [
      ...(onlyActive ? ['This is the only active backend: after it is retired nothing can be dispatched until you add or enable one.'] : []),
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
      this.showResult(res, `Retired ${b.name}`);
      await this.load();
    };
    return h('div', { class: 'warn-box', attrs: { 'data-panel': 'retire' } },
      ...notes.map((n) => h('p', { text: n })),
      h('span', { class: 'inline-check' }, cancel, 'Cancel stages running on this backend now'),
      h('div', { class: 'row-actions' },
        button('Retire backend', () => void run(), { primary: true, field: 'confirm-retire', icon: 'trash', word: true }),
        button('Keep it', () => { this.retiring.delete(b.name); this.render(); }, { field: 'cancel-retire', icon: 'cancel', word: true })));
  }

  private card(b: Backend): HTMLElement {
    if (this.editing.has(b.name)) {
      return this.backendForm(b, () => { this.editing.delete(b.name); void this.load(); });
    }
    const models = b.models.map((m) => h('span', { class: 'row-actions' }, m.model, m.paid ? badge('paid') : null,
      m.max_concurrent > 0 ? badge(`max ${m.max_concurrent} at once`, 'muted') : null,
      m.context_tokens ? badge(formatTokens(m.context_tokens), 'muted') : null,
      m.tier ? badge(`tier ${m.tier}`, m.tier === 3 ? 'warn' : 'muted') : null));
    return h('div', { class: 'profile-card', attrs: { 'data-backend': b.name } },
      h('div', { class: 'profile-header' },
        h('span', { class: 'row-actions' },
          h('strong', { text: b.name }),
          b.paid ? badge('paid') : null,
          b.hosted ? badge('hosted', 'muted') : null,
          b.has_key ? badge('key set', 'ok') : null,
          b.hosted && b.api_type !== 'opencode' && isPrivateHost(b.host) ? h('span', { class: 'badge badge-warn', text: '\u26A0 hosted?', attrs: { title: 'Flagged hosted, but the host is a private or local address: it may be mis-flagged. Edit the backend and untick Hosted if it runs on your own machine or network.', 'data-role': 'hosted-warning' } }) : null,
          b.retired ? badge('retired', 'warn') : null,
          b.down_until ? badge('unreachable', 'warn') : null),
        h('div', { class: 'profile-actions' },
          button('Edit', () => { this.editing.add(b.name); this.render(); }, { field: 'edit', icon: 'edit' }),
          b.retired
            ? button('Enable', () => void this.enable(b.name), { field: 'enable', icon: 'approve', word: true })
            : button('Retire', () => { this.retiring.add(b.name); this.render(); }, { field: 'retire', icon: 'trash', word: true }))),
      h('div', { class: 'profile-fields' },
        b.api_type === 'opencode' ? null : h('div', {}, h('label', { text: 'Host' }), h('span', { text: b.host })),
        h('div', {}, h('label', { text: 'API type' }), h('span', { text: b.api_type })),
        h('div', {}, h('label', { text: 'Slots' }), h('span', { text: String(b.slots) })),
        h('div', {}, h('label', { text: 'Models' }), ...models)),
      this.retiring.has(b.name) ? this.retirePanel(b) : null);
  }

  private async enable(name: string): Promise<void> {
    try {
      this.showResult(await this.deps.enableBackend(name), `Enabled ${name}`);
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
      h('div', { class: 'profile-list' }, ...this.backends.map((b) => this.card(b))));
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
        this.projects.length > 0 || this.backends.length > 0 ? this.limitsSection() : null));
  }
}

/** mountBackends is the Backends tab's mount. */
export function mountBackends(host: HTMLElement): () => void {
  return new BackendsView(host, defaultBackendsDeps()).mount();
}
