// effort-settings.ts — the per-effort settings of the FRD tab: the deliberate
// planning toggle with its keyword suggestion (RP5) and the token and time
// budgets (AT12), plus the budget progress bar the Board shows. Text from the
// FRD and the server reaches the page through h(), so it is text.

import type { BudgetView, EffortView } from './api.js';
import { autosave } from './autosave.js';
import { api } from './api.js';
import { h } from './dom.js';
import { FormErrors, badge, button, checkbox, fieldRow, section, textInput } from './forms.js';

export type EffortSettingsDeps = Pick<typeof api, 'patchEffort'> & { notify(message: string): void };

/** DELIBERATE_PATTERNS are the topics that suggest deliberate planning: security, data migration and personal data. */
const DELIBERATE_PATTERNS: Array<[string, RegExp]> = [
  ['auth', /\b(oauth|auth(n|z|entication|enticate\w*|orization|orize\w*|orisation)?)\b/i],
  ['login', /\blog-?in\b/i],
  ['password', /\bpasswords?\b/i],
  ['credentials', /\bcredentials?\b/i],
  ['migration', /\bmigrat\w+/i],
  ['PII', /\bpii\b|\bpersonal(ly)?[ -](identifiable )?(data|information)\b/i],
  ['GDPR', /\bgdpr\b/i],
  ['encryption', /\bencrypt\w*/i],
  ['payment', /\bpayments?\b/i],
];

/** deliberateTriggers lists the topics the text mentions that suggest deliberate planning (a simple keyword match). */
export function deliberateTriggers(text: string): string[] {
  return DELIBERATE_PATTERNS.filter(([, re]) => re.test(text)).map(([name]) => name);
}

/** formatTokens writes a token count compactly: 950, 12.5k, 3.2M. */
export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${Number((n / 1_000_000).toFixed(1))}M`;
  if (n >= 1_000) return `${Number((n / 1_000).toFixed(1))}k`;
  return String(n);
}

/** formatSeconds writes a duration as 45s, 12m or 2.5h. */
export function formatSeconds(s: number): string {
  if (s >= 3600) return `${Number((s / 3600).toFixed(1))}h`;
  if (s >= 60) return `${Math.round(s / 60)}m`;
  return `${s}s`;
}

/** budgetText is "tokens 4.2k/10k · time 12m/1.0h" over the configured limits. */
export function budgetText(b: BudgetView): string {
  const parts: string[] = [];
  if (b.token_limit > 0) parts.push(`tokens ${formatTokens(b.tokens)}/${formatTokens(b.token_limit)}`);
  if (b.seconds_limit > 0) parts.push(`time ${formatSeconds(b.seconds)}/${formatSeconds(b.seconds_limit)}`);
  return parts.join(' · ');
}

/** budgetBar is the budget progress row: a bar that turns amber at 90% and red at 100%, with the numbers and what the state means. */
export function budgetBar(b: BudgetView): HTMLElement {
  const pct = Math.min(100, Math.round(b.fraction * 100));
  const cls = b.state === 'exhausted' ? ' progress-over' : b.state === 'no_new_tasks' ? ' progress-warn' : '';
  const note = b.state === 'exhausted' ? 'budget used up: the run is stopped'
    : b.state === 'no_new_tasks' ? 'over 90%: no new task starts, running ones finish' : '';
  return h('div', { class: 'console-header', attrs: { 'data-role': 'budget' } },
    h('span', { class: 'pane-title', text: 'Budget' }),
    h('div', { class: 'progress-bar-wrap' }, h('div', { class: 'progress-bar' + cls, attrs: { style: `width: ${pct}%` } })),
    h('span', { class: 'progress-pct', text: `${pct}% — ${budgetText(b)}` }),
    note ? badge(note, b.state === 'exhausted' ? 'warn' : 'muted') : null);
}

/** effortSettings is the FRD tab's section for the selected, unfinished effort: deliberate planning and the budgets. */
export function effortSettings(ef: EffortView, frdText: string, deps: EffortSettingsDeps, onSaved: () => void): HTMLElement {
  const errors = new FormErrors();
  const deliberate = checkbox('effort_deliberate', ef.deliberate);
  const tokens = textInput('budget_tokens', ef.budget_tokens ? String(ef.budget_tokens) : '', { type: 'number', placeholder: 'no limit' });
  const minutes = textInput('budget_minutes', ef.budget_seconds ? String(Number((ef.budget_seconds / 60).toFixed(2))) : '', { type: 'number', placeholder: 'no limit' });
  const triggers = ef.deliberate ? [] : deliberateTriggers(frdText);
  const suggestion = triggers.length === 0 ? null : h('div', { class: 'warn-box', attrs: { 'data-role': 'deliberate-suggestion' } },
    h('p', { text: `This FRD mentions ${triggers.join(', ')}. Deliberate planning adds a pre-mortem and a fuller test plan, which suits this kind of work.` }),
    button('Turn on deliberate planning', () => { deliberate.checked = true; void save().then((ok) => { if (ok) onSaved(); }); }, { field: 'accept-suggestion', icon: 'approve', word: true }));
  const body = (): { deliberate: boolean; budget_tokens: number; budget_seconds: number } => ({
    deliberate: deliberate.checked,
    budget_tokens: Math.max(0, Math.round(Number(tokens.value) || 0)),
    budget_seconds: Math.max(0, Math.round((Number(minutes.value) || 0) * 60)),
  });
  const save = async (): Promise<boolean> => {
    errors.clear();
    try {
      await deps.patchEffort(ef.project, ef.id, body(), { quiet: true });
    } catch (err) {
      errors.show(err);
      return false;
    }
    return true;
  };
  const auto = autosave({ snapshot: () => JSON.stringify(body()), save });
  const el = section(`Settings for #${ef.id}`,
    suggestion,
    errors.add('deliberate', fieldRow('Deliberate planning', deliberate,
      'The planner adds a pre-mortem (three failure scenarios) and an expanded test plan, and the critic checks for them')).el,
    h('div', { class: 'profile-fields' },
      errors.add('budget_tokens', fieldRow('Token budget', tokens, 'Runner tokens for the whole effort; blank is no limit')).el,
      errors.add('budget_seconds', fieldRow('Time budget (minutes)', minutes,
        'Total stage run time, so stages running in parallel add up; blank is no limit. From 90% no new task starts; at 100% the run stops')).el),
    errors.general,
    h('div', { class: 'settings-actions' }, auto.status));
  auto.watch(el);
  return el;
}
