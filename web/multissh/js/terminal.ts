// TerminalSession owns one xterm.js terminal and its dedicated WebSocket to the
// Go SSH bridge. Each panel gets its own session (and thus its own socket), so
// every connection runs fully in parallel.

import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

import { wsURL } from "./api";
import type { HostConfig, ServerControl, SessionStatus } from "./types";

const encoder = new TextEncoder();

export class TerminalSession {
  private readonly term: Terminal;
  private readonly fit: FitAddon;
  private ws: WebSocket | null = null;
  private statusValue: SessionStatus = "disconnected";

  /** When true, this panel ignores master-field broadcasts. */
  paused = false;

  /** Notified on every status transition (for the indicator + buttons). */
  onStatus: (status: SessionStatus, message?: string) => void = () => {};

  constructor(container: HTMLElement) {
    this.term = new Terminal({
      convertEol: false,
      cursorBlink: true,
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      theme: { background: "#0b0f17" },
    });
    this.fit = new FitAddon();
    this.term.loadAddon(this.fit);
    this.term.open(container);
    this.safeFit();

    // Local typing in this panel goes straight to its own connection.
    this.term.onData((data) => {
      if (this.statusValue === "connected") {
        this.sendBytes(data);
      }
    });
  }

  get status(): SessionStatus {
    return this.statusValue;
  }

  connect(cfg: HostConfig): void {
    this.disconnect();
    this.setStatus("connecting");
    this.term.clear();

    let socket: WebSocket;
    try {
      socket = new WebSocket(wsURL());
    } catch {
      this.setStatus("error", "unable to open socket");
      return;
    }
    socket.binaryType = "arraybuffer";
    this.ws = socket;

    socket.onopen = () => {
      this.safeFit();
      // Exactly one credential: the bridge rejects a frame carrying both, so
      // the unused one is sent empty rather than left over from a mode switch.
      const usesPassword = cfg.authMethod === "password";
      socket.send(
        JSON.stringify({
          type: "connect",
          ip: cfg.ip,
          host: cfg.ip,
          port: cfg.port,
          user: cfg.user,
          key: usesPassword ? "" : cfg.key,
          password: usesPassword ? cfg.password : "",
          cols: this.term.cols,
          rows: this.term.rows,
        }),
      );
    };

    socket.onmessage = (ev: MessageEvent) => {
      if (typeof ev.data === "string") {
        this.handleControl(ev.data);
      } else {
        this.term.write(new Uint8Array(ev.data as ArrayBuffer));
      }
    };

    socket.onerror = () => {
      if (this.statusValue !== "connected") {
        this.setStatus("error", "socket error");
      }
    };

    socket.onclose = () => {
      if (this.ws === socket) {
        this.ws = null;
      }
      if (this.statusValue !== "error") {
        this.setStatus("disconnected");
      }
    };
  }

  disconnect(): void {
    const socket = this.ws;
    if (!socket) {
      return;
    }
    this.ws = null;
    try {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "disconnect" }));
      }
      socket.close();
    } catch {
      // ignore close races
    }
    this.setStatus("disconnected");
  }

  /** Send input from the master field; respected only when active + unpaused. */
  broadcast(data: string): void {
    if (this.statusValue === "connected" && !this.paused) {
      this.sendBytes(data);
      this.term.focus();
    }
  }

  /** Send Ctrl-C (ETX) to this host regardless of pause state. */
  interrupt(): void {
    if (this.statusValue === "connected") {
      this.sendBytes("\x03");
      this.term.focus();
    }
  }

  /** Re-fit the terminal to its container and inform the remote PTY. */
  resize(): void {
    this.safeFit();
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(
        JSON.stringify({
          type: "resize",
          cols: this.term.cols,
          rows: this.term.rows,
        }),
      );
    }
  }

  private sendBytes(data: string): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(encoder.encode(data));
    }
  }

  private handleControl(raw: string): void {
    let msg: ServerControl;
    try {
      msg = JSON.parse(raw) as ServerControl;
    } catch {
      return;
    }
    if (msg.type === "error") {
      this.setStatus("error", msg.message ?? "connection error");
      if (msg.message) {
        this.term.writeln(`\r\n\x1b[31m${msg.message}\x1b[0m`);
      }
      return;
    }
    if (msg.type === "status") {
      if (msg.state === "connected") {
        this.setStatus("connected");
      } else if (msg.state === "disconnected") {
        this.setStatus("disconnected");
      }
    }
  }

  private setStatus(status: SessionStatus, message?: string): void {
    this.statusValue = status;
    this.onStatus(status, message);
  }

  private safeFit(): void {
    try {
      this.fit.fit();
    } catch {
      // container not laid out yet; ignore
    }
  }
}
