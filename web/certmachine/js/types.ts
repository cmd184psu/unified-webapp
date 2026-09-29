/**
 * Wire types for the certmachine API, matching `internal/certmachine`
 * (see the slice 8 handoff for the field-by-field JSON contract). Every
 * field name here is copy-checked against that document, not guessed.
 */

/** Subject alternative names parsed from a certificate. */
export interface SANs {
  dns: string[];
  ip: string[];
}

/**
 * A leaf certificate row from GET /api/certs and GET /api/certs/{id}.
 *
 * `serial`/`notBefore`/`notAfter`/`fingerprint` are `null` (never absent,
 * never `""`) when the stored certificate did not parse -- distinct from the
 * optional fields, which are omitted entirely when not applicable. `keyPem`
 * is never present in any response and has no field here. `status` is the
 * three-value server enum; "expired" is never sent by the server and is
 * derived client-side, in `status.ts`, from `notAfter` -- see that module's
 * doc comment for why this is the *only* place that computation happens.
 */
export interface Cert {
  id: number;
  fqdn: string;
  serial: string | null;
  notBefore: string | null;
  notAfter: string | null;
  sans: SANs;
  fingerprint: string | null;
  status: "active" | "archived" | "quarantined";
  /** Present only on GET /api/certs/{id} and generate/renew responses. */
  certPem?: string;
  /** Absent unless this row was created by the legacy import. */
  importedFrom?: string;
  /** Absent unless `status` is "quarantined". */
  quarantineReason?: string;
  /** Absent unless the cert does not chain to the stored CA. */
  importWarning?: string;
  /**
   * The CA row that signed this cert (`certs.ca_id`, CA-replacement plan
   * P1). `null` means "unknown signer" -- neither stale nor current -- which
   * covers quarantined rows and rows whose stored certificate does not chain
   * to any recorded CA. Always present (never absent), unlike the optional
   * fields above.
   */
  caId: number | null;
  /** The signing CA's subject. Present only where the server joins against `ca` (list/detail). */
  caSubject?: string;
  /** True exactly when `caId` is non-null and differs from the current CA's id. */
  stale: boolean;
  created: string;
}

/** GET /api/certs response envelope. `certPem` is absent on every entry. */
export interface CertListResponse {
  certs: Cert[];
}

/** GET /api/config response. */
export interface AppConfig {
  defaultValidityDays: number;
  expiryWarnDays: number;
  certCount: number;
  legacyImportAvailable: boolean;
  legacyImportDir: string;
  legacyImportReason: string;
  /** True only when `certmachine.trust_device_enabled` is set AND this host's OS was recognized. */
  trustDeviceAvailable: boolean;
  /** The detected platform ("darwin" / "rhel" / "debian"). Absent unless `trustDeviceAvailable` is true. */
  trustPlatform?: string;
  /** True when the CA can be trusted on another machine over SSH. */
  trustRemoteAvailable: boolean;
  /** Why remote trust is unavailable, when it is. */
  trustRemoteReason?: string;
}

/**
 * POST /api/certs and POST /api/certs/{id}/renew share this response shape.
 * Not called from this slice (generate/renew UI lands in slice 10) -- the
 * type is defined now, as instructed, so the `validityClamped` /
 * `requestedNotAfter` contract is settled before that UI is built against it.
 */
export interface CertMutationResponse {
  cert: Cert;
  validityClamped: boolean;
  /** Present only when `validityClamped` is true. */
  requestedNotAfter?: string;
  /**
   * True when this mutation (renew/edit/delete can all trigger it) caused
   * the previous CA to drop -- it no longer signed any active certificate,
   * so it and its archived rows were removed (CA-replacement plan P2).
   * Always present -- the server never omits it (`issueResponse`, handler.go).
   */
  previousDropped: boolean;
}

/**
 * POST /api/ca/replace's 200 body (CA-replacement plan §4). `ca` is the
 * post-replace `GET /api/ca` shape, defined in `api.ts` as `CAStatus` --
 * imported here rather than re-declared, so the two never drift apart.
 */
export interface ReplaceResult {
  ca: import("./api").CAStatus;
  reissued: number;
  deleted: number;
  kept: number;
  clamped: number;
  previousDropped: boolean;
}

/**
 * One progress event from `replaceCAWithProgress`'s NDJSON stream (the
 * CA-replacement plan's progress-bar feature). `total` -- the overall
 * leaf-key count, 0 when none will be re-issued -- is present on every
 * phase, so the client can compute an overall percentage from the very
 * first event (see `percentFor` in `ndjson.ts`); `done` is present only for
 * `"leaf-keys"`, matching the server's own `omitempty` there.
 */
export interface ReplaceProgressEvent {
  phase: "ca-key" | "leaf-keys" | "saving";
  done?: number;
  total?: number;
}

/** One entry in an import preview/execute report. */
export interface ImportItem {
  path: string;
  fqdn: string;
  status: "importable" | "expired" | "quarantined" | "skipped";
  /** Present only on "quarantined" and "skipped" items. */
  reason?: string;
}

/**
 * GET /api/import/preview and POST /api/import share this shape. Not
 * consumed by this slice (the import wizard lands in slice 10); defined here
 * so slice 10 does not need to re-derive the contract from the handoff.
 */
export interface ImportReport {
  importable: number;
  expired: number;
  broken: number;
  skipped: number;
  items: ImportItem[];
  strayFiles: string[];
}
