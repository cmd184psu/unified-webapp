// seatspage.ts — the Seats tab (AF-13): the seats the owner runs by hand (deepseek4, fb-beam),
// as the coordinator module sees them: the task fed to each, its queue file, whether its
// screen is idle or shows a halt text, and a Nudge button; then the newest coordinator pass's
// decisions with a "Dry run now" button. Text goes through h().

import { api, type CoordReport, type ExternalSeat } from './api.js';
import { h, replace } from './dom.js';

export type SeatsPageDeps = Pick<typeof api, 'coordStatus' | 'coordRun' | 'externalSeats' | 'seatQueue' | 'nudgeSeat'>;

function seatState(s: ExternalSeat): string {
  if (!s.running) return 'no session';
  if (s.dead) return 'session ended';
  if (s.halt) return `waits for the owner: ${s.halt}`;
  return s.ready ? `at its input box since ${new Date(s.since).toLocaleTimeString()}` : 'working';
}

export class SeatsPage {
  private msg = '';
  constructor(private host: HTMLElement, private deps: SeatsPageDeps) {}

  mount(): () => void {
    void this.load();
    return () => {};
  }

  async load(): Promise<void> {
    let seats: ExternalSeat[] = [];
    let report: CoordReport | null = null;
    try {
      const st = await this.deps.coordStatus({ quiet: true });
      report = st.last_run;
      seats = await this.deps.externalSeats({ quiet: true });
      this.render(seats, report, st.enabled ? (st.dry_run ? 'on, dry run' : 'on') : 'configured, not running');
    } catch (err) {
      replace(this.host, h('p', { class: 'field-hint', text: `The coordinator module is off: ${err instanceof Error ? err.message : String(err)}` }));
    }
  }

  private render(seats: ExternalSeat[], report: CoordReport | null, mode: string): void {
    replace(this.host,
      h('h2', { text: `Hand-run seats (coordinator ${mode})` }),
      this.msg ? h('p', { class: 'field-hint', attrs: { 'data-role': 'seats-msg' }, text: this.msg }) : null,
      h('ul', { class: 'seat-list' }, ...seats.map((s) => h('li', { attrs: { 'data-seat': s.callsign } },
        h('strong', { text: s.callsign }),
        h('span', { text: ` (${s.session}) — ${seatState(s)}${s.current ? `; task ${s.current}` : ''}${s.last_action ? `; ${s.last_action}` : ''}` }),
        h('button', { text: 'Queue', attrs: { type: 'button', 'data-role': 'seat-queue' }, on: { click: () => void this.showQueue(s.callsign) } }),
        s.nudge ? h('button', { text: 'Nudge', attrs: { type: 'button', 'data-role': 'seat-nudge' }, on: { click: () => void this.nudge(s.callsign) } }) : null))),
      h('h3', { text: report ? `Last pass ${new Date(report.finished).toLocaleString()}${report.dry ? ' (dry run)' : ''}` : 'No pass yet' }),
      report?.error ? h('p', { class: 'warn-box', text: report.error }) : null,
      h('ul', { attrs: { 'data-role': 'coord-decisions' } }, ...(report?.decisions ?? []).map((d) =>
        h('li', { text: `${d.kind} ${d.task ?? ''} ${d.seat ?? ''}: ${d.detail}` }))),
      h('button', { text: 'Dry run now', attrs: { type: 'button', 'data-role': 'coord-dry' }, on: { click: () => void this.dryRun() } }));
  }

  private async showQueue(cs: string): Promise<void> {
    try {
      const q = await this.deps.seatQueue(cs);
      this.msg = '';
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
