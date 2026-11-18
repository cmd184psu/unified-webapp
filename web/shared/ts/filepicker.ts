import { openTreePicker } from "./filetree.js";
import type { TreeEntry } from "./filetree.js";

export interface FilePickerOptions {
  /** Loads folders on demand (`null` = the top level). Caller-owned: each
   * module maps its own file-listing endpoint onto TreeEntry here. */
  load: (dir: TreeEntry | null) => Promise<TreeEntry[]>;
  /** Opened to and preselected. */
  startPath?: string;
}

/** Human byte count for picker `meta` lines. */
export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1_048_576) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1_048_576).toFixed(2)} MB`;
}

export interface DirPickerOptions {
  /** Loads folders on demand (`null` = the top level). Caller-owned. */
  load: (dir: TreeEntry | null) => Promise<TreeEntry[]>;
  /** Opened to and preselected. */
  startPath?: string;
  /** Who the folders belong to, e.g. "op@vm1.lab" (named in the note). */
  place: string;
}

/**
 * Open a server-side file browser (the shared tree picker with the one
 * canonical chrome). Resolves with the chosen file's path, or null on
 * cancel.
 */
export async function openFilePicker({
  load,
  startPath = "",
}: FilePickerOptions): Promise<string | null> {
  const chosen = await openTreePicker({
    title: "Choose server file",
    select: "file",
    note: "Open folders with the arrow; double-click a file (or select it and press the button) to use it.",
    confirmLabel: "Use this file",
    emptyText: "No files here.",
    startPath: startPath || undefined,
    load,
  });
  return chosen?.path ?? null;
}

/**
 * Open a remote-directory browser (the shared tree picker with the one
 * canonical chrome). Resolves with the chosen absolute path, or null on
 * cancel.
 */
export async function openDirPicker({
  load,
  startPath = "",
  place,
}: DirPickerOptions): Promise<string | null> {
  const chosen = await openTreePicker({
    title: "Choose remote directory",
    select: "dir",
    note: `Folders on ${place}. Select one and press the button, or double-click it.`,
    confirmLabel: "Use this directory",
    emptyText: "No subdirectories.",
    startPath: startPath || undefined,
    load,
  });
  return chosen?.path ?? null;
}
