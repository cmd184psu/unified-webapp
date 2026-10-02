// applyAction.ts — runs an Apply/Restore request and turns every answer into
// exactly one toast (the pure mapping lives in pending.ts).

import { showToast } from '@shared'
import { ApiError, ApplyResult, Issue } from './api'
import { applyErrorToast, applyToast } from './pending'

/** Resolves to the blocking issues of a 409 (empty otherwise). */
export async function runApplyLike(fn: () => Promise<ApplyResult>): Promise<Issue[]> {
  try {
    const t = applyToast(await fn())
    showToast(t.message, t.tone)
    return []
  } catch (e) {
    if (e instanceof ApiError) {
      const t = applyErrorToast(e.status, e.message, e.issues)
      showToast(t.message, t.tone)
      return e.status === 409 ? e.issues.filter(i => i.severity === 'error') : []
    }
    showToast(`Apply failed: ${e instanceof Error ? e.message : String(e)}`, 'error')
    return []
  }
}
