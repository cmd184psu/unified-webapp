import type {
  AppConfig,
  Cert,
  CertListResponse,
  CertMutationResponse,
  ImportReport,
  ReplaceProgressEvent,
  ReplaceResult,
} from "./types";
import { parseNDJSONChunk, parseNDJSONFinal } from "./ndjson";

/** Config values used if `/api/config` cannot be reached, matching the server's own defaults. */
const FALLBACK_CONFIG: AppConfig = {
  defaultValidityDays: 365,
  expiryWarnDays: 30,
  certCount: 0,
  legacyImportAvailable: false,
  legacyImportDir: "",
  legacyImportReason: "",
  trustDeviceAvailable: false,
  trustRemoteAvailable: false,
};

/**
 * Fetch the server's runtime configuration.
 *
 * Never rejects: badges need `expiryWarnDays` to render at all, so a
 * transient boot failure falls back to the server's own default rather than
 * blanking the whole list.
 */
export async function fetchConfig(): Promise<AppConfig> {
  try {
    const res = await fetch("/api/config");
    if (!res.ok) throw new Error(`config fetch failed: ${res.status}`);
    return (await res.json()) as AppConfig;
  } catch (err) {
    console.warn("certmachine: falling back to default config:", err);
    return FALLBACK_CONFIG;
  }
}

/** Extract the server's `{"error": "..."}` message from a failed response, or a generic fallback. */
async function errorMessage(res: Response, fallback: string): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    if (body.error) return body.error;
  } catch {
    /* body wasn't JSON -- fall through */
  }
  return `${fallback}: ${res.status}`;
}

/**
 * Fetch every cert row. Unlike `fetchConfig`, this rejects on failure -- the
 * cert list is the entire point of this screen, so a fetch failure is
 * something the caller must show the operator, not paper over with an empty
 * list that looks like "no certs exist yet".
 */
export async function fetchCerts(): Promise<Cert[]> {
  const res = await fetch("/api/certs");
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to load certificates"));
  }
  const body = (await res.json()) as CertListResponse;
  return body.certs ?? [];
}

/** Fetch the full detail of one cert row, including `certPem` (used by the detail view's copy-PEM action). */
export async function fetchCertDetail(id: number): Promise<Cert> {
  const res = await fetch(`/api/certs/${id}`);
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to load certificate"));
  }
  return (await res.json()) as Cert;
}

/** Request body for `POST /api/certs`. `dnsSans`/`ipSans` may be empty. */
export interface GenerateInput {
  fqdn: string;
  dnsSans: string[];
  ipSans: string[];
}

/**
 * Generate a new leaf certificate. The server's own error text (bad FQDN,
 * malformed SAN, duplicate active FQDN, CA missing/expiring) is what
 * `err.message` carries on rejection -- callers must display it verbatim,
 * per FR-9 ("no alert()-and-hope").
 */
export async function generateCert(input: GenerateInput): Promise<CertMutationResponse> {
  const res = await fetch("/api/certs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to generate certificate"));
  }
  return (await res.json()) as CertMutationResponse;
}

/**
 * Renew an existing cert row by id. Rejects with the server's own message on
 * failure -- e.g. a quarantined row, or an archived row with a newer active
 * successor -- rather than a client-invented explanation.
 */
export async function renewCert(id: number): Promise<CertMutationResponse> {
  const res = await fetch(`/api/certs/${id}/renew`, { method: "POST" });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to renew certificate"));
  }
  return (await res.json()) as CertMutationResponse;
}

/**
 * Delete a cert row. `confirmFqdn` is sent as the `confirm` query param; the
 * server independently re-checks it case-insensitively against the row's
 * real FQDN, so a stale/mismatched value fails server-side even if the UI's
 * own gating (see `detail.ts`) is somehow bypassed.
 *
 * The server answers 200 with `{previousDropped}` (CA-replacement plan §4:
 * delete used to be a bare 204, but deleting a row can itself trigger the
 * auto-drop of a previous CA that no longer signs anything active).
 */
export async function deleteCert(id: number, confirmFqdn: string): Promise<{ previousDropped: boolean }> {
  const res = await fetch(`/api/certs/${id}?confirm=${encodeURIComponent(confirmFqdn)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to delete certificate"));
  }
  return (await res.json()) as { previousDropped: boolean };
}

/** Request body for `POST /api/certs/{id}/edit` (CA-replacement plan P5). */
export interface EditInput {
  fqdn: string;
  dnsSans: string[];
  ipSans: string[];
  validityDays: number;
}

/**
 * Edit an existing cert row: re-issue it (possibly under a new FQDN and/or a
 * requested validity) rather than just renewing with the default validity.
 * Rejects with the server's own message -- e.g. `ErrQuarantined`,
 * `ErrDuplicateActive` on an FQDN collision, or `ErrNewerActiveExists`.
 */
export async function editCert(id: number, input: EditInput): Promise<CertMutationResponse> {
  const res = await fetch(`/api/certs/${id}/edit`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to edit certificate"));
  }
  return (await res.json()) as CertMutationResponse;
}

/**
 * The previous CA's identity plus how many active rows still depend on it
 * (CA-replacement plan FR-R7/D7). `activeCount` drives both the panel's
 * display and the forced `previousStale` choice in the Replace dialog.
 */
export interface PreviousCA {
  id: number;
  subject: string;
  notBefore: string;
  notAfter: string;
  fingerprint: string;
  activeCount: number;
}

/**
 * GET/POST /api/ca response shape. Every field but `exists` (and, once a CA
 * exists, `unknownSignerActiveCount`) is absent when no CA exists.
 * `previous` is present only when there is a previous CA; `id` and
 * `unknownSignerActiveCount` are the CA-replacement plan's additions.
 */
export interface CAStatus {
  exists: boolean;
  id?: number;
  subject?: string;
  serial?: string;
  notBefore?: string;
  notAfter?: string;
  fingerprint?: string;
  importedFrom?: string;
  previous?: PreviousCA;
  /** Always present, even 0 and even when no CA exists yet -- the server never omits it. */
  unknownSignerActiveCount: number;
}

const FALLBACK_CA_STATUS: CAStatus = { exists: false, unknownSignerActiveCount: 0 };

/**
 * Fetch CA status. Never rejects -- like `fetchConfig`, a transient boot
 * hiccup should degrade to "no CA" (which only hides the generate action and
 * shows the init/import panel) rather than blanking the whole cert list,
 * which does not depend on CA status to render.
 */
export async function fetchCA(): Promise<CAStatus> {
  try {
    const res = await fetch("/api/ca");
    if (!res.ok) throw new Error(`CA status fetch failed: ${res.status}`);
    return (await res.json()) as CAStatus;
  } catch (err) {
    console.warn("certmachine: falling back to 'no CA' status:", err);
    return FALLBACK_CA_STATUS;
  }
}

/** Initialize a new root CA. Rejects with the server's message (e.g. `ErrImportPending`) on failure. */
/** Create the root CA, optionally named (its Common Name; blank = the default). */
export async function initCA(name = ""): Promise<CAStatus> {
  const res = await fetch("/api/ca/init", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to initialize the certificate authority"));
  }
  return (await res.json()) as CAStatus;
}

/** Request body for `POST /api/ca/replace` (CA-replacement plan §4, D1/D2/D7). */
export interface ReplaceCAInput {
  name: string;
  existing: "reissue" | "delete" | "keep";
  /** Required only when the outgoing previous CA still signs an active row (409 `ErrPreviousStaleChoiceRequired` otherwise). */
  previousStale?: "reissue" | "delete";
}

/**
 * Replace the current CA with a newly generated one. Rejects with the
 * server's own message on failure -- a name collision (400), a required but
 * omitted `previousStale` choice (409), or a concurrent-change conflict (409).
 */
export async function replaceCA(input: ReplaceCAInput): Promise<ReplaceResult> {
  const res = await fetch("/api/ca/replace", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to replace the certificate authority"));
  }
  return (await res.json()) as ReplaceResult;
}

/** One line of POST /api/ca/replace's NDJSON stream (handler.go's ndjson*Line types). */
interface ndjsonLine {
  type: "progress" | "result" | "error";
  phase?: string;
  done?: number;
  total?: number;
  status?: number;
  error?: string;
  ca?: unknown;
  reissued?: number;
  deleted?: number;
  kept?: number;
  clamped?: number;
  previousDropped?: boolean;
}

function isReplaceProgressPhase(phase: string | undefined): phase is ReplaceProgressEvent["phase"] {
  return phase === "ca-key" || phase === "leaf-keys" || phase === "saving";
}

/**
 * Replace the current CA with a newly generated one, the same as `replaceCA`,
 * but over the streaming NDJSON progress mode (`Accept: application/x-ndjson`)
 * instead of a single JSON response: `onProgress` is called once per event as
 * the server reports it (a new CA key, each re-issued leaf's key, then the
 * short transaction that saves everything). Rejects with the same,
 * client-safe message either path can produce -- a non-OK status before any
 * streaming started, or the stream's own final `"error"` line once it did.
 *
 * A browser without streaming `Response.body` support (or a test
 * environment) falls back to decoding the whole response as JSON, since a
 * non-streaming client still receives the exact same bytes, just without
 * incremental delivery -- `onProgress` simply never fires in that case.
 */
export async function replaceCAWithProgress(
  input: ReplaceCAInput,
  onProgress: (ev: ReplaceProgressEvent) => void,
): Promise<ReplaceResult> {
  const res = await fetch("/api/ca/replace", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/x-ndjson" },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to replace the certificate authority"));
  }
  let result: ReplaceResult | null = null;

  const handleLine = (line: ndjsonLine): void => {
    if (line.type === "progress") {
      if (isReplaceProgressPhase(line.phase)) {
        onProgress({ phase: line.phase, done: line.done, total: line.total });
      }
      return;
    }
    if (line.type === "error") {
      throw new Error(line.error || "failed to replace the certificate authority");
    }
    // "result": every field but "type" is ReplaceResult's own shape.
    const { type: _type, ...rest } = line;
    result = rest as ReplaceResult;
  };

  if (res.body) {
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let remainder = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      const chunk = parseNDJSONChunk<ndjsonLine>(remainder, decoder.decode(value, { stream: true }));
      remainder = chunk.remainder;
      for (const line of chunk.values) handleLine(line);
    }
    const finalLine = parseNDJSONFinal<ndjsonLine>(remainder);
    if (finalLine) handleLine(finalLine);
  } else {
    // No streaming `Response.body` support: the whole NDJSON body arrives as
    // one string. It is still multiple `\n`-separated lines, so it goes
    // through the exact same per-line handling as the streaming path above --
    // NOT res.json(), which would throw a SyntaxError on anything but a
    // single-line body.
    const text = await res.text();
    const chunk = parseNDJSONChunk<ndjsonLine>("", text);
    for (const line of chunk.values) handleLine(line);
    const finalLine = parseNDJSONFinal<ndjsonLine>(chunk.remainder);
    if (finalLine) handleLine(finalLine);
  }

  if (result === null) {
    throw new Error("failed to replace the certificate authority: the response stream ended with no result");
  }
  return result;
}

/**
 * Swap the previous CA back to being current (CA-replacement plan D9).
 * Rejects with the server's own message -- e.g. `ErrNoPreviousCA` or an
 * expiring/expired previous CA (both 409).
 */
export async function switchBackCA(): Promise<{ ca: CAStatus }> {
  const res = await fetch("/api/ca/switch-back", { method: "POST" });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to switch back to the previous certificate authority"));
  }
  return (await res.json()) as { ca: CAStatus };
}

/** Step 1 of the import wizard: a dry-run scan of the legacy directory. Writes nothing. */
export async function fetchImportPreview(): Promise<ImportReport> {
  const res = await fetch("/api/import/preview");
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to preview the legacy import"));
  }
  return (await res.json()) as ImportReport;
}

/**
 * Rejection type for `runImport`. A failed import is the one case where the
 * server sends more than an error envelope: its body carries the partial
 * `ImportReport` the scan reached before the write failed, and that report is
 * the only thing naming *which* legacy directory was the problem. A plain
 * `Error` would drop it, leaving the operator a single sentence to go on
 * against a tree that may hold hundreds of directories.
 */
export class ImportFailedError extends Error {
  /** The partial classification, or `null` when the import failed before classifying anything. */
  readonly report: ImportReport | null;

  constructor(message: string, report: ImportReport | null) {
    super(message);
    this.name = "ImportFailedError";
    this.report = report;
  }
}

/**
 * Step 2 of the import wizard: actually import. `confirmNonEmpty` must be
 * true when the cert list is already non-empty (the server otherwise
 * refuses with `ErrImportConfirmRequired`) -- the wizard never sends a
 * manifest of what to import, only this one confirmation flag, so preview
 * and execute can never disagree about which files exist.
 *
 * Rejects with `ImportFailedError`, carrying the server's own message and its
 * partial report when the body has one.
 */
export async function runImport(confirmNonEmpty: boolean): Promise<ImportReport> {
  const res = await fetch("/api/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(confirmNonEmpty ? { confirmNonEmpty: true } : {}),
  });
  if (!res.ok) {
    let message = `import failed: ${res.status}`;
    let report: ImportReport | null = null;
    try {
      const body = (await res.json()) as { error?: string; report?: ImportReport };
      if (body.error) message = body.error;
      if (body.report) report = body.report;
    } catch {
      /* body wasn't JSON -- keep the status-based message */
    }
    throw new ImportFailedError(message, report);
  }
  return (await res.json()) as ImportReport;
}

/** POST /api/ca/trust's response body. */
export interface TrustResult {
  platform?: string;
  output?: string;
}

/**
 * Rejection type for `trustDevice`. `output` carries the ran commands'
 * combined stdout+stderr whenever any command was actually attempted (e.g. a
 * `sudo -n` permission refusal, or `update-ca-certificates` failing) -- a
 * bare error message would leave the operator guessing which of the (up to
 * two) commands failed and why.
 */
export class TrustFailedError extends Error {
  readonly output: string;

  constructor(message: string, output: string) {
    super(message);
    this.name = "TrustFailedError";
    this.output = output;
  }
}

/**
 * Run this host's native "trust this root CA system-wide" procedure via
 * sudo (server-side; see `internal/certmachine/trust.go`). Only meaningful
 * when `AppConfig.trustDeviceAvailable` is true -- the server refuses with
 * 409 otherwise, surfaced here the same way any other disabled-feature
 * refusal is. Rejects with `TrustFailedError` on failure.
 */
/** Inputs for POST /api/ca/trust/remote: a key name OR a password, not both. */
export interface RemoteTrustRequest {
  host: string;
  port: number;
  user: string;
  key?: string;
  password?: string;
}

/**
 * Trust the root CA on another machine over SSH. The server detects the OS
 * and installs only on macOS, Windows, Rocky/RHEL or Ubuntu/Debian; anything
 * else installs nothing. Rejects with `TrustFailedError` (carrying the
 * commands' output, when any ran) on failure.
 */
export async function trustRemote(req: RemoteTrustRequest): Promise<TrustResult> {
  const res = await fetch("/api/ca/trust/remote", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  let body: TrustResult & { error?: string } = {};
  try {
    body = (await res.json()) as TrustResult & { error?: string };
  } catch {
    /* non-JSON error body */
  }
  if (!res.ok) {
    throw new TrustFailedError(body.error ?? `remote trust failed: ${res.status}`, body.output ?? "");
  }
  return body;
}

/** GET /api/ssh/keys: the server's SSH key folder, names only. */
export async function fetchSSHKeys(): Promise<Array<{ name: string; isDir: boolean }>> {
  try {
    const res = await fetch("/api/ssh/keys");
    if (!res.ok) return [];
    const body = (await res.json()) as { keys?: Array<{ name: string; isDir: boolean }> };
    return body.keys ?? [];
  } catch {
    return []; // unreachable server: the picker just shows "No keys found"
  }
}

export async function trustDevice(): Promise<TrustResult> {
  const res = await fetch("/api/ca/trust", { method: "POST" });
  let body: TrustResult & { error?: string } = {};
  try {
    body = (await res.json()) as TrustResult & { error?: string };
  } catch {
    /* body wasn't JSON -- keep the status-based message below */
  }
  if (!res.ok) {
    throw new TrustFailedError(body.error ?? `device trust install failed: ${res.status}`, body.output ?? "");
  }
  return body;
}
