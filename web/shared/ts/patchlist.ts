// patchlist.ts — a surgical DOM list patcher.
//
// Moved verbatim out of web/taskmaster/js/ui/live.ts (plan
// docs/PLAN-utuber-taskmaster-lane.md §4.11), which now keeps only
// LiveController. The key attribute is renamed data-ui-key (was
// data-tm-key) now that this is shared, not taskmaster-only.
//
// Usage:
//   patchList(container, tasks, {
//     key: (t) => t.id,
//     create: (t) => renderTaskRow(t),
//     update: (el, t) => updateTaskRow(el, t),
//   });

export interface PatchListOptions<T> {
  /** Stable identity for a list item, used to match old/new DOM nodes. */
  key: (item: T) => string | number;
  /** Builds a brand-new DOM node for an item not currently rendered. */
  create: (item: T) => HTMLElement;
  /** Updates an existing DOM node in place to reflect the item's new data. */
  update: (el: HTMLElement, item: T) => void;
}

const KEY_ATTR = "data-ui-key";

/**
 * Surgically reconciles `container`'s children to match `items`, in order:
 * inserts nodes for new keys, removes nodes for keys no longer present, and
 * calls `update` in place (no re-creation) for keys that persist —
 * reordering existing DOM nodes rather than rebuilding them. Never resets
 * innerHTML, so scroll position, focus, and selection inside untouched rows
 * are preserved. This is the anti-jitter primitive backing the live/pause
 * + surgical-refresh UI policy.
 */
export function patchList<T>(
  container: HTMLElement,
  items: T[],
  opts: PatchListOptions<T>
): void {
  const existingByKey = new Map<string, HTMLElement>();
  for (const child of Array.from(container.children)) {
    const el = child as HTMLElement;
    const k = el.getAttribute(KEY_ATTR);
    if (k !== null) existingByKey.set(k, el);
  }

  const seenKeys = new Set<string>();
  let cursor: ChildNode | null = container.firstChild;

  for (const item of items) {
    const key = String(opts.key(item));
    seenKeys.add(key);

    let el = existingByKey.get(key);
    if (el) {
      opts.update(el, item);
    } else {
      el = opts.create(item);
      el.setAttribute(KEY_ATTR, key);
    }

    // Ensure `el` is at the current cursor position without disturbing
    // other untouched nodes.
    if (cursor !== el) {
      container.insertBefore(el, cursor);
    } else {
      cursor = cursor.nextSibling;
      continue;
    }
    cursor = el.nextSibling;
  }

  // Remove any nodes whose keys are no longer present.
  for (const [key, el] of existingByKey) {
    if (!seenKeys.has(key)) {
      el.remove();
    }
  }
}
