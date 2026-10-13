// api.ts — typed client for the agentflow API.

import { alertDialog } from '@shared';

// ─── Model shapes (mirror internal/agentflow/store.go) ──────────────────────

export interface QueueTask {
  id: string;
  title: string;
  status: string;
  attempts: number;
  prompt?: string;
  acceptance?: string[] | string;
  verify?: string;
  last_review?: { verdict: string; summary: string } | null;
}

export interface QueueData {
  tasks: QueueTask[];
}

export interface ProgressCounts {
  total: number;
  passed: number;
  ready: number;
  blocked: number;
  review: number;
  repair: number;
  failed: number;
}

// ─── Fetch plumbing ────────────────────────────────────────────────────────

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
  queue() {
    return apiFetch<QueueData>('/queue');
  },

  progress() {
    return apiFetch<ProgressCounts>('/progress');
  },

  approveTask(id: string) {
    return apiFetch<{ status: string }>(`/tasks/${encodeURIComponent(id)}/approve`, {
      method: 'POST',
    });
  },

  repairTask(id: string) {
    return apiFetch<{ status: string }>(`/tasks/${encodeURIComponent(id)}/repair`, {
      method: 'POST',
    });
  },

  failTask(id: string) {
    return apiFetch<{ status: string }>(`/tasks/${encodeURIComponent(id)}/fail`, {
      method: 'POST',
    });
  },

  runNext() {
    return apiFetch<{ status: string }>('/run-next', { method: 'POST' });
  },
};