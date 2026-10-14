// projects.ts — project create and edit (PJ1): slug, absolute repo_path with
// the git-init offer, model scope, the feedback commands as argv rows and the
// per-role candidate override (role preferences). A command is a program and
// its arguments, split on whitespace and run as a fixed argv, never through a
// shell, so the editor shows the argv and warns when a shell operator would
// only be passed as a plain argument.

import { confirmDialog, showToast } from '@shared';
import { api, ApiError, type Backend, type Candidate, type PluginPin, type ProjectInput, type ProjectView } from './api.js';
import { ROLES, buildRoleEditor, type CandRow } from './candidates.js';
import { h, replace } from './dom.js';
import { FormErrors, RowsEditor, button, checkbox, fieldRow, section, selectInput, textInput } from './forms.js';

export type ProjectDeps = Pick<
  typeof api,
  'createProject' | 'patchProject' | 'gitInit' | 'deleteProject' | 'backends' | 'candidates' | 'putProjectCandidates' | 'clearProjectCandidates'
> & {
  confirm(message: string, confirmLabel?: string): Promise<boolean>;
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
  confirm: (m, label = 'Confirm') => confirmDialog(m, { confirmLabel: label }),
  notify: (m) => void showToast(m, 'success'),
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
  private repo: HTMLInputElement;
  private scope: HTMLSelectElement;
  private cleanup: HTMLInputElement;
  private backends: Backend[] = [];
  private cands: Candidate[] = [];
  private drafts = new Map<string, CandRow[]>();
  private rolesHost = h('div');

  constructor(private host: HTMLElement, private deps: ProjectDeps, private mode: ProjectMode, private done: (slug: string) => void) {
    const p = mode.project;
    this.slug = textInput('slug', p?.slug ?? '', { readOnly: !!p, placeholder: 'my-project' });
    this.repo = textInput('repo_path', p?.repo_path ?? '', { placeholder: '/absolute/path/to/repository' });
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
    }, p?.feedback_cmds ?? [], () => '', 'Add command');
    this.plugins = new RowsEditor<PluginPin>((pl) => {
      const name = textInput('plugin_name', pl.name, { placeholder: 'package-name' });
      const version = textInput('plugin_version', pl.version, { placeholder: '1.2.3' });
      return { el: h('div', { class: 'row-actions' }, name, version), read: () => ({ name: name.value.trim(), version: version.value.trim() }) };
    }, p?.plugins ?? [], () => ({ name: '', version: '' }), 'Add plugin');
  }

  async mount(): Promise<void> {
    if (this.mode.project) {
      try {
        [this.backends, this.cands] = await Promise.all([this.deps.backends(), this.deps.candidates()]);
      } catch {
        // apiFetch already told the user; the role section stays empty.
      }
    }
    this.render();
  }

  private body(): ProjectInput {
    return {
      slug: this.slug.value.trim(),
      repo_path: this.repo.value.trim(),
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
      ? this.deps.patchProject(this.mode.project.slug, { repo_path: body.repo_path, model_scope: body.model_scope, feedback_cmds: body.feedback_cmds, cleanup_enabled: body.cleanup_enabled, plugins: body.plugins }, { quiet: true })
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

  private async remove(): Promise<void> {
    const p = this.mode.project;
    if (!p || !(await this.deps.confirm(`Delete project ${p.slug}? Its history is kept only if it has none; a project with efforts cannot be deleted.`, 'Delete'))) return;
    try {
      await this.deps.deleteProject(p.slug);
    } catch {
      return;
    }
    this.deps.notify(`Deleted ${p.slug}`);
    this.done('');
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
          extra: overridden ? [button('Use global list', () => void this.clearRole(role), { field: 'clear-override' })] : [],
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

  render(): void {
    const p = this.mode.project;
    const e = this.errors;
    this.drawRoles();
    replace(this.host,
      h('div', { class: 'settings-host', attrs: { 'data-form': p ? 'project-edit' : 'project-create' } },
        section(p ? `Edit ${p.slug}` : 'New project',
          h('div', { class: 'profile-fields' },
            e.add('slug', fieldRow('Slug', this.slug, 'Lowercase letters, digits and dashes; fixed once created')).el,
            e.add('repo_path', fieldRow('Repository path', this.repo, 'Absolute path of an existing git repository; if it is not one you are offered git init')).el,
            e.add('model_scope', fieldRow('Model scope', this.scope, 'local keeps this project off hosted backends')).el,
            e.add('cleanup_enabled', fieldRow('Cleanup pass', this.cleanup,
              'After a task passes review, run a cleanup agent and verify again; if that breaks verification the cleanup is rolled back')).el),
          e.add('feedback_cmds', fieldRow('Feedback commands', this.cmds.el,
            'Run in the sandbox to verify each task, as a program and arguments, never through a shell. Blank: detected from go.mod, pyproject.toml or package.json')).el,
          e.add('plugins', fieldRow('Runner plugins', this.plugins.el,
            'Opt-in only: none are loaded unless listed here. Each is pinned to an exact version such as 1.2.3 (no latest, no ranges); listed plugins are fetched by the runner when a stage starts')).el,
          e.general,
          h('div', { class: 'settings-actions' },
            p ? button('Delete project', () => void this.remove(), { field: 'delete-project' }) : null,
            button('Cancel', () => this.done(p?.slug ?? ''), { field: 'cancel' }),
            button(p ? 'Save project' : 'Create project', () => void this.save(), { primary: true, field: 'save-project' }))),
        p ? section('Role preferences',
          h('p', { class: 'field-hint', text: 'Override the global candidate list for one role in this project; a role without an override uses the global list.' }),
          this.rolesHost) : h('p', { class: 'field-hint', text: 'Role preferences can be set once the project exists.' })));
  }
}

/** showProjectForm mounts the form into host. */
export function showProjectForm(host: HTMLElement, mode: ProjectMode, done: (slug: string) => void): void {
  void new ProjectForm(host, defaultProjectDeps(), mode, done).mount();
}
