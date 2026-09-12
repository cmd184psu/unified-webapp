import type {
  AppConfig,
  Cert,
  CertListResponse,
  CertMutationResponse,
  ImportReport,
} from "./types";

/** Config values used if `/api/config` cannot be reached, matching the server's own defaults. */
const FALLBACK_CONFIG: AppConfig = {
  defaultValidityDays: 365,
  expiryWarnDays: 30,
  certCount: 0,
  legacyImportAvailable: false,
  legacyImportDir: "",
  legacyImportReason: "",
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
 */
export async function deleteCert(id: number, confirmFqdn: string): Promise<void> {
  const res = await fetch(`/api/certs/${id}?confirm=${encodeURIComponent(confirmFqdn)}`, {
    method: "DELETE",
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(await errorMessage(res, "failed to delete certificate"));
  }
}

/** GET/POST /api/ca response shape. Every field but `exists` is absent when no CA exists. */
export interface CAStatus {
  exists: boolean;
  subject?: string;
  serial?: string;
  notBefore?: string;
  notAfter?: string;
  fingerprint?: string;
  importedFrom?: string;
}

const FALLBACK_CA_STATUS: CAStatus = { exists: false };

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
export async function initCA(): Promise<CAStatus> {
  const res = await fetch("/api/ca/init", { method: "POST" });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to initialize the certificate authority"));
  }
  return (await res.json()) as CAStatus;
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
