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
const DEFAULT_MAX_SESSIONS = 3;

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
 * DEFAULT_MAX_SESSIONS and logs, so a transient boot failure renders three
 * panels rather than NaN of them.
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

/** AuthError carries the HTTP status of a failed auth request. */
export class AuthError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** A passkey ceremony returned by the begin-register/begin-login endpoints. */
export interface AuthCeremony {
  challengeId: string;
  options: unknown;
}

/** A redacted view of an enrolled passkey credential. */
export interface AuthPasskey {
  id: string;
  friendlyName: string;
  createdAt: number;
  lastUsedAt?: number;
}

/** The current authentication session state. */
export interface AuthSession {
  authenticated: boolean;
  username?: string;
  groups?: string[];
  authVia?: string;
  expiresAt?: number;
  passkeyEnrolled?: boolean;
}

async function authJSON<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let msg = `request failed: ${res.status}`;
    try {
      const b = (await res.json()) as { error?: string };
      if (b.error) msg = b.error;
    } catch { /* ignore */ }
    throw new AuthError(res.status, msg);
  }
  return (await res.json()) as T;
}

/** Report the configured auth mode ("none" | "ldap" | "ldap_passkey"). */
export async function fetchAuthMode(): Promise<string> {
  const res = await fetch("/api/auth/mode");
  const body = await authJSON<{ mode?: string }>(res);
  return body.mode ?? "none";
}

/** Fetch the current session, if any. */
export async function fetchAuthSession(): Promise<AuthSession> {
  const res = await fetch("/api/auth/session");
  return authJSON<AuthSession>(res);
}

/** Sign in with an LDAP username/password. */
export async function loginLDAP(username: string, password: string): Promise<AuthSession> {
  const res = await fetch("/api/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  return authJSON<AuthSession>(res);
}

/** Sign out and clear the session cookie. */
export async function logout(): Promise<void> {
  await fetch("/api/auth/logout", { method: "POST" });
}

/** List the current user's enrolled passkeys. */
export async function listPasskeys(): Promise<AuthPasskey[]> {
  const res = await fetch("/api/auth/passkeys");
  const body = await authJSON<{ passkeys?: AuthPasskey[] }>(res);
  return body.passkeys ?? [];
}

/** Begin a passkey registration ceremony for the current user. */
export async function beginPasskeyRegistration(friendlyName: string): Promise<AuthCeremony> {
  const res = await fetch("/api/auth/passkeys/register/begin", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ friendlyName }),
  });
  return authJSON<AuthCeremony>(res);
}

/** Finish a passkey registration ceremony. */
export async function finishPasskeyRegistration(
  challengeId: string,
  friendlyName: string,
  credential: unknown,
): Promise<AuthPasskey> {
  const res = await fetch("/api/auth/passkeys/register/finish", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ challengeId, friendlyName, credential }),
  });
  return authJSON<AuthPasskey>(res);
}

/** Begin a passkey login ceremony for the given username. */
export async function beginPasskeyLogin(username: string): Promise<AuthCeremony> {
  const res = await fetch("/api/auth/passkeys/login/begin", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username }),
  });
  return authJSON<AuthCeremony>(res);
}

/** Finish a passkey login ceremony. */
export async function finishPasskeyLogin(
  challengeId: string,
  credential: unknown,
): Promise<AuthSession> {
  const res = await fetch("/api/auth/passkeys/login/finish", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ challengeId, credential }),
  });
  return authJSON<AuthSession>(res);
}
