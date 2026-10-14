// stream.ts — the slide-in #stream-panel (UI3). For a running stage it opens
// the SSE proxy /api/stages/{id}/stream and appends each output line as text;
// for a finished stage it shows the stored output. There is no polling: the
// server pushes "output", "status" and "done" events. Lines are text nodes, so
// model output can never become markup (UI1).

import { stageStreamURL, type Stage } from './api.js';
import { h, replace } from './dom.js';
import { button } from './forms.js';

/** The part of EventSource the panel uses, so tests can drive it. */
export interface SourceLike {
  addEventListener(type: string, fn: (ev: { data?: string }) => void): void;
  close(): void;
}

export interface StreamDeps {
  open(url: string): SourceLike;
}

export const defaultStreamDeps = (): StreamDeps => ({ open: (url) => new EventSource(url) as unknown as SourceLike });

/** MAX_LINES bounds the DOM: older lines are dropped from the top. */
export const MAX_LINES = 2000;

/** What the panel needs to know about a stage. */
export type StreamStage = Pick<Stage, 'id' | 'role' | 'backend' | 'model' | 'state' | 'outcome' | 'raw_output'>;

export class StreamPanel {
  private source: SourceLike | null = null;
  private out = h('div', { class: 'stream-output', attrs: { 'data-role': 'stream-output' } });
  private status = h('span', { class: 'stream-status', attrs: { 'data-role': 'stream-status' } });
  private lines = 0;
  /** The stage being shown; 0 when closed. */
  stageId = 0;

  constructor(private panel: HTMLElement, private deps: StreamDeps) {}

  /** open shows a stage: live output for a running one, the stored output otherwise. */
  open(st: StreamStage): void {
    this.closeSource();
    this.stageId = st.id;
    this.lines = 0;
    replace(this.out);
    replace(this.panel,
      h('div', { class: 'stream-header' },
        h('strong', { text: `${st.role} · ${st.backend}/${st.model}` }),
        this.status,
        button('Close', () => this.close(), { field: 'close-stream', icon: 'cancel' })),
      this.out);
    this.panel.removeAttribute('hidden');
    if (st.state === 'running') {
      this.setStatus('running', 'running');
      const es = this.deps.open(stageStreamURL(st.id));
      this.source = es;
      es.addEventListener('output', (ev) => this.onOutput(ev.data ?? ''));
      es.addEventListener('status', (ev) => this.setStatus(ev.data ?? '', ev.data === 'completed' ? 'completed' : 'running'));
      es.addEventListener('done', () => { this.setStatus('finished', 'completed'); this.closeSource(); });
      es.addEventListener('error', () => {
        // Close ourselves: the browser would otherwise retry a stage that has not started or has gone.
        this.setStatus('stream ended', '');
        this.closeSource();
      });
    } else {
      this.setStatus(st.outcome || st.state, st.outcome === 'pass' ? 'completed' : '');
      this.append(st.raw_output || '(no output recorded)');
    }
  }

  close(): void {
    this.closeSource();
    this.stageId = 0;
    this.panel.setAttribute('hidden', '');
    replace(this.panel);
  }

  private closeSource(): void {
    this.source?.close();
    this.source = null;
  }

  private setStatus(text: string, cls: string): void {
    this.status.textContent = text;
    this.status.className = 'stream-status' + (cls ? ' ' + cls : '');
  }

  private onOutput(data: string): void {
    let line = data;
    try {
      const parsed = JSON.parse(data) as { line?: string };
      if (typeof parsed.line === 'string') line = parsed.line;
    } catch {
      /* a plain-text frame: show it as it is */
    }
    this.append(line);
  }

  private append(text: string): void {
    this.out.appendChild(document.createTextNode(text + '\n'));
    this.lines++;
    while (this.lines > MAX_LINES && this.out.childNodes.length > 0) {
      this.out.removeChild(this.out.childNodes[0]);
      this.lines--;
    }
    this.out.scrollTop = this.out.scrollHeight;
  }
}
