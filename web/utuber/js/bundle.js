// web/utuber/js/main.ts
import { ThemeManager, HamburgerMenu, confirmDialog, showToast } from "/shared/dist/shared.mjs";
var themes = new ThemeManager({ module: "utuber", default: "dark" });
themes.apply();
var hamburger = new HamburgerMenu({
  title: "uTuber",
  items: [
    { section: "Settings" },
    {
      id: "python-setting",
      render(host) {
        const field = document.createElement("div");
        field.className = "field";
        const label = document.createElement("label");
        label.htmlFor = "python-bin";
        label.textContent = "Python interpreter";
        const input = document.createElement("input");
        input.type = "text";
        input.id = "python-bin";
        input.placeholder = "python3.12";
        input.autocomplete = "off";
        input.spellcheck = false;
        const hint = document.createElement("span");
        hint.className = "optional-hint";
        hint.textContent = 'Used by "Update yt-dlp". Blank resets to the server default.';
        field.append(label, input, hint);
        const submitRow = document.createElement("div");
        submitRow.className = "submit-row";
        const saveBtn = document.createElement("button");
        saveBtn.className = "btn btn-ghost btn-sm";
        saveBtn.textContent = "Save";
        saveBtn.addEventListener("click", saveSettings);
        const status = document.createElement("span");
        status.id = "settings-status";
        status.style.fontSize = "0.75rem";
        status.style.color = "var(--color-text-faint)";
        submitRow.append(saveBtn, status);
        host.append(field, submitRow);
        loadSettings();
      }
    },
    { separator: true },
    {
      id: "ytdlp-update",
      render(host) {
        const btn = document.createElement("button");
        btn.className = "btn btn-ghost btn-sm";
        btn.id = "update-btn";
        btn.textContent = "Update yt-dlp";
        btn.addEventListener("click", runYtdlpUpdate);
        const status = document.createElement("span");
        status.id = "update-status";
        status.style.fontSize = "0.75rem";
        status.style.color = "var(--color-text-faint)";
        const log = document.createElement("div");
        log.id = "update-log";
        log.className = "update-log";
        host.append(btn, status, log);
      }
    }
  ],
  themePicker: true,
  themes,
  mountTrigger: document.getElementById("settings-btn"),
  // The trigger sits at the right end of the topbar; the drawer opens beside it.
  side: "right"
});
var currentMode = "video";
var pendingFormData = null;
function setMode(mode) {
  currentMode = mode;
  document.getElementById("mode-input").value = mode;
  document.querySelectorAll(".mode-tab").forEach((t) => {
    t.classList.toggle("active", t.dataset.mode === mode);
  });
  const btn = document.getElementById("submit-btn");
  btn.className = mode === "audio" ? "btn btn-audio" : "btn btn-video";
}
function hideDupBanner() {
  const b = document.getElementById("dup-banner");
  b.style.display = "none";
  b.innerHTML = "";
  pendingFormData = null;
}
function showDupBanner(data, formData) {
  pendingFormData = formData;
  const b = document.getElementById("dup-banner");
  const fname = data.output_file || "(unknown)";
  b.innerHTML = `
    <div>
      <strong>Already downloaded.</strong>
      "${esc(data.show_name)}" was previously saved as
      <a href="/downloads/${encodeURIComponent(fname)}">${esc(fname)}</a>.
    </div>
    <div class="dup-actions">
      <button class="dup-btn" id="dup-force-btn">Download again anyway</button>
      <button class="dup-btn dismiss" id="dup-dismiss-btn">Dismiss</button>
    </div>`;
  b.style.display = "block";
  document.getElementById("dup-force-btn").addEventListener("click", forceEnqueue);
  document.getElementById("dup-dismiss-btn").addEventListener("click", hideDupBanner);
}
async function forceEnqueue() {
  if (!pendingFormData) return;
  pendingFormData.set("force", "1");
  await submitForm(pendingFormData);
  hideDupBanner();
}
function showErrToast(msg) {
  const t = document.getElementById("err-toast");
  t.textContent = msg;
  t.style.display = "block";
}
function hideErrToast() {
  const t = document.getElementById("err-toast");
  t.style.display = "none";
  t.textContent = "";
}
async function submitForm(formData) {
  hideErrToast();
  let res;
  try {
    res = await fetch("/enqueue", { method: "POST", body: formData });
  } catch (err) {
    showErrToast("Could not reach server: " + err.message);
    return;
  }
  if (res.status === 409) {
    const data = await res.json();
    showDupBanner(data, formData);
    return;
  }
  if (res.status === 204) {
    document.querySelector('input[name="url"]').value = "";
    hideDupBanner();
    refreshJobs();
    return;
  }
  const body = await res.text().catch(() => "");
  showErrToast(`Server error ${res.status}${body ? ": " + body.trim() : ""}`);
}
document.getElementById("enqueue-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  hideDupBanner();
  await submitForm(new FormData(e.target));
});
async function refreshJobs() {
  const res = await fetch("/jobs.json");
  const jobs = await res.json();
  const list = document.getElementById("jobs-list");
  const countEl = document.getElementById("jobs-count");
  if (!jobs || jobs.length === 0) {
    list.innerHTML = '<div class="empty-state">No jobs yet \u2014 add one above.</div>';
    countEl.textContent = "";
    return;
  }
  countEl.textContent = jobs.length + (jobs.length === 1 ? " job" : " jobs");
  list.innerHTML = jobs.map((j) => jobRow(j)).join("");
}
function jobRow(j) {
  const epLabel = (j.Season > 0 ? `S${String(j.Season).padStart(2, "0")}` : "") + (j.Episode > 0 ? `E${String(j.Episode).padStart(2, "0")}` : "");
  const mode = j.Mode || "video";
  const { barClass, pct, label } = parseProgress(j.Progress, j.Status);
  const statusClass = "status-" + (j.Status || "queued");
  const isAudio = mode === "audio";
  const dlLink = j.Status === "completed" ? `<div class="dl-actions">
        <a class="download-link ${isAudio ? "audio-dl" : ""}" href="/downloads/${encodeURIComponent(j.OutputFile)}" target="_blank" rel="noopener">
          <svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor">
            <path d="M6 4l10 6-10 6V4z"/>
          </svg>
          Play
        </a>
        <a class="download-link ${isAudio ? "audio-dl" : ""}" href="/downloads/${encodeURIComponent(j.OutputFile)}" download>
          <svg width="12" height="12" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <path d="M10 3v10M5 13l5 5 5-5"/><line x1="3" y1="19" x2="17" y2="19"/>
          </svg>
          Download
        </a>
      </div>` : "";
  const errLine = j.Error ? `<div class="job-error" title="${esc(j.Error)}">\u26A0 ${esc(j.Error)}</div>` : "";
  return `
    <div class="job-item">
      <div class="job-meta">
        <div class="job-name">${esc(j.ShowName)} \u2014 ${esc(j.EpisodeTitle)}</div>
        <div class="job-sub">
          ${epLabel ? `<span>${epLabel}</span>` : ""}
          <span class="mode-badge ${mode}">${mode}</span>
        </div>
        ${errLine}
      </div>
      <div class="job-right">
        <div class="job-status-row">
          <span class="status-pill ${statusClass}">${j.Status}</span>
          ${j.Status === "running" ? "" : `<button type="button" class="job-delete" data-job-id="${esc(j.ID)}" data-job-status="${esc(j.Status)}" data-job-name="${esc(j.ShowName)} \u2014 ${esc(j.EpisodeTitle)}" title="${j.Status === "queued" ? "Remove from queue" : "Remove from list"}" aria-label="${j.Status === "queued" ? "Remove from queue" : "Remove from list"}">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/></svg>
          </button>`}
        </div>
        ${barClass ? `
        <div class="progress-wrap">
          <div class="progress-bar ${barClass}" style="width:${pct}%"></div>
        </div>
        <span class="progress-label">${label}</span>
        ` : ""}
        ${dlLink}
      </div>
    </div>`;
}
function parseProgress(p, status) {
  if (status === "completed") return { barClass: "done", pct: 100, label: "" };
  if (!p) return { barClass: "", pct: 0, label: "" };
  if (p.startsWith("download")) {
    const pct = parseFloat(p.split(" ")[1]) || 0;
    return { barClass: "dl", pct, label: `Downloading ${pct.toFixed(0)}%` };
  }
  if (p.startsWith("convert") || p === "converting") {
    const pct = parseFloat(p.split(" ")[1]) || 0;
    return { barClass: "conv", pct: pct || 50, label: "Converting\u2026" };
  }
  if (p === "done") return { barClass: "done", pct: 100, label: "" };
  return { barClass: "", pct: 0, label: p };
}
function esc(s) {
  return String(s || "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}
function runYtdlpUpdate() {
  const btn = document.getElementById("update-btn");
  const status = document.getElementById("update-status");
  const log = document.getElementById("update-log");
  btn.disabled = true;
  btn.textContent = "Updating\u2026";
  status.textContent = "";
  log.textContent = "";
  log.style.display = "block";
  const es = new EventSource("/ytdlp-update");
  es.onmessage = (e) => {
    if (e.data === "__done__") {
      es.close();
      btn.disabled = false;
      btn.textContent = "Update yt-dlp";
      status.textContent = "Done.";
      return;
    }
    if (e.data.startsWith("ERROR:")) {
      log.textContent += e.data + "\n";
      es.close();
      btn.disabled = false;
      btn.textContent = "Update yt-dlp";
      status.style.color = "var(--color-danger)";
      status.textContent = "Update failed.";
      return;
    }
    log.textContent += e.data + "\n";
    log.scrollTop = log.scrollHeight;
  };
  es.onerror = () => {
    es.close();
    btn.disabled = false;
    btn.textContent = "Update yt-dlp";
    status.style.color = "var(--color-danger)";
    status.textContent = "Connection error.";
  };
}
async function loadSettings() {
  const status = document.getElementById("settings-status");
  try {
    const res = await fetch("/settings.json");
    const data = await res.json();
    document.getElementById("python-bin").value = data.python_bin || "";
    status.textContent = "";
  } catch {
    status.style.color = "var(--color-danger)";
    status.textContent = "Connection error.";
  }
}
async function saveSettings() {
  const status = document.getElementById("settings-status");
  const input = document.getElementById("python-bin");
  try {
    const res = await fetch("/settings.json", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ python_bin: input.value })
    });
    if (res.ok) {
      const data = await res.json();
      input.value = data.python_bin || "";
      status.style.color = "var(--color-success)";
      status.textContent = "Saved.";
    } else {
      status.style.color = "var(--color-danger)";
      status.textContent = await res.text();
    }
  } catch {
    status.style.color = "var(--color-danger)";
    status.textContent = "Connection error.";
  }
}
document.querySelectorAll(".mode-tab").forEach((tab) => {
  tab.addEventListener("click", () => setMode(tab.dataset.mode));
});
document.getElementById("jobs-list").addEventListener("click", async (e) => {
  const btn = e.target.closest(".job-delete");
  if (!btn) return;
  const id = btn.dataset.jobId;
  const queued = btn.dataset.jobStatus === "queued";
  const ok = await confirmDialog(
    queued ? `Remove "${btn.dataset.jobName}" from the queue? It won't be downloaded.` : `Remove "${btn.dataset.jobName}" from the list? Any downloaded file is kept.`,
    { title: queued ? "Remove queued download" : "Remove from list", confirmLabel: "Remove" }
  );
  if (!ok) return;
  const res = await fetch(`/jobs/delete?id=${encodeURIComponent(id)}`, { method: "POST" });
  if (res.status === 409) {
    showToast("That download has already started and can't be removed.", "notice");
  } else if (!res.ok && res.status !== 404) {
    showToast("Could not remove it.", "error");
  }
  void refreshJobs();
});
setInterval(refreshJobs, 2e3);
refreshJobs();
