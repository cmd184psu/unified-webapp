// seatspage.ts — the Seats tab (AF-13, AF-16): one card per hand-driven seat with the exact
// message to paste into it (the stopgap's pastes.py rules, now served by the coordinator
// module), its own copy button, its queue file, its screen state, and the owner's nudge policy
// with a pause button. The owner registers seats in the form at the bottom; nothing here types
// into a session, and freebuff/gui seats start with nudges off.

import { api, type CoordReport, type ExternalSeat, type SeatInput, type SeatPolicy } from './api.js';
import { h, replace } from './dom.js';
import { subscribe } from './events.js';
import { SeatTaskBacklog } from './seatbacklog.js';

export type SeatsPageDeps = Pick<
  typeof api,
  'coordStatus' | 'coordRun' | 'externalSeats' | 'seatQueue' | 'nudgeSeat' | 'saveSeat' | 'deleteSeat' | 'pauseSeat'
  | 'seatTasks' | 'createSeatTask' | 'setSeatTaskState'
> & {
  /** copy writes text to the clipboard; the default tries the Clipboard API, then execCommand. */
  copy?: (text: string) => Promise<boolean>;
};

/** copyText is the card's copy button: Clipboard API first, a hidden textarea and
 * document.execCommand('copy') when that is unavailable (an insecure origin, an older page). */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Fall through to the legacy path.
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = typeof document.execCommand === 'function' && document.execCommand('copy');
    ta.remove();
    return !!ok;
  } catch {
    return false;
  }
}

/** policyLine renders the nudge policy in one line. */
export function policyLine(p: SeatPolicy): string {
  if (!p || !p.enabled) return 'nudges off';
  if (p.paused) return 'nudges paused';
  const parts = [`nudges on (${p.trigger || 'idle'}`];
  if (p.idle_minutes > 0) parts.push(`after ${p.idle_minutes}m idle`);
  if (p.min_gap_minutes > 0) parts.push(`gap ${p.min_gap_minutes}m`);
  if (p.max_per_hour > 0) parts.push(`max ${p.max_per_hour}/h`);
  if (p.quiet_hours) parts.push(`quiet ${p.quiet_hours}`);
  return parts.join(', ') + ')';
}

/** seatState is the card's live state line. A gui seat has no terminal, so it is unknown. */
export function seatState(s: ExternalSeat): string {
  if (s.kind === 'gui') return 'state unknown (no terminal)';
  if (!s.running) return 'no session';
  if (s.dead) return 'session ended';
  if (s.halt) return `waits for the owner: ${s.halt}`;
  return s.ready ? `at its input box since ${new Date(s.since).toLocaleTimeString()}` : 'working';
}

export class SeatsPage {
  seats: ExternalSeat[] = [];
  report: CoordReport | null = null;
  mode = '';
  msg = '';
  private disposed = false;
  private timer: ReturnType<typeof setInterval> | undefined;
  /** the backlog panel lives in its own element so a re-render keeps its filters and form. */
  private backlogHost = h('div', { attrs: { 'data-role': 'seat-backlog-panel' } });
  private backlog: SeatTaskBacklog | undefined;

  constructor(private host: HTMLElement, private deps: SeatsPageDeps) {}

  mount(): () => void {
    this.backlog = new SeatTaskBacklog(this.backlogHost, this.deps);
    this.backlog.mount();
    void this.load();
    this.timer = setInterval(() => {
      if (!this.disposed) void this.load(true);
    }, 15000);
    const off = subscribe((ev) => {
      if (ev.type === 'state.changed') void this.load(true);
    });
    return () => {
      this.disposed = true;
      if (this.timer) clearInterval(this.timer);
      this.backlog?.unmount();
      off();
    };
  }

  async load(quiet = false): Promise<void> {
    try {
      const st = await this.deps.coordStatus({ quiet: true });
      this.report = st.last_run;
      this.mode = st.enabled ? (st.dry_run ? 'on, dry run' : 'on') : 'configured, not running';
      this.seats = await this.deps.externalSeats({ quiet: true });
      this.render();
    } catch (err) {
      if (quiet) return;
      replace(this.host, h('p', { class: 'field-hint', text: `The coordinator module is off: ${err instanceof Error ? err.message : String(err)}` }));
    }
  }

  private render(): void {
    const cards = this.seats.map((s) => this.card(s));
    replace(
      this.host,
      h('h2', { text: `Hand-run seats (coordinator ${this.mode})` }),
      this.msg ? h('p', { class: 'field-hint', attrs: { 'data-role': 'seats-msg' }, text: this.msg }) : null,
      cards.length ? h('div', { class: 'seat-cards' }, ...cards) : h('p', { class: 'field-hint', text: 'No seats registered yet.' }),
      this.backlogHost,
      this.addForm(),
      h('h3', { text: this.report ? `Last pass ${new Date(this.report.finished).toLocaleString()}${this.report.dry ? ' (dry run)' : ''}` : 'No pass yet' }),
      this.report?.error ? h('p', { class: 'warn-box', text: this.report.error }) : null,
      h('ul', { attrs: { 'data-role': 'coord-decisions' } }, ...(this.report?.decisions ?? []).map((d) =>
        h('li', { text: `${d.kind} ${d.task ?? ''} ${d.seat ?? ''}: ${d.detail}` }))),
      h('button', { text: 'Dry run now', attrs: { type: 'button', 'data-role': 'coord-dry' }, on: { click: () => void this.dryRun() } }),
    );
  }

  private card(s: ExternalSeat): HTMLElement {
    const message = s.suggested?.message ?? '';
    const status = s.suggested?.status ?? '';
    const ta = h('textarea', { class: 'mono', attrs: { readonly: true, rows: 4, 'data-role': 'seat-message' }, text: message }) as HTMLTextAreaElement;
    ta.value = message;
    const copy = h('button', {
      text: 'Copy',
      attrs: { type: 'button', 'data-role': 'seat-copy', 'data-seat': s.callsign },
      on: { click: () => void this.copy(message, copy) },
    });
    const log = (s.nudge_log ?? []).slice(-3);
    return h('section', { class: 'seat-card', attrs: { 'data-seat': s.callsign } },
      h('h3', { text: `${s.callsign} (${s.kind})` }),
      h('p', { class: 'field-hint', text: `${s.where} — ${seatState(s)}; task ${s.current ?? 'none'}` }),
      h('p', { class: 'field-hint', text: policyLine(s.policy) }),
      status ? h('p', { text: `status: ${status}` }) : null,
      message ? ta : h('p', { class: 'field-hint', attrs: { 'data-role': 'seat-empty' }, text: 'nothing to send' }),
      h('div', { class: 'row-actions' },
        message ? copy : null,
        h('button', {
          text: s.policy?.paused ? 'Resume nudges' : 'Pause nudges',
          attrs: { type: 'button', 'data-role': 'seat-pause', 'data-seat': s.callsign },
          on: { click: () => void this.pause(s) },
        }),
        h('button', {
          text: 'Queue',
          attrs: { type: 'button', 'data-role': 'seat-queue', 'data-seat': s.callsign },
          on: { click: () => void this.showQueue(s.callsign) },
        }),
        s.nudge ? h('button', {
          text: 'Nudge',
          attrs: { type: 'button', 'data-role': 'seat-nudge', 'data-seat': s.callsign },
          on: { click: () => void this.nudge(s.callsign) },
        }) : null,
        h('button', {
          text: 'Remove',
          attrs: { type: 'button', 'data-role': 'seat-remove', 'data-seat': s.callsign },
          on: { click: () => void this.remove(s.callsign) },
        }),
      ),
      ...log.map((e) => h('p', {
        class: 'field-hint',
        text: `nudged ${new Date(e.at).toLocaleString()}${e.typed ? '' : ' (not typed)'}: ${e.message.slice(0, 80)}`,
      })),
    );
  }

  private addForm(): HTMLElement {
    const callsign = h('input', { attrs: { placeholder: 'callsign', 'data-field': 'callsign' } }) as HTMLInputElement;
    const kind = h('select', { attrs: { 'data-field': 'kind' } },
      ...['freebuff', 'kilo', 'opencode', 'gui', 'other'].map((k) => h('option', { attrs: { value: k }, text: k }))) as HTMLSelectElement;
    const where = h('input', { attrs: { placeholder: 'tmux session or GUI label', 'data-field': 'where' } }) as HTMLInputElement;
    const queue = h('input', { attrs: { placeholder: 'queue file (optional)', 'data-field': 'queue_file' } }) as HTMLInputElement;
    const enabled = h('input', { attrs: { type: 'checkbox', 'data-field': 'enabled' } }) as HTMLInputElement;
    return h('section', { class: 'seat-add' },
      h('h3', { text: 'Register a seat' }),
      callsign, kind, where, queue,
      h('label', { class: 'field-hint' }, enabled, ' nudges on'),
      h('button', {
        text: 'Add seat',
        attrs: { type: 'button', 'data-role': 'seat-add' },
        on: { click: () => void this.add(callsign, kind, where, queue, enabled) },
      }),
    );
  }

  private async add(callsign: HTMLInputElement, kind: HTMLSelectElement, where: HTMLInputElement, queue: HTMLInputElement, enabled: HTMLInputElement): Promise<void> {
    const seat: SeatInput = {
      callsign: callsign.value.trim(),
      kind: kind.value,
      where: where.value.trim(),
      queue_file: queue.value.trim() || undefined,
      policy: {
        enabled: enabled.checked,
        trigger: 'both',
        idle_minutes: 60,
        min_gap_minutes: 60,
        max_per_hour: 1,
      },
    };
    try {
      await this.deps.saveSeat(seat);
      this.msg = `registered ${seat.callsign}`;
    } catch (err) {
      this.msg = err instanceof Error ? err.message : String(err);
    }
    await this.load();
  }

  private async copy(message: string, b: HTMLElement): Promise<void> {
    const ok = await (this.deps.copy ?? copyText)(message);
    b.textContent = ok ? 'Copied' : 'Copy failed';
    setTimeout(() => { b.textContent = 'Copy'; }, 1500);
  }

  private async pause(s: ExternalSeat): Promise<void> {
    try {
      await this.deps.pauseSeat(s.callsign, !s.policy?.paused);
      this.msg = `${s.callsign}: nudges ${s.policy?.paused ? 'resumed' : 'paused'}`;
    } catch (err) {
      this.msg = err instanceof Error ? err.message : String(err);
    }
    await this.load();
  }

  private async remove(cs: string): Promise<void> {
    try {
      await this.deps.deleteSeat(cs);
      this.msg = `removed ${cs}`;
    } catch (err) {
      this.msg = err instanceof Error ? err.message : String(err);
    }
    await this.load();
  }

  private async showQueue(cs: string): Promise<void> {
    try {
      const q = await this.deps.seatQueue(cs);
      this.msg = `${cs} queue: ${q.text ? 'read' : '(empty)'}`;
      await this.load();
      this.host.append(h('pre', { class: 'mono', attrs: { 'data-role': 'seat-queue-text' }, text: q.text || '(empty)' }));
    } catch (err) {
      this.msg = `${cs}: ${err instanceof Error ? err.message : String(err)}`;
      await this.load();
    }
  }

  private async nudge(cs: string): Promise<void> {
    try {
      await this.deps.nudgeSeat(cs);
      this.msg = `${cs} nudged`;
    } catch (err) {
      this.msg = `${cs}: ${err instanceof Error ? err.message : String(err)}`;
    }
    await this.load();
  }

  private async dryRun(): Promise<void> {
    try {
      const r = await this.deps.coordRun(true);
      this.msg = `dry run: ${(r.decisions ?? []).length} decision(s), ${(r.writes ?? []).length} BotChat write(s) held back`;
    } catch (err) {
      this.msg = err instanceof Error ? err.message : String(err);
    }
    await this.load();
  }
}

export function mountSeatsPage(host: HTMLElement): () => void {
  return new SeatsPage(host, api).mount();
}
