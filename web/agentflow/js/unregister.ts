// unregister.ts — the Unregister flow shared by Edit project and the Overview
// rows: a project with no active effort and a stopped run can be unregistered.
// Its AgentFlow history and working files are removed; the repository and its
// branches are not touched, so the response's delete commands are shown with
// copy buttons for the person to run. All text is placed with h().

import { confirmDialog, copyText, openModal, showToast } from '@shared';
import { api, type ProjectView, type Unregistered } from './api.js';
import { h } from './dom.js';
import { icon } from './icons.js';

export interface UnregisterDeps {
  deleteProject(slug: string): Promise<Unregistered>;
  confirm(message: string, confirmLabel?: string): Promise<boolean>;
  notify(message: string): void;
  /** Shows the leftover-branch commands. */
  showCommands(slug: string, commands: string[]): void;
}

export const defaultUnregisterDeps = (): UnregisterDeps => ({
  deleteProject: api.deleteProject,
  confirm: (m, label = 'Confirm') => confirmDialog(m, { confirmLabel: label }),
  notify: (m) => void showToast(m, 'success'),
  showCommands: (slug, commands) => showBranchCommands(slug, commands, (t) => void copyText(t)),
});

/** unregisterBlock is why the project cannot be unregistered now, or '' when it can. */
export function unregisterBlock(p: Pick<ProjectView, 'run_state' | 'effort'>): string {
  if (p.run_state !== 'stopped') return 'Stop the run first';
  if (p.effort) return 'Finish or abandon the active effort first';
  return '';
}

/** unregister asks, deletes and shows the branch commands; true when the project is gone. */
export async function unregister(deps: UnregisterDeps, slug: string): Promise<boolean> {
  if (!(await deps.confirm(`Unregister ${slug}? Its history is removed; the repository is not touched.`, 'Unregister'))) return false;
  let res: Unregistered;
  try {
    res = await deps.deleteProject(slug);
  } catch {
    return false;
  }
  deps.notify(`Unregistered ${slug}`);
  if (res.commands?.length) deps.showCommands(slug, res.commands);
  return true;
}

/** branchCommands is the body of the modal: one copyable command per leftover branch. */
export function branchCommands(slug: string, commands: string[], copy: (text: string) => void): HTMLElement {
  return h('div', { attrs: { 'data-role': 'branch-commands' } },
    h('p', { text: `${slug} is unregistered. Its branches are still in the repository; to delete them, run:` }),
    ...commands.map((c) => h('div', { class: 'merge-command' },
      h('pre', { class: 'detail-mono', text: c }),
      h('button', { class: 'icon-btn', attrs: { type: 'button', title: 'Copy this command', 'aria-label': 'Copy this command', 'data-action': 'copy-command' }, on: { click: () => copy(c) } }, icon('copy')))));
}

function showBranchCommands(slug: string, commands: string[], copy: (text: string) => void): void {
  openModal(branchCommands(slug, commands, copy), { title: 'Branches left behind' });
}
