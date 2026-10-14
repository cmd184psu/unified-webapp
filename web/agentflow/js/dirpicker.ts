// dirpicker.ts — the server-side directory picker (project Repository field and
// the browse-roots setting): a modal that lists the subdirectories of one
// directory, descends on click, goes up, marks git repositories and returns the
// directory chosen. The server only lists inside its browse roots, so there is
// never a path to type. Every name is placed with h(), so it is text.

import { openModal } from '@shared';
import { api, type FsDirs } from './api.js';
import { h, replace } from './dom.js';
import { badge, button } from './forms.js';

export type FsDirsFn = (path?: string) => Promise<FsDirs>;

export class DirPicker {
  private cur: FsDirs | null = null;
  private error = '';
  private list = h('div', { class: 'wizard-list', attrs: { 'data-role': 'dir-list' } });
  private where = h('code', { attrs: { 'data-role': 'dir-path' } });
  private err = h('div', { class: 'field-error', attrs: { role: 'alert', hidden: true } });
  private up = button('Parent folder', () => void this.go(this.cur?.parent ?? ''), { field: 'dir-up', icon: 'up' });
  /** Bumped per request so a slow answer cannot overwrite a newer one. */
  private seq = 0;
  readonly el: HTMLElement;

  constructor(private fsDirs: FsDirsFn, private start: string, private onChoose: (path: string) => void, private onCancel: () => void) {
    this.el = h('div', { class: 'dir-picker', attrs: { 'data-role': 'dir-picker' } },
      h('div', { class: 'row-actions' }, this.up, this.where),
      this.err,
      this.list,
      h('div', { class: 'settings-actions' },
        button('Cancel', () => this.onCancel(), { field: 'dir-cancel', icon: 'cancel' }),
        button('Choose this folder', () => { if (this.cur) this.onChoose(this.cur.path); }, { primary: true, field: 'dir-choose', icon: 'folder' })));
  }

  /** mount shows the start directory, or the first browse root when it cannot be listed. */
  async mount(): Promise<void> {
    if (!(await this.go(this.start)) && this.start) await this.go('');
  }

  /** go lists path; false (with the error shown) when the server refuses it. */
  async go(path: string): Promise<boolean> {
    const mine = ++this.seq;
    try {
      const res = await this.fsDirs(path);
      if (mine !== this.seq) return true;
      this.cur = res;
      this.error = '';
    } catch (err) {
      if (mine !== this.seq) return false;
      this.error = err instanceof Error ? err.message : String(err);
      this.draw();
      return false;
    }
    this.draw();
    return true;
  }

  private draw(): void {
    const cur = this.cur;
    this.where.textContent = cur?.path ?? '';
    this.err.textContent = this.error;
    if (this.error) this.err.removeAttribute('hidden');
    else this.err.setAttribute('hidden', '');
    if (cur?.parent) this.up.removeAttribute('disabled');
    else this.up.setAttribute('disabled', '');
    replace(this.list, ...(cur && cur.dirs.length > 0
      ? cur.dirs.map((d) => h('button', {
        class: 'ui-modal-btn dir-row',
        attrs: { type: 'button', 'data-dir': d.path, title: d.git ? `${d.path} (git repository)` : d.path },
        on: { click: () => void this.go(d.path) },
      }, d.name, d.git ? badge('git', 'ok') : null))
      : [h('p', { class: 'field-hint', text: cur ? 'No subfolders' : '' })]));
  }
}

/** pickDirectory opens the picker in a modal and resolves with the chosen path, or null when cancelled. */
export function pickDirectory(start = '', fsDirs: FsDirsFn = api.fsDirs): Promise<string | null> {
  return new Promise((resolve) => {
    let picked: string | null = null;
    const body = h('div', {});
    const modal = openModal(body, { title: 'Choose a folder', onClose: () => resolve(picked) });
    const picker = new DirPicker(fsDirs, start, (p) => { picked = p; modal.close(); }, () => modal.close());
    body.appendChild(picker.el);
    void picker.mount();
  });
}
