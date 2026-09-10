/** Server-reported runtime configuration, from GET /api/config. */
export interface AppConfig {
  /** Number of host cards and terminal panels to render. */
  maxSessions: number;
}

/** Which credential a host authenticates with; exactly one, never both. */
export type AuthMethod = "key" | "password";

/**
 * The host fields the server persists -- the exact shape of one element of
 * `PUT /api/hosts`. It has no password field, which is what keeps a password
 * out of `hosts_path`: the persisted type simply has nowhere to put one.
 */
export interface PersistedHost {
  ip: string;
  port: number;
  user: string;
  /** Base name of a key file inside the server user's ~/.ssh (never a path). */
  key: string;
  remoteDir: string;
}

/**
 * One host as edited in the rail: the persisted fields plus credential state
 * that lives only in this tab's memory. `password` is sent in the WebSocket
 * connect frame and in a broadcast body and nowhere else -- never to
 * `PUT /api/hosts`, never to localStorage or sessionStorage.
 */
export interface HostConfig extends PersistedHost {
  authMethod: AuthMethod;
  password: string;
}

/** Lifecycle of a single terminal's SSH connection. */
export type SessionStatus =
  | "disconnected"
  | "connecting"
  | "connected"
  | "error";

/** One entry returned by GET /api/ssh/keys. */
export interface KeyFile {
  name: string;
  isDir: boolean;
}

/** One entry returned by POST /api/sftp/listdir. */
export interface RemoteDirEntry {
  name: string;
  isDir: boolean;
}

/** One entry returned by GET /api/files. */
export interface ServerFileEntry {
  name: string;
  isDir: boolean;
  size: number;
}

/** Control frame sent to the bridge as WebSocket text. */
export type ClientControl =
  | ({ type: "connect" } & PersistedHost & {
      host: string;
      /** Present only for a password host; mutually exclusive with `key`. */
      password?: string;
      cols: number;
      rows: number;
    })
  | { type: "resize"; cols: number; rows: number }
  | { type: "disconnect" };

/** Control frame received from the bridge as WebSocket text. */
export interface ServerControl {
  type: "status" | "error";
  state?: "connected" | "disconnected";
  message?: string;
}

/** One file returned by GET /api/uploads. */
export interface UploadInfo {
  id: string;
  name: string;
  size: number;
}

/** One SSH target for a broadcast job. */
export interface BroadcastTarget {
  host: string;
  port: number;
  user: string;
  /** Empty for a password host; the server requires exactly one of the two. */
  key: string;
  /** Present only for a password host; sent per job, never persisted. */
  password?: string;
  remoteDir: string;
}

/** Body sent to POST /api/broadcast. */
export interface BroadcastRequest {
  uploadId?: string;
  filePath?: string;
  targets: BroadcastTarget[];
}

/** Per-target progress frame from the broadcast WebSocket. */
export interface BroadcastProgress {
  type: "progress";
  index: number;
  host: string;
  bytes: number;
  total: number;
  state: "pending" | "transferring" | "done" | "error";
  message?: string;
}

/** Final frame from the broadcast WebSocket. */
export interface BroadcastComplete {
  type: "complete";
}
