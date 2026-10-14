// settings.ts — the Settings tab: the global limits (CF1), verify slots and
// backend cooldown, and the opencode path with its version probe (OC1). A 400
// from the server names the field that is wrong and the message shows next to
// that field (UI5). The shared limit field list is also used by the
// per-project limits form in backends.ts (CF2).

import { showToast } from '@shared';
import { api, type OpenCodeStatus, type Settings } from './api.js';
import { h, replace } from './dom.js';
import { subscribe, type AgentflowEvent } from './events.js';
import { FormErrors, badge, button, fieldRow, section, textInput } from './forms.js';

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
  { key: 'worker_timeout', label: 'Worker timeout', hint: 'A duration such as 30m or 1h' },
  { key: 'review_timeout', label: 'Review timeout', hint: 'A duration such as 15m' },
  { key: 'plan_timeout', label: 'Plan timeout', hint: 'A duration such as 20m' },
  { key: 'verify_timeout', label: 'Verify timeout', hint: 'A duration such as 20m' },
];

const EXECUTION_FIELDS: LimitField[] = [
  { key: 'verify_slots', label: 'Verify slots', hint: 'Verify and merge stages that run at once, 1 to 64' },
  { key: 'backend_cooldown', label: 'Backend cooldown', hint: 'How long an unreachable backend is skipped, such as 2m' },
];

export type SettingsDeps = Pick<typeof api, 'settings' | 'putSettings' | 'opencodeVersion'> & {
  notify(message: string): void;
};

export const defaultSettingsDeps = (): SettingsDeps => ({
  settings: api.settings,
  putSettings: api.putSettings,
  opencodeVersion: api.opencodeVersion,
  notify: (m) => void showToast(m, 'success'),
});

const str = (v: string | number | undefined): string => (v === undefined ? '' : String(v));

export class SettingsView {
  private values: Settings = {};
  private inputs = new Map<string, HTMLInputElement>();
  private errors = new FormErrors();
  private oc: OpenCodeStatus | null = null;
  private ocHost = h('div');
  private dirty = false;
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
    };
  }

  async load(): Promise<void> {
    try {
      const [settings, oc] = await Promise.all([this.deps.settings(), this.deps.opencodeVersion(false)]);
      this.values = settings;
      this.oc = oc;
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
        button('Refresh', () => void this.refreshVersion(), { field: 'oc-refresh' })));
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

  /** save sends every field; the server validates and names a bad field. */
  async save(): Promise<void> {
    this.errors.clear();
    const body: Record<string, string> = {};
    for (const [key, input] of this.inputs) body[key] = input.value.trim();
    try {
      this.values = await this.deps.putSettings(body, { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return;
    }
    this.dirty = false;
    this.deps.notify('Settings saved');
    this.render();
  }

  render(): void {
    this.inputs.clear();
    this.errors = new FormErrors();
    const limits = h('div', { class: 'profile-fields' }, ...LIMIT_FIELDS.map((f) => this.row(f)));
    const exec = h('div', { class: 'profile-fields' }, ...EXECUTION_FIELDS.map((f) => this.row(f)));
    const path = this.row({ key: 'opencode_path', label: 'opencode path', hint: 'Program to run; a change takes effect after a restart' });
    this.renderOpenCode();
    replace(this.host,
      h('div', { class: 'settings-host' },
        section('Limits', limits),
        section('Execution', exec),
        section('opencode', path, this.ocHost),
        this.errors.general,
        h('div', { class: 'settings-actions' }, button('Save settings', () => void this.save(), { primary: true, field: 'save' }))));
  }
}

/** mountSettings is the Settings tab's mount. */
export function mountSettings(host: HTMLElement): () => void {
  return new SettingsView(host, defaultSettingsDeps()).mount();
}
