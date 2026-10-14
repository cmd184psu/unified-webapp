// merge-card.ts — the "Ready to merge" card shown on the Board and the Overview
// for a complete effort. agentflow only ever writes the effort's own branch, so
// the card says plainly that main is untouched and lists the commands to run,
// each with a copy button. Branch, commits and diffstat come from
// GET .../git-commands and may be missing. All text is placed with h().

import type { Effort, GitCommands } from './api.js';
import { h } from './dom.js';

type Merged = Pick<Effort, 'id' | 'title' | 'integration_branch'>;

/** mergeBranch is the branch the effort's work is on: the server's name, else the effort's own. */
export function mergeBranch(ef: Merged, git: GitCommands | null): string {
  return git?.branch || ef.integration_branch || `agentflow/effort-${ef.id}`;
}

/** mergeCard builds the card; git is null when the commands could not be fetched. */
export function mergeCard(ef: Merged, git: GitCommands | null, copy: (text: string) => void): HTMLElement {
  const branch = mergeBranch(ef, git);
  const files = git?.files;
  return h('div', { class: 'merge-card', attrs: { 'data-role': 'git-commands' } },
    h('h4', { text: `Ready to merge: ${ef.title}` }),
    h('p', { text: `Nothing was merged into main. The finished work is on the branch ${branch}; main is unchanged until you merge it yourself. agentflow never merges into main and never pushes.` }),
    git?.commits?.length
      ? h('ul', { class: 'plan-review-notes', attrs: { 'data-role': 'merge-commits' } }, ...git.commits.map((c) => h('li', { text: `${c.sha.slice(0, 7)} ${c.subject}` })))
      : null,
    files !== undefined || git?.diffstat
      ? h('div', { attrs: { 'data-role': 'merge-diffstat' } },
        files !== undefined ? h('p', { class: 'field-hint', text: `${files} file${files === 1 ? '' : 's'} changed` }) : null,
        git?.diffstat ? h('pre', { class: 'detail-mono', text: git.diffstat }) : null)
      : null,
    git?.commands.length
      ? h('div', {},
        h('p', { text: 'To merge it, run:' }),
        ...git.commands.map((c) => h('div', { class: 'merge-command' },
          h('pre', { class: 'detail-mono', text: c }),
          h('button', { class: 'icon-btn', text: '⧉', attrs: { type: 'button', title: 'Copy this command', 'aria-label': 'Copy this command', 'data-action': 'copy-command' }, on: { click: () => copy(c) } }))))
      : h('p', { class: 'field-hint', text: `The commands could not be loaded. To merge, run: git merge --no-ff ${branch}` }));
}
