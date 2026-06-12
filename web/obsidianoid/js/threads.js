"use strict";
window.ThreadsView = /* @__PURE__ */ (function() {
  let threads = [];
  let editingIndex = null;
  let draftContent = "";
  const renderCache = /* @__PURE__ */ new Map();
  function vaultParam() {
    const sel = document.getElementById("vault-selector");
    return sel ? `vault=${sel.value}` : "vault=0";
  }
  async function fetchThreads() {
    const res = await fetch(`/api/threads?${vaultParam()}`);
    if (!res.ok) throw new Error(`GET /api/threads: ${res.status}`);
    return res.json();
  }
  async function saveThreads(ts) {
    const res = await fetch(`/api/threads?${vaultParam()}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(ts)
    });
    if (!res.ok) throw new Error(`PUT /api/threads: ${res.status}`);
  }
  async function renderMarkdown(content) {
    if (!content.trim()) {
      return '<span class="md-empty">No content \u2014 click Edit to add.</span>';
    }
    if (renderCache.has(content)) return renderCache.get(content);
    try {
      const res = await fetch("/api/render", {
        method: "POST",
        headers: { "Content-Type": "text/plain" },
        body: content
      });
      const html = await res.text();
      renderCache.set(content, html);
      return html;
    } catch {
      return '<em style="color:var(--color-error)">Render failed</em>';
    }
  }
  function escapeHtml(str) {
    return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }
  function renderCard(index, bodyHtml) {
    const thread = threads[index];
    const isEditing = editingIndex === index;
    const isDisabled = thread.disabled;
    const label = `THREAD_${String(index + 1).padStart(2, "0")}`;
    const editButtons = isEditing ? `<button class="card-btn btn-save" data-action="save" data-index="${index}">Save</button>
         <button class="card-btn btn-cancel" data-action="cancel" data-index="${index}">Cancel</button>` : `<button class="card-btn btn-edit" data-action="edit" data-index="${index}"${isDisabled ? " disabled" : ""}>Edit</button>`;
    const body = isEditing ? `<textarea class="card-editor" id="thread-editor-${index}" data-index="${index}">${escapeHtml(draftContent)}</textarea>` : `<div class="md-render">${bodyHtml}</div>
         ${isDisabled ? '<div class="disabled-overlay">\u2014 DISABLED \u2014</div>' : ""}`;
    return `
      <div class="thread-card${isDisabled ? " disabled" : ""}" data-card="${index}">
        <div class="card-header">
          <span class="card-label">${label}</span>
          <div class="card-header-spacer"></div>
          ${editButtons}
          <div class="toggle-wrap">
            <span class="toggle-label">${isDisabled ? "OFF" : "ON"}</span>
            <label class="toggle">
              <input type="checkbox"${!isDisabled ? " checked" : ""} data-action="toggle" data-index="${index}" />
              <span class="toggle-track"></span>
              <span class="toggle-thumb"></span>
            </label>
          </div>
        </div>
        <div class="card-body">
          ${body}
        </div>
      </div>`;
  }
  async function renderApp() {
    const grid = document.getElementById("threads-grid");
    if (!grid) return;
    const htmlParts = await Promise.all(
      threads.map(
        (t, i) => i === editingIndex ? Promise.resolve("") : renderMarkdown(t.content)
      )
    );
    grid.innerHTML = threads.map((_, i) => renderCard(i, htmlParts[i])).join("");
    if (editingIndex !== null) {
      const ta = document.getElementById(`thread-editor-${editingIndex}`);
      if (ta) {
        ta.focus();
        ta.setSelectionRange(ta.value.length, ta.value.length);
      }
    }
  }
  function captureDraft() {
    if (editingIndex === null) return;
    const ta = document.getElementById(`thread-editor-${editingIndex}`);
    if (ta) draftContent = ta.value;
  }
  function handleClick(e) {
    const btn = e.target.closest("[data-action]");
    if (!btn) return;
    const action = btn.dataset.action;
    const index = parseInt(btn.dataset.index ?? "0", 10);
    if (action === "edit") {
      if (threads[index].disabled) return;
      if (editingIndex !== null && editingIndex !== index) {
        captureDraft();
        const oldContent = threads[editingIndex].content;
        threads[editingIndex].content = draftContent;
        renderCache.delete(oldContent);
        saveThreads(threads).catch(() => showToast("Save failed", "error"));
      }
      editingIndex = index;
      draftContent = threads[index].content;
      renderApp();
      return;
    }
    if (action === "save") {
      const ta = document.getElementById(`thread-editor-${index}`);
      if (ta) draftContent = ta.value;
      renderCache.delete(threads[index].content);
      threads[index].content = draftContent;
      editingIndex = null;
      draftContent = "";
      renderApp();
      saveThreads(threads).then(() => showToast("Saved")).catch(() => showToast("Save failed", "error"));
      return;
    }
    if (action === "cancel") {
      editingIndex = null;
      draftContent = "";
      renderApp();
      return;
    }
  }
  function handleChange(e) {
    const target = e.target;
    if (target.dataset.action !== "toggle") return;
    const index = parseInt(target.dataset.index ?? "0", 10);
    if (editingIndex === index) {
      editingIndex = null;
      draftContent = "";
    }
    threads[index].disabled = !target.checked;
    renderApp();
    saveThreads(threads).then(() => showToast(threads[index].disabled ? "Thread disabled" : "Thread enabled")).catch(() => showToast("Save failed", "error"));
  }
  function init() {
    document.addEventListener("click", handleClick);
    document.addEventListener("change", handleChange);
    document.addEventListener("keydown", captureDraft, { capture: true });
  }
  async function activate() {
    editingIndex = null;
    draftContent = "";
    const grid = document.getElementById("threads-grid");
    if (grid) grid.innerHTML = '<div class="threads-loading">Loading\u2026</div>';
    try {
      threads = await fetchThreads();
    } catch {
      if (grid) grid.innerHTML = '<div class="threads-error">Failed to load threads.</div>';
      return;
    }
    await renderApp();
  }
  async function flush() {
    if (editingIndex === null) return;
    captureDraft();
    const oldContent = threads[editingIndex].content;
    threads[editingIndex].content = draftContent;
    renderCache.delete(oldContent);
    editingIndex = null;
    draftContent = "";
    await saveThreads(threads);
  }
  return { init, activate, flush };
})();
