// settings.ts — the settings section of the hamburger drawer: the global limits
// (CF1), verify slots and backend cooldown, the opencode path with its status
// (OC1) and the folders the directory picker may browse. Every setting applies
// as soon as it is saved, with no restart. A 400
// from the server names the field that is wrong and the message shows next to
// that field (UI5). The shared limit field list is also used by the
// per-project limits form in backends.ts (CF2).

import { autosave, type Autosave } from './autosave.js';
import { showToast } from '@shared';
import { api, type OpenCodeStatus, type RestartView, type ServiceHoldView, type Settings } from './api.js';
import { h, replace } from './dom.js';
import { subscribe, type AgentflowEvent } from './events.js';
import { pickDirectory } from './dirpicker.js';
import { FormErrors, badge, button, checkbox, fieldRow, section, textInput } from './forms.js';

export interface LimitField {
  key: string;
  label: string;
  hint: string;
}

/** LIMIT_FIELDS are the settings a project may override, in display order. */
export const LIMIT_FIELDS: LimitField[] = [
  { key: 'max_attempts', label: 'Max attempts', hint: 'Whole number, 2 to 50' },
  { key: 'stuck_threshold', label: 'Stuck threshold', hint: 'Identical failures before a task is stuck; at most max attempts' },
  { key: 'max_infra_retries', label: 'Max infra retries', hint: 'Whole number, 0 to 50' },
  { key: 'plan_max_rounds', label: 'Plan max rounds', hint: 'ralplan rounds before the best plan is presented, 1 to 50' },
  { key: 'acceptance_rounds', label: 'Acceptance rounds', hint: 'How many times a finished effort is checked against its FRD, with gaps turned into follow-up tasks; 0 turns the check off, 2 by default' },
  { key: 'review_panel', label: 'Review panel', hint: 'How many different reviewer models must each pass a task before it merges; 1 to 3, 1 by default' },
  { key: 'stall_minutes', label: 'Stall minutes', hint: 'Minutes a stage may say nothing, or a running effort sit idle, before it counts as stalled; 1 to 100, 10 by default' },
  { key: 'worker_timeout', label: 'Worker timeout', hint: 'A duration such as 30m or 1h' },
  { key: 'review_timeout', label: 'Review timeout', hint: 'A duration such as 15m' },
  { key: 'plan_timeout', label: 'Plan timeout', hint: 'A duration such as 20m' },
  { key: 'verify_timeout', label: 'Verify timeout', hint: 'A duration such as 20m' },
  { key: 'resolver_mode', label: 'Resolver mode', hint: 'What the resolver may do with a stuck task: off, advise (its decision waits for you to apply it; the default) or auto (applied at once)' },
];

// BACKLOG_FIELDS controls AF-15's ready backlog: how many efforts auto-start
// keeps running, and how many revise rounds a plan gets before a person is
// asked to settle it.
const BACKLOG_FIELDS: LimitField[] = [
  { key: 'min_running_efforts', label: 'Minimum running efforts', hint: 'Auto-start ready efforts until this many run across all projects; 0 turns auto-start off' },
  { key: 'plan_revise_limit', label: 'Plan revise limit', hint: 'After this many revise rounds on the same FRD, stop and ask a person; 3 by default' },
];

const EXECUTION_FIELDS: LimitField[] = [
  { key: 'verify_slots', label: 'Verify slots', hint: 'Verify and merge stages that run at once, 1 to 64' },
  { key: 'context_floor_tokens', label: 'Context floor', hint: 'Tokens of context assumed for a model whose served window is not known, 2048 or more' },
  { key: 'backend_cooldown', label: 'Backend cooldown', hint: 'How long an unreachable backend is skipped, such as 2m' },
];

export type SettingsDeps = Pick<typeof api, 'settings' | 'putSettings' | 'opencodeVersion' | 'restartStatus' | 'restartService' | 'cancelRestart' | 'serviceHold' | 'holdService' | 'liftHold'> & {
  /** Opens the directory picker at start; the chosen path, or null when cancelled. */
  pickDir(start: string): Promise<string | null>;
  notify(message: string): void;
};

export const defaultSettingsDeps = (): SettingsDeps => ({
  settings: api.settings,
  putSettings: api.putSettings,
  opencodeVersion: api.opencodeVersion,
  restartStatus: api.restartStatus,
  restartService: api.restartService,
  cancelRestart: api.cancelRestart,
  serviceHold: api.serviceHold,
  holdService: api.holdService,
  liftHold: api.liftHold,
  pickDir: (start) => pickDirectory(start),
  notify: (m) => void showToast(m, 'success'),
});

const str = (v: string | number | string[] | undefined): string => (v === undefined || Array.isArray(v) ? '' : String(v));

export class SettingsView {
  private values: Settings = {};
  private inputs = new Map<string, HTMLInputElement>();
  /** The BotChat dispatch switch; off is mirror mode. It is saved as botchat_mode. */
  private dispatch: { checked: boolean } | null = null;
  private errors = new FormErrors();
  private oc: OpenCodeStatus | null = null;
  private ocHost = h('div');
  private roots: string[] = [];
  private svc: RestartView | null = null;
  private hold: ServiceHoldView | null = null;
  private svcHost = h('div', { attrs: { 'data-role': 'service' } });
  private svcTimer: ReturnType<typeof setInterval> | null = null;
  private rootsHost = h('div', { class: 'wizard-list', attrs: { 'data-role': 'browse-roots' } });
  private dirty = false;
  private auto: Autosave | null = null;
  private off: (() => void) | null = null;

  constructor(private host: HTMLElement, private deps: SettingsDeps) {}

  /** mount loads the settings and returns the dispose. */
  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => {
      if (ev.type === 'settings.update' && !this.dirty) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      if (this.svcTimer) clearInterval(this.svcTimer);
      this.svcTimer = null;
      void this.auto?.flush();
    };
  }

  async load(): Promise<void> {
    try {
      const [settings, oc] = await Promise.all([this.deps.settings(), this.deps.opencodeVersion(false)]);
      // Our own save echoes back as an update: nothing to redraw, and redrawing would drop what is being typed.
      const same = this.auto !== null && JSON.stringify(settings) === JSON.stringify(this.values);
      this.values = settings;
      this.oc = oc;
      this.svc = await this.deps.restartStatus({ quiet: true }).catch(() => null);
      this.hold = await this.deps.serviceHold({ quiet: true }).catch(() => null);
      if (same) return;
      this.roots = Array.isArray(settings.browse_roots) ? [...settings.browse_roots] : [];
    } catch {
      // apiFetch already told the user; keep what is shown.
      if (Object.keys(this.values).length === 0) return;
    }
    this.dirty = false;
    this.render();
  }

  private row(f: { key: string; label: string; hint: string }): HTMLElement {
    const input = textInput(f.key, str(this.values[f.key]));
    input.addEventListener('input', () => { this.dirty = true; });
    this.inputs.set(f.key, input);
    return this.errors.add(f.key, fieldRow(f.label, input, f.hint)).el;
  }

  private renderOpenCode(): void {
    const oc = this.oc;
    replace(this.ocHost,
      h('div', { class: 'row-actions' },
        oc ? badge(oc.status === 'ok' ? 'available' : 'unavailable', oc.status === 'ok' ? 'ok' : 'warn') : null,
        h('span', { class: 'field-hint', attrs: { 'data-role': 'oc-version' },
          text: oc ? (oc.version ? `version ${oc.version}` : oc.error ?? '') : 'not probed yet' }),
        button('Refresh', () => void this.refreshVersion(), { field: 'oc-refresh', icon: 'refresh' })));
  }

  /**
   * The Service section: hold the whole service so it starts nothing new
   * (AF-10), and restart it once running work has ended (H25).
   */
  private renderService(): void {
    const svc = this.svc;
    const hold = this.hold;
    const cancel = checkbox('cancel_running', false);
    const stages = (n: number): string => `${n} running stage${n === 1 ? '' : 's'}`;
    const run = async (call: () => Promise<RestartView>, said: string): Promise<void> => {
      try {
        this.svc = await call();
      } catch {
        return;
      }
      this.deps.notify(said);
      this.renderService();
    };
    const runHold = async (call: () => Promise<ServiceHoldView>, said: string): Promise<void> => {
      try {
        this.hold = await call();
      } catch {
        return;
      }
      this.deps.notify(said);
      this.renderService();
    };
    if (this.svcTimer) clearInterval(this.svcTimer);
    this.svcTimer = null;
    if (svc?.state === 'draining') {
      // The service goes away when the work ends; ask again until it does, then until it answers.
      this.svcTimer = setInterval(() => {
        void this.deps.restartStatus({ quiet: true }).then((v) => { this.svc = v; this.renderService(); }, () => undefined);
      }, 5000);
    }
    if (hold?.active) {
      // Nothing new starts, so there is nothing else to offer but lifting it.
      replace(this.svcHost,
        h('div', { class: 'warn-box', attrs: { 'data-role': 'service-hold' } },
          h('p', { text: `Service held: ${hold.reason ?? ''}` }),
          h('p', { class: 'field-hint', text: hold.restart
            ? `A restart waits for the ${stages(hold.open_stages)} to end, and nothing new starts until it does.`
            : `${stages(hold.open_stages)} still finishing; no project starts anything new.` }),
          h('div', { class: 'row-actions' },
            button(hold.restart ? 'Lift hold and cancel restart' : 'Lift hold', () => void runHold(() => this.deps.liftHold(), 'Hold lifted'), { field: 'lift-hold', icon: 'cancel', word: true }))));
      return;
    }
    const reason = textInput('hold_reason', '', { placeholder: 'why the service should start nothing' });
    const restartBox = svc?.state === 'draining'
      ? h('div', { class: 'warn-box' },
          h('p', { text: `Restarting when the ${stages(svc.open_stages)} end. ${svc.plan?.cancel_running ? 'Running stages were cancelled.' : 'Nothing new starts meanwhile.'}` }),
          button('Cancel restart', () => void run(() => this.deps.cancelRestart(), 'Restart cancelled'), { field: 'cancel-restart', icon: 'cancel', word: true }))
      : h('div', {},
          h('span', { class: 'inline-check' }, cancel, 'Cancel running stages instead of waiting for them'),
          h('div', { class: 'row-actions' },
            button('Restart service', () => void run(() => this.deps.restartService(cancel.checked), 'Restart requested'), { field: 'restart-service', icon: 'refresh', word: true }),
            h('span', { class: 'field-hint', text: svc ? `running since ${new Date(svc.started).toLocaleString()}` : '' })));
    replace(this.svcHost,
      restartBox,
      h('p', { class: 'field-hint', text: 'A hold pauses every running project and lets nothing new start, for a change you make by hand.' }),
      h('div', { class: 'inline-row' },
        reason,
        button('Hold service', () => void runHold(() => this.deps.holdService(reason.value.trim()), 'Service held'), { field: 'hold-service', icon: 'pause', word: true })));
  }

  private renderRoots(): void {
    const edit = (next: string[]): void => { this.roots = next; this.dirty = true; this.renderRoots(); this.auto?.touch(); };
    replace(this.rootsHost,
      ...this.roots.map((r, i) => h('div', { class: 'inline-row', attrs: { 'data-root': i } },
        h('code', { text: r }),
        button('Remove', () => edit(this.roots.filter((_, j) => j !== i)), { field: 'remove-root', icon: 'remove' }))),
      button('Add folder', () => void this.addRoot(edit), { field: 'add-root', icon: 'add' }));
  }

  private async addRoot(edit: (next: string[]) => void): Promise<void> {
    const chosen = await this.deps.pickDir(this.roots[this.roots.length - 1] ?? '');
    if (chosen !== null && !this.roots.includes(chosen)) edit([...this.roots, chosen]);
  }

  /** refreshVersion asks the server to probe opencode again (OC1). */
  async refreshVersion(): Promise<void> {
    try {
      this.oc = await this.deps.opencodeVersion(true);
    } catch {
      return;
    }
    this.renderOpenCode();
  }

  /** saveNow saves a pending change now (what the pause would do). */
  saveNow(): Promise<void> {
    return this.auto?.flush() ?? Promise.resolve();
  }

  private body(): Settings {
    const body: Settings = {};
    for (const [key, input] of this.inputs) body[key] = input.value.trim();
    if (this.dispatch) body.botchat_mode = this.dispatch.checked ? 'dispatch' : 'mirror';
    if ('browse_roots' in this.values || this.roots.length > 0) body.browse_roots = this.roots;
    return body;
  }

  /** save sends every field; the server validates and names a bad field. It does not redraw: the form is being edited. */
  async save(): Promise<boolean> {
    this.errors.clear();
    try {
      this.values = await this.deps.putSettings(this.body(), { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return false;
    }
    this.dirty = false;
    return true;
  }

  render(): void {
    this.inputs.clear();
    this.errors = new FormErrors();
    const limits = h('div', { class: 'profile-fields' }, ...LIMIT_FIELDS.map((f) => this.row(f)));		const exec = h('div', { class: 'profile-fields' }, ...EXECUTION_FIELDS.map((f) => this.row(f)));
		const backlog = h('div', { class: 'profile-fields' }, ...BACKLOG_FIELDS.map((f) => this.row(f)));
    const path = this.row({ key: 'opencode_path', label: 'opencode path', hint: 'Program to run' });
    const bcUrl = this.row({ key: 'botchat_url', label: 'BotChat address', hint: 'e.g. https://botchat.example.net; empty turns the relay off' });
    const dispatch = checkbox('botchat_dispatch', str(this.values.botchat_mode) === 'dispatch');
    this.dispatch = dispatch;
    dispatch.addEventListener('change', () => { this.dirty = true; this.auto?.touch(); });
    const bcMode = fieldRow('Send work through BotChat', dispatch, 'Off (the usual): agentflow runs each stage itself and posts to BotChat what happens. On: each stage is also handed to an agent through BotChat, with a direct run if BotChat does not answer; this adds model tokens and about 2 s per stage.');
    const bcKey = this.row({ key: 'botchat_key_file', label: 'BotChat key file', hint: 'File holding the API key, e.g. ~/.botchat_apikey; read on every post' });
    this.renderOpenCode();
    this.renderService();
    this.renderRoots();
    this.auto?.stop();
    this.auto = autosave({ snapshot: () => JSON.stringify(this.body()), save: () => this.save() });
    const root = h('div', { class: 'settings-host settings-host-drawer' },
        section('Limits', limits),				section('Execution', exec),
				section('Backlog', backlog),
        section('opencode', path, this.ocHost),
        section('BotChat', bcUrl, bcKey, bcMode.el),
        section('Service', this.svcHost),
        section('Browse limit', this.rootsHost),
        this.errors.general,
        h('div', { class: 'settings-actions' }, this.auto.status));
    replace(this.host, root);
    this.auto.watch(root);
  }
}
