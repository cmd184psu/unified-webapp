// reviewpage.ts — the Reviews tab (AF-14): branch reviews the API ran, with
// their verify state, verdict and numbered findings. Text goes through h().

import { api, type Review } from './api.js';
import { h, replace } from './dom.js';

export type ReviewsPageDeps = Pick<typeof api, 'reviews'>;

function stateText(r: Review): string {
  if (r.state === 'failed') return `failed: ${r.error ?? 'unknown error'}`;
  if (r.state !== 'done') return r.state;
  return `${r.verdict || 'no verdict'}${r.reviewer_model ? ` by ${r.reviewer_model}` : ''}`;
}

export class ReviewsPage {
  private msg = '';
  constructor(private host: HTMLElement, private deps: ReviewsPageDeps) {}

  mount(): () => void {
    void this.load();
    return () => {};
  }

  async load(): Promise<void> {
    try {
      const list = await this.deps.reviews('', { quiet: true });
      this.render(list);
    } catch (err) {
      replace(this.host, h('p', { class: 'field-hint', text: `Reviews are unavailable: ${err instanceof Error ? err.message : String(err)}` }));
    }
  }

  private render(list: Review[]): void {
    replace(this.host,
      h('h2', { text: 'Reviews' }),
      h('p', { class: 'field-hint', text: 'Independent verify + reviewer runs requested through the review API.' }),
      this.msg ? h('p', { class: 'field-hint', attrs: { 'data-role': 'reviews-msg' }, text: this.msg }) : null,
      h('button', { text: 'Refresh', attrs: { type: 'button', 'data-role': 'reviews-refresh' }, on: { click: () => void this.load() } }),
      list.length === 0
        ? h('p', { class: 'field-hint', text: 'No reviews yet.' })
        : h('ul', { class: 'review-list' }, ...list.map((r) => h('li', { attrs: { 'data-review': String(r.id) } },
          h('strong', { text: `${r.task_id} — ${r.branch}` }),
          h('span', { text: ` (${r.project}) — ${stateText(r)}` }),
          r.findings ? h('pre', { class: 'mono', text: r.findings }) : null))));
  }
}

export function mountReviewsPage(host: HTMLElement): () => void {
  return new ReviewsPage(host, api).mount();
}
