// projects.ts — project create and edit (PJ1): slug, absolute repo_path with
// the git-init offer, model scope, the feedback commands as argv rows and the
// per-role candidate override (role preferences). A command is a program and
// its arguments, split on whitespace and run as a fixed argv, never through a
// shell, so the editor shows the argv and warns when a shell operator would
// only be passed as a plain argument.

import { autosave, type Autosave } from './autosave.js';
import { confirmDialog, showToast } from '@shared';
import { defaultUnregisterDeps, unregister, unregisterBlock, type UnregisterDeps } from './unregister.js';
import { api, ApiError, type Backend, type Candidate, type PluginPin, type ProjectInput, type ProjectView } from './api.js';
import { ROLES, buildRoleEditor, type CandRow } from './candidates.js';
import { h, replace } from './dom.js';
import { pickDirectory } from './dirpicker.js';
import { FormErrors, RowsEditor, button, checkbox, type ToggleEl, fieldRow, section, selectInput, textInput } from './forms.js';

export type ProjectDeps = Pick<
  typeof api,
  'createProject' | 'patchProject' | 'gitInit' | 'deleteProject' | 'backends' | 'candidates' | 'putProjectCandidates' | 'clearProjectCandidates' | 'definitionOfDone'
> & Partial<Pick<UnregisterDeps, 'showCommands'>> & {
  confirm(message: string, confirmLabel?: string): Promise<boolean>;
  /** Opens the directory picker at start; the chosen path, or null when cancelled. */
  pickDir(start: string): Promise<string | null>;
  notify(message: string): void;
};

export const defaultProjectDeps = (): ProjectDeps => ({
  createProject: api.createProject,
  patchProject: api.patchProject,
  gitInit: api.gitInit,
  deleteProject: api.deleteProject,
  backends: api.backends,
  candidates: api.candidates,
  putProjectCandidates: api.putProjectCandidates,
  clearProjectCandidates: api.clearProjectCandidates,
  definitionOfDone: api.definitionOfDone,
  confirm: (m, label = 'Confirm') => confirmDialog(m, { confirmLabel: label }),
  pickDir: (start) => pickDirectory(start),
  notify: (m) => void showToast(m, 'success'),
  showCommands: defaultUnregisterDeps().showCommands,
});

/** splitArgv splits a command line on whitespace into its argv; there is no quoting. */
export function splitArgv(s: string): string[] {
  return s.split(/\s+/).filter((t) => t !== '');
}

/** joinArgv is the stored form of an argv: the tokens joined by single spaces. */
export function joinArgv(argv: string[]): string {
  return argv.join(' ');
}

const SHELL_TOKENS = new Set(['|', '||', '&', '&&', ';', '>', '>>', '<', '2>&1']);

/** shellOperators lists the argv tokens that look like shell syntax; they would be plain arguments. */
export function shellOperators(argv: string[]): string[] {
  return argv.filter((t) => SHELL_TOKENS.has(t) || t.includes('$(') || t.includes('`'));
}

/** isNotARepo recognises the 400 that offers git init. */
export function isNotARepo(err: unknown): boolean {
  return err instanceof ApiError && err.field === 'repo_path' && /not a git repository/.test(err.message);
}

export const SCOPES: Array<[string, string]> = [['local', 'local (only local backends)'], ['any', 'any (hosted backends allowed)']];

export interface ProjectMode {
  /** The project being edited; undefined creates one. */
  project?: ProjectView;
}

export class ProjectForm {
  private errors = new FormErrors();
  private cmds: RowsEditor<string>;
  private plugins: RowsEditor<PluginPin>;
  private slug: HTMLInputElement;
  private repoPath: string;
  private repoBtn: HTMLElement;
  private scope: HTMLSelectElement;
  private cleanup: ToggleEl;
  private backends: Backend[] = [];
  private cands: Candidate[] = [];
  private drafts = new Map<string, CandRow[]>();
  private rolesHost = h('div');
  /** Definition of done: the detected list, and the override rows once the project has or starts one. */
  private dodDetected: string[] = [];
  private dodRows: RowsEditor<string> | null = null;
  private dodReset = false;
  private dodHost = h('div', { attrs: { 'data-role': 'dod-editor' } });
  /** An existing project saves its edits by itself; creating one is one explicit step. */
  private auto: Autosave | null = null;
  private bump = (): void => this.auto?.touch();

  constructor(private host: HTMLElement, private deps: ProjectDeps, private mode: ProjectMode, private done: (slug: string) => void) {
    const p = mode.project;
    this.slug = textInput('slug', p?.slug ?? '', { readOnly: !!p, placeholder: 'my-project' });
    this.repoPath = p?.repo_path ?? '';
    this.repoBtn = h('button', {
      class: 'ui-modal-btn repo-pick',
      attrs: { type: 'button', 'data-field': 'repo_path', title: 'Choose the repository folder' },
      on: { click: () => void this.pickRepo() },
    });
    this.drawRepo();
    this.scope = selectInput('model_scope', SCOPES, p?.model_scope ?? 'local');
    this.cleanup = checkbox('cleanup_enabled', p?.cleanup_enabled ?? false);
    this.cmds = new RowsEditor<string>((c) => {
      const input = textInput('feedback_cmd', c, { placeholder: 'go test ./...' });
      const preview = h('code', { class: 'field-hint', attrs: { 'data-role': 'argv' } });
      const warn = h('div', { class: 'field-error', attrs: { 'data-role': 'shell-warning', hidden: true } });
      const sync = (): void => {
        const argv = splitArgv(input.value);
        preview.textContent = argv.length ? `argv: ${JSON.stringify(argv)}` : '';
        const ops = shellOperators(argv);
        warn.textContent = ops.length ? `No shell is used: ${ops.join(' ')} would be passed as a plain argument` : '';
        if (ops.length) warn.removeAttribute('hidden');
        else warn.setAttribute('hidden', '');
      };
      input.addEventListener('input', sync);
      sync();
      return { el: h('div', { class: 'wizard-field' }, input, preview, warn), read: () => joinArgv(splitArgv(input.value)) };
    }, p?.feedback_cmds ?? [], () => '', 'Add command', () => this.bump());
    this.plugins = new RowsEditor<PluginPin>((pl) => {
      const name = textInput('plugin_name', pl.name, { placeholder: 'package-name' });
      const version = textInput('plugin_version', pl.version, { placeholder: '1.2.3' });
      return { el: h('div', { class: 'row-actions' }, name, version), read: () => ({ name: name.value.trim(), version: version.value.trim() }) };
    }, p?.plugins ?? [], () => ({ name: '', version: '' }), 'Add plugin', () => this.bump());
  }

  private drawRepo(): void {
    this.repoBtn.textContent = this.repoPath || 'Choose a folder...';
  }

  /** pickRepo lets the user choose the repository folder on the server. */
  async pickRepo(): Promise<void> {
    const chosen = await this.deps.pickDir(this.repoPath);
    if (chosen === null) return;
    this.repoPath = chosen;
    this.drawRepo();
    this.bump();
  }

  async mount(): Promise<void> {
    if (this.mode.project) {
      try {
        [this.backends, this.cands] = await Promise.all([this.deps.backends(), this.deps.candidates()]);
      } catch {
        // apiFetch already told the user; the role section stays empty.
      }
      try {
        const dod = await this.deps.definitionOfDone(this.mode.project.slug, { quiet: true });
        if (dod.source === 'project') this.startDod(dod.items);
        else this.dodDetected = dod.items;
      } catch {
        // No definition of done to show.
      }
    }
    this.render();
  }

  private body(): ProjectInput {
    return {
      slug: this.slug.value.trim(),
      repo_path: this.repoPath,
      model_scope: this.scope.value,
      feedback_cmds: this.cmds.read().filter((c) => c !== ''),
      cleanup_enabled: this.cleanup.checked,
      plugins: this.plugins.read().filter((pl) => pl.name !== '' || pl.version !== ''),
    };
  }

  /** save creates or updates the project; a repo path that is not yet a repository offers git init (PJ1). */
  async save(): Promise<void> {
    this.errors.clear();
    const body = this.body();
    const attempt = (): Promise<ProjectView> => this.mode.project
      ? this.deps.patchProject(this.mode.project.slug, { repo_path: body.repo_path, model_scope: body.model_scope, feedback_cmds: body.feedback_cmds, cleanup_enabled: body.cleanup_enabled, plugins: body.plugins, ...this.dodPatch() }, { quiet: true })
      : this.deps.createProject(body, { quiet: true });
    let saved: ProjectView;
    try {
      saved = await attempt();
    } catch (err) {
      if (!isNotARepo(err)) {
        this.errors.show(err);
        return;
      }
      if (!(await this.deps.confirm(`${body.repo_path} is not a git repository. Run "git init" there? Agentflow needs a repository to work in.`, 'Run git init'))) {
        this.errors.show(err);
        return;
      }
      try {
        await this.deps.gitInit(body.repo_path);
        saved = await attempt();
      } catch (err2) {
        this.errors.show(err2);
        return;
      }
    }
    this.deps.notify(this.mode.project ? `Saved ${saved.slug}` : `Created ${saved.slug}`);
    this.done(saved.slug);
  }

  /** autoSave stores an edit of an existing project: no prompt, no navigation, and the form stays as it is. */
  private async autoSave(): Promise<boolean> {
    const p = this.mode.project;
    if (!p) return false;
    this.errors.clear();
    const b = this.body();
    try {
      await this.deps.patchProject(p.slug, { repo_path: b.repo_path, model_scope: b.model_scope, feedback_cmds: b.feedback_cmds, cleanup_enabled: b.cleanup_enabled, plugins: b.plugins, ...this.dodPatch() }, { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return false;
    }
    this.dodReset = false;
    return true;
  }

  private async remove(): Promise<void> {
    const p = this.mode.project;
    if (!p || unregisterBlock(p)) return;
    const showCommands = this.deps.showCommands ?? (() => {});
    if (await unregister({ ...this.deps, showCommands }, p.slug)) this.done('');
  }

  // ─── Definition of done: detected from the toolchain, optionally overridden per project ───

  private startDod(items: string[]): void {
    this.dodRows = new RowsEditor<string>((t) => {
      const input = textInput('dod_item', t);
      return { el: input, read: () => input.value.trim() };
    }, items, () => '', 'Add item', () => this.bump());
    this.dodReset = false;
  }

  /** dodPatch is the patch fields for the list: the override rows, null to go back to detected, nothing when untouched. */
  private dodPatch(): { definition_of_done?: string[] | null } {
    if (this.dodReset) return { definition_of_done: null };
    return this.dodRows ? { definition_of_done: this.dodRows.read().filter((t) => t !== '') } : {};
  }

  private drawDod(): void {
    if (!this.mode.project) return;
    replace(this.dodHost,
      this.dodRows
        ? h('div', {}, this.dodRows.el, button('Reset to detected', () => { this.dodRows = null; this.dodReset = true; this.drawDod(); this.bump(); }, { field: 'reset-dod', icon: 'retry' }))
        : h('div', {},
          h('ul', { class: 'field-hint' }, ...this.dodDetected.map((t) => h('li', { text: t }))),
          button('Override for this project', () => { this.startDod(this.dodDetected); this.drawDod(); }, { field: 'override-dod', icon: 'edit' })));
  }

  // ─── Role preferences: a per-project candidate list overriding the global one ───

  private projectRows(role: string): CandRow[] {
    const slug = this.mode.project?.slug;
    return this.cands
      .filter((c) => c.scope === 'project' && c.project === slug && c.role === role)
      .sort((a, b) => a.position - b.position)
      .map((c) => ({ backend: c.backend, model: c.model, optIn: true }));
  }

  private drawRoles(): void {
    const p = this.mode.project;
    if (!p) return;
    replace(this.rolesHost, ...ROLES.map((role) => {
      const overridden = this.projectRows(role).length > 0;
      const global = this.cands.filter((c) => c.scope === 'global' && c.role === role).sort((a, b) => a.position - b.position);
      return h('div', {},
        buildRoleEditor({
          role,
          backends: this.backends,
          rows: () => this.drafts.get(role) ?? this.projectRows(role),
          hasDraft: () => this.drafts.has(role),
          setDraft: (rows) => { if (rows) this.drafts.set(role, rows); else this.drafts.delete(role); },
          save: async (body) => {
            this.cands = await this.deps.putProjectCandidates(p.slug, role, body, { quiet: true });
            this.deps.notify(`Saved ${role} override for ${p.slug}`);
          },
          emptyNote: global.length
            ? `Uses the global list: ${global.map((c) => `${c.backend}/${c.model}`).join(', ')}`
            : 'Uses the global list, which is empty.',
          extra: overridden ? [button('Use global list', () => void this.clearRole(role), { field: 'clear-override', icon: 'retry', word: true })] : [],
        }));
    }));
  }

  private async clearRole(role: string): Promise<void> {
    const p = this.mode.project;
    if (!p) return;
    try {
      await this.deps.clearProjectCandidates(p.slug, role);
      this.cands = await this.deps.candidates();
    } catch {
      return;
    }
    this.drafts.delete(role);
    this.drawRoles();
  }

  private unregisterButton(p: ProjectView): HTMLElement {
    const why = unregisterBlock(p);
    const b = button('Unregister', () => void this.remove(), { field: 'delete-project', icon: 'trash', word: true });
    if (why) {
      (b as HTMLButtonElement).disabled = true;
      b.setAttribute('title', why);
      b.setAttribute('aria-label', `Unregister: ${why}`);
    }
    return b;
  }

  render(): void {
    const p = this.mode.project;
    const e = this.errors;
    this.drawRoles();
    this.drawDod();
    this.auto?.stop();
    this.auto = p ? autosave({ snapshot: () => JSON.stringify([this.body(), this.dodPatch()]), save: () => this.autoSave() }) : null;
    const root = h('div', { class: 'settings-host', attrs: { 'data-form': p ? 'project-edit' : 'project-create' } },
        section(p ? `Edit ${p.slug}` : 'New project',
          h('div', { class: 'profile-fields' },
            e.add('slug', fieldRow('Slug', this.slug, 'Lowercase letters, digits and dashes; fixed once created')).el,
            e.add('repo_path', fieldRow('Repository', this.repoBtn, 'A git repository on this machine; if the folder is not one you are offered git init')).el,
            e.add('model_scope', fieldRow('Model scope', this.scope, 'local keeps this project off hosted backends')).el,
            e.add('cleanup_enabled', fieldRow('Cleanup pass', this.cleanup,
              'After a task passes review, run a cleanup agent and verify again; if that breaks verification the cleanup is rolled back')).el),
          e.add('feedback_cmds', fieldRow('Feedback commands', this.cmds.el,
            'Run in the sandbox to verify each task, as a program and arguments, never through a shell. Blank: detected from go.mod, pyproject.toml or package.json')).el,
          p ? e.add('definition_of_done', fieldRow('Definition of done', this.dodHost, 'Applies to every task; need not be written as acceptance criteria')).el : null,
          e.add('plugins', fieldRow('Runner plugins', this.plugins.el,
            'Opt-in only: none are loaded unless listed here. Each is pinned to an exact version such as 1.2.3 (no latest, no ranges); listed plugins are fetched by the runner when a stage starts')).el,
          e.general,
          h('div', { class: 'settings-actions' },
            p ? this.unregisterButton(p) : null,
            this.auto?.status ?? null,
            p ? null : button('Cancel', () => this.done(''), { field: 'cancel', icon: 'cancel' }),
            p
              ? button('Done', () => void (this.auto?.flush() ?? Promise.resolve()).then(() => this.done(p.slug)), { primary: true, field: 'close', icon: 'approve' })
              : button('Create project', () => void this.save(), { primary: true, field: 'save-project', icon: 'approve' }))),
        p ? section('Role preferences',
          h('p', { class: 'field-hint', text: 'Override the global candidate list for one role in this project; a role without an override uses the global list.' }),
          this.rolesHost) : h('p', { class: 'field-hint', text: 'Role preferences can be set once the project exists.' }));
    replace(this.host, root);
    this.auto?.watch(root);
  }
}

/** showProjectForm mounts the form into host. */
export function showProjectForm(host: HTMLElement, mode: ProjectMode, done: (slug: string) => void): void {
  void new ProjectForm(host, defaultProjectDeps(), mode, done).mount();
}
