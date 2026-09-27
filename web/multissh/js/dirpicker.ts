import { openTreePicker } from "@shared";
import type { TreeEntry } from "@shared";
import { listRemoteDir } from "./api";

/**
 * Open a remote-directory browser (the shared tree picker) on the target
 * host, starting at startPath. Resolves with the chosen absolute path or
 * null on cancel.
 */
export async function openDirPicker(
  target: { host: string; port: number; user: string; key: string },
  startPath = "/tmp",
): Promise<string | null> {
  const chosen = await openTreePicker({
    title: "Choose remote directory",
    select: "dir",
    note: `Folders on ${target.user}@${target.host}. Select one and press the button, or double-click it.`,
    confirmLabel: "Use this directory",
    emptyText: "No subdirectories.",
    startPath,
    load: async (dir): Promise<TreeEntry[]> => {
      // The top level is the filesystem root itself.
      if (!dir) return [{ name: "/", path: "/", isDir: true }];
      const { path, entries } = await listRemoteDir(target, dir.path);
      return entries
        .filter((e) => e.isDir)
        .map((e) => ({ name: e.name, path: path === "/" ? `/${e.name}` : `${path}/${e.name}`, isDir: true }));
    },
  });
  return chosen?.path ?? null;
}
