// web/utuber/js/main.ts
import { ThemeManager, HamburgerMenu, confirmDialog, showToast, QueuePanel } from "/shared/dist/shared.mjs";
var themes = new ThemeManager({ module: "utuber", default: "dark" });
themes.apply();
var hamburger = new HamburgerMenu({
  title: "uTuber",
  items: [
    { section: "Queue" },
    {
      id: "queue-setting",
      render(host) {
        host.appendChild(numberField(
          "age-out-days",
          "Age-out (days)",
          1,
          365,
          (v) => saveQueueSetting({ age_out_days: v })
        ));
        host.appendChild(numberField(
          "concurrent-downloads",
          "Concurrent downloads",
          1,
          8,
          (v) => saveQueueSetting({ concurrent_downloads: v })
        ));
        const showField = document.createElement("label");
        showField.className = "field field-check";
        const cb = document.createElement("input");
        cb.type = "checkbox";
        cb.id = "show-in-taskmaster";
        cb.addEventListener("change", () => void saveQueueSetting({ show_in_taskmaster: cb.checked }));
        const cbText = document.createElement("span");
        cbText.textContent = "Show in taskmaster";
        showField.append(cb, cbText);
        host.appendChild(showField);
        const status = document.createElement("span");
        status.id = "queue-settings-status";
        status.style.fontSize = "0.75rem";
        status.style.color = "var(--color-text-faint)";
        host.appendChild(status);
        void loadQueueSettings();
      }
    },
    { separator: true },
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
      id: "cookies-setting",
      render(host) {
        const heading = document.createElement("div");
        heading.textContent = "Age-restricted cookies";
        heading.style.fontWeight = "600";
        heading.style.marginBottom = "0.25rem";
        const hint = document.createElement("p");
        hint.className = "optional-hint";
        hint.textContent = 'yt-dlp cannot sign in on its own. Export a Netscape-format cookie file from a browser that is already signed into YouTube \u2014 either the "Get cookies.txt LOCALLY" extension, or on that machine: yt-dlp --cookies-from-browser chrome --cookies cookies.txt --skip-download <any-url> \u2014 then paste its contents below.';
        hint.style.whiteSpace = "pre-wrap";
        const status = document.createElement("span");
        status.id = "cookies-status";
        status.style.display = "block";
        status.style.fontSize = "0.75rem";
        status.style.color = "var(--color-text-faint)";
        status.style.marginBottom = "0.5rem";
        const textarea = document.createElement("textarea");
        textarea.id = "cookies-txt";
        textarea.placeholder = "# Netscape HTTP Cookie File\n...";
        textarea.rows = 4;
        textarea.spellcheck = false;
        textarea.style.width = "100%";
        textarea.style.fontFamily = "monospace";
        textarea.style.fontSize = "0.75rem";
        const submitRow = document.createElement("div");
        submitRow.className = "submit-row";
        const saveBtn = document.createElement("button");
        saveBtn.className = "btn btn-ghost btn-sm";
        saveBtn.textContent = "Save";
        saveBtn.addEventListener("click", saveCookies);
        const clearBtn = document.createElement("button");
        clearBtn.className = "btn btn-ghost btn-sm";
        clearBtn.textContent = "Clear";
        clearBtn.addEventListener("click", clearCookies);
        const saveStatus = document.createElement("span");
        saveStatus.id = "cookies-save-status";
        saveStatus.style.fontSize = "0.75rem";
        saveStatus.style.color = "var(--color-text-faint)";
        submitRow.append(saveBtn, clearBtn, saveStatus);
        host.append(heading, hint, status, textarea, submitRow);
        loadCookiesStatus();
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
function numberField(id, label, min, max, onSave) {
  const field = document.createElement("div");
  field.className = "field field-num";
  const lbl = document.createElement("label");
  lbl.htmlFor = id;
  lbl.textContent = label;
  const input = document.createElement("input");
  input.type = "number";
  input.id = id;
  input.min = String(min);
  input.max = String(max);
  input.addEventListener("change", () => {
    const v = parseInt(input.value, 10);
    if (Number.isFinite(v) && v >= min && v <= max) void onSave(v);
  });
  field.append(lbl, input);
  return field;
}
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
    void refreshJobs();
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
function jobSection(j) {
  if (j.status === "queued") return "upnext";
  if (j.status === "running") return "running";
  return "recent";
}
function jobMeta(j) {
  const ep = (j.season > 0 ? "S" + String(j.season).padStart(2, "0") : "") + (j.episode > 0 ? "E" + String(j.episode).padStart(2, "0") : "");
  const mode = j.mode || "video";
  return [ep, mode].filter(Boolean).join(" \xB7 ");
}
function jobProgress(j) {
  return j.status === "running" ? j.progress : null;
}
function jobActions(j) {
  switch (j.status) {
    case "queued":
      return ["cancel", "remove"];
    case "running":
      return ["cancel"];
    case "failed":
    case "canceled":
      return ["rerun", "remove"];
    default:
      return ["remove"];
  }
}
async function onJobAction(action, j) {
  if (action === "cancel") {
    const ok = await confirmDialog(
      `Cancel "${j.label}"?`,
      { title: "Cancel download", confirmLabel: "Cancel download" }
    );
    if (!ok) return;
    await postAction("/jobs/cancel?id=" + encodeURIComponent(j.id), "cancel");
  } else if (action === "rerun") {
    await postAction("/jobs/rerun?id=" + encodeURIComponent(j.id), "rerun");
  } else if (action === "remove") {
    const queued = j.status === "queued";
    const ok = await confirmDialog(
      queued ? `Remove "${j.label}" from the queue? It won't be downloaded.` : `Remove "${j.label}" from the list? Any downloaded file is kept.`,
      { title: queued ? "Remove queued download" : "Remove from list", confirmLabel: "Remove" }
    );
    if (!ok) return;
    await postAction("/jobs/delete?id=" + encodeURIComponent(j.id), "remove");
  }
  void refreshJobs();
}
async function postAction(url, kind) {
  let res;
  try {
    res = await fetch(url, { method: "POST" });
  } catch {
    showToast("Could not reach the server.", "error");
    return;
  }
  if (res.status === 409) {
    const msg = (await res.text().catch(() => "")).trim();
    showToast(msg || "That download has already changed state.", "notice");
  } else if (!res.ok && res.status !== 404) {
    showToast(`Could not ${kind} it.`, "error");
  }
}
function decorateJobRow(row, j) {
  row.querySelector(".utuber-links")?.remove();
  row.querySelector(".utuber-error")?.remove();
  if (j.status === "success" && j.output_file) {
    const href = "/downloads/" + encodeURIComponent(j.output_file);
    const wrap = document.createElement("div");
    wrap.className = "utuber-links";
    const play = document.createElement("a");
    play.className = "download-link";
    play.href = href;
    play.target = "_blank";
    play.rel = "noopener";
    play.textContent = "Play";
    const dl = document.createElement("a");
    dl.className = "download-link";
    dl.href = href;
    dl.setAttribute("download", "");
    dl.textContent = "Download";
    wrap.append(play, dl);
    row.appendChild(wrap);
  } else if (j.status === "failed" && j.error) {
    const err = document.createElement("div");
    err.className = "utuber-error";
    err.textContent = "\u26A0 " + j.error;
    row.appendChild(err);
  }
}
var jobsAdapter = {
  key: (j) => j.id,
  section: jobSection,
  title: (j) => j.label,
  status: (j) => j.status,
  meta: jobMeta,
  progress: jobProgress,
  actions: jobActions,
  onAction: onJobAction,
  decorate: decorateJobRow
};
var jobsHost = document.getElementById("jobs-list");
jobsHost.innerHTML = "";
var jobsPanel = new QueuePanel(jobsHost, jobsAdapter, {
  header: { paused: false, onTogglePause: togglePause }
});
async function refreshJobs() {
  let jobs;
  try {
    const res = await fetch("/jobs.json");
    jobs = await res.json();
  } catch {
    return;
  }
  jobs = jobs || [];
  jobsPanel.update(jobs);
  const countEl = document.getElementById("jobs-count");
  countEl.textContent = jobs.length ? jobs.length + (jobs.length === 1 ? " job" : " jobs") : "";
}
async function refreshQueueState() {
  let s;
  try {
    const res = await fetch("/settings.json");
    s = await res.json();
  } catch {
    return;
  }
  const braked = s.brake_engaged === true || s.queue_paused_by === "brake";
  const note = braked ? "Paused by the taskmaster hand brake" : void 0;
  jobsPanel.setPaused(!!s.queue_paused, note);
  syncQueueMenu(s);
}
async function togglePause() {
  let s;
  try {
    const res = await fetch("/settings.json");
    s = await res.json();
  } catch {
    return;
  }
  if (s.brake_engaged === true || s.queue_paused_by === "brake") return;
  await saveQueueSetting({ queue_paused: !s.queue_paused });
  void refreshQueueState();
}
function syncQueueMenu(s) {
  const age = document.getElementById("age-out-days");
  if (age && document.activeElement !== age && s.age_out_days !== void 0) age.value = String(s.age_out_days);
  const conc = document.getElementById("concurrent-downloads");
  if (conc && document.activeElement !== conc && s.concurrent_downloads !== void 0) conc.value = String(s.concurrent_downloads);
  const show = document.getElementById("show-in-taskmaster");
  if (show && s.show_in_taskmaster !== void 0) show.checked = s.show_in_taskmaster;
}
async function loadQueueSettings() {
  try {
    const res = await fetch("/settings.json");
    syncQueueMenu(await res.json());
  } catch {
  }
}
async function saveQueueSetting(patch) {
  const status = document.getElementById("queue-settings-status");
  try {
    const res = await fetch("/settings.json", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch)
    });
    if (res.ok) {
      if (status) {
        status.style.color = "var(--color-success)";
        status.textContent = "Saved.";
      }
      void refreshQueueState();
    } else if (status) {
      status.style.color = "var(--color-danger)";
      status.textContent = (await res.text().catch(() => "")).trim() || "Save failed.";
    }
  } catch {
    if (status) {
      status.style.color = "var(--color-danger)";
      status.textContent = "Connection error.";
    }
  }
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
function cookieAgeText(s) {
  if (!s.cookies_configured) return "No cookie file configured.";
  if (!s.cookies_updated_at) return "Cookie file configured.";
  const updated = new Date(s.cookies_updated_at);
  if (Number.isNaN(updated.getTime())) return "Cookie file configured.";
  const ageDays = Math.floor((Date.now() - updated.getTime()) / 864e5);
  const ageText = ageDays <= 0 ? "today" : ageDays === 1 ? "1 day ago" : `${ageDays} days ago`;
  const stale = ageDays >= 14 ? " \u2014 likely stale; YouTube rotates these, re-export if downloads start failing." : "";
  return `Cookie file saved ${ageText}.${stale}`;
}
async function loadCookiesStatus() {
  const status = document.getElementById("cookies-status");
  if (!status) return;
  try {
    const res = await fetch("/settings.json");
    const data = await res.json();
    status.textContent = cookieAgeText(data);
  } catch {
    status.textContent = "Connection error.";
  }
}
async function saveCookies() {
  const textarea = document.getElementById("cookies-txt");
  const saveStatus = document.getElementById("cookies-save-status");
  try {
    const res = await fetch("/settings.json", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ cookies_txt: textarea.value })
    });
    if (res.ok) {
      const data = await res.json();
      textarea.value = "";
      if (saveStatus) {
        saveStatus.style.color = "var(--color-success)";
        saveStatus.textContent = "Saved.";
      }
      const status = document.getElementById("cookies-status");
      if (status) status.textContent = cookieAgeText(data);
    } else if (saveStatus) {
      saveStatus.style.color = "var(--color-danger)";
      saveStatus.textContent = (await res.text().catch(() => "")).trim() || "Save failed.";
    }
  } catch {
    if (saveStatus) {
      saveStatus.style.color = "var(--color-danger)";
      saveStatus.textContent = "Connection error.";
    }
  }
}
async function clearCookies() {
  const textarea = document.getElementById("cookies-txt");
  textarea.value = "";
  await saveCookies();
}
document.querySelectorAll(".mode-tab").forEach((tab) => {
  tab.addEventListener("click", () => setMode(tab.dataset.mode));
});
setInterval(() => {
  void refreshJobs();
  void refreshQueueState();
}, 2e3);
void refreshJobs();
void refreshQueueState();
