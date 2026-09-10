import { listServerFiles } from "./api";
import type { ServerFileEntry } from "./types";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1_048_576) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1_048_576).toFixed(2)} MB`;
}

function parentPath(p: string): string {
  if (p === "") return "";
  const idx = p.lastIndexOf("/");
  if (idx < 0) return "";
  return p.slice(0, idx);
}

/**
 * Open a navigable server-side file browser. Resolves with the chosen file
 * path or null on cancel.
 */
export function openFilePicker(startPath = ""): Promise<string | null> {
  return new Promise((resolve) => {
    const overlay = el("div", "modal-overlay");
    const dialog = el("div", "modal");

    const header = el("div", "modal-header");
    const title = el("h2", "modal-title");
    title.textContent = "Choose server file";
    const closeBtn = el("button", "modal-close");
    closeBtn.type = "button";
    closeBtn.textContent = "\u00d7";
    header.append(title, closeBtn);

    const pathBar = el("div", "nav-path-bar");
    const pathText = el("span", "nav-path-text");
    pathBar.append(pathText);

    const note = el("p", "modal-note");
    note.textContent = "Loading\u2026";

    const list = el("ul", "key-list");

    dialog.append(header, pathBar, note, list);
    overlay.append(dialog);
    document.body.append(overlay);

    let currentPath = startPath;
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

    const navigate = (path: string): void => {
      note.textContent = "Loading\u2026";
      list.innerHTML = "";
      pathText.textContent = path === "" ? "(root)" : path;

      listServerFiles(path)
        .then(({ path: resolved, entries }) => {
          currentPath = resolved;
          pathText.textContent = resolved === "" ? "(root)" : resolved;
          renderFileList(list, note, entries, currentPath, navigate, finish);
        })
        .catch((err: unknown) => {
          note.textContent = `Error: ${(err as Error).message}`;
        });
    };

    navigate(startPath);
  });
}

function renderFileList(
  list: HTMLElement,
  note: HTMLElement,
  entries: ServerFileEntry[],
  currentPath: string,
  navigate: (path: string) => void,
  finish: (path: string | null) => void,
): void {
  list.innerHTML = "";

  if (entries.length === 0) {
    note.textContent = "Empty directory.";
  } else {
    note.textContent = "Click a file to select it; click a folder to open it.";
  }

  if (currentPath !== "") {
    const upItem = el("li", "key-item nav-up");
    const icon = el("span", "key-icon");
    icon.textContent = "\u{1F4C2}";
    const name = el("span", "key-name");
    name.textContent = "..";
    upItem.append(icon, name);
    upItem.addEventListener("click", () => navigate(parentPath(currentPath)));
    list.append(upItem);
  }

  for (const e of entries) {
    const item = el("li", e.isDir ? "key-item" : "key-item file-item");
    const icon = el("span", "key-icon");
    icon.textContent = e.isDir ? "\u{1F4C1}" : "\u{1F4C4}";
    const name = el("span", "key-name");
    name.textContent = e.name;
    item.append(icon, name);

    if (e.isDir) {
      item.addEventListener("click", () => {
        const sep = currentPath === "" || currentPath.endsWith("/") ? "" : "/";
        navigate(`${currentPath}${sep}${e.name}`);
      });
    } else {
      const sizeEl = el("span", "key-tag");
      sizeEl.textContent = fmtBytes(e.size);
      item.append(sizeEl);
      item.addEventListener("click", () => {
        const sep = currentPath === "" || currentPath.endsWith("/") ? "" : "/";
        finish(`${currentPath}${sep}${e.name}`);
      });
    }

    list.append(item);
  }
}
