// api.ts — the one place agentflow talks to the server (UI2). Every call goes
// through apiFetch, which shows each failure in an alert dialog and throws an
// ApiError. The typed wrappers below cover the whole /api contract (progress.md,
// "API contract for the UI"); views only consume them.

import { alertDialog } from '@shared';

// ─── Shapes (mirror internal/agentflow/api and model) ───────────────────────

export type RunState = 'stopped' | 'running' | 'pausing' | 'paused';
export type RunOp = 'start' | 'pause' | 'stop' | 'run-one';

export interface Effort {
  id: number;
  project: string;
  title: string;
  status: string;
  run_state: string;
  integration_branch: string;
  base_commit: string;
  yolo: boolean;
  deliberate: boolean;
  created: number;
  updated: number;
}

export interface DocInfo {
  latest: number;
  approved: number;
}

export interface QueueInfo {
  revision: number;
  hash: string;
  status: string;
}

export interface EffortView extends Effort {
  frd: DocInfo;
  plan: DocInfo;
  queue: QueueInfo | null;
}

/** The stage a running task is in right now (PJ5). */
export interface WorkItem {
  task: string;
  attempt: number;
  stage: number;
  role: string;
  backend: string;
  model: string;
  state: string;
}

/** A GET /api/projects row: the overview. progress holds "total" plus a count per task status. */
export interface ProjectView {
  slug: string;
  repo_path: string;
  model_scope: string;
  feedback_cmds: string[];
  limits: Record<string, string>;
  created: number;
  updated: number;
  run_state: string;
  remembered_run_state: string;
  effort: Effort | null;
  progress: Record<string, number>;
  current: WorkItem[];
}

export interface ProjectInput {
  slug: string;
  repo_path: string;
  model_scope?: string;
  feedback_cmds?: string[];
  limits?: Record<string, string | number>;
}

export type ProjectPatch = Partial<ProjectInput>;

export interface LimitsView {
  limits: Record<string, string>;
  effective: Record<string, string>;
}

export interface Health {
  status: 'ok' | 'degraded';
  opencode: OpenCodeStatus;
}

export interface OpenCodeStatus {
  status: 'ok' | 'unavailable';
  version?: string;
  error?: string;
}

export interface BackendModel {
  model: string;
  paid: boolean;
}

export interface Backend {
  name: string;
  host: string;
  api_type: string;
  hosted: boolean;
  paid: boolean;
  slots: number;
  retired: boolean;
  models: BackendModel[];
  down_until?: number;
}

export interface BackendInput {
  name: string;
  host: string;
  api_type: string;
  hosted: boolean;
  paid: boolean;
  slots: number;
  models: BackendModel[];
}

/** A backend write answers with the backend and the SV3 warnings. */
export interface BackendResult {
  backend: Backend;
  rerouted?: number[];
  canceled?: string[];
  warnings: string[];
}

export interface Candidate {
  scope: string;
  project?: string;
  role: string;
  position: number;
  backend: string;
  model: string;
  paid: boolean;
}

export interface CandidateInput {
  backend: string;
  model: string;
}

export type Settings = Record<string, string | number>;

export interface DocView<T = unknown> {
  revision: number;
  created_by: string;
  created: number;
  approved: boolean;
  json?: T;
}

export interface PlanReview {
  [field: string]: unknown;
}

export interface PlanRound {
  round: number;
  reviews: PlanReview[];
}

export interface PlanRounds {
  rounds: PlanRound[];
  revision: number;
  not_critic_approved: boolean;
  warnings: string[] | null;
  waiting: unknown;
}

export interface TaskDef {
  key: string;
  phase: string;
  title: string;
  prompt: string;
  acceptance: string[];
  files_touched: string[];
  depends_on: string[];
}

export interface QueueView {
  effort: number;
  effort_status: string;
  revision: number;
  hash: string;
  status: string;
  approved_by?: string;
  tasks: Array<TaskDef & { id: number; queue: number; waiting_reason: string }>;
}

export interface TaskView extends TaskDef {
  id: number;
  queue: number;
  waiting_reason: string;
  status: string;
  halted: boolean;
  attempts: number;
  current: WorkItem | null;
}

export interface Stage {
  id: number;
  attempt: number;
  effort: number;
  role: string;
  backend: string;
  model: string;
  lane_job_id: string;
  queue_hash: string;
  state: string;
  outcome: string;
  raw_output: string;
  parsed: unknown;
  prompt_path: string;
  started: number;
  finished: number;
}

export interface Attempt {
  id: number;
  task: number;
  n: number;
  kind: string;
  worktree: string;
  outcome: string;
  fingerprint: string;
  worker_model: string;
  reviewer_model: string;
  started: number;
  finished: number;
  stages: Stage[];
}

export interface TaskDetail extends TaskView {
  attempt_list: Attempt[];
  progress: string[];
}

export interface Invariant {
  id: string;
  text: string;
}

// ─── Fetch plumbing ─────────────────────────────────────────────────────────

/** ApiError is what every failed call throws; field is set for 400 validation errors. */
export class ApiError extends Error {
  readonly status: number;
  readonly field?: string;
  constructor(status: number, message: string, field?: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.field = field;
  }
}

export interface FetchOptions {
  method?: string;
  /** JSON-encoded unless it is already a string. */
  body?: unknown;
  /** Skip the alert dialog; the error is still thrown. For callers that show field errors inline. */
  quiet?: boolean;
}

function fail(err: ApiError, quiet: boolean | undefined): never {
  if (!quiet) void alertDialog(err.message);
  throw err;
}

/**
 * apiFetch is the only fetch in agentflow. Paths must be /api/...; a failure is
 * shown in an alert dialog (unless quiet) and thrown as an ApiError.
 */
export async function apiFetch<T>(path: string, opts: FetchOptions = {}): Promise<T> {
  if (!path.startsWith('/api/')) throw new Error(`apiFetch: ${path} is not an /api path`);
  const init: RequestInit = { method: opts.method ?? 'GET' };
  if (opts.body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' };
    init.body = typeof opts.body === 'string' ? opts.body : JSON.stringify(opts.body);
  }
  let resp: Response;
  try {
    resp = await fetch(path, init);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return fail(new ApiError(0, 'Network error contacting the server: ' + msg), opts.quiet);
  }
  if (resp.status === 401) {
    window.location.reload();
    throw new ApiError(401, 'unauthorized');
  }
  if (!resp.ok) {
    let message = resp.statusText || `HTTP ${resp.status}`;
    let field: string | undefined;
    try {
      const body = (await resp.json()) as { error?: string; field?: string };
      if (body.error) message = body.error;
      field = body.field;
    } catch {
      /* non-JSON error body: keep the status text */
    }
    return fail(new ApiError(resp.status, message, field), opts.quiet);
  }
  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

const enc = encodeURIComponent;
const proj = (slug: string): string => `/api/projects/${enc(slug)}`;
const eff = (slug: string, id: number): string => `${proj(slug)}/efforts/${id}`;
const get = <T>(path: string, o?: FetchOptions): Promise<T> => apiFetch<T>(path, o);
const send = <T>(method: string, path: string, body?: unknown, o?: FetchOptions): Promise<T> =>
  apiFetch<T>(path, { ...o, method, body });

// ─── Typed endpoints ────────────────────────────────────────────────────────

export const api = {
  // Health and opencode
  health: (): Promise<Health> => get('/api/health'),
  opencodeVersion: (refresh = false): Promise<OpenCodeStatus> =>
    get(`/api/opencode/version${refresh ? '?refresh=1' : ''}`),

  // Projects
  projects: (): Promise<ProjectView[]> => get('/api/projects'),
  project: (slug: string): Promise<ProjectView> => get(proj(slug)),
  createProject: (p: ProjectInput, o?: FetchOptions): Promise<ProjectView> => send('POST', '/api/projects', p, o),
  gitInit: (repoPath: string): Promise<{ repo_path: string }> =>
    send('POST', '/api/projects/git-init', { repo_path: repoPath, confirm: true }),
  patchProject: (slug: string, p: ProjectPatch, o?: FetchOptions): Promise<ProjectView> =>
    send('PATCH', proj(slug), p, o),
  deleteProject: (slug: string): Promise<void> => send('DELETE', proj(slug)),
  limits: (slug: string): Promise<LimitsView> => get(`${proj(slug)}/limits`),
  putLimits: (slug: string, limits: Record<string, string | number>, o?: FetchOptions): Promise<LimitsView> =>
    send('PUT', `${proj(slug)}/limits`, { limits }, o),

  // Backends, candidates, settings
  backends: (): Promise<Backend[]> => get('/api/backends'),
  createBackend: (b: BackendInput, o?: FetchOptions): Promise<BackendResult> => send('POST', '/api/backends', b, o),
  patchBackend: (name: string, b: Partial<BackendInput>, o?: FetchOptions): Promise<BackendResult> =>
    send('PATCH', `/api/backends/${enc(name)}`, b, o),
  retireBackend: (name: string, cancelRunning: boolean): Promise<BackendResult> =>
    send('POST', `/api/backends/${enc(name)}/retire`, { cancel_running: cancelRunning }),
  enableBackend: (name: string): Promise<BackendResult> => send('POST', `/api/backends/${enc(name)}/enable`),
  candidates: (): Promise<Candidate[]> => get('/api/candidates'),
  putCandidates: (role: string, candidates: CandidateInput[], o?: FetchOptions): Promise<Candidate[]> =>
    send('PUT', `/api/candidates/${enc(role)}`, { candidates }, o),
  putProjectCandidates: (slug: string, role: string, candidates: CandidateInput[], o?: FetchOptions): Promise<Candidate[]> =>
    send('PUT', `${proj(slug)}/candidates/${enc(role)}`, { candidates }, o),
  clearProjectCandidates: (slug: string, role: string): Promise<void> =>
    send('DELETE', `${proj(slug)}/candidates/${enc(role)}`),
  settings: (): Promise<Settings> => get('/api/settings'),
  putSettings: (s: Record<string, string | number>, o?: FetchOptions): Promise<Settings> => send('PUT', '/api/settings', s, o),

  // Efforts
  efforts: (slug: string): Promise<EffortView[]> => get(`${proj(slug)}/efforts`),
  effort: (slug: string, id: number): Promise<EffortView> => get(eff(slug, id)),
  createEffort: (slug: string, title: string, deliberate: boolean, o?: FetchOptions): Promise<EffortView> =>
    send('POST', `${proj(slug)}/efforts`, { title, deliberate }, o),
  importFRD: (slug: string, frdJSON: string, o?: FetchOptions): Promise<EffortView> =>
    send('POST', `${proj(slug)}/efforts/import`, frdJSON, o),
  abandonEffort: (slug: string, id: number): Promise<EffortView> => send('POST', `${eff(slug, id)}/abandon`),
  setYOLO: (slug: string, id: number, on: boolean, confirm: boolean): Promise<EffortView> =>
    send('POST', `${eff(slug, id)}/yolo`, { on, confirm }),
  yoloInvariants: (): Promise<Invariant[]> => get('/api/yolo/invariants'),

  // FRD and plan documents
  frd: <T = unknown>(slug: string, id: number): Promise<DocView<T>> => get(`${eff(slug, id)}/frd`),
  putFRD: <T = unknown>(slug: string, id: number, frd: T, o?: FetchOptions): Promise<DocView<T>> =>
    send('PUT', `${eff(slug, id)}/frd`, frd, o),
  frdRevisions: (slug: string, id: number): Promise<DocView[]> => get(`${eff(slug, id)}/frd/revisions`),
  frdRevision: <T = unknown>(slug: string, id: number, n: number): Promise<DocView<T>> =>
    get(`${eff(slug, id)}/frd/revisions/${n}`),
  approveFRD: (slug: string, id: number, revision?: number): Promise<EffortView> =>
    send('POST', `${eff(slug, id)}/frd/approve`, revision === undefined ? {} : { revision }),
  generatePlan: (slug: string, id: number): Promise<EffortView> => send('POST', `${eff(slug, id)}/plan/generate`),
  plan: <T = unknown>(slug: string, id: number): Promise<DocView<T>> => get(`${eff(slug, id)}/plan`),
  putPlan: <T = unknown>(slug: string, id: number, plan: T, o?: FetchOptions): Promise<DocView<T>> =>
    send('PUT', `${eff(slug, id)}/plan`, plan, o),
  planRounds: (slug: string, id: number): Promise<PlanRounds> => get(`${eff(slug, id)}/plan/rounds`),
  planRevisions: (slug: string, id: number): Promise<DocView[]> => get(`${eff(slug, id)}/plan/revisions`),
  planRevision: <T = unknown>(slug: string, id: number, n: number): Promise<DocView<T>> =>
    get(`${eff(slug, id)}/plan/revisions/${n}`),
  approvePlan: (slug: string, id: number): Promise<EffortView> => send('POST', `${eff(slug, id)}/plan/approve`),

  // Queue and tasks
  compile: (slug: string, id: number): Promise<EffortView> => send('POST', `${eff(slug, id)}/compile`),
  queue: (slug: string, id: number): Promise<QueueView> => get(`${eff(slug, id)}/queue`),
  approveQueue: (slug: string, id: number, hash: string): Promise<QueueView> =>
    send('POST', `${eff(slug, id)}/queue/approve`, { hash }),
  tasks: (slug: string, id: number): Promise<TaskView[]> => get(`${eff(slug, id)}/tasks`),
  task: (slug: string, id: number, key: string): Promise<TaskDetail> => get(`${eff(slug, id)}/tasks/${enc(key)}`),
  editTask: (slug: string, id: number, key: string, def: Partial<TaskDef>, o?: FetchOptions): Promise<TaskDetail> =>
    send('PUT', `${eff(slug, id)}/tasks/${enc(key)}`, def, o),
  retryTask: (slug: string, id: number, key: string, clean: boolean): Promise<TaskDetail> =>
    send('POST', `${eff(slug, id)}/tasks/${enc(key)}/retry`, { clean }),
  overrideTask: (slug: string, id: number, key: string, reason: string): Promise<TaskDetail> =>
    send('POST', `${eff(slug, id)}/tasks/${enc(key)}/override`, { reason }),

  // Run control: answers with the project's overview
  run: (slug: string, op: RunOp): Promise<ProjectView> => send('POST', `${proj(slug)}/run/${op}`),
};

// ─── Non-JSON URLs the views link to or stream from ─────────────────────────

/** exportURL is the download link for an effort export (json or md). */
export function exportURL(effort: number, format: 'json' | 'md', doc?: 'frd' | 'plan', revision?: number): string {
  const base = doc ? `/api/efforts/${effort}/${doc}/export` : `/api/efforts/${effort}/export`;
  return `${base}?format=${format}${revision ? `&revision=${revision}` : ''}`;
}

/** stageStreamURL is the SSE proxy for one stage's live output. */
export function stageStreamURL(stage: number): string {
  return `/api/stages/${stage}/stream`;
}
