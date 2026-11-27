// seatbacklog.ts — the seat task backlog (AF-21, SEATS-UI-01): the tasks the coordinator may
// hand to a hand-run seat, under the seat cards on the Seats page. It lists them with state,
// eligible seats and priority, filters by state and by seat, adds one, and moves one forward
// ready -> fed -> done through the routes that already exist. Delete and the rest of the
// filters belong to SEATS-UI-02; this panel adds no route.

import { api, type SeatTask, type SeatTaskInput } from './api.js';
import { h, replace } from './dom.js';

export type SeatTaskBacklogDeps = Pick<typeof api, 'seatTasks' | 'createSeatTask' | 'setSeatTaskState'>;

const STATES = ['ready', 'fed', 'done'];

/** seatsOf is the seats a set of tasks is eligible for, sorted, without repeats. */
function seatsOf(tasks: SeatTask[]): string[] {
  return [...new Set(tasks.flatMap((t) => t.eligible_seats))].sort();
}

/** nextState is the only move this panel makes: ready -> fed -> done, then nothing. */
export function nextState(state: string): string {
  if (state === 'ready') return 'fed';
  if (state === 'fed') return 'done';
  return '';
}

export class SeatTaskBacklog {
  tasks: SeatTask[] = [];
  /** the filters; empty means every state, every seat */
  stateFilter = '';
  seatFilter = '';
  msg = '';
  /** the seats the seat filter offers: every seat in the backlog, not just the filtered rows */
  knownSeats: string[] = [];
  private seatsLoaded = false;
  private disposed = false;

  constructor(private host: HTMLElement, private deps: SeatTaskBacklogDeps) {}

  mount(): () => void {
    void this.load();
    return () => { this.disposed = true; };
  }

  unmount(): void {
    this.disposed = true;
  }

  async load(): Promise<void> {
    try {
      if (!this.seatsLoaded) {
        // The dropdown comes from the unfiltered backlog: built from the filtered rows, a
        // state filter would hide the seats whose work is elsewhere and the select would
        // then display a different value than seatFilter holds.
        this.knownSeats = seatsOf(await this.deps.seatTasks('', '', { quiet: true }));
        this.seatsLoaded = true;
      }
      this.tasks = await this.deps.seatTasks(this.stateFilter, this.seatFilter, { quiet: true });
      // A freshly added task may name a seat the first read did not know yet.
      this.knownSeats = [...new Set([...this.knownSeats, ...seatsOf(this.tasks)])].sort();
    } catch (err) {
      this.msg = `The seat task backlog is unavailable: ${err instanceof Error ? err.message : String(err)}`;
    }
    if (this.disposed) return;
    this.render();
    if (this.seatFilter && !this.knownSeats.includes(this.seatFilter)) {
      this.seatFilter = ''; // that seat has no task left: drop the filter instead of showing a stale one
      await this.load();
    }
  }

  private render(): void {
    replace(
      this.host,
      h('h3', { text: 'Seat task backlog' }),
      this.msg ? h('p', { class: 'field-hint', attrs: { 'data-role': 'backlog-msg' }, text: this.msg }) : null,
      this.filters(this.knownSeats),
      this.tasks.length
        ? h('ul', { attrs: { 'data-role': 'seat-backlog' } }, ...this.tasks.map((t) => this.row(t)))
        : h('p', { class: 'field-hint', text: 'No seat tasks yet.' }),
      this.form(),
    );
  }

  private filters(seats: string[]): HTMLElement {
    const select = (field: string, values: string[], any: string, pick: (v: string) => void, set: string): HTMLElement => {
      const el = h('select', {
        attrs: { 'data-field': field },
        on: { change: (ev) => pick((ev.target as HTMLSelectElement).value) },
      }, ...['', ...values].map((v) => h('option', { attrs: { value: v }, text: v || any }))) as HTMLSelectElement;
      el.value = set;
      return el;
    };
    const refilter = (pick: (v: string) => void) => (v: string) => {
      this.msg = ''; // a message belongs to the change that made it, not to a new view
      pick(v);
      void this.load();
    };
    return h('div', { class: 'row-actions' },
      select('backlog-state', STATES, 'all states', refilter((v) => { this.stateFilter = v; }), this.stateFilter),
      select('backlog-seat', seats, 'any seat', refilter((v) => { this.seatFilter = v; }), this.seatFilter),
    );
  }

  private row(t: SeatTask): HTMLElement {
    const next = nextState(t.state);
    const facts = [t.state, t.eligible_seats.join(', ') || 'any seat', t.priority ? `priority ${t.priority}` : ''];
    return h('li', { attrs: { 'data-task': String(t.id) } },
      h('span', { class: 'mono', text: `#${t.id}` }),
      ' ',
      h('strong', { text: t.title }),
      h('span', { class: 'field-hint', text: ` — ${facts.filter(Boolean).join(' · ')}` }),
      next ? h('button', {
        attrs: { type: 'button', 'data-role': 'backlog-advance', 'data-task': String(t.id) },
        text: next === 'fed' ? 'Feed' : 'Done',
        on: { click: () => void this.advance(t, next) },
      }) : null,
    );
  }

  private form(): HTMLElement {
    const title = h('input', { attrs: { placeholder: 'title', 'data-field': 'backlog-title' } }) as HTMLInputElement;
    const objective = h('input', { attrs: { placeholder: 'objective (optional)', 'data-field': 'backlog-objective' } }) as HTMLInputElement;
    const seats = h('input', { attrs: { placeholder: 'eligible seats, comma separated', 'data-field': 'backlog-seats' } }) as HTMLInputElement;
    const priority = h('input', { attrs: { type: 'number', placeholder: 'priority', 'data-field': 'backlog-priority' } }) as HTMLInputElement;
    return h('section', { class: 'seat-add' },
      h('h3', { text: 'Add a seat task' }),
      title, objective, seats, priority,
      h('button', {
        attrs: { type: 'button', 'data-role': 'backlog-add' },
        text: 'Add task',
        on: { click: () => void this.add(title, objective, seats, priority) },
      }),
    );
  }

  private async add(title: HTMLInputElement, objective: HTMLInputElement, seats: HTMLInputElement, priority: HTMLInputElement): Promise<void> {
    const n = Number(priority.value.trim());
    const input: SeatTaskInput = {
      title: title.value.trim(),
      objective: objective.value.trim() || undefined,
      eligible_seats: seats.value.split(',').map((s) => s.trim()).filter(Boolean),
      // An empty or non-numeric priority is left out, so the server's default applies.
      priority: priority.value.trim() && Number.isFinite(n) ? n : undefined,
    };
    await this.run(() => this.deps.createSeatTask(input), (created) => `added #${created.id} ${created.title}`);
  }

  private async advance(t: SeatTask, state: string): Promise<void> {
    await this.run(() => this.deps.setSeatTaskState(t.id, state), (updated) => `#${updated.id} is ${updated.state}`);
  }

  /** run sends one change, keeps its outcome in msg, then re-reads the list. */
  private async run<T>(send: () => Promise<T>, done: (v: T) => string): Promise<void> {
    this.msg = '';
    try {
      this.msg = done(await send());
    } catch (err) {
      this.msg = err instanceof Error ? err.message : String(err);
    }
    await this.load();
  }
}
