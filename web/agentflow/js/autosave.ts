// autosave.ts — saves an edited card by itself once the user has been idle
// for a while, and only when something really differs from what was last
// saved. It never saves an unchanged form, never retries a failed save on
// its own, and never reloads the card: the caller's save keeps the form as is.

import { h } from './dom.js';

export const AUTOSAVE_DELAY_MS = 5000;

/** timing lets a test shorten the pause. */
export const timing = { delay: AUTOSAVE_DELAY_MS };

export interface Autosave {
  /** The small state mark to place in the card: pending, saving, saved or failed. */
  status: HTMLElement;
  /** watch re-arms the timer on every input or change inside el. */
  watch(el: HTMLElement): void;
  /** touch re-arms the timer after a change the page made itself (a reorder, a picked folder). */
  touch(): void;
  /** flush saves now if something changed, e.g. when the card is closed. */
  flush(): Promise<void>;
  /** stop cancels a pending save without saving. */
  stop(): void;
}

export interface AutosaveOptions {
  /** The current value of everything the card edits, as text: equal text means no change. */
  snapshot: () => string;
  /** Saves it; true when stored, false when refused (the caller has shown why). */
  save: () => Promise<boolean>;
  delay?: number;
  /** The card is rebuilt around unsaved edits: treat it as changed and save it after the pause. */
  startDirty?: boolean;
}

const MARKS = {
  idle: { text: '', title: '' },
  pending: { text: '…', title: 'Unsaved changes: saves after a few seconds without typing' },
  saving: { text: '↻', title: 'Saving' },
  saved: { text: '✓', title: 'Saved' },
  failed: { text: '⚠', title: 'Not saved: fix the field marked above, then change something to try again' },
} as const;

/** autosave watches a card. The baseline is the snapshot at the time of the call, so build the card first. */
export function autosave(o: AutosaveOptions): Autosave {
  const status = h('span', { class: 'autosave-state', attrs: { 'data-role': 'autosave-state', 'aria-live': 'polite' } });
  let saved: string | null = o.startDirty ? null : o.snapshot();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let busy = false;
  const mark = (k: keyof typeof MARKS): void => {
    status.textContent = MARKS[k].text;
    status.setAttribute('title', MARKS[k].title);
    status.setAttribute('data-state', k);
  };
  mark('idle');

  const run = async (): Promise<void> => {
    timer = undefined;
    if (busy) {
      timer = setTimeout(() => void run(), o.delay ?? timing.delay);
      return;
    }
    const now = o.snapshot();
    if (now === saved) {
      mark('idle');
      return;
    }
    busy = true;
    mark('saving');
    let ok = false;
    try {
      ok = await o.save();
    } catch {
      ok = false;
    }
    busy = false;
    if (ok) saved = now;
    // Edits made while the save ran are saved by the timer they re-armed.
    if (timer === undefined) mark(ok ? 'saved' : 'failed');
  };

  const touch = (): void => {
    if (timer !== undefined) clearTimeout(timer);
    mark(o.snapshot() === saved ? 'idle' : 'pending');
    timer = setTimeout(() => void run(), o.delay ?? timing.delay);
  };

  return {
    status,
    watch(el) {
      el.addEventListener('input', touch);
      el.addEventListener('change', touch);
    },
    touch,
    async flush() {
      if (timer !== undefined) clearTimeout(timer);
      await run();
    },
    stop() {
      if (timer !== undefined) clearTimeout(timer);
      timer = undefined;
    },
  };
}
