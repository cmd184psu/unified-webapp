import { listRemoteDir } from "./api";
import type { RemoteDirEntry } from "./types";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function parentPath(p: string): string {
  if (p === "/" || p === "") return "/";
  const idx = p.lastIndexOf("/");
  if (idx <= 0) return "/";
  return p.slice(0, idx);
}

/**
 * Open a navigable remote-directory picker. Resolves with the chosen path or
 * null on cancel. If target lacks host/user/key, resolves null immediately.
 */
export function openDirPicker(
  target: { host: string; port: number; user: string; key: string },
  startPath = "/tmp",
): Promise<string | null> {
  return new Promise((resolve) => {
    const overlay = el("div", "modal-overlay");
    const dialog = el("div", "modal");

    const header = el("div", "modal-header");
    const title = el("h2", "modal-title");
    title.textContent = "Choose remote directory";
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

    const footer = el("div", "modal-footer");
    const useBtn = el("button", "btn btn-connect nav-use-btn");
    useBtn.type = "button";
    useBtn.textContent = "Use this directory";
    footer.append(useBtn);

    dialog.append(header, pathBar, note, list, footer);
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
    useBtn.addEventListener("click", () => finish(currentPath));

    const navigate = (path: string): void => {
      note.textContent = "Loading\u2026";
      list.innerHTML = "";
      pathText.textContent = path;

      listRemoteDir(target, path)
        .then(({ path: resolved, entries }) => {
          currentPath = resolved;
          pathText.textContent = resolved;
          renderDirList(list, note, entries, resolved, navigate);
        })
        .catch((err: unknown) => {
          note.textContent = `Error: ${(err as Error).message}`;
        });
    };

    navigate(startPath);
  });
}

function renderDirList(
  list: HTMLElement,
  note: HTMLElement,
  entries: RemoteDirEntry[],
  currentPath: string,
  navigate: (path: string) => void,
): void {
  list.innerHTML = "";
  const dirs = entries.filter((e) => e.isDir);
  note.textContent =
    dirs.length === 0
      ? "No subdirectories."
      : "Click a directory to navigate into it.";

  if (currentPath !== "/") {
    const upItem = el("li", "key-item nav-up");
    const icon = el("span", "key-icon");
    icon.textContent = "\u{1F4C2}";
    const name = el("span", "key-name");
    name.textContent = "..";
    upItem.append(icon, name);
    upItem.addEventListener("click", () => navigate(parentPath(currentPath)));
    list.append(upItem);
  }

  for (const e of dirs) {
    const item = el("li", "key-item");
    const icon = el("span", "key-icon");
    icon.textContent = "\u{1F4C1}";
    const name = el("span", "key-name");
    name.textContent = e.name;
    item.append(icon, name);
    item.addEventListener("click", () => {
      const sep = currentPath.endsWith("/") ? "" : "/";
      navigate(`${currentPath}${sep}${e.name}`);
    });
    list.append(item);
  }
}
