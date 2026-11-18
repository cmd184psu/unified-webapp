// configfile.ts — export and import of the configuration (H13): backends with
// their models, tiers and tool flags, the global candidate list of each role,
// and the limit settings. Projects, efforts and keys are never in the file. An
// import is previewed first (a dry run) and applied only on the second click.

import { api, type ImportReport } from './api.js';
import { h, replace } from './dom.js';
import { button } from './forms.js';

export interface ConfigDeps {
  importConfig(config: unknown, apply: boolean): Promise<ImportReport>;
}

export const defaultConfigDeps = (): ConfigDeps => ({ importConfig: (c, apply) => api.importConfig(c, apply) });

/** describe is the preview sentence list for a report. */
export function describe(r: ImportReport): string[] {
  const out: string[] = [];
  if (r.created_backends.length) out.push(`create backend(s): ${r.created_backends.join(', ')}`);
  if (r.updated_backends.length) out.push(`update backend(s), replacing their model lists: ${r.updated_backends.join(', ')}`);
  if (r.candidate_roles.length) out.push(`replace the candidate list of: ${r.candidate_roles.join(', ')}`);
  if (r.settings.length) out.push(`set: ${r.settings.join(', ')}`);
  if (out.length === 0) out.push('nothing to change');
  return out;
}

export class ConfigPanel {
  readonly el: HTMLElement = h('div', { class: 'config-panel', attrs: { 'data-role': 'config-panel' } });
  private pending: unknown = null;
  private report: ImportReport | null = null;
  private error = '';
  private done = '';

  constructor(private deps: ConfigDeps, private onApplied: () => void = () => {}) {}

  /** loadText reads the chosen file's text and asks the server what importing it would do. */
  async loadText(text: string): Promise<void> {
    this.error = '';
    this.done = '';
    this.report = null;
    this.pending = null;
    try {
      this.pending = JSON.parse(text);
    } catch {
      this.error = 'That file is not JSON.';
      this.render();
      return;
    }
    try {
      this.report = await this.deps.importConfig(this.pending, false);
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      this.pending = null;
    }
    this.render();
  }

  private async apply(): Promise<void> {
    if (this.pending === null) return;
    try {
      const r = await this.deps.importConfig(this.pending, true);
      this.done = `Imported: ${describe(r).join('; ')}.`;
      this.pending = null;
      this.report = null;
      this.error = '';
      this.onApplied();
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
    }
    this.render();
  }

  private cancel(): void {
    this.pending = null;
    this.report = null;
    this.error = '';
    this.render();
  }

  render(): void {
    const file = h('input', { attrs: { type: 'file', accept: '.json,application/json', 'data-field': 'config-file' } }) as HTMLInputElement;
    file.addEventListener('change', () => {
      const f = file.files?.[0];
      if (f) void f.text().then((t) => this.loadText(t));
    });
    const r = this.report;
    replace(this.el,
      h('div', { class: 'inline-row' },
        h('a', { class: 'ui-modal-btn', text: 'Export configuration', attrs: { href: '/api/config/export', download: '', 'data-role': 'export-config' } }),
        h('span', { class: 'field-hint', text: 'Import a configuration file:' }), file),
      h('p', { class: 'field-hint', text: 'The file holds backends, models, tiers, tool flags, candidate lists and limits. It holds no projects and no API keys.' }),
      this.error ? h('div', { class: 'field-error', text: this.error, attrs: { 'data-role': 'config-error' } }) : null,
      this.done ? h('div', { class: 'field-hint', text: this.done, attrs: { 'data-role': 'config-done' } }) : null,
      r ? h('div', { class: 'warn-box', attrs: { 'data-role': 'config-preview' } },
        h('strong', { text: 'Importing this file would:' }),
        h('ul', {}, ...describe(r).map((t) => h('li', { text: t }))),
        ...r.warnings.map((w) => h('p', { class: 'field-hint', text: w })),
        h('div', { class: 'inline-row' },
          button('Apply import', () => void this.apply(), { primary: true, field: 'apply-import' }),
          button('Cancel', () => this.cancel(), { field: 'cancel-import' }))) : null);
  }
}
