import { openKeyPicker } from "./keypicker";
import { openDirPicker } from "./dirpicker";
import { fetchHosts, saveHosts } from "./api";
import { confirmDialog } from "@shared";
import type { AuthMethod, HostConfig, PersistedHost } from "./types";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function makeDefault(): HostConfig {
  return {
    name: "",
    ip: "",
    port: 22,
    user: "",
    key: "",
    remoteDir: "/tmp",
    authMethod: "key",
    password: "",
  };
}

/** The label shown on a host's rail card and terminal panel. */
export function hostDisplayName(h: { name: string }, index: number): string {
  return h.name.trim() || `Host ${index + 1}`;
}

/** True when the host has the one credential its auth method calls for. */
export function hostHasCredential(h: HostConfig): boolean {
  return h.authMethod === "password" ? h.password !== "" : h.key !== "";
}

/**
 * Build an `ssh` CLI invocation for the given host config.
 *
 * A password host gets no `-i` and, emphatically, no password in any form --
 * not inline, not as a comment, not wrapped in `sshpass`. The operator types
 * it at ssh's own prompt.
 */
export function buildSSHCommand(h: HostConfig): string {
  const parts = ["ssh"];
  if (h.authMethod === "key" && h.key) parts.push("-i", `~/.ssh/${h.key}`);
  if (h.port && h.port !== 22) parts.push("-p", String(h.port));
  parts.push(`${h.user}@${h.ip}`);
  return parts.join(" ");
}

/** Copy text to the clipboard, falling back to a temporary textarea. */
async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    void 0;
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.append(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}

function labeledInput(
  parent: HTMLElement,
  label: string,
  placeholder: string,
): HTMLInputElement {
  const row = el("div", "field");
  const lbl = el("label", "field-label");
  lbl.textContent = label;
  const input = el("input", "field-input");
  input.type = "text";
  input.placeholder = placeholder;
  input.autocomplete = "off";
  row.append(lbl, input);
  parent.append(row);
  return input;
}

/** Shared store for the host configurations (one per session slot). */
export interface HostStore {
  /** Returns the live array of host configs (mutated in place on edits). */
  getHosts(): HostConfig[];
  /**
   * Register a callback invoked after hydration, after each debounced save,
   * and after every add, remove or reorder (the array's order is the rail's).
   */
  onChange(cb: (hosts: HostConfig[]) => void): void;
}

const GRIP_SVG =
  '<svg viewBox="0 0 14 14" fill="currentColor" width="14" height="14">' +
  '<circle cx="4" cy="3" r="1.2"/><circle cx="10" cy="3" r="1.2"/>' +
  '<circle cx="4" cy="7" r="1.2"/><circle cx="10" cy="7" r="1.2"/>' +
  '<circle cx="4" cy="11" r="1.2"/><circle cx="10" cy="11" r="1.2"/></svg>';

/**
 * Mount the persistent host-config rail into root; returns the shared store.
 *
 * `maxHosts` is the server-reported `maxSessions` (GET /api/config): hosts can
 * be added up to it, since that is the most the server accepts on PUT.
 */
export function mountHostRail(root: HTMLElement, maxHosts: number): HostStore {
  root.classList.add("host-rail");

  const titleEl = el("h1", "rail-title");
  titleEl.textContent = "Hosts";
  const list = el("div", "host-list");
  const addBtn = el("button", "btn host-add");
  addBtn.type = "button";
  addBtn.textContent = "+ Add host";
  root.append(titleEl, list, addBtn);

  const hosts: HostConfig[] = [];
  /** The one expanded card (accordion); null when all are collapsed. */
  let openHost: HostConfig | null = null;
  const listeners: Array<(hosts: HostConfig[]) => void> = [];

  const notify = (): void => {
    for (const cb of listeners) cb(hosts);
  };

  let saveTimer: ReturnType<typeof setTimeout> | null = null;
  const scheduleSave = (): void => {
    if (saveTimer !== null) clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
      saveTimer = null;
      void saveHosts(hosts).catch(() => undefined);
      notify();
    }, 400);
  };

  /** Save now and tell the panels: used for add, remove and reorder. */
  const commitStructure = (): void => {
    if (saveTimer !== null) clearTimeout(saveTimer);
    saveTimer = null;
    render();
    void saveHosts(hosts).catch(() => undefined);
    notify();
  };

  const cardHost = new WeakMap<HTMLElement, HostConfig>();

  /** Move `h` before or after `target`, keeping the rail a single column. */
  const moveHost = (h: HostConfig, target: HostConfig, after: boolean): void => {
    const from = hosts.indexOf(h);
    let to = hosts.indexOf(target) + (after ? 1 : 0);
    if (from < 0 || to < 0) return;
    if (from < to) to--;
    if (from === to) return;
    hosts.splice(from, 1);
    hosts.splice(to, 0, h);
    commitStructure();
  };

  const clearDropMarks = (): void => {
    list.querySelectorAll(".drop-above, .drop-below").forEach((c) => {
      c.classList.remove("drop-above", "drop-below");
    });
  };

  /** Pointer drag from the grip only; a line shows where the card will land. */
  const attachDrag = (grip: HTMLElement, card: HTMLElement, h: HostConfig): void => {
    grip.addEventListener("pointerdown", (e) => {
      if (e.button !== 0) return;
      e.preventDefault();
      grip.setPointerCapture(e.pointerId);
      card.classList.add("is-dragging");
      let drop: { target: HostConfig; after: boolean } | null = null;

      const onMove = (ev: PointerEvent): void => {
        clearDropMarks();
        drop = null;
        const hit = document.elementFromPoint(ev.clientX, ev.clientY);
        const over = hit?.closest<HTMLElement>(".host-card");
        if (!over || over === card || !list.contains(over)) return;
        const target = cardHost.get(over);
        if (!target) return;
        const r = over.getBoundingClientRect();
        const after = ev.clientY >= r.top + r.height / 2;
        over.classList.add(after ? "drop-below" : "drop-above");
        drop = { target, after };
      };
      const onEnd = (): void => {
        grip.removeEventListener("pointermove", onMove);
        grip.removeEventListener("pointerup", onEnd);
        grip.removeEventListener("pointercancel", onEnd);
        card.classList.remove("is-dragging");
        clearDropMarks();
        if (drop) moveHost(h, drop.target, drop.after);
      };
      grip.addEventListener("pointermove", onMove);
      grip.addEventListener("pointerup", onEnd);
      grip.addEventListener("pointercancel", onEnd);
    });
  };

  const buildCard = (h: HostConfig, i: number): HTMLElement => {
    const card = el("div", "host-card");
    cardHost.set(card, h);
    const cardHeader = el("div", "host-card-header");
    const grip = el("span", "drag-handle");
    grip.title = "Drag to reorder";
    grip.innerHTML = GRIP_SVG;
    const collapseBtn = el("button", "host-card-collapse");
    collapseBtn.type = "button";
    const cardTitle = el("h3", "host-card-title");
    cardTitle.textContent = hostDisplayName(h, i);
    const removeBtn = el("button", "host-card-remove");
    removeBtn.type = "button";
    removeBtn.textContent = "\u2715";
    removeBtn.title = "Remove this host";
    removeBtn.disabled = hosts.length <= 1;
    cardHeader.append(grip, collapseBtn, cardTitle, removeBtn);
    card.append(cardHeader);
    attachDrag(grip, card, h);

    const cardBody = el("div", "host-card-body");
    card.append(cardBody);

    const collapsed = openHost !== h;
    card.classList.toggle("collapsed", collapsed);
    collapseBtn.textContent = collapsed ? "\u25b8" : "\u25be";
    collapseBtn.setAttribute("aria-expanded", collapsed ? "false" : "true");
    collapseBtn.title = collapsed ? "Expand this host" : "Collapse this host";
    // Accordion: opening one card closes whichever was open.
    collapseBtn.addEventListener("click", () => {
      openHost = openHost === h ? null : h;
      render();
    });

    removeBtn.addEventListener("click", () => {
      void confirmDialog(
        `Remove ${hostDisplayName(h, hosts.indexOf(h))}? Its terminal will be disconnected and closed.`,
        { title: "Remove host", confirmLabel: "Remove" },
      ).then((ok) => {
        const at = hosts.indexOf(h);
        if (!ok || at < 0 || hosts.length <= 1) return;
        hosts.splice(at, 1);
        if (openHost === h) openHost = null;
        commitStructure();
      });
    });

    const nameInput = labeledInput(cardBody, "Name", `Host ${i + 1}`);
    nameInput.value = h.name;
    nameInput.addEventListener("input", () => {
      h.name = nameInput.value;
      cardTitle.textContent = hostDisplayName(h, i);
      scheduleSave();
    });

    const ipInput = labeledInput(cardBody, "IP / hostname", "e.g. 10.0.0.5");
    ipInput.value = h.ip;
    ipInput.addEventListener("input", () => {
      h.ip = ipInput.value.trim();
      scheduleSave();
    });

    const userInput = labeledInput(cardBody, "User", "e.g. root");
    userInput.value = h.user;
    userInput.addEventListener("input", () => {
      h.user = userInput.value.trim();
      scheduleSave();
    });

    const portInput = labeledInput(cardBody, "Port", "22");
    portInput.value = String(h.port);
    portInput.addEventListener("input", () => {
      const v = parseInt(portInput.value, 10);
      h.port = isNaN(v) ? 22 : v;
      scheduleSave();
    });

    const authRow = el("div", "field auth-field");
    const authLabel = el("label", "field-label");
    authLabel.textContent = "Authenticate with";
    const authChoices = el("div", "auth-choices");
    const authRadio = (value: AuthMethod, text: string): HTMLInputElement => {
      const choice = el("label", "auth-choice");
      const radio = el("input");
      radio.type = "radio";
      radio.name = `auth-method-${i}`;
      radio.value = value;
      radio.checked = h.authMethod === value;
      const span = el("span");
      span.textContent = text;
      choice.append(radio, span);
      authChoices.append(choice);
      return radio;
    };
    const keyRadio = authRadio("key", "SSH key");
    const passwordRadio = authRadio("password", "Password");
    authRow.append(authLabel, authChoices);
    cardBody.append(authRow);

    const keyRow = el("div", "field");
    const keyLabel = el("label", "field-label");
    keyLabel.textContent = "SSH key";
    const keyInput = el("input", "field-input key-field");
    keyInput.type = "text";
    keyInput.readOnly = true;
    keyInput.placeholder = "Click to select from ~/.ssh\u2026";
    keyInput.value = h.key;
    keyInput.addEventListener("click", () => {
      void openKeyPicker().then((name) => {
        if (name) {
          h.key = name;
          keyInput.value = name;
          scheduleSave();
        }
      });
    });
    keyRow.append(keyLabel, keyInput);
    cardBody.append(keyRow);

    // The password lives here and in the frames sent to the bridge and to
    // /api/broadcast. It is never put in the object sent to PUT /api/hosts
    // (see persistedFields in api.ts) and never written to storage.
    const pwRow = el("div", "field password-field");
    const pwLabel = el("label", "field-label");
    pwLabel.textContent = "Password (memory only)";
    const pwInput = el("input", "field-input password-input");
    pwInput.type = "password";
    pwInput.autocomplete = "off";
    pwInput.placeholder = "Not saved; cleared on reload";
    pwInput.value = h.password;
    pwInput.addEventListener("input", () => {
      h.password = pwInput.value;
      // Notify without saving: the panels need the new credential, and there
      // is nothing here for the server to persist.
      notify();
    });
    pwRow.append(pwLabel, pwInput);
    cardBody.append(pwRow);

    const dirRow = el("div", "field");
    const dirLabel = el("label", "field-label");
    dirLabel.textContent = "Remote directory";
    const dirInput = el("input", "field-input dir-field");
    dirInput.type = "text";
    dirInput.readOnly = true;
    dirInput.placeholder = "/tmp";
    dirInput.value = h.remoteDir;
    dirInput.addEventListener("click", () => {
      // Remote browsing goes through /api/sftp/listdir, which authenticates
      // with a key only -- a password host types its path instead (the input
      // is left editable for exactly that case).
      if (h.authMethod === "password") return;
      if (!h.ip || !h.user || !h.key) {
        const prev = dirInput.placeholder;
        dirInput.placeholder = "Set host, user & key first";
        setTimeout(() => (dirInput.placeholder = prev), 2000);
        return;
      }
      void openDirPicker(
        { host: h.ip, port: h.port, user: h.user, key: h.key },
        h.remoteDir || "/tmp",
      ).then((path) => {
        if (path !== null) {
          h.remoteDir = path;
          dirInput.value = path;
          scheduleSave();
        }
      });
    });
    dirInput.addEventListener("input", () => {
      if (h.authMethod !== "password") return;
      h.remoteDir = dirInput.value.trim() || "/tmp";
      scheduleSave();
    });
    dirRow.append(dirLabel, dirInput);
    cardBody.append(dirRow);

    const copyHint = el("p", "copy-hint");
    copyHint.textContent =
      "The command has no password in it \u2014 ssh will prompt for it.";

    /** Show only the fields the selected auth method uses. */
    const applyAuthMethod = (): void => {
      const usesPassword = h.authMethod === "password";
      keyRow.hidden = usesPassword;
      pwRow.hidden = !usesPassword;
      copyHint.hidden = !usesPassword;
      dirInput.readOnly = !usesPassword;
      dirInput.placeholder = usesPassword
        ? "/tmp"
        : "Click to browse the remote host\u2026";
    };

    const chooseAuth = (method: AuthMethod): void => {
      h.authMethod = method;
      applyAuthMethod();
      notify();
    };
    keyRadio.addEventListener("change", () => {
      if (keyRadio.checked) chooseAuth("key");
    });
    passwordRadio.addEventListener("change", () => {
      if (passwordRadio.checked) chooseAuth("password");
    });
    applyAuthMethod();

    const copyBtn = el("button", "btn host-copy-ssh");
    copyBtn.type = "button";
    copyBtn.textContent = "Copy ssh command";
    copyBtn.title = "Copy an ssh CLI command for this host";
    let copyResetTimer: ReturnType<typeof setTimeout> | null = null;
    copyBtn.addEventListener("click", () => {
      if (!h.ip || !h.user) {
        const prev = copyBtn.textContent;
        copyBtn.textContent = "Set host & user first";
        if (copyResetTimer !== null) clearTimeout(copyResetTimer);
        copyResetTimer = setTimeout(() => {
          copyBtn.textContent = prev;
        }, 2000);
        return;
      }
      void copyText(buildSSHCommand(h)).then((ok) => {
        copyBtn.textContent = ok ? "Copied!" : "Copy failed";
        if (copyResetTimer !== null) clearTimeout(copyResetTimer);
        copyResetTimer = setTimeout(() => {
          copyBtn.textContent = "Copy ssh command";
        }, 2000);
      });
    });
    cardBody.append(copyBtn, copyHint);

    return card;
  };

  function render(): void {
    list.replaceChildren(...hosts.map((h, i) => buildCard(h, i)));
    addBtn.disabled = hosts.length >= maxHosts;
    addBtn.title = addBtn.disabled
      ? `The server allows at most ${maxHosts} hosts`
      : "Add another host";
  }

  addBtn.addEventListener("click", () => {
    if (hosts.length >= maxHosts) return;
    const h = makeDefault();
    hosts.push(h);
    openHost = h;
    commitStructure();
  });

  const hydrate = (loaded: PersistedHost[]): void => {
    for (const src of loaded.slice(0, maxHosts)) {
      const h = makeDefault();
      h.name = src.name ?? "";
      h.ip = src.ip ?? "";
      h.user = src.user ?? "";
      h.port = src.port ?? 22;
      h.key = src.key ?? "";
      h.remoteDir = src.remoteDir ?? "/tmp";
      hosts.push(h);
    }
    if (hosts.length === 0) hosts.push(makeDefault());
    openHost = hosts[0] ?? null;
    render();
    notify();
  };

  fetchHosts()
    .then(hydrate)
    .catch(() => hydrate([]));

  return {
    getHosts: () => hosts,
    onChange: (cb) => {
      listeners.push(cb);
    },
  };
}
