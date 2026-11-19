// seats.ts — the seat panel (GET /api/seats): every open stage worked by a seat, a BotChat
// callsign or an interactive program agentflow runs in a terminal session. A terminal seat
// shows the command that opens its session (`tmux attach -t <name>`), so the owner can watch
// and type into it. A seat waiting on the owner (a question, or silence past its budget) takes
// an answer, which is typed into its session; Cancel stops the seat and the stage. Hidden while
// no seat is working. Text goes through h().

import { api, type SeatStage } from './api.js';
import { h, replace } from './dom.js';
import { startPoll, subscribe } from './events.js';

export type SeatDeps = Pick<typeof api, 'seats' | 'answerSeat' | 'cancelSeat'>;

/** seatLabel is where the stage sits: project, effort, task and role. */
export function seatLabel(s: SeatStage): string {
  const where = [s.project, s.effort ? `effort ${s.effort}` : '', s.task ?? ''].filter(Boolean).join(', ');
  return `${where} — ${s.role} on ${s.backend} (stage ${s.stage})`;
}

export class SeatPanel {
  private seats: SeatStage[] = [];
  private armed = new Set<number>(); // stages whose Cancel was pressed once
  private off: (() => void) | null = null;
  private stopPoll: (() => void) | null = null;

  constructor(private host: HTMLElement, private deps: SeatDeps) {}

  mount(): () => void {
    this.off = subscribe((ev) => { if (ev.type === 'state.changed') void this.load(); });
    this.stopPoll = startPoll(() => true, () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.stopPoll?.();
      this.off = this.stopPoll = null;
    };
  }

  async load(): Promise<void> {
    try {
      this.seats = await this.deps.seats({ quiet: true });
    } catch {
      return;
    }
    this.render();
  }

  private async answer(stage: number, input: HTMLInputElement): Promise<void> {
    const text = input.value.trim();
    if (!text) return;
    await this.deps.answerSeat(stage, text);
    input.value = '';
    await this.load();
  }

  private async cancel(stage: number): Promise<void> {
    if (!this.armed.has(stage)) {
      this.armed.add(stage);
      this.render();
      return;
    }
    this.armed.delete(stage);
    await this.deps.cancelSeat(stage);
    await this.load();
  }

  render(): void {
    if (this.seats.length === 0) {
      replace(this.host);
      this.host.setAttribute('hidden', '');
      return;
    }
    replace(this.host, h('div', { class: 'seat-panel', attrs: { 'data-role': 'seat-panel' } },
      h('p', { text: this.seats.length === 1 ? 'One seat is working:' : `${this.seats.length} seats are working:` }),
      h('ul', {}, ...this.seats.map((s) => {
        const input = h('input', { attrs: { type: 'text', placeholder: 'Answer the seat', 'data-role': 'seat-answer' } }) as HTMLInputElement;
        return h('li', { attrs: { 'data-stage': String(s.stage) } },
          h('span', { text: seatLabel(s) }),
          s.attach ? h('code', { class: 'seat-attach', text: s.attach, attrs: { 'data-role': 'seat-attach' } }) : h('span', { text: ` — ${s.model} on BotChat` }),
          s.waiting ? h('div', { class: 'warn-box', text: s.waiting.message }) : h('span', {}),
          h('div', { class: 'seat-actions' },
            input,
            h('button', { text: 'Send', attrs: { type: 'button', 'data-role': 'seat-send' }, on: { click: () => { void this.answer(s.stage, input); } } }),
            h('button', { text: this.armed.has(s.stage) ? 'Really cancel the stage' : 'Cancel', attrs: { type: 'button', 'data-role': 'seat-cancel' }, on: { click: () => { void this.cancel(s.stage); } } })));
      }))));
    this.host.removeAttribute('hidden');
  }
}
