// api.ts — typed wrappers around the smbed REST API

export interface GlobalEntry {
  key: string
  value: string
}

export interface Share {
  name: string
  path: string
  comment?: string
  writable: boolean
  public: boolean
  browseable: boolean
  enabled: boolean
}

export interface AppConfig {
  smb_conf_path: string
  samba_log_path: string
  share_owner: string
  globals: GlobalEntry[]
  shares: Share[]
  theme: 'dark' | 'light' | 'system'
}

export interface FolderEntry {
  name: string
  path: string
}

export interface RestartResult {
  success: boolean
  output: string
}

export interface SaveRestartResponse {
  written: boolean
  restart: RestartResult
  path: string
}

export interface ImportResponse {
  globals: GlobalEntry[]
  shares: Share[]
  share_owner?: string
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`${method} ${path} → ${res.status}: ${text}`)
  }
  return res.json() as Promise<T>
}

export const api = {
  getConfig: () => request<AppConfig>('GET', '/api/config'),
  putConfig: (patch: Partial<AppConfig>) => request<AppConfig>('PUT', '/api/config', patch),

  getShares: () => request<Share[]>('GET', '/api/shares'),
  putShares: (shares: Share[]) => request<Share[]>('PUT', '/api/shares', shares),

  getGlobals: () => request<GlobalEntry[]>('GET', '/api/globals'),
  putGlobals: (globals: GlobalEntry[]) => request<GlobalEntry[]>('PUT', '/api/globals', globals),

  getFolders: () => request<FolderEntry[]>('GET', '/api/folders'),

  importConf: (path?: string) =>
    request<ImportResponse>('POST', '/api/import', path ? { path } : undefined),

  preview: async (): Promise<string> => {
    const res = await fetch('/api/preview')
    if (!res.ok) throw new Error('preview failed: ' + res.status)
    return res.text()
  },

  saveAndRestart: () => request<SaveRestartResponse>('POST', '/api/save-and-restart'),

  version: () => request<{ version: string }>('GET', '/api/version'),
}
