// Modal lightbox for picking an SSH key. It lists only the server user's
// ~/.ssh directory (resolved server-side) and never allows navigation: the user
// just selects a file name to feed to the SSH connection. Subdirectories are
// shown disabled so it is clear they cannot be entered.

import { fetchKeys } from "./api";
import type { KeyFile } from "./types";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

/**
 * Open the key picker. Resolves with the chosen base name, or null if the user
 * cancels. The returned name is a single file name within ~/.ssh.
 */
export function openKeyPicker(): Promise<string | null> {
  return new Promise((resolve) => {
    const overlay = el("div", "modal-overlay");
    const dialog = el("div", "modal");

    const header = el("div", "modal-header");
    const title = el("h2", "modal-title");
    title.textContent = "Select SSH key (~/.ssh)";
    const closeBtn = el("button", "modal-close");
    closeBtn.type = "button";
    closeBtn.textContent = "\u00d7";
    header.append(title, closeBtn);

    const list = el("ul", "key-list");
    const note = el("p", "modal-note");
    note.textContent = "Loading\u2026";

    dialog.append(header, note, list);
    overlay.append(dialog);
    document.body.append(overlay);

    let settled = false;
    const finish = (value: string | null): void => {
      if (settled) return;
      settled = true;
      overlay.remove();
      document.removeEventListener("keydown", onKey);
      resolve(value);
    };

    const onKey = (e: KeyboardEvent): void => {
      if (e.key === "Escape") finish(null);
    };
    document.addEventListener("keydown", onKey);
    overlay.addEventListener("click", (e) => {
      if (e.target === overlay) finish(null);
    });
    closeBtn.addEventListener("click", () => finish(null));

    fetchKeys()
      .then((keys) => renderList(list, note, keys, finish))
      .catch((err: unknown) => {
        note.textContent = `Failed to read ~/.ssh: ${(err as Error).message}`;
      });
  });
}

function renderList(
  list: HTMLElement,
  note: HTMLElement,
  keys: KeyFile[],
  finish: (value: string | null) => void,
): void {
  if (keys.length === 0) {
    note.textContent = "No files found in ~/.ssh.";
    return;
  }
  note.textContent = "Click a file to use it as the SSH key.";
  for (const k of keys) {
    const item = el("li", k.isDir ? "key-item is-dir" : "key-item");
    const icon = el("span", "key-icon");
    icon.textContent = k.isDir ? "\u{1F4C1}" : "\u{1F511}";
    const name = el("span", "key-name");
    name.textContent = k.name;
    item.append(icon, name);
    if (k.isDir) {
      const tag = el("span", "key-tag");
      tag.textContent = "directory";
      item.append(tag);
    } else {
      item.addEventListener("click", () => finish(k.name));
    }
    list.append(item);
  }
}
