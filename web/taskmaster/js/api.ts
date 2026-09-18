// api.ts — typed client for the taskmaster API (lane board rework, Slice A).
//
// Every endpoint the coordinator exposes (internal/taskmaster/coordinator/
// coordinator.go is authoritative for the route list) gets a typed method
// here. On error, callers get a rejected Promise — this module never calls
// the native alert(); UI code surfaces failures via ui/modal.ts's
// alertDialog per the global "no native dialogs" policy (FRD §8a). A 401
// reloads the page so the platform auth gate can re-challenge.

import { alertDialog } from '@shared';

// ─── Model shapes (mirror internal/taskmaster/models/models.go) ────────────

export interface LaneConfig {
  name: string;
  width: number;
}

export interface Lane extends LaneConfig {
  paused: boolean;
  paused_at?: string;
  paused_by?: string;
  created_at: string;
  updated_at: string;
}

export interface LaneStatus extends Lane {
  running_count: number;
}

export interface Task {
  id: number;
  name: string;
  lane_name: string;
  command: string;
  position: number;
  enabled: boolean;
  paused: boolean;
  repeat: boolean;
  cooldown_seconds: number;
  sudo: boolean;
  output_file?: string;
  created_at: string;
  updated_at: string;
}

export interface TaskExecution {
  id: number;
  task_id: number;
  task_name?: string;
  scheduled_at?: string;
  started_at?: string;
  finished_at?: string;
  status: string;
  error_message?: string;
  worker_id?: string;
  duration_ms?: number;
  schedule_delay_ms?: number;
  /** Present only for a running execution, merged in from the in-memory process registry. */
  pid?: number;
  suspended?: boolean;
}

export interface MetricSummary {
  task_name: string;
  group_name: string;
  success_count: number;
  failed_count: number;
  canceled_count: number;
  avg_duration_ms?: number;
  min_duration_ms?: number;
  max_duration_ms?: number;
  avg_schedule_delay_ms?: number;
  last_execution?: string;
}

export interface Capabilities {
  allow_sudo: boolean;
}

export interface AuthMode {
  methods: string[];
}

export interface BrakeState {
  engaged: boolean;
}

// ─── Fetch plumbing ──────────────────────────────────────────────────────

async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const existingHeaders = options.headers as Record<string, string> | undefined;
  if (existingHeaders) {
    Object.assign(headers, existingHeaders);
  }
  let resp: Response;
  try {
    resp = await fetch(path, { ...options, headers });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    void alertDialog('Network error contacting the server: ' + msg);
    throw err;
  }
  if (resp.status === 401) {
    window.location.reload();
    throw new Error('unauthorized');
  }
  if (!resp.ok) {
    let errMsg = resp.statusText;
    try {
      const body = (await resp.json()) as { error?: string };
      if (body.error) errMsg = body.error;
    } catch {
      /* ignore — non-JSON error body */
    }
    void alertDialog('Request failed: ' + errMsg);
    throw new Error(errMsg);
  }
  if (resp.status === 204) return undefined as unknown as T;
  return resp.json() as Promise<T>;
}

export const api = {
  health() {
    return apiFetch<{ status: string; build?: string }>('/api/health');
  },

  capabilities() {
    return apiFetch<Capabilities>('/api/capabilities');
  },

  setCapabilities(allowSudo: boolean) {
    return apiFetch<Capabilities>('/api/capabilities', {
      method: 'POST',
      body: JSON.stringify({ allow_sudo: allowSudo }),
    });
  },

  authMode() {
    return apiFetch<AuthMode>('/api/auth/mode');
  },

  logout() {
    return fetch('/api/auth/logout', { method: 'POST' });
  },

  // ─── Lanes ────────────────────────────────────────────────────────────
  listLanes() {
    return apiFetch<LaneStatus[]>('/api/lanes');
  },
  getLane(name: string) {
    return apiFetch<LaneStatus>('/api/lanes/' + encodeURIComponent(name));
  },
  createLane(l: LaneConfig) {
    return apiFetch<Lane>('/api/lanes', { method: 'POST', body: JSON.stringify(l) });
  },
  updateLane(name: string, updates: Partial<LaneConfig>) {
    return apiFetch<Lane>('/api/lanes/' + encodeURIComponent(name), {
      method: 'PUT',
      body: JSON.stringify(updates),
    });
  },
  deleteLane(name: string) {
    return apiFetch<void>('/api/lanes/' + encodeURIComponent(name), { method: 'DELETE' });
  },
  pauseLane(name: string) {
    return apiFetch<{ status: string }>('/api/lanes/' + encodeURIComponent(name) + '/pause', {
      method: 'POST',
    });
  },
  resumeLane(name: string) {
    return apiFetch<{ status: string }>('/api/lanes/' + encodeURIComponent(name) + '/resume', {
      method: 'POST',
    });
  },
  setLaneWidth(name: string, width: number) {
    return apiFetch<Lane>('/api/lanes/' + encodeURIComponent(name) + '/width', {
      method: 'PUT',
      body: JSON.stringify({ width }),
    });
  },
  setLaneOrder(name: string, order: string[]) {
    return apiFetch<Task[]>('/api/lanes/' + encodeURIComponent(name) + '/order', {
      method: 'PUT',
      body: JSON.stringify({ order }),
    });
  },

  // ─── Tasks ────────────────────────────────────────────────────────────
  listTasks(lane?: string) {
    const q = lane ? '?lane=' + encodeURIComponent(lane) : '';
    return apiFetch<Task[]>('/api/tasks' + q);
  },
  getTask(name: string) {
    return apiFetch<Task>('/api/tasks/' + encodeURIComponent(name));
  },
  addTask(task: Partial<Task>) {
    return apiFetch<Task>('/api/tasks', { method: 'POST', body: JSON.stringify(task) });
  },
  updateTask(name: string, updates: Partial<Task>) {
    return apiFetch<Task>('/api/tasks/' + encodeURIComponent(name), {
      method: 'PUT',
      body: JSON.stringify(updates),
    });
  },
  deleteTask(name: string) {
    return apiFetch<void>('/api/tasks/' + encodeURIComponent(name), { method: 'DELETE' });
  },
  pauseTask(name: string) {
    return apiFetch<{ status: string }>('/api/tasks/' + encodeURIComponent(name) + '/pause', {
      method: 'POST',
    });
  },
  resumeTask(name: string) {
    return apiFetch<{ status: string }>('/api/tasks/' + encodeURIComponent(name) + '/resume', {
      method: 'POST',
    });
  },
  upNext(name: string) {
    return apiFetch<{ execution_id: number }>('/api/tasks/' + encodeURIComponent(name) + '/up-next', {
      method: 'POST',
    });
  },
  moveTask(name: string, laneName: string) {
    return apiFetch<Task>('/api/tasks/' + encodeURIComponent(name) + '/move', {
      method: 'POST',
      body: JSON.stringify({ lane_name: laneName }),
    });
  },

  // ─── Executions ───────────────────────────────────────────────────────
  listExecutions(taskName?: string, limit = 50) {
    const params = new URLSearchParams({ limit: String(limit) });
    if (taskName) params.set('task', taskName);
    return apiFetch<TaskExecution[]>('/api/executions?' + params);
  },
  // If the execution already finished (a genuine race for a fast task —
  // between the board rendering it as running and the click landing), the
  // server answers 200 {"status":"not_running"} rather than an error: that
  // outcome isn't a mistake, so there is nothing here to special-case — a
  // real error status (400 signal failure, 404 no-such-execution) still
  // surfaces through the normal apiFetch error path.
  cancelExecution(id: number) {
    return apiFetch<{ status: string }>('/api/executions/' + id + '/cancel', { method: 'POST' });
  },
  pauseExecution(id: number) {
    return apiFetch<{ status: string }>('/api/executions/' + id + '/pause', { method: 'POST' });
  },
  resumeExecution(id: number) {
    return apiFetch<{ status: string }>('/api/executions/' + id + '/resume', { method: 'POST' });
  },
  /** Opens a live SSE stream of an execution's stdout/stderr. Caller owns close(). */
  openExecutionOutput(id: number): EventSource {
    return new EventSource('/api/executions/' + id + '/output');
  },

  // ─── Metrics ──────────────────────────────────────────────────────────
  getMetrics(lane?: string, task?: string, hours = 24) {
    const params = new URLSearchParams({ hours: String(hours) });
    if (lane) params.set('lane', lane);
    if (task) params.set('task', task);
    return apiFetch<MetricSummary[]>('/api/metrics?' + params);
  },

  // ─── Hand brake ───────────────────────────────────────────────────────
  getBrake() {
    return apiFetch<BrakeState>('/api/brake');
  },
  engageBrake() {
    return apiFetch<BrakeState>('/api/brake', { method: 'POST' });
  },
  releaseBrake() {
    return apiFetch<BrakeState>('/api/brake', { method: 'DELETE' });
  },
};
