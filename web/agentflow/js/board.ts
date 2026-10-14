// board.ts — the Board sub-tab (AT9, GIT4, UI3): the task board in the
// existing console layout. A progress header with the run controls, the task
// list with status chips, a detail pane with the attempts (VerifyReport,
// reviewer issues, waiting reason, backend and model), the slide-in
// #stream-panel for live stage output, the halted-task actions (retry, edit,
// override, abandon, each behind a confirmation) and, once the effort is
// complete, the git commands for the human to run. Everything from the server
// or the models is placed with h(), so it is text.

import { confirmDialog, copyText, showToast } from '@shared';
import { api, ApiError, type Attempt, type EffortView, type Stage, type TaskDef, type TaskDetail, type TaskView } from './api.js';
import { chooseEffort, effortBar } from './current.js';
import { chip, h, replace, statusKey, type Child } from './dom.js';
import { subscribe, type AgentflowEvent } from './events.js';
import { FormErrors, badge, button, checkbox, fieldRow, textArea, textInput } from './forms.js';
import { buttonsFor, progressText } from './overview.js';
import { defaultYoloDeps, type YoloDeps } from './yolo.js';
import { StreamPanel, defaultStreamDeps, type StreamDeps } from './stream.js';

export type BoardDeps = Pick<
  typeof api,
  'efforts' | 'tasks' | 'task' | 'run' | 'retryTask' | 'editTask' | 'overrideTask' | 'abandonEffort' | 'gitCommands'
> & {
  confirm(message: string, confirmLabel?: string): Promise<boolean>;
  copy(text: string): void;
  notify(message: string): void;
};

export const defaultBoardDeps = (): BoardDeps => ({
  efforts: api.efforts,
  tasks: api.tasks,
  task: api.task,
  run: api.run,
  retryTask: api.retryTask,
  editTask: api.editTask,
  overrideTask: api.overrideTask,
  abandonEffort: api.abandonEffort,
  gitCommands: api.gitCommands,
  confirm: (m, label = 'Confirm') => confirmDialog(m, { confirmLabel: label }),
  copy: (t) => void copyText(t),
  notify: (m) => void showToast(m, 'success'),
});

type Rec = Record<string, unknown>;
const rec = (v: unknown): Rec => (v && typeof v === 'object' ? (v as Rec) : {});
const strs = (v: unknown): string[] => (Array.isArray(v) ? v.map((x) => String(x)) : []);

/** counts is the progress map (total plus a count per status) for a task list. */
export function counts(tasks: TaskView[]): Record<string, number> {
  const out: Record<string, number> = { total: tasks.length };
  for (const t of tasks) out[t.status] = (out[t.status] ?? 0) + 1;
  return out;
}

/** The stage of an attempt that carries a given role's parsed output. */
const stageOf = (a: Attempt, role: string): Stage | undefined => a.stages.filter((s) => s.role === role).pop();

function block(title: string, ...children: Child[]): HTMLElement {
  return h('div', { class: 'detail-section' }, h('h4', { text: title }), ...children);
}

const mono = (text: string): HTMLElement => h('pre', { class: 'detail-mono', text });
const bullets = (items: string[]): HTMLElement => h('ul', { class: 'plan-review-notes' }, ...items.map((i) => h('li', { text: i })));

/** renderVerify shows a VerifyReport. */
export function renderVerify(parsed: unknown): HTMLElement {
  const r = rec(parsed);
  const checks = Array.isArray(r.checks) ? (r.checks as Rec[]) : [];
  return h('div', { attrs: { 'data-role': 'verify-report' } },
    h('div', { class: 'row-actions' }, chip(r.pass === false ? 'failed' : 'passed'), h('span', { class: 'field-hint', text: `${checks.length} check${checks.length === 1 ? '' : 's'}` })),
    checks.length ? h('ul', { class: 'plan-review-notes' }, ...checks.map((c) =>
      h('li', {}, h('strong', { text: String(c.name ?? '') }), ` ${c.pass ? 'passed' : 'failed'}`, c.detail ? ` — ${String(c.detail)}` : null))) : null,
    strs(r.new_failures).length ? block('New failures', bullets(strs(r.new_failures))) : null,
    strs(r.baseline_failures).length ? block('Baseline failures', bullets(strs(r.baseline_failures))) : null,
    strs(r.warnings).length ? block('Warnings', bullets(strs(r.warnings))) : null);
}

/** renderReviewOut shows a task reviewer's verdict, summary and issues. */
export function renderReviewOut(parsed: unknown): HTMLElement {
  const r = rec(parsed);
  const issues = Array.isArray(r.issues) ? (r.issues as Rec[]) : [];
  return h('div', { attrs: { 'data-role': 'reviewer-report' } },
    h('div', { class: 'row-actions' }, r.verdict ? chip(String(r.verdict)) : null),
    r.summary ? h('p', { text: String(r.summary) }) : null,
    issues.length ? h('ul', { class: 'plan-review-notes' }, ...issues.map((i) =>
      h('li', {}, h('strong', { text: String(i.severity ?? '') }), ` ${String(i.description ?? '')}`, i.file ? ` (${String(i.file)}${i.line ? ':' + String(i.line) : ''})` : null))) : null,
    r.repair_prompt ? block('Repair prompt', mono(String(r.repair_prompt))) : null,
    r.blocked ? h('p', { text: `Blocked: ${String(r.blocked)}` }) : null);
}

export class Board {
  efforts: EffortView[] = [];
  current: EffortView | undefined;
  tasks: TaskView[] = [];
  selected = '';
  detail: TaskDetail | null = null;
  runState = 'stopped';
  gitLines: string[] | null = null;
  private editing = false;
  private loaded = false;
  private stream: StreamPanel;
  private off: (() => void) | null = null;
  private errors = new FormErrors();

  constructor(private host: HTMLElement, private panel: HTMLElement, private slug: string, private deps: BoardDeps, streamDeps: StreamDeps, private yolo: YoloDeps = defaultYoloDeps()) {
    this.stream = new StreamPanel(panel, streamDeps);
  }

  mount(): () => void {
    this.off = subscribe((ev: AgentflowEvent) => this.handle(ev));
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      this.stream.close();
    };
  }

  /** handle applies one stream event: a run-state change shows at once, the rest refetch. */
  handle(ev: AgentflowEvent): void {
    if (ev.type === 'state.changed') {
      void this.refresh();
    } else if (ev.type === 'project.run_state' && ev.project === this.slug) {
      this.runState = ev.state;
      this.render();
      void this.refresh();
    } else if ((ev.type === 'effort.update' || ev.type === 'task.update' || ev.type === 'project.update') && ev.project === this.slug) {
      void this.refresh();
    }
  }

  async load(): Promise<void> {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.current = chooseEffort(this.slug, this.efforts);
    this.selected = '';
    this.detail = null;
    this.gitLines = null;
    this.stream.close();
    await this.refresh();
  }

  /** refresh refetches the efforts, the task list and the open task. */
  async refresh(): Promise<void> {
    try {
      this.efforts = await this.deps.efforts(this.slug);
      this.current = this.efforts.find((e) => e.id === this.current?.id) ?? chooseEffort(this.slug, this.efforts);
      this.loaded = true;
      const ef = this.current;
      this.tasks = ef ? await this.deps.tasks(this.slug, ef.id) : [];
      if (ef) this.runState = ef.run_state;
      if (ef && this.selected && this.tasks.some((t) => t.key === this.selected)) this.detail = await this.deps.task(this.slug, ef.id, this.selected);
      if (ef?.status === 'complete' && !this.gitLines) this.gitLines = (await this.deps.gitCommands(this.slug, ef.id)).commands;
    } catch {
      // apiFetch already told the user; keep what is shown.
    }
    this.render();
  }

  async select(key: string): Promise<void> {
    const ef = this.current;
    if (!ef) return;
    this.selected = key;
    this.editing = false;
    try {
      this.detail = await this.deps.task(this.slug, ef.id, key);
    } catch {
      return;
    }
    this.render();
  }

  private async control(op: Parameters<typeof api.run>[1]): Promise<void> {
    if (op === 'stop' && !(await this.deps.confirm(`Stop ${this.slug}? Running stages are cancelled.`, 'Stop'))) return;
    try {
      await this.deps.run(this.slug, op);
    } catch {
      return;
    }
    await this.refresh();
  }

  // ─── Halted-task actions (AT9) ───────────────────────────────────────────

  private async retry(key: string, clean: boolean): Promise<void> {
    const ef = this.current;
    if (!ef || !(await this.deps.confirm(`Retry ${key}? Its attempt counters are reset${clean ? ' and its worktree is recreated clean' : ''}.`, 'Retry'))) return;
    try {
      await this.deps.retryTask(this.slug, ef.id, key, clean);
    } catch {
      return;
    }
    this.deps.notify(`Retrying ${key}`);
    await this.refresh();
  }

  private async override(key: string, reason: string): Promise<void> {
    const ef = this.current;
    if (!ef) return;
    if (reason.trim() === '') {
      this.errors.show(new ApiError(400, 'A reason is required', 'reason'));
      return;
    }
    if (!(await this.deps.confirm(`Mark ${key} as passed without a passing attempt? The reason is recorded.`, 'Override'))) return;
    try {
      await this.deps.overrideTask(this.slug, ef.id, key, reason.trim());
    } catch {
      return;
    }
    this.deps.notify(`Overrode ${key}`);
    await this.refresh();
  }

  private async abandon(): Promise<void> {
    const ef = this.current;
    if (!ef || !(await this.deps.confirm(`Abandon effort #${ef.id}? It ends the effort; its tasks and history stay for reference.`, 'Abandon'))) return;
    try {
      await this.deps.abandonEffort(this.slug, ef.id);
    } catch {
      return;
    }
    this.deps.notify('Effort abandoned');
    await this.refresh();
  }

  private async saveEdit(def: TaskDef): Promise<void> {
    const ef = this.current;
    if (!ef) return;
    this.errors.clear();
    if (!(await this.deps.confirm(`Save ${def.key}? The queue returns to draft and must be approved again before anything runs.`, 'Save task'))) return;
    try {
      await this.deps.editTask(this.slug, ef.id, def.key, def, { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return;
    }
    this.editing = false;
    this.deps.notify(`Saved ${def.key}; approve the queue again`);
    await this.refresh();
  }

  private editForm(d: TaskDetail): HTMLElement {
    this.errors = new FormErrors();
    const title = textInput('title', d.title);
    const phase = textInput('phase', d.phase);
    const prompt = textArea('prompt', d.prompt, 6);
    const acceptance = textArea('acceptance', d.acceptance.join('\n'), 3);
    const files = textArea('files_touched', d.files_touched.join('\n'), 3);
    const deps = textInput('depends_on', d.depends_on.join(', '));
    const split = (s: string): string[] => s.split('\n').map((l) => l.trim()).filter((l) => l !== '');
    const e = this.errors;
    return h('div', { attrs: { 'data-role': 'edit-task' } },
      e.add('title', fieldRow('Title', title)).el,
      e.add('phase', fieldRow('Phase', phase)).el,
      e.add('prompt', fieldRow('Prompt', prompt)).el,
      e.add('acceptance', fieldRow('Acceptance criteria', acceptance, 'One per line')).el,
      e.add('files_touched', fieldRow('Files touched', files, 'One path per line')).el,
      e.add('depends_on', fieldRow('Depends on', deps, 'Task keys, comma separated')).el,
      e.general,
      h('div', { class: 'row-actions' },
        button('Save task', () => void this.saveEdit({
          key: d.key, title: title.value.trim(), phase: phase.value.trim(), prompt: prompt.value,
          acceptance: split(acceptance.value), files_touched: split(files.value),
          depends_on: deps.value.split(',').map((s) => s.trim()).filter((s) => s !== ''),
        }), { primary: true, field: 'save-task' }),
        button('Cancel', () => { this.editing = false; this.render(); }, { field: 'cancel-edit' })));
  }

  private actions(d: TaskDetail): HTMLElement {
    const clean = checkbox('clean_worktree', false);
    const reason = textArea('override_reason', '', 2);
    this.errors = new FormErrors();
    const reasonRow = this.errors.add('reason', fieldRow('Override reason', reason, 'Recorded with your name when you mark the task passed'));
    return h('div', { class: 'detail-section', attrs: { 'data-panid': 'actions' } },
      h('h4', { text: 'Halted: choose how to continue' }),
      h('label', { class: 'inline-check' }, clean, 'Recreate the worktree clean when retrying'),
      button('Retry', () => void this.retry(d.key, clean.checked), { field: 'retry' }),
      button('Edit task', () => { this.editing = true; this.render(); }, { field: 'edit' }),
      reasonRow.el,
      button('Override as passed', () => void this.override(d.key, reason.value), { field: 'override' }),
      button('Abandon effort', () => void this.abandon(), { field: 'abandon' }),
      this.errors.general);
  }

  // ─── Detail pane ─────────────────────────────────────────────────────────

  private attempt(a: Attempt): HTMLElement {
    const verify = stageOf(a, 'verify');
    const review = stageOf(a, 'reviewer');
    const worker = stageOf(a, 'worker');
    return h('div', { class: 'profile-card', attrs: { 'data-attempt': a.n } },
      h('div', { class: 'row-actions' },
        h('strong', { text: `Attempt ${a.n}` }), a.kind === 'repair' ? badge('repair', 'warn') : null,
        chip(a.outcome || 'running'),
        h('span', { class: 'field-hint', text: `worker ${a.worker_model || '-'} · reviewer ${a.reviewer_model || '-'}` })),
      h('div', { class: 'wizard-list' }, ...a.stages.map((s) =>
        h('div', { class: 'inline-row', attrs: { 'data-stage': s.id } },
          h('span', { text: `${s.role} · ${s.backend ? `${s.backend}/${s.model}` : 'engine'} · ${s.outcome || s.state}` }),
          button('Output', () => this.stream.open(s), { field: 'open-stream' })))),
      worker?.parsed ? block('Worker summary', h('p', { text: String(rec(worker.parsed).summary ?? '') })) : null,
      verify?.parsed ? block('VerifyReport', renderVerify(verify.parsed)) : null,
      review?.parsed ? block('Reviewer', renderReviewOut(review.parsed)) : null);
  }

  private detailPane(): HTMLElement {
    const d = this.detail;
    if (!d) return h('div', { class: 'console-right', attrs: { 'data-role': 'detail-pane', hidden: true } });
    const cur = d.current;
    return h('div', { class: 'console-right', attrs: { 'data-role': 'detail-pane' } },
      h('div', { class: 'detail-body' },
        h('div', { class: 'detail-header' }, h('h3', { text: `${d.key} · ${d.title}` }), chip(d.status),
          h('button', { class: 'icon-btn detail-close-btn', text: 'Close', attrs: { type: 'button', 'data-action': 'close-detail' }, on: {
            click: () => { this.selected = ''; this.detail = null; this.editing = false; this.render(); },
          } })),
        h('div', { class: 'detail-attempts', text: `Attempts: ${d.attempts}` }),
        d.waiting_reason ? block('Waiting', h('p', { text: d.waiting_reason, attrs: { 'data-role': 'waiting-reason' } })) : null,
        cur ? block('Running now', h('p', { text: `${cur.role} · ${cur.backend}/${cur.model} · ${cur.state}` }),
          h('div', { class: 'row-actions' }, button('Watch live', () => this.stream.open({
            id: cur.stage, role: cur.role, backend: cur.backend, model: cur.model, state: cur.state === 'running' ? 'running' : cur.state, outcome: '', raw_output: '',
          }), { field: 'watch-live' }))) : null,
        this.editing ? this.editForm(d) : null,
        d.halted && !this.editing ? this.actions(d) : null,
        block('Prompt', mono(d.prompt)),
        d.acceptance.length ? block('Acceptance', bullets(d.acceptance)) : null,
        d.files_touched.length ? block('Files touched', mono(d.files_touched.join('\n'))) : null,
        d.depends_on.length ? block('Depends on', h('p', { text: d.depends_on.join(', ') })) : null,
        block('Attempts', ...(d.attempt_list.length ? d.attempt_list.map((a) => this.attempt(a)) : [h('p', { class: 'field-hint', text: 'No attempts yet.' })])),
        d.progress.length ? block('Progress log', bullets(d.progress)) : null));
  }

  private gitBox(): HTMLElement | null {
    if (!this.gitLines) return null;
    const text = this.gitLines.join('\n');
    return h('div', { class: 'detail-section', attrs: { 'data-role': 'git-commands' } },
      h('h4', { text: 'Merge and push (run these yourself; agentflow never pushes)' }),
      mono(text),
      button('Copy commands', () => this.deps.copy(text), { field: 'copy-git' }));
  }

  render(): void {
    if (!this.loaded) {
      replace(this.host, h('p', { class: 'field-hint', text: 'Loading board…' }));
      return;
    }
    const ef = this.current;
    if (!ef) {
      replace(this.host, h('div', { class: 'plan-empty' }, h('h3', { text: 'No effort yet' }), h('p', { text: 'Create an effort on the FRD tab; its tasks appear here once the queue is approved.' })));
      return;
    }
    const { pct, text } = progressText(counts(this.tasks));
    const bar = h('div', { class: 'progress-bar-wrap' }, h('div', { class: 'progress-bar', attrs: { style: `width: ${pct}%` } }));
    const controls = buttonsFor(this.runState).map((b) =>
      h('button', {
        class: 'ui-modal-btn' + (b.primary ? ' ui-modal-btn-primary' : ''), text: b.label,
        attrs: { type: 'button', 'data-op': b.op }, on: { click: () => void this.control(b.op) },
      }));
    replace(this.host,
      effortBar(this.slug, this.efforts, ef, (id) => { this.current = this.efforts.find((e) => e.id === id); void this.load(); }, { deps: this.yolo, onChanged: () => void this.refresh() }),
      h('div', { class: 'console-header' },
        h('span', { class: 'pane-title', text: 'Progress' }), bar, h('span', { class: 'progress-pct', text }), chip(this.runState), ...controls),
      this.gitBox(),
      h('div', { class: 'console-body' },
        h('div', { class: 'console-left' },
          h('h3', { class: 'pane-title', text: 'Tasks' }),
          this.tasks.length === 0
            ? h('p', { class: 'field-hint', text: ef.queue ? 'The queue has no tasks.' : 'No queue yet: plan, compile and approve the queue first.' })
            : h('ul', { class: 'task-list' }, ...this.tasks.map((t) =>
              h('li', { class: `task-item status-${statusKey(t.status)}` + (t.key === this.selected ? ' selected' : ''), attrs: { 'data-task': t.key }, on: {
                click: () => void this.select(t.key),
              } },
                h('div', { class: 'task-item-title', text: t.title }),
                h('div', { class: 'task-item-meta' }, chip(t.status), h('span', { text: `${t.key} · attempts ${t.attempts}` })),
                t.waiting_reason ? h('div', { class: 'task-item-meta', text: t.waiting_reason }) : null,
                t.current ? h('div', { class: 'task-item-meta', text: `${t.current.role} · ${t.current.backend}/${t.current.model}` }) : null)))),
        this.detailPane()));
  }
}

/** mountBoard is the Board sub-tab's mount. */
export function mountBoard(host: HTMLElement, slug: string): () => void {
  const panel = document.getElementById('stream-panel');
  if (!panel) throw new Error('board: #stream-panel is missing from the page');
  return new Board(host, panel, slug, defaultBoardDeps(), defaultStreamDeps()).mount();
}
