import type {
  AppConfig,
  HostConfig,
  PersistedHost,
  KeyFile,
  UploadInfo,
  BroadcastRequest,
  RemoteDirEntry,
  ServerFileEntry,
} from "./types";

/** The panel count used when the server cannot be asked. */
const DEFAULT_MAX_SESSIONS = 10;

/** Guards against triggering more than one reload when several calls 401 at once. */
let sessionExpiredHandled = false;

/** If `status` is 401, notify the user and reload to the auth gate's login page. */
function checkAuth(status: number): void {
  if (status !== 401) return;
  if (!sessionExpiredHandled) {
    sessionExpiredHandled = true;
    console.warn("multissh: session expired, reloading to sign in");
    window.location.reload();
  }
  throw new Error("session expired — reloading to sign in");
}

/**
 * Fetch the server's runtime configuration.
 *
 * Never rejects: a failed request or a non-numeric `maxSessions` falls back to
 * DEFAULT_MAX_SESSIONS and logs, so a transient boot failure still allows the
 * default host count rather than NaN of them.
 */
export async function fetchConfig(): Promise<AppConfig> {
  try {
    const res = await fetch("/api/config");
    checkAuth(res.status);
    if (!res.ok) throw new Error(`config fetch failed: ${res.status}`);
    const body = (await res.json()) as { maxSessions?: unknown };
    const n = body.maxSessions;
    if (typeof n !== "number" || !Number.isFinite(n) || n < 1) {
      throw new Error(`config returned an unusable maxSessions: ${String(n)}`);
    }
    return { maxSessions: Math.floor(n) };
  } catch (err) {
    console.warn(
      `multissh: falling back to ${DEFAULT_MAX_SESSIONS} sessions:`,
      err,
    );
    return { maxSessions: DEFAULT_MAX_SESSIONS };
  }
}

/** Fetch the regular files in the server user's ~/.ssh for the key picker. */
export async function fetchKeys(): Promise<KeyFile[]> {
  const res = await fetch("/api/ssh/keys");
  checkAuth(res.status);
  if (!res.ok) {
    throw new Error(`key listing failed: ${res.status}`);
  }
  const body = (await res.json()) as { keys?: KeyFile[] };
  return body.keys ?? [];
}

/** Build the same-origin ws:// or wss:// URL for the terminal bridge. */
export function wsURL(): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/api/ssh/ws`;
}

/** Upload a file via XHR (multipart/form-data, field "file"); reports progress. */
export function uploadFile(
  file: File,
  onProgress: (loaded: number, total: number) => void,
): Promise<UploadInfo> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const form = new FormData();
    form.append("file", file);
    xhr.open("POST", "/api/upload");
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded, e.total);
    };
    xhr.onload = () => {
      try {
        checkAuth(xhr.status);
      } catch (err) {
        reject(err);
        return;
      }
      if (xhr.status === 413) {
        reject(new Error("File too large"));
        return;
      }
      if (xhr.status !== 200) {
        let msg = `Upload failed: ${xhr.status}`;
        try {
          const b = JSON.parse(xhr.responseText) as { error?: string };
          if (b.error) msg = b.error;
        } catch { /* ignore */ }
        reject(new Error(msg));
        return;
      }
      resolve(JSON.parse(xhr.responseText) as UploadInfo);
    };
    xhr.onerror = () => reject(new Error("Network error during upload"));
    xhr.send(form);
  });
}

/** Delete a previously uploaded file. */
export async function deleteUpload(id: string): Promise<void> {
  const res = await fetch(`/api/uploads/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  checkAuth(res.status);
  if (!res.ok && res.status !== 204) {
    throw new Error(`delete failed: ${res.status}`);
  }
}

/** Start a broadcast job; returns the job ID. */
export async function startBroadcast(
  req: BroadcastRequest,
): Promise<{ jobId: string }> {
  const res = await fetch("/api/broadcast", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  checkAuth(res.status);
  if (!res.ok) {
    const body = (await res.json()) as { error?: string };
    throw new Error(body.error ?? `broadcast failed: ${res.status}`);
  }
  return (await res.json()) as { jobId: string };
}

/** Build the same-origin ws:// or wss:// URL for broadcast progress. */
export function broadcastWsURL(jobId: string): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${location.host}/api/broadcast/ws?job=${encodeURIComponent(jobId)}`;
}

/** Fetch the persisted host configurations. */
export async function fetchHosts(): Promise<PersistedHost[]> {
  const res = await fetch("/api/hosts");
  checkAuth(res.status);
  if (!res.ok) throw new Error(`fetch hosts failed: ${res.status}`);
  const body = (await res.json()) as { hosts?: PersistedHost[] };
  return body.hosts ?? [];
}

/**
 * Copy out the five persisted fields of a host.
 *
 * Named and used deliberately instead of spreading the host: this is the only
 * object that reaches `PUT /api/hosts`, and building it field by field is what
 * guarantees `password` cannot ride along.
 */
function persistedFields(h: HostConfig): PersistedHost {
  return {
    name: h.name,
    ip: h.ip,
    port: h.port,
    user: h.user,
    key: h.key,
    remoteDir: h.remoteDir,
  };
}

/** Persist host configurations (credentials excluded); returns the saved list. */
export async function saveHosts(hosts: HostConfig[]): Promise<PersistedHost[]> {
  const payload = hosts.map(persistedFields);
  const res = await fetch("/api/hosts", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ hosts: payload }),
  });
  checkAuth(res.status);
  if (!res.ok) throw new Error(`save hosts failed: ${res.status}`);
  const body = (await res.json()) as { hosts?: PersistedHost[] };
  return body.hosts ?? payload;
}

/** List directory entries on a remote host via SFTP. */
export async function listRemoteDir(
  target: { host: string; port: number; user: string; key: string },
  path: string,
): Promise<{ path: string; entries: RemoteDirEntry[] }> {
  const res = await fetch("/api/sftp/listdir", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...target, path }),
  });
  checkAuth(res.status);
  if (!res.ok) throw new Error(`listdir failed: ${res.status}`);
  return (await res.json()) as { path: string; entries: RemoteDirEntry[] };
}

/** List server-resident files at the given relative path. */
export async function listServerFiles(
  path: string,
): Promise<{ path: string; entries: ServerFileEntry[] }> {
  const res = await fetch(`/api/files?path=${encodeURIComponent(path)}`);
  checkAuth(res.status);
  if (!res.ok) throw new Error(`list files failed: ${res.status}`);
  return (await res.json()) as { path: string; entries: ServerFileEntry[] };
}
