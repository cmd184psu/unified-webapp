import { openFilePicker } from "./filepicker";
import {
  uploadFile,
  deleteUpload,
  startBroadcast,
  broadcastWsURL,
} from "./api";
import type {
  UploadInfo,
  BroadcastTarget,
  BroadcastProgress,
  BroadcastComplete,
} from "./types";
import { hostHasCredential } from "./hosts";
import type { HostStore } from "./hosts";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1_048_576) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1_048_576).toFixed(2)} MB`;
}

type BroadcastSource =
  | { kind: "upload"; info: UploadInfo }
  | { kind: "file"; path: string };

/** Mount the upload/broadcast tab. */
export function mountUploadApp(root: HTMLElement, store: HostStore): void {
  root.innerHTML = "";
  root.classList.add("upload-app");

  const scroll = el("div", "upload-scroll");
  root.append(scroll);

  let source: BroadcastSource | null = null;

  // ---- Stage A: source selection ----
  const stageA = el("section", "upload-section");
  const aTitle = el("h2", "upload-section-title");
  aTitle.textContent = "1 · Choose broadcast source";
  stageA.append(aTitle);

  const dropZone = el("div", "drop-zone");
  const dropText = el("p", "drop-text");
  dropText.textContent = "Drag & drop a file here, or click to upload";
  const fileInput = el("input");
  fileInput.type = "file";
  fileInput.style.display = "none";
  dropZone.append(dropText, fileInput);
  stageA.append(dropZone);

  const serverFileBtn = el("button", "btn source-server-btn");
  serverFileBtn.type = "button";
  serverFileBtn.textContent = "Or choose a server-resident file\u2026";
  stageA.append(serverFileBtn);

  const fileInfo = el("div", "file-info");
  fileInfo.style.display = "none";
  const fileNameEl = el("span", "file-info-name");
  const fileSizeEl = el("span", "file-info-size");
  const clearBtn = el("button", "btn btn-clear");
  clearBtn.type = "button";
  clearBtn.textContent = "Remove";
  fileInfo.append(fileNameEl, fileSizeEl, clearBtn);
  stageA.append(fileInfo);

  const uploadProgress = el("div", "upload-progress");
  uploadProgress.style.display = "none";
  const upBar = el("div", "progress-bar");
  const upFill = el("div", "progress-fill");
  upBar.append(upFill);
  const upText = el("span", "progress-text");
  uploadProgress.append(upBar, upText);
  stageA.append(uploadProgress);

  const errorMsg = el("p", "upload-error");
  errorMsg.style.display = "none";
  stageA.append(errorMsg);

  scroll.append(stageA);

  // ---- Stage B: broadcast ----
  const stageB = el("section", "upload-section");
  const bTitle = el("h2", "upload-section-title");
  bTitle.textContent = "2 · Broadcast to hosts";
  stageB.append(bTitle);

  const hostChecksEl = el("div", "host-checks");
  stageB.append(hostChecksEl);

  const broadcastBtn = el("button", "btn btn-connect broadcast-btn");
  broadcastBtn.type = "button";
  broadcastBtn.textContent = "Broadcast";
  stageB.append(broadcastBtn);

  const jobProgress = el("div", "job-progress");
  jobProgress.style.display = "none";
  stageB.append(jobProgress);

  scroll.append(stageB);

  // ---- checked state (parallel to store hosts) ----
  const checked: boolean[] = [];

  const renderHostChecks = (): void => {
    hostChecksEl.innerHTML = "";
    const hosts = store.getHosts();
    let hasAny = false;
    for (let i = 0; i < hosts.length; i++) {
      const h = hosts[i];
      if (!h) continue;
      const selectable = !!(h.ip && h.user) && hostHasCredential(h);
      if (selectable) hasAny = true;

      const row = el("label", "host-check-row");
      const cb = el("input");
      cb.type = "checkbox";
      cb.disabled = !selectable;
      cb.checked = selectable && (checked[i] ?? false);
      cb.addEventListener("change", () => {
        checked[i] = cb.checked;
      });
      const text = el("span", "host-check-label");
      text.textContent = h.ip
        ? `Host ${i + 1} — ${h.ip}`
        : `Host ${i + 1} (not configured)`;
      row.append(cb, text);
      hostChecksEl.append(row);
    }
    if (!hasAny) {
      const hint = el("p", "modal-note");
      hint.textContent = "Configure at least one host (IP, user, key) in the rail on the left.";
      hostChecksEl.append(hint);
    }
  };

  renderHostChecks();
  store.onChange(() => renderHostChecks());

  // ---- helpers ----
  const setError = (msg: string): void => {
    errorMsg.textContent = msg;
    errorMsg.style.display = "";
  };
  const clearError = (): void => {
    errorMsg.style.display = "none";
  };

  const showSource = (s: BroadcastSource): void => {
    source = s;
    if (s.kind === "upload") {
      fileNameEl.textContent = s.info.name;
      fileSizeEl.textContent = fmtBytes(s.info.size);
    } else {
      fileNameEl.textContent = s.path;
      fileSizeEl.textContent = "server file";
    }
    fileInfo.style.display = "";
    dropZone.style.display = "none";
    serverFileBtn.style.display = "none";
    uploadProgress.style.display = "none";
    jobProgress.style.display = "none";
    jobProgress.innerHTML = "";
  };

  const resetSource = (): void => {
    if (source?.kind === "upload") {
      void deleteUpload(source.info.id).catch(() => undefined);
    }
    source = null;
    fileInfo.style.display = "none";
    uploadProgress.style.display = "none";
    dropZone.style.display = "";
    serverFileBtn.style.display = "";
    clearError();
  };

  const doUpload = (file: File): void => {
    clearError();
    dropZone.style.display = "none";
    serverFileBtn.style.display = "none";
    fileInfo.style.display = "none";
    uploadProgress.style.display = "";
    upFill.style.width = "0%";
    upText.textContent = "0%";

    uploadFile(file, (loaded, total) => {
      const pct = total > 0 ? Math.round((loaded / total) * 100) : 0;
      upFill.style.width = `${pct}%`;
      upText.textContent = `${pct}% · ${fmtBytes(loaded)} / ${fmtBytes(total)}`;
    })
      .then((info) => {
        showSource({ kind: "upload", info });
      })
      .catch((err: unknown) => {
        uploadProgress.style.display = "none";
        dropZone.style.display = "";
        serverFileBtn.style.display = "";
        setError((err as Error).message);
      });
  };

  dropZone.addEventListener("click", () => fileInput.click());
  dropZone.addEventListener("dragover", (e) => {
    e.preventDefault();
    dropZone.classList.add("is-dragover");
  });
  dropZone.addEventListener("dragleave", () => {
    dropZone.classList.remove("is-dragover");
  });
  dropZone.addEventListener("drop", (e) => {
    e.preventDefault();
    dropZone.classList.remove("is-dragover");
    const file = e.dataTransfer?.files[0];
    if (file) doUpload(file);
  });
  fileInput.addEventListener("change", () => {
    const file = fileInput.files?.[0];
    if (file) {
      doUpload(file);
      fileInput.value = "";
    }
  });

  serverFileBtn.addEventListener("click", () => {
    void openFilePicker("").then((path) => {
      if (path !== null) {
        showSource({ kind: "file", path });
      }
    });
  });

  clearBtn.addEventListener("click", resetSource);

  broadcastBtn.addEventListener("click", () => {
    if (!source) {
      setError("Choose a broadcast source first.");
      return;
    }
    const hosts = store.getHosts();
    const activeTargets: BroadcastTarget[] = [];
    for (let i = 0; i < hosts.length; i++) {
      if (!checked[i]) continue;
      const h = hosts[i];
      if (!h || !h.ip || !h.user || !hostHasCredential(h)) continue;
      const usesPassword = h.authMethod === "password";
      activeTargets.push({
        host: h.ip,
        port: h.port,
        user: h.user,
        // Exactly one credential per target, same rule as the terminal bridge.
        key: usesPassword ? "" : h.key,
        password: usesPassword ? h.password : "",
        remoteDir: h.remoteDir || "/tmp",
      });
    }
    if (activeTargets.length === 0) {
      setError("Check at least one configured host as target.");
      return;
    }
    clearError();
    broadcastBtn.disabled = true;
    jobProgress.style.display = "";
    jobProgress.innerHTML = "";

    const req =
      source.kind === "upload"
        ? { uploadId: source.info.id, targets: activeTargets }
        : { filePath: source.path, targets: activeTargets };

    startBroadcast(req)
      .then(({ jobId }) => {
        runBroadcastJob(jobId, activeTargets, jobProgress, () => {
          broadcastBtn.disabled = false;
        });
      })
      .catch((err: unknown) => {
        broadcastBtn.disabled = false;
        setError((err as Error).message);
        jobProgress.style.display = "none";
      });
  });
}

interface ProgressRow {
  update(p: BroadcastProgress): void;
}

function buildProgressRow(host: string, container: HTMLElement): ProgressRow {
  const row = el("div", "job-row");
  const hostEl = el("span", "job-host");
  hostEl.textContent = host;
  const stateEl = el("span", "job-state");
  stateEl.textContent = "pending";
  stateEl.dataset.state = "pending";
  const bar = el("div", "progress-bar");
  const fill = el("div", "progress-fill");
  bar.append(fill);
  const bytesEl = el("span", "progress-text");
  bytesEl.textContent = "\u2014";
  row.append(hostEl, stateEl, bar, bytesEl);
  container.append(row);

  return {
    update(p: BroadcastProgress): void {
      stateEl.textContent = p.message ? `${p.state}: ${p.message}` : p.state;
      stateEl.dataset.state = p.state;
      if (p.total > 0) {
        const pct = Math.round((p.bytes / p.total) * 100);
        fill.style.width = `${pct}%`;
        bytesEl.textContent = `${fmtBytes(p.bytes)} / ${fmtBytes(p.total)} (${pct}%)`;
      }
    },
  };
}

function runBroadcastJob(
  jobId: string,
  targets: BroadcastTarget[],
  container: HTMLElement,
  onDone: () => void,
): void {
  const rows = targets.map((t) => buildProgressRow(t.host, container));

  const ws = new WebSocket(broadcastWsURL(jobId));
  let done = false;

  const finish = (): void => {
    if (done) return;
    done = true;
    onDone();
  };

  ws.onmessage = (ev: MessageEvent) => {
    if (typeof ev.data !== "string") return;
    let msg: BroadcastProgress | BroadcastComplete;
    try {
      msg = JSON.parse(ev.data) as BroadcastProgress | BroadcastComplete;
    } catch {
      return;
    }
    if (msg.type === "complete") {
      ws.close();
      finish();
      return;
    }
    if (msg.type === "progress") {
      const row = rows[msg.index];
      if (row) row.update(msg);
    }
  };

  ws.onerror = () => {
    ws.close();
    finish();
  };

  ws.onclose = () => finish();
}
