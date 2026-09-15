// status.ts — shared execution-status presentation helpers.
//
// board.ts (the lane board) and taskdetail.ts (the per-task drill-in) both
// need to turn an execution's status into a CSS class, a compact symbol, and
// a rendered badge. This used to be defined twice — copy-pasted, not
// shared — which is exactly how a fix lands in one copy and silently misses
// the other. Defined once here instead (owner DRY policy).

export type ExecStatus = "success" | "failed" | "canceled" | "suspended" | "running" | "pending" | string;

export function statusBadgeClass(status: ExecStatus): string {
  if (status === "success") return "badge-green";
  if (status === "failed") return "badge-red";
  if (status === "canceled") return "badge-yellow";
  if (status === "suspended") return "badge-yellow";
  if (status === "running") return "badge-blue";
  return "badge-muted";
}

// A word badge ("RUNNING", "SUCCESS"...) says less at a glance than a
// symbol, and costs more row width — use symbols for this small, well-known
// status set (owner UI policy: symbols over word badges where appropriate).
// The full word survives as the badge's title/aria-label.
export function statusSymbol(status: ExecStatus): string {
  if (status === "success") return "✓";
  if (status === "failed") return "✕";
  if (status === "canceled") return "⊘";
  if (status === "suspended") return "⏸";
  if (status === "running") return "●";
  if (status === "pending") return "…";
  return "•";
}

// A suspended run is still status "running" server-side (suspension is an
// in-memory overlay, not a DB status) — but it should read as one
// "suspended" state, not "running" plus a second badge layered on top.
export function effectiveStatus(status: string, suspended: boolean | undefined): ExecStatus {
  return status === "running" && suspended ? "suspended" : status;
}

/** Sets an existing `.badge` element's class/text/title to show `status` (as a symbol). */
export function renderStatusBadge(badge: HTMLElement, status: string, suspended?: boolean): void {
  const eff = effectiveStatus(status, suspended);
  badge.className = "badge badge-symbol " + statusBadgeClass(eff);
  badge.textContent = statusSymbol(eff);
  badge.title = eff;
  badge.setAttribute("aria-label", eff);
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

function fmtDurationSeconds(totalSec: number): string {
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  if (h > 0) return h + ":" + pad2(m) + ":" + pad2(s);
  return m + ":" + pad2(s);
}

/** Live "how long has this been running" — mm:ss, or hh:mm:ss past an hour. */
export function fmtElapsed(startedAt: string): string {
  const startMs = new Date(startedAt).getTime();
  if (Number.isNaN(startMs)) return "running…";
  const totalSec = Math.max(0, Math.floor((Date.now() - startMs) / 1000));
  return fmtDurationSeconds(totalSec);
}

export function fmtMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined) return "—";
  return (ms / 1000).toFixed(2) + "s";
}

export function fmtDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}
