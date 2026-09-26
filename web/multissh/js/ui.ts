import { TerminalSession } from "./terminal";
import type { HostConfig, SessionStatus } from "./types";
import { hostHasCredential, hostDisplayName } from "./hosts";
import type { HostStore } from "./hosts";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

interface Panel {
  el: HTMLElement;
  updateConfig(config: HostConfig, index: number): void;
  session: TerminalSession;
}

/** Mount the SSH console tab: master broadcast bar + one terminal panel per host. */
export function mountSSHApp(
  root: HTMLElement,
  store: HostStore,
  maxSessions: number,
): void {
  root.innerHTML = "";
  root.classList.add("ssh-app");

  const terminals = el("section", "ssh-terminals");
  const master = buildMasterBar();
  const grid = el("div", "term-grid");
  terminals.append(master.bar, grid);

  // One panel per host object, so adding, removing or reordering hosts in the
  // rail keeps every surviving panel's connection and scrollback.
  const panels = new Map<HostConfig, Panel>();

  const sync = (hosts: HostConfig[]): void => {
    const live = new Set(hosts);
    for (const [host, panel] of panels) {
      if (live.has(host)) continue;
      panel.session.dispose();
      panel.el.remove();
      panels.delete(host);
    }
    hosts.forEach((host, i) => {
      let panel = panels.get(host);
      if (!panel) {
        panel = buildPanel(i, host, grid);
        panels.set(host, panel);
      } else {
        panel.updateConfig(host, i);
      }
      // append() moves an existing node, so this also applies the rail order.
      grid.append(panel.el);
    });
    grid.dataset.panelCount = String(hosts.length);
  };

  sync(store.getHosts());
  store.onChange(sync);

  master.onSend = (text: string) => {
    for (const p of panels.values()) {
      p.session.broadcast(text);
    }
  };

  root.append(terminals);

  requestAnimationFrame(() => {
    for (const p of panels.values()) p.session.resize();
  });
}

/**
 * Keyword -> byte sequence table for the `key:` prefix on the blast line.
 *
 * Adding a keyword is one line here and nothing else: `"ctrl+d": "\x04",`.
 */
const KEY_SEQUENCES: Record<string, string> = {
  "ctrl+c": "\x03",
};

function buildMasterBar(): {
  bar: HTMLElement;
  onSend: (text: string) => void;
} {
  const bar = el("div", "master-bar");
  const label = el("label", "master-label");
  label.textContent = "Broadcast";
  const input = el("input", "master-input");
  input.type = "text";
  input.placeholder =
    "Type a command to send to all connected hosts, or key:ctrl+c…";
  input.autocomplete = "off";
  const sendBtn = el("button", "master-send");
  sendBtn.type = "button";
  sendBtn.textContent = "Send to all";
  const hint = el("span", "master-hint");

  const api = { bar, onSend: (_text: string) => {} };

  let hintTimer: ReturnType<typeof setTimeout> | null = null;
  const showHint = (text: string): void => {
    hint.textContent = text;
    if (hintTimer !== null) clearTimeout(hintTimer);
    hintTimer = setTimeout(() => {
      hint.textContent = "";
    }, 4000);
  };

  // Blast-line history: in memory, this browser session only, never persisted.
  // `cursor === null` means "editing a fresh line"; `draft` holds that fresh
  // line while the user is walking back through history.
  const history: string[] = [];
  let cursor: number | null = null;
  let draft = "";

  const recall = (delta: -1 | 1): void => {
    if (history.length === 0) return;
    if (cursor === null) {
      if (delta === 1) return; // Down on a fresh line does nothing
      draft = input.value;
      cursor = history.length - 1;
    } else {
      const next = cursor + delta;
      if (next < 0) return; // already at the oldest entry
      if (next >= history.length) {
        // Past the newest entry: back to the line being typed.
        cursor = null;
        input.value = draft;
        input.setSelectionRange(input.value.length, input.value.length);
        return;
      }
      cursor = next;
    }
    input.value = history[cursor] as string;
    input.setSelectionRange(input.value.length, input.value.length);
  };

  const remember = (line: string): void => {
    if (line.trim() !== "" && history[history.length - 1] !== line) {
      history.push(line);
    }
    cursor = null;
    draft = "";
  };

  const submit = (): void => {
    const text = input.value;
    const keyword = parseKeyword(text);

    if (keyword !== null) {
      const seq = KEY_SEQUENCES[keyword];
      if (seq === undefined) {
        // Unknown keyword: send nothing at all rather than blasting the raw
        // text at every host, and leave it in the field to be corrected.
        showHint(
          `unknown key "${keyword}" — known: ${Object.keys(KEY_SEQUENCES).join(", ")}`,
        );
        return;
      }
      api.onSend(seq);
    } else {
      api.onSend(text + "\n");
    }

    remember(text);
    input.value = "";
    input.focus();
  };

  sendBtn.addEventListener("click", submit);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      submit();
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      recall(-1);
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      recall(1);
    }
  });

  bar.append(label, input, sendBtn, hint);
  return api;
}

/** Return the lower-cased keyword of a `key:<name>` line, or null if it is not one. */
function parseKeyword(text: string): string | null {
  const trimmed = text.trim();
  if (!trimmed.toLowerCase().startsWith("key:")) return null;
  return trimmed.slice(4).trim().toLowerCase();
}

function buildPanel(
  index: number,
  initialConfig: HostConfig,
  container: HTMLElement,
): Panel {
  let config: HostConfig = { ...initialConfig };

  const panelEl = el("div", "term-panel");
  const header = el("div", "term-header");

  const collapseBtn = el("button", "term-collapse");
  collapseBtn.type = "button";
  collapseBtn.textContent = "\u25be";
  collapseBtn.title = "Collapse this terminal (the session stays connected)";
  collapseBtn.setAttribute("aria-expanded", "true");

  const dot = el("span", "status-dot");
  const titleEl = el("span", "term-title");
  titleEl.textContent = hostDisplayName(config, index);
  const statusText = el("span", "status-text");
  statusText.textContent = "disconnected";

  const spacer = el("span", "term-spacer");

  const pauseLabel = el("label", "pause-toggle");
  const pause = el("input");
  pause.type = "checkbox";
  const pauseText = el("span");
  pauseText.textContent = "Pause";
  pauseLabel.append(pause, pauseText);

  const interruptBtn = el("button", "btn btn-interrupt");
  interruptBtn.type = "button";
  interruptBtn.textContent = "Ctrl-C";
  interruptBtn.title = "Send Ctrl-C (SIGINT) to this host";
  interruptBtn.disabled = true;

  const connectBtn = el("button", "btn btn-connect");
  connectBtn.type = "button";
  connectBtn.textContent = "Connect";
  const disconnectBtn = el("button", "btn btn-disconnect");
  disconnectBtn.type = "button";
  disconnectBtn.textContent = "Disconnect";
  disconnectBtn.disabled = true;

  header.append(
    collapseBtn,
    dot,
    titleEl,
    statusText,
    spacer,
    pauseLabel,
    interruptBtn,
    connectBtn,
    disconnectBtn,
  );

  const body = el("div", "term-body");
  panelEl.append(header, body);
  // Attach before the terminal opens so xterm measures a laid-out element.
  container.append(panelEl);

  const session = new TerminalSession(body);

  const setStatus = (status: SessionStatus, message?: string): void => {
    panelEl.dataset.status = status;
    statusText.textContent = message ? `${status}: ${message}` : status;
    connectBtn.disabled = status === "connecting" || status === "connected";
    disconnectBtn.disabled =
      status === "disconnected" || status === "connecting";
    interruptBtn.disabled = status !== "connected";
  };
  session.onStatus = setStatus;
  setStatus("disconnected");

  // Collapsing is CSS only: the WebSocket stays open, the xterm instance is
  // never disposed, and pause state is untouched -- so output keeps arriving
  // and is there on expand. Re-fit after the panel is laid out again.
  collapseBtn.addEventListener("click", () => {
    const collapsed = panelEl.classList.toggle("is-collapsed");
    collapseBtn.textContent = collapsed ? "\u25b8" : "\u25be";
    collapseBtn.setAttribute("aria-expanded", collapsed ? "false" : "true");
    collapseBtn.title = collapsed
      ? "Expand this terminal"
      : "Collapse this terminal (the session stays connected)";
    if (!collapsed) {
      requestAnimationFrame(() => session.resize());
    }
  });

  pause.addEventListener("change", () => {
    session.paused = pause.checked;
    panelEl.classList.toggle("is-paused", pause.checked);
  });

  connectBtn.addEventListener("click", () => {
    if (!config.ip || !config.user) {
      setStatus("error", "set IP and user in the host rail");
      return;
    }
    if (!hostHasCredential(config)) {
      setStatus(
        "error",
        config.authMethod === "password"
          ? "enter this host's password in the host rail"
          : "select an SSH key in the host rail",
      );
      return;
    }
    session.connect(config);
  });
  disconnectBtn.addEventListener("click", () => session.disconnect());
  interruptBtn.addEventListener("click", () => session.interrupt());

  return {
    el: panelEl,
    updateConfig(updated: HostConfig, i: number): void {
      config = { ...updated };
      titleEl.textContent = hostDisplayName(config, i);
    },
    session,
  };
}
