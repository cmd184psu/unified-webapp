// status.ts — shared execution-status vocabulary.
//
// Moved out of web/taskmaster/js/status.ts (plan
// docs/PLAN-utuber-taskmaster-lane.md §4.11) so a second module (utuber, on
// a taskmaster lane) can share the same status→symbol mapping instead of
// copy-pasting it. taskmaster's own status.ts keeps statusBadgeClass /
// renderStatusBadge (their CSS classes carry color literals the shared CSS
// gate forbids) and re-exports these three from "@shared".

export type ExecStatus = "success" | "failed" | "canceled" | "suspended" | "running" | "pending" | string;

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
