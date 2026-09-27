import { openTreePicker } from "@shared";
import type { TreeEntry } from "@shared";
import { listServerFiles } from "./api";

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1_048_576) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1_048_576).toFixed(2)} MB`;
}

/**
 * Open a server-side file browser (the shared tree picker). Resolves with
 * the chosen file's path (relative to the server's file root) or null on
 * cancel.
 */
export async function openFilePicker(startPath = ""): Promise<string | null> {
  const chosen = await openTreePicker({
    title: "Choose server file",
    select: "file",
    note: "Open folders with the arrow; double-click a file (or select it and press the button) to use it.",
    confirmLabel: "Use this file",
    emptyText: "No files here.",
    startPath: startPath || undefined,
    load: async (dir): Promise<TreeEntry[]> => {
      const { path, entries } = await listServerFiles(dir ? dir.path : "");
      return entries.map((e) => ({
        name: e.name,
        path: path === "" ? e.name : `${path}/${e.name}`,
        isDir: e.isDir,
        meta: e.isDir ? undefined : fmtBytes(e.size),
      }));
    },
  });
  return chosen?.path ?? null;
}
