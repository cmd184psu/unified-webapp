import { openKeyPicker } from "./keypicker";
import { openDirPicker } from "./dirpicker";
import { fetchHosts, saveHosts } from "./api";
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
    ip: "",
    port: 22,
    user: "",
    key: "",
    remoteDir: "/tmp",
    authMethod: "key",
    password: "",
  };
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
  /** Register a callback invoked after hydration and after each debounced save. */
  onChange(cb: (hosts: HostConfig[]) => void): void;
}

/**
 * Mount the persistent host-config rail into root; returns the shared store.
 *
 * `hostCount` is the server-reported `maxSessions` (GET /api/config), so the
 * rail renders exactly as many cards as the server will accept on PUT.
 */
export function mountHostRail(root: HTMLElement, hostCount: number): HostStore {
  root.classList.add("host-rail");
  root.dataset.hostCount = String(hostCount);

  const titleEl = el("h1", "rail-title");
  titleEl.textContent = "Hosts";
  const subEl = el("p", "rail-subtitle");
  subEl.textContent = `Configure up to ${hostCount} host${hostCount === 1 ? "" : "s"} shared across both tabs.`;
  root.append(titleEl, subEl);

  const hosts: HostConfig[] = Array.from({ length: hostCount }, makeDefault);
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

  const inputRefs: {
    ip: HTMLInputElement;
    user: HTMLInputElement;
    port: HTMLInputElement;
    key: HTMLInputElement;
    remoteDir: HTMLInputElement;
  }[] = [];

  for (let i = 0; i < hostCount; i++) {
    const h = hosts[i] as HostConfig;
    const card = el("div", "host-card");
    const cardTitle = el("h3", "host-card-title");
    cardTitle.textContent = `Host ${i + 1}`;
    card.append(cardTitle);

    const ipInput = labeledInput(card, "IP / hostname", "e.g. 10.0.0.5");
    ipInput.addEventListener("input", () => {
      h.ip = ipInput.value.trim();
      scheduleSave();
    });

    const userInput = labeledInput(card, "User", "e.g. root");
    userInput.addEventListener("input", () => {
      h.user = userInput.value.trim();
      scheduleSave();
    });

    const portInput = labeledInput(card, "Port", "22");
    portInput.value = "22";
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
    card.append(authRow);

    const keyRow = el("div", "field");
    const keyLabel = el("label", "field-label");
    keyLabel.textContent = "SSH key";
    const keyInput = el("input", "field-input key-field");
    keyInput.type = "text";
    keyInput.readOnly = true;
    keyInput.placeholder = "Click to select from ~/.ssh\u2026";
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
    card.append(keyRow);

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
    pwInput.addEventListener("input", () => {
      h.password = pwInput.value;
      // Notify without saving: the panels need the new credential, and there
      // is nothing here for the server to persist.
      notify();
    });
    pwRow.append(pwLabel, pwInput);
    card.append(pwRow);

    const dirRow = el("div", "field");
    const dirLabel = el("label", "field-label");
    dirLabel.textContent = "Remote directory";
    const dirInput = el("input", "field-input dir-field");
    dirInput.type = "text";
    dirInput.readOnly = true;
    dirInput.placeholder = "/tmp";
    dirInput.value = "/tmp";
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
    card.append(dirRow);

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
    card.append(copyBtn, copyHint);

    inputRefs.push({
      ip: ipInput,
      user: userInput,
      port: portInput,
      key: keyInput,
      remoteDir: dirInput,
    });

    root.append(card);
  }

  fetchHosts()
    .then((loaded) => {
      for (let i = 0; i < Math.min(loaded.length, hostCount); i++) {
        const src = loaded[i] as PersistedHost;
        const h = hosts[i] as HostConfig;
        const refs = inputRefs[i] as (typeof inputRefs)[0];
        h.ip = src.ip ?? "";
        h.user = src.user ?? "";
        h.port = src.port ?? 22;
        h.key = src.key ?? "";
        h.remoteDir = src.remoteDir ?? "/tmp";
        refs.ip.value = h.ip;
        refs.user.value = h.user;
        refs.port.value = String(h.port);
        refs.key.value = h.key;
        refs.remoteDir.value = h.remoteDir;
      }
      for (const cb of listeners) cb(hosts);
    })
    .catch(() => undefined);

  return {
    getHosts: () => hosts,
    onChange: (cb) => {
      listeners.push(cb);
    },
  };
}
