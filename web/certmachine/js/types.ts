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
