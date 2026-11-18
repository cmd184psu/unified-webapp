// health.ts — the banner at the top of the app (GET /api/health). Hidden while
// everything is ok; when opencode is missing or degraded it shows the server's
// message and keeps re-checking until it recovers. Text goes through h().

import { api, type Health } from './api.js';
import { h, replace } from './dom.js';
import { startPoll, subscribe } from './events.js';

export type HealthDeps = Pick<typeof api, 'health'>;

/** healthMessage is the banner text for a health response, or '' when all is well. */
export function healthMessage(hl: Health | null): string {
  if (!hl || hl.status === 'ok') return '';
  const oc = hl.opencode;
  const why = oc?.error ? `: ${oc.error}` : '';
  return `AgentFlow is degraded. opencode is ${oc?.status ?? 'unavailable'}${why}. Planning and running tasks will fail until it is fixed.`;
}

export class HealthBanner {
  private health: Health | null = null;
  private stopPoll: (() => void) | null = null;
  private off: (() => void) | null = null;

  constructor(private host: HTMLElement, private deps: HealthDeps) {}

  mount(): () => void {
    this.off = subscribe((ev) => { if (ev.type === 'settings.update' || ev.type === 'state.changed') void this.load(); });
    this.stopPoll = startPoll(() => healthMessage(this.health) !== '', () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.stopPoll?.();
      this.off = this.stopPoll = null;
    };
  }

  async load(): Promise<void> {
    try {
      this.health = await this.deps.health({ quiet: true });
    } catch {
      return; // an unreachable server shows no banner of its own; the views already report errors
    }
    this.render();
  }

  render(): void {
    const msg = healthMessage(this.health);
    if (msg) {
      replace(this.host, h('div', { class: 'warn-box health-banner', attrs: { role: 'alert', 'data-role': 'health-banner' } }, h('p', { text: msg })));
      this.host.removeAttribute('hidden');
    } else {
      replace(this.host);
      this.host.setAttribute('hidden', '');
    }
  }
}
