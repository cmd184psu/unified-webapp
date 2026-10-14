// events.ts — the live event stream (RC5, UI3). One EventSource on
// /api/events; the server names every message "agentflow" and the JSON data
// carries its own type. A dropped stream reconnects with backoff. There is no
// polling: state.changed tells subscribers to refetch what they show.

export interface RunSnapshot {
  project: string;
  state: string;
  effort: number;
  remembered: string;
}

export type AgentflowEvent =
  | { type: 'snapshot'; projects: RunSnapshot[] }
  | { type: 'project.run_state'; project: string; effort: number; state: string; action: string; actor: string }
  | { type: 'project.update'; project: string }
  | { type: 'project.delete'; project: string }
  | { type: 'effort.update'; project: string; effort: number }
  | { type: 'task.update'; project: string; effort: number; task: string }
  | { type: 'backend.update'; backend: string }
  | { type: 'candidates.update'; role: string; project?: string }
  | { type: 'settings.update' }
  | { type: 'state.changed' };

export type EventType = AgentflowEvent['type'];
export type Listener = (ev: AgentflowEvent) => void;

/** backoffMs is the reconnect delay after the nth consecutive failure: 1s doubling to 30s. */
export function backoffMs(failures: number): number {
  return Math.min(30000, 1000 * 2 ** Math.max(0, failures));
}

const EVENT_NAME = 'agentflow';
const URL = '/api/events';

const listeners = new Set<Listener>();
let source: EventSource | null = null;
let failures = 0;
let timer: ReturnType<typeof setTimeout> | null = null;
let wanted = false;

/** subscribe registers a listener for every event and returns its unsubscribe. */
export function subscribe(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** dispatch parses one message's data and hands it to the listeners; unparseable data is dropped. */
export function dispatch(data: string): void {
  let ev: AgentflowEvent;
  try {
    ev = JSON.parse(data) as AgentflowEvent;
  } catch {
    return;
  }
  if (!ev || typeof ev.type !== 'string') return;
  for (const fn of [...listeners]) fn(ev);
}

function connect(): void {
  if (!wanted || source) return;
  const es = new EventSource(URL);
  source = es;
  es.addEventListener(EVENT_NAME, (m) => {
    failures = 0;
    dispatch((m as MessageEvent<string>).data);
  });
  es.onerror = () => {
    // Close and reconnect ourselves so the delay is ours, not the browser's.
    es.close();
    if (source === es) source = null;
    if (!wanted || timer) return;
    timer = setTimeout(() => {
      timer = null;
      connect();
    }, backoffMs(failures));
    failures++;
  };
}

/** startEvents opens the stream (idempotent). */
export function startEvents(): void {
  wanted = true;
  connect();
}

/** stopEvents closes the stream and cancels a pending reconnect. */
export function stopEvents(): void {
  wanted = false;
  if (timer) clearTimeout(timer);
  timer = null;
  source?.close();
  source = null;
  failures = 0;
}
