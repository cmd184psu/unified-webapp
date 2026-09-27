import { openTreePicker } from '@shared'
import { api, FolderEntry } from './api'

/**
 * Asks for a folder under /opt with the shared tree picker. Resolves with
 * the chosen folder, or null if canceled.
 */
export async function pickFolder(): Promise<FolderEntry | null> {
  const chosen = await openTreePicker({
    title: 'Pick a folder from /opt',
    select: 'dir',
    confirmLabel: 'Select',
    emptyText: '/opt has no subdirectories.',
    // /opt's folders, one level: they can be chosen but not opened.
    load: async () => (await api.getFolders()).map(f => ({ name: f.name, path: f.path, isDir: true, leaf: true })),
  })
  return chosen ? { name: chosen.name, path: chosen.path } : null
}
