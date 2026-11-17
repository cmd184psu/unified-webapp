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
  /** Token and stage-time budgets (AT12); 0 or absent means no limit. */
  budget_tokens?: number;
  budget_seconds?: number;
  created: number;
  updated: number;
}

/** An effort's usage against its budgets; state is ok, no_new_tasks (from 90%) or exhausted (100%). */
export interface BudgetView {
  tokens: number;
  seconds: number;
  token_limit: number;
  seconds_limit: number;
  fraction: number;
  state: 'ok' | 'no_new_tasks' | 'exhausted';
}

export interface EffortBudgetInput {
  budget_tokens?: number;
  budget_seconds?: number;
}

export interface EffortPatch extends EffortBudgetInput {
  deliberate?: boolean;
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
  budget?: BudgetView | null;
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
  /** Run the optional cleanup stage after a review pass (AT11). */
  cleanup_enabled?: boolean;
  /** Runner plugins the project opted into, each pinned to an exact version (OA3). */
  plugins?: PluginPin[];
  created: number;
  updated: number;
  run_state: string;
  remembered_run_state: string;
  /** Why the system stopped the run ("budget"), else empty. */
  stop_reason?: string;
  effort: Effort | null;
  progress: Record<string, number>;
  current: WorkItem[];
}

/** Dropped is POST /api/projects/_all_: every project removed, the database copy taken first, and the branches left in the repositories. */
export interface Dropped {
  dropped: string[];
  archive: string;
  branches: string[];
  commands: string[];
}

/** Unregistered is DELETE /api/projects/{slug}: the repository's leftover branches and the commands that delete them. */
export interface Unregistered {
  deleted: string;
  branches: string[];
  commands: string[];
}

export interface PluginPin {
  name: string;
  version: string;
}

export interface ProjectInput {
  slug: string;
  repo_path: string;
  model_scope?: string;
  feedback_cmds?: string[];
  limits?: Record<string, string | number>;
  cleanup_enabled?: boolean;
  plugins?: PluginPin[];
}

/** ProjectPatch: definition_of_done replaces the detected list; null clears the override. */
export type ProjectPatch = Partial<ProjectInput> & { definition_of_done?: string[] | null };

/** DefinitionOfDone is GET .../definition-of-done: what every task must meet, detected or overridden. */
export interface DefinitionOfDone {
  items: string[];
  source: 'detected' | 'project';
  toolchains?: string[];
}

export interface LimitsView {
  limits: Record<string, string>;
  effective: Record<string, string>;
}

/** AttentionItem is one thing that stopped on its own and needs the owner (GET /api/attention). */
export interface AttentionItem {
  project: string;
  effort: number;
  /** the task key; absent for an item about the whole effort */
  task?: string;
  kind: 'stalled' | 'draft_failed' | 'plan_halted' | 'compile_halted' | 'task_halted' | 'task_unassignable';
  message: string;
  /** Unix ms, 0 when not known */
  since: number;
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
  /** Most stages that may run this model at once on its backend; 0 = no limit (PX5). */
  max_concurrent: number;
  /** The largest context window the model supports, as the host reports it; 0 or absent = unknown. */
  context_tokens?: number;
  /** The context window the model is served with; 0 or absent = unknown (the floor setting is assumed). A value sent in is the owner's setting; 0 keeps what the server learned. */
  window_tokens?: number;
  /** Where window_tokens came from: set (the owner), observed (a run was cut off at this size), host. Set by the server. */
  window_source?: string;
  /** 1 strong, 2 capable, 3 light, 0 or absent = not rated (H3). */
  tier?: number;
  /** Whether the host says the model supports tool calls, which every role needs (H10); empty or absent = not known. Set by the server. */
  tools?: 'yes' | 'no' | '';
  too_big?: boolean;
}

/** The last check of one model (H27): ok when a one-word prompt was answered; class says why not. */
/** ModelHold is a model whose work waits for it while it rests, instead of moving to the next candidate. */
export interface ModelHold {
  backend: string;
  model: string;
}

/** ModelRest is a model that hit a rate limit or stalled; it takes no new stage until `until` (unix ms). */
export interface ModelRest {
  backend: string;
  model: string;
  until: number;
  reason: string;
  streak: number;
  since: number;
}

export interface ModelCheck {
  Backend: string;
  Model: string;
  OK: boolean;
  Class: string;
  Message: string;
  Millis: number;
  Checked: number;
}

/** One model's score in one layer of the reviewer check (H32). */
export interface EvalSummary {
  model: string;
  layer: string;
  caught: number;
  defects: number;
  false_alarms: number;
  fixed: number;
  unread: number;
  ran: number;
}

export interface EvalRow {
  model: string;
  layer: string;
  case: string;
  variant: string;
  run: number;
  outcome: string;
  detail?: string;
  millis: number;
}

export interface EvalView {
  state: 'idle' | 'running';
  current?: string;
  done: number;
  total: number;
  summary: EvalSummary[];
  rows: EvalRow[];
  cases: Array<{ id: string; title: string }>;
}

export interface CheckView {
  state: 'idle' | 'running';
  current?: string;
  results: ModelCheck[];
}

export interface Backend {
  name: string;
  host: string;
  api_type: string;
  hosted: boolean;
  paid: boolean;
  slots: number;
  /** Memory one loaded model may use on the host, weights and context cache, in MB; 0 = not set (H15d). */
  memory_budget_mb?: number;
  /** Cap on an extended window, in tokens; 0 = none. */
  window_cap_tokens?: number;
  /** Share of the time the backend may be kept busy, 10 to 100; 100 = no cap (H20). */
  duty_percent?: number;
  retired: boolean;
  /** True when an API key is stored; the key itself is never sent to the UI. */
  has_key?: boolean;
  models: BackendModel[];
  down_until?: number;
  /** Set (unix ms) while the backend is suspended because it stopped answering; it returns when it answers (H8). */
  suspended_since?: number;
}

/** GET /api/service/restart: a restart waits for running work to end, then the service starts again (H25). */
export interface RestartView {
  state: 'idle' | 'draining';
  plan?: { requested: string; cancel_running: boolean; paused: string[] };
  open_stages: number;
  pid: number;
  started: number;
}

export interface BackendInput {
  /** Write-only: absent keeps the stored key; the server never returns it. */
  api_key?: string;
  name: string;
  host: string;
  api_type: string;
  hosted: boolean;
  paid: boolean;
  slots: number;
  memory_budget_mb: number;
  window_cap_tokens: number;
  duty_percent?: number;
  models: BackendModel[];
}

/** One model's outcome from POST /api/backends/{name}/extend-windows (H15d). */
export interface ExtendResult {
  model: string;
  previous: number;
  window: number;
  applied: boolean;
  note: string;
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

/** GlobalSettings is GET /api/settings: the limits (numbers or Go durations), opencode_path, verify_slots, backend_cooldown. */
export type Settings = Record<string, string | number | string[]>;

/** ModelStat is one row of GET /api/stats/models (MB8); pass_rate is 0..1 over passed + failed stages. */
export interface ModelStat {
  model: string;
  backend: string;
  role: string;
  stages: number;
  passed: number;
  failed: number;
  infra: number;
  pass_rate: number;
  attempts_total: number;
  attempts_passed: number;
  /** Time the model's stages ran: total and mean per stage, in ms. */
  total_ms: number;
  avg_ms: number;
  tokens: number;
  /** The owner's rating (0 = not rated) and the family derived from the name. */
  tier: number;
  family: string;
  /** The model's current streak of strikes in this role (H7): since its last success or clear. Benched at 3. */
  strikes: number;
  /** Every strike it has ever had. */
  strikes_total: number;
  benched: boolean;
}

/** Install is one pull of a model onto an Ollama backend's host (H6); completed and total are bytes of the layer in flight. */
export interface Install {
  id: number;
  backend: string;
  model: string;
  tier: number;
  state: 'running' | 'done' | 'failed' | 'cancelled';
  status: string;
  completed: number;
  total: number;
  error?: string;
  started: number;
  finished?: number;
}

/** ImportReport is what POST /api/config/import says it did (or, without apply, would do) (H13). */
export interface ImportReport {
  applied: boolean;
  created_backends: string[];
  updated_backends: string[];
  candidate_roles: string[];
  settings: string[];
  warnings: string[];
}

/** Strike is one entry of the strike history (GET /api/strikes); counted is whether it is in the current streak. */
export interface Strike {
  backend: string;
  model: string;
  role: string;
  kind: 'unparseable' | 'violation' | 'repeat';
  task?: number;
  effort?: number;
  project?: string;
  at: number;
  counted: boolean;
  /** Why it stopped counting: a later success, or the owner's Clear. */
  ended?: 'success' | 'cleared';
}

export interface StrikeDay {
  day: string;
  backend: string;
  model: string;
  role: string;
  kind: string;
  count: number;
}

export interface StrikeHistory {
  events: Strike[];
  daily: StrikeDay[];
  total: number;
}

/** DraftState is GET .../frd/draft: the FRD drafting stage (draft or polish). */
export interface DraftState {
  state: 'idle' | 'running' | 'failed';
  /** Which stage ran or is running; absent on older servers (read as draft). */
  purpose?: 'draft' | 'polish' | 'split';
  /** Start time in ms. */
  started?: number;
  error?: string;
}

/** SizeView is GET .../size: the FRD against the smallest planning context. budget_tokens 0 or over missing = contexts unknown. */
export interface SizeView {
  frd_tokens: number;
  plan_tokens?: number;
  budget_tokens: number;
  context_tokens_min?: number;
  over?: boolean;
  models?: Array<{ backend: string; model: string; context_tokens: number }>;
  advice?: string;
  /** Models whose context window is not known, so the budget ignores them. */
  unknown_models?: Array<{ backend: string; model: string }>;
}

/** FrdPart is one row of GET .../frd/parts after a split; started_effort 0 = not started. */
export interface FrdPart {
  n: number;
  title: string;
  summary?: string;
  tokens: number;
  started_effort: number;
}

export interface DocView<T = unknown> {
  revision: number;
  created_by: string;
  created: number;
  approved: boolean;
  json?: T;
}

// ─── FRD (mirrors model.FRD; state_model, findings and planning_spikes ride along untouched) ───

export interface FRDGroup { id: string; title: string; summary?: string }
export interface FRDRequirement { id: string; group?: string; priority?: string; findings?: unknown; description: string }
export interface FRDAC { id: string; requirements?: string[]; criterion: string }
export interface FRDDecision { id: string; decision: string; rationale?: string }

export interface FRD {
  id: string;
  title: string;
  status?: string;
  created?: string;
  approved_date?: string;
  objective: string;
  context?: string;
  constraints?: string[];
  groups?: FRDGroup[];
  state_model?: unknown;
  requirements: FRDRequirement[];
  acceptance_criteria: FRDAC[];
  decisions?: FRDDecision[];
  findings?: unknown;
  out_of_scope?: string[];
  planning_spikes?: unknown;
}

// ─── Plan (mirrors schema.PlanOut and engine.PlanView) ───

export interface PlanIssue { description: string; severity: string; file?: string; line?: number }

/** PlanReview is one architect or critic review. */
export interface PlanReview {
  revision: number;
  role: string;
  round: number;
  plan_revision: number;
  stage: number;
  verdict: string;
  summary?: string;
  issues: PlanIssue[];
}

export interface PlanRound {
  round: number;
  reviews: PlanReview[];
}

export interface PlanDoc {
  principles: string[];
  drivers: string[];
  options: Array<{ name: string; description: string; pros?: string[]; cons?: string[] }>;
  phases: Array<{ id: string; name: string; tasks_description: string; depends_on?: string[] }>;
  adr: { decision: string; drivers: string[]; alternatives: string[]; why: string; consequences: string[]; followups: string[] };
  premortem?: string[];
  test_plan?: string[];
  round?: number;
  stage?: number;
}

/** PlanRounds is GET .../plan/rounds: the reviews grouped by round. */
export interface PlanRounds {
  rounds: PlanRound[];
  revision: number;
  not_critic_approved: boolean;
  warnings: string[] | null;
  waiting?: string;
}

/** PlanView is GET .../plan: the presented plan revision and the planning state. */
export interface PlanView {
  effort: number;
  status: string;
  rounds: number;
  revision: number;
  plan?: PlanDoc;
  not_critic_approved: boolean;
  reviews: PlanReview[];
  warnings: string[] | null;
  waiting?: string;
}

/** GET .../git-commands: what to run to merge a complete effort; commits, diffstat and files are optional extras. */
export interface GitCommands {
  commands: string[];
  branch?: string;
  commits?: Array<{ sha: string; subject: string }>;
  diffstat?: string;
  files?: number;
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
  review_independence?: string;
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

/** FsDirs is GET /api/fs/dirs: the subdirectories of one directory under the browse roots. */
export interface FsDirs {
  path: string;
  /** Empty when path is a browse root (nothing above it may be shown). */
  parent: string;
  dirs: Array<{ name: string; path: string; git: boolean }>;
}

// ─── Typed endpoints ────────────────────────────────────────────────────────

export const api = {
  // Health and opencode
  health: (o?: FetchOptions): Promise<Health> => get('/api/health', o),
  attention: (o?: FetchOptions): Promise<AttentionItem[]> => get('/api/attention', o),
  opencodeVersion: (refresh = false): Promise<OpenCodeStatus> =>
    get(`/api/opencode/version${refresh ? '?refresh=1' : ''}`),
  restartStatus: (o?: FetchOptions): Promise<RestartView> => get('/api/service/restart', o),
  restartService: (cancelRunning: boolean): Promise<RestartView> => send('POST', '/api/service/restart', { cancel_running: cancelRunning }),
  cancelRestart: (): Promise<RestartView> => send('DELETE', '/api/service/restart'),

  fsDirs: (path = ''): Promise<FsDirs> => get(`/api/fs/dirs${path ? `?path=${encodeURIComponent(path)}` : ''}`, { quiet: true }),

  // Projects
  projects: (): Promise<ProjectView[]> => get('/api/projects'),
  project: (slug: string): Promise<ProjectView> => get(proj(slug)),
  createProject: (p: ProjectInput, o?: FetchOptions): Promise<ProjectView> => send('POST', '/api/projects', p, o),
  gitInit: (repoPath: string): Promise<{ repo_path: string }> =>
    send('POST', '/api/projects/git-init', { repo_path: repoPath, confirm: true }),
  patchProject: (slug: string, p: ProjectPatch, o?: FetchOptions): Promise<ProjectView> =>
    send('PATCH', proj(slug), p, o),
  deleteProject: (slug: string): Promise<Unregistered> => send('DELETE', proj(slug)),
  dropAllProjects: (): Promise<Dropped> => send('POST', '/api/projects/_all_', { confirm: true }),
  definitionOfDone: (slug: string, o?: FetchOptions): Promise<DefinitionOfDone> => get(`${proj(slug)}/definition-of-done`, o),
  limits: (slug: string): Promise<LimitsView> => get(`${proj(slug)}/limits`),
  putLimits: (slug: string, limits: Record<string, string | number>, o?: FetchOptions): Promise<LimitsView> =>
    send('PUT', `${proj(slug)}/limits`, { limits }, o),

  // Backends, candidates, settings
  backends: (): Promise<Backend[]> => get('/api/backends'),
  createBackend: (b: BackendInput, o?: FetchOptions): Promise<BackendResult> => send('POST', '/api/backends', b, o),
  probeBackend: (host: string, apiType: string, key = '', backend = ''): Promise<{ models: string[]; contexts?: Record<string, number>; tools?: Record<string, string> }> =>
    send('POST', '/api/backends/probe', { host, api_type: apiType, api_key: key, backend }, { quiet: true }),
  patchBackend: (name: string, b: Partial<BackendInput>, o?: FetchOptions): Promise<BackendResult> =>
    send('PATCH', `/api/backends/${enc(name)}`, b, o),
  retireBackend: (name: string, cancelRunning: boolean): Promise<BackendResult> =>
    send('POST', `/api/backends/${enc(name)}/retire`, { cancel_running: cancelRunning }),
  extendWindows: (name: string, apply = true): Promise<ExtendResult[]> => send('POST', `/api/backends/${enc(name)}/extend-windows`, { apply }),
  enableBackend: (name: string): Promise<BackendResult> => send('POST', `/api/backends/${enc(name)}/enable`),
  deleteBackend: (name: string): Promise<void> => send('DELETE', `/api/backends/${enc(name)}`),
  rests: (): Promise<ModelRest[]> => get('/api/rests', { quiet: true }),
  wakeModel: (backend: string, model: string): Promise<void> => send('DELETE', `/api/rests?backend=${enc(backend)}&model=${enc(model)}`),
  holds: (): Promise<ModelHold[]> => get('/api/holds', { quiet: true }),
  setHold: (backend: string, model: string, hold: boolean): Promise<void> => send('PUT', `/api/holds?backend=${enc(backend)}&model=${enc(model)}&hold=${hold}`),
  checkModels: (name: string): Promise<CheckView> => send('POST', `/api/backends/${enc(name)}/check`),
  checkStatus: (name: string): Promise<CheckView> => get(`/api/backends/${enc(name)}/check`, { quiet: true }),
  checkReviewers: (name: string, body: { layers?: string[]; repeats?: number } = {}): Promise<EvalView> => send('POST', `/api/backends/${enc(name)}/evals`, body),
  evalStatus: (name: string): Promise<EvalView> => get(`/api/backends/${enc(name)}/evals`, { quiet: true }),
  candidates: (): Promise<Candidate[]> => get('/api/candidates'),
  putCandidates: (role: string, candidates: CandidateInput[], o?: FetchOptions): Promise<Candidate[]> =>
    send('PUT', `/api/candidates/${enc(role)}`, { candidates }, o),
  putProjectCandidates: (slug: string, role: string, candidates: CandidateInput[], o?: FetchOptions): Promise<Candidate[]> =>
    send('PUT', `${proj(slug)}/candidates/${enc(role)}`, { candidates }, o),
  clearProjectCandidates: (slug: string, role: string): Promise<void> =>
    send('DELETE', `${proj(slug)}/candidates/${enc(role)}`),
  settings: (): Promise<Settings> => get('/api/settings'),
  putSettings: (s: Settings, o?: FetchOptions): Promise<Settings> => send('PUT', '/api/settings', s, o),

  modelStats: (project = ''): Promise<ModelStat[]> => get(`/api/stats/models${project ? `?project=${enc(project)}` : ''}`),
  strikeHistory: (project = '', limit = 50): Promise<StrikeHistory> =>
    get(`/api/strikes?limit=${limit}${project ? `&project=${enc(project)}` : ''}`),
  installs: (backend: string): Promise<Install[]> => get(`/api/backends/${enc(backend)}/installs`),
  startInstall: (backend: string, model: string, tier: number): Promise<Install> =>
    send('POST', `/api/backends/${enc(backend)}/install`, { model, tier }),
  cancelInstall: (id: number): Promise<void> => send('POST', `/api/installs/${id}/cancel`),
  importConfig: (config: unknown, apply: boolean): Promise<ImportReport> => send('POST', '/api/config/import', { config, apply }),
  clearStrikes: (backend: string, model: string, role: string): Promise<void> => send('POST', '/api/strikes/clear', { backend, model, role }),

  // Efforts
  efforts: (slug: string): Promise<EffortView[]> => get(`${proj(slug)}/efforts`),
  effort: (slug: string, id: number): Promise<EffortView> => get(eff(slug, id)),
  createEffort: (slug: string, title: string, deliberate: boolean, o?: FetchOptions, budget?: EffortBudgetInput): Promise<EffortView> =>
    send('POST', `${proj(slug)}/efforts`, { title, deliberate, ...budget }, o),
  patchEffort: (slug: string, id: number, patch: EffortPatch, o?: FetchOptions): Promise<EffortView> =>
    send('PATCH', eff(slug, id), patch, o),
  importFRD: (slug: string, frdJSON: string, o?: FetchOptions): Promise<EffortView> =>
    send('POST', `${proj(slug)}/efforts/import`, frdJSON, o),
  abandonEffort: (slug: string, id: number): Promise<EffortView> => send('POST', `${eff(slug, id)}/abandon`),
  setYOLO: (slug: string, id: number, on: boolean, confirm: boolean): Promise<EffortView> =>
    send('POST', `${eff(slug, id)}/yolo`, { on, confirm }),
  yoloInvariants: (): Promise<Invariant[]> => get('/api/yolo/invariants'),

  // FRD and plan documents
  frd: (slug: string, id: number, o?: FetchOptions): Promise<DocView<FRD>> => get(`${eff(slug, id)}/frd`, o),
  putFRD: (slug: string, id: number, frd: FRD, o?: FetchOptions): Promise<DocView<FRD>> =>
    send('PUT', `${eff(slug, id)}/frd`, frd, o),
  frdRevisions: (slug: string, id: number): Promise<DocView[]> => get(`${eff(slug, id)}/frd/revisions`),
  frdRevision: (slug: string, id: number, n: number): Promise<DocView<FRD>> =>
    get(`${eff(slug, id)}/frd/revisions/${n}`),
  approveFRD: (slug: string, id: number, revision?: number): Promise<EffortView> =>
    send('POST', `${eff(slug, id)}/frd/approve`, revision === undefined ? {} : { revision }),
  draftFRD: (slug: string, id: number, description: string, polish: boolean, o?: FetchOptions): Promise<DraftState> =>
    send('POST', `${eff(slug, id)}/frd/draft`, { description, polish }, o),
  draftState: (slug: string, id: number, o?: FetchOptions): Promise<DraftState> => get(`${eff(slug, id)}/frd/draft`, o),
  frdSize: (slug: string, id: number, o?: FetchOptions): Promise<SizeView> => get(`${eff(slug, id)}/size`, o),
  splitFRD: (slug: string, id: number, o?: FetchOptions): Promise<unknown> => send('POST', `${eff(slug, id)}/frd/split`, {}, o),
  frdParts: (slug: string, id: number, o?: FetchOptions): Promise<FrdPart[]> => get(`${eff(slug, id)}/frd/parts`, o),
  startPart: (slug: string, id: number, n: number, o?: FetchOptions): Promise<EffortView> =>
    send('POST', `${eff(slug, id)}/frd/parts/${n}/start`, undefined, o),
  generatePlan: (slug: string, id: number): Promise<EffortView> => send('POST', `${eff(slug, id)}/plan/generate`),
  plan: (slug: string, id: number, o?: FetchOptions): Promise<PlanView> => get(`${eff(slug, id)}/plan`, o),
  putPlan: (slug: string, id: number, plan: PlanDoc, o?: FetchOptions): Promise<DocView<PlanDoc>> =>
    send('PUT', `${eff(slug, id)}/plan`, plan, o),
  planRounds: (slug: string, id: number): Promise<PlanRounds> => get(`${eff(slug, id)}/plan/rounds`),
  planRevisions: (slug: string, id: number): Promise<DocView[]> => get(`${eff(slug, id)}/plan/revisions`),
  planRevision: (slug: string, id: number, n: number): Promise<DocView<PlanDoc>> =>
    get(`${eff(slug, id)}/plan/revisions/${n}`),
  approvePlan: (slug: string, id: number): Promise<EffortView> => send('POST', `${eff(slug, id)}/plan/approve`),

  // Queue and tasks
  compile: (slug: string, id: number): Promise<EffortView> => send('POST', `${eff(slug, id)}/compile`),
  queue: (slug: string, id: number, o?: FetchOptions): Promise<QueueView> => get(`${eff(slug, id)}/queue`, o),
  /** gitCommands lists the commands to merge and push a complete effort's branch; agentflow runs none of them (GIT4). */
  gitCommands: (slug: string, id: number, o?: FetchOptions): Promise<GitCommands> => get(`${eff(slug, id)}/git-commands`, o),
  approveQueue: (slug: string, id: number, hash: string, o?: FetchOptions): Promise<QueueView> =>
    send('POST', `${eff(slug, id)}/queue/approve`, { hash }, o),
  tasks: (slug: string, id: number): Promise<TaskView[]> => get(`${eff(slug, id)}/tasks`),
  task: (slug: string, id: number, key: string): Promise<TaskDetail> => get(`${eff(slug, id)}/tasks/${enc(key)}`),
  /** editTask answers with the queue. A queue that was approved stays approved (the edit is the review) and a run goes on; one still in draft stays in draft. */
  editTask: (slug: string, id: number, key: string, def: TaskDef, o?: FetchOptions): Promise<QueueView> =>
    send('PUT', `${eff(slug, id)}/tasks/${enc(key)}?approve=true`, def, o),
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
