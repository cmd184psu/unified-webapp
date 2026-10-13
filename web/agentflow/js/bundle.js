// web/agentflow/js/api.ts
import { alertDialog } from "/shared/dist/shared.mjs";
async function apiFetch(path, options = {}) {
  const headers = { "Content-Type": "application/json" };
  const existingHeaders = options.headers;
  if (existingHeaders) {
    Object.assign(headers, existingHeaders);
  }
  let resp;
  try {
    resp = await fetch(path, { ...options, headers });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    void alertDialog("Network error contacting the server: " + msg);
    throw err;
  }
  if (resp.status === 401) {
    window.location.reload();
    throw new Error("unauthorized");
  }
  if (!resp.ok) {
    let errMsg = resp.statusText;
    try {
      const body = await resp.json();
      if (body.error) errMsg = body.error;
    } catch {
    }
    void alertDialog("Request failed: " + errMsg);
    throw new Error(errMsg);
  }
  if (resp.status === 204) return void 0;
  return resp.json();
}
var api = {
  queue() {
    return apiFetch("/queue");
  },
  progress() {
    return apiFetch("/progress");
  },
  approveTask(id) {
    return apiFetch(`/tasks/${encodeURIComponent(id)}/approve`, {
      method: "POST"
    });
  },
  repairTask(id) {
    return apiFetch(`/tasks/${encodeURIComponent(id)}/repair`, {
      method: "POST"
    });
  },
  failTask(id) {
    return apiFetch(`/tasks/${encodeURIComponent(id)}/fail`, {
      method: "POST"
    });
  },
  runNext() {
    return apiFetch("/run-next", { method: "POST" });
  }
};

// web/agentflow/js/main.ts
import { ThemeManager, HamburgerMenu, alertDialog as alertDialog6 } from "/shared/dist/shared.mjs";

// web/agentflow/js/frd-wizard.ts
import { alertDialog as alertDialog2 } from "/shared/dist/shared.mjs";
var frdData = {
  id: "frd-001",
  title: "",
  objective: "",
  requirements: [],
  acceptance_criteria: [],
  constraints: [],
  context: ""
};
var currentStepIndex = 0;
var steps = [
  {
    id: "objective",
    title: "Objective",
    render: (host, data) => {
      host.innerHTML = `
        <div class="wizard-field">
          <label for="frd-id">FRD ID</label>
          <input type="text" id="frd-id" value="${data.id}" pattern="^frd-d{3,}$" required>
          <span class="field-hint">Format: frd-001, frd-100, etc.</span>
        </div>
        <div class="wizard-field">
          <label for="frd-title">Title</label>
          <input type="text" id="frd-title" value="${data.title}" maxlength="120" required placeholder="One-line summary">
        </div>
        <div class="wizard-field">
          <label for="frd-objective">Objective</label>
          <textarea id="frd-objective" rows="4" required placeholder="One paragraph: what and why">${data.objective}</textarea>
        </div>
        <div class="wizard-field">
          <label for="frd-context">Context (optional)</label>
          <textarea id="frd-context" rows="3" placeholder="Background info, links, prior art">${data.context}</textarea>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-next">Next</button>
        </div>
      `;
      const titleEl = host.querySelector("#frd-title");
      const objEl = host.querySelector("#frd-objective");
      const ctxEl = host.querySelector("#frd-context");
      const idEl = host.querySelector("#frd-id");
      titleEl.addEventListener("input", () => data.title = titleEl.value);
      objEl.addEventListener("input", () => data.objective = objEl.value);
      ctxEl.addEventListener("input", () => data.context = ctxEl.value);
      idEl.addEventListener("input", () => data.id = idEl.value);
      host.querySelector("#wizard-next")?.addEventListener("click", () => {
        const err = steps[currentStepIndex].validate(data);
        if (err) {
          alertDialog2(err);
          return;
        }
        if (steps[currentStepIndex].next) goToStep(steps[currentStepIndex].next);
      });
    },
    validate: (data) => {
      if (!data.id.match(/^frd-\d{3,}$/)) return "FRD ID must match frd-NNN format";
      if (!data.title.trim()) return "Title is required";
      if (!data.objective.trim()) return "Objective is required";
      return null;
    },
    next: "requirements"
  },
  {
    id: "requirements",
    title: "Requirements",
    render: (host, data) => {
      host.innerHTML = `
        <div class="wizard-list">
          <div class="list-header">
            <h4>Requirements</h4>
            <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="add-req">Add Requirement</button>
          </div>
          <div id="req-list" class="req-list"></div>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn" id="wizard-back">Back</button>
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-next">Next</button>
        </div>
      `;
      renderRequirementList(host, data);
      host.querySelector("#add-req")?.addEventListener("click", () => {
        data.requirements.push({ id: nextReqId(data.requirements), description: "", priority: "must" });
        renderRequirementList(host, data);
      });
      host.querySelector("#wizard-back")?.addEventListener("click", () => goToStep("objective"));
      host.querySelector("#wizard-next")?.addEventListener("click", () => {
        const err = steps[currentStepIndex].validate(data);
        if (err) {
          alertDialog2(err);
          return;
        }
        if (steps[currentStepIndex].next) goToStep(steps[currentStepIndex].next);
      });
    },
    validate: (data) => {
      if (data.requirements.length === 0) return "At least one requirement is required";
      for (const r of data.requirements) {
        if (!r.description.trim()) return "All requirements must have a description";
        if (!r.id.match(/^R\d+$/)) return `Requirement ID ${r.id} must match R1, R2... format`;
      }
      return null;
    },
    next: "acceptance"
  },
  {
    id: "acceptance",
    title: "Acceptance Criteria",
    render: (host, data) => {
      host.innerHTML = `
        <div class="wizard-list">
          <div class="list-header">
            <h4>Acceptance Criteria</h4>
            <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="add-ac">Add Criterion</button>
          </div>
          <div id="ac-list" class="ac-list"></div>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn" id="wizard-back">Back</button>
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-next">Next</button>
        </div>
      `;
      renderACList(host, data);
      host.querySelector("#add-ac")?.addEventListener("click", () => {
        data.acceptance_criteria.push({ id: nextACId(data.acceptance_criteria), criterion: "" });
        renderACList(host, data);
      });
      host.querySelector("#wizard-back")?.addEventListener("click", () => goToStep("requirements"));
      host.querySelector("#wizard-next")?.addEventListener("click", () => {
        const err = steps[currentStepIndex].validate(data);
        if (err) {
          alertDialog2(err);
          return;
        }
        if (steps[currentStepIndex].next) goToStep(steps[currentStepIndex].next);
      });
    },
    validate: (data) => {
      if (data.acceptance_criteria.length === 0) return "At least one acceptance criterion is required";
      for (const ac of data.acceptance_criteria) {
        if (!ac.criterion.trim()) return "All criteria must have a description";
        if (!ac.id.match(/^AC\d+$/)) return `Criterion ID ${ac.id} must match AC1, AC2... format`;
      }
      return null;
    },
    next: "constraints"
  },
  {
    id: "constraints",
    title: "Constraints",
    render: (host, data) => {
      host.innerHTML = `
        <div class="wizard-field">
          <label>Constraints (one per line)</label>
          <textarea id="frd-constraints" rows="6" placeholder="Go standard library only; no new external dependencies.&#10;Queue and plan JSON formats are byte-compatible...">${data.constraints.join("\n")}</textarea>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn" id="wizard-back">Back</button>
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-next">Next</button>
        </div>
      `;
      const ta = host.querySelector("#frd-constraints");
      ta.addEventListener("input", () => {
        data.constraints = ta.value.split("\n").map((s) => s.trim()).filter((s) => s);
      });
      host.querySelector("#wizard-back")?.addEventListener("click", () => goToStep("acceptance"));
      host.querySelector("#wizard-next")?.addEventListener("click", () => {
        const err = steps[currentStepIndex].validate(data);
        if (err) {
          alertDialog2(err);
          return;
        }
        if (steps[currentStepIndex].next) goToStep(steps[currentStepIndex].next);
      });
    },
    validate: () => null,
    next: "review"
  },
  {
    id: "review",
    title: "Review & Submit",
    render: (host, data) => {
      host.innerHTML = `
        <div class="review-section">
          <h4>FRD ID</h4>
          <pre>${data.id}</pre>
        </div>
        <div class="review-section">
          <h4>Title</h4>
          <pre>${data.title}</pre>
        </div>
        <div class="review-section">
          <h4>Objective</h4>
          <pre>${data.objective}</pre>
        </div>
        <div class="review-section">
          <h4>Requirements (${data.requirements.length})</h4>
          <pre>${data.requirements.map((r) => `${r.id}: ${r.description} [${r.priority}]`).join("\n")}</pre>
        </div>
        <div class="review-section">
          <h4>Acceptance Criteria (${data.acceptance_criteria.length})</h4>
          <pre>${data.acceptance_criteria.map((ac) => `${ac.id}: ${ac.criterion}`).join("\n")}</pre>
        </div>
        <div class="review-section">
          <h4>Constraints (${data.constraints.length})</h4>
          <pre>${data.constraints.join("\n")}</pre>
        </div>
        <div class="review-section">
          <h4>Context</h4>
          <pre>${data.context || "(none)"}</pre>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn" id="wizard-back">Back</button>
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-submit">Submit FRD</button>
        </div>
      `;
      host.querySelector("#wizard-back")?.addEventListener("click", () => goToStep("constraints"));
      host.querySelector("#wizard-submit")?.addEventListener("click", () => submitFRD(data));
    },
    validate: () => null
  }
];
function nextReqId(arr) {
  const nums = arr.map((r) => parseInt(r.id.slice(1))).filter((n) => !isNaN(n));
  return "R" + (Math.max(0, ...nums) + 1);
}
function nextACId(arr) {
  const nums = arr.map((ac) => parseInt(ac.id.slice(2))).filter((n) => !isNaN(n));
  return "AC" + (Math.max(0, ...nums) + 1);
}
function renderRequirementList(host, data) {
  const list = host.querySelector("#req-list");
  list.innerHTML = data.requirements.map((r, i) => `
    <div class="req-item" data-index="${i}">
      <div class="req-header">
        <input type="text" class="req-id" value="${r.id}" readonly>
        <select class="req-priority">
          <option value="must" ${r.priority === "must" ? "selected" : ""}>Must</option>
          <option value="should" ${r.priority === "should" ? "selected" : ""}>Should</option>
          <option value="could" ${r.priority === "could" ? "selected" : ""}>Could</option>
        </select>
        <button type="button" class="icon-btn req-delete" title="Delete">\u2715</button>
      </div>
      <textarea class="req-desc" rows="2" placeholder="Description">${r.description}</textarea>
    </div>
  `).join("");
  list.querySelectorAll(".req-item").forEach((item, i) => {
    const desc = item.querySelector(".req-desc");
    const priority = item.querySelector(".req-priority");
    const del = item.querySelector(".req-delete");
    desc.addEventListener("input", () => data.requirements[i].description = desc.value);
    priority.addEventListener("change", () => data.requirements[i].priority = priority.value);
    del.addEventListener("click", () => {
      data.requirements.splice(i, 1);
      renderRequirementList(host, data);
    });
  });
}
function renderACList(host, data) {
  const list = host.querySelector("#ac-list");
  list.innerHTML = data.acceptance_criteria.map((ac, i) => `
    <div class="ac-item" data-index="${i}">
      <div class="ac-header">
        <input type="text" class="ac-id" value="${ac.id}" readonly>
        <button type="button" class="icon-btn ac-delete" title="Delete">\u2715</button>
      </div>
      <textarea class="ac-criterion" rows="2" placeholder="Criterion">${ac.criterion}</textarea>
    </div>
  `).join("");
  list.querySelectorAll(".ac-item").forEach((item, i) => {
    const criterion = item.querySelector(".ac-criterion");
    const del = item.querySelector(".ac-delete");
    criterion.addEventListener("input", () => data.acceptance_criteria[i].criterion = criterion.value);
    del.addEventListener("click", () => {
      data.acceptance_criteria.splice(i, 1);
      renderACList(host, data);
    });
  });
}
function goToStep(stepId) {
  const index = steps.findIndex((s) => s.id === stepId);
  if (index >= 0) {
    currentStepIndex = index;
    renderWizard();
  }
}
function renderWizard() {
  const host = document.getElementById("wizard-host");
  const step = steps[currentStepIndex];
  if (!host || !step) return;
  host.innerHTML = `
    <div class="wizard-progress">
      ${steps.map((s, i) => `<span class="wizard-step ${i === currentStepIndex ? "active" : ""} ${i < currentStepIndex ? "done" : ""}">${s.title}</span>`).join("")}
    </div>
    <div id="wizard-content"></div>
  `;
  step.render(document.getElementById("wizard-content"), frdData);
}
async function submitFRD(data) {
  try {
    const resp = await fetch("/frd", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(data)
    });
    if (!resp.ok) {
      const err = await resp.json();
      throw new Error(err.error || "Failed to save FRD");
    }
    await alertDialog2("FRD saved successfully");
    location.hash = "#plan";
  } catch (err) {
    await alertDialog2("Failed to save FRD: " + err.message);
  }
}
function initFRDWizard() {
  const container = document.getElementById("frd-wizard");
  if (!container) return;
  container.innerHTML = `
    <div id="wizard-host"></div>
  `;
  currentStepIndex = 0;
  frdData = {
    id: "frd-001",
    title: "",
    objective: "",
    requirements: [],
    acceptance_criteria: [],
    constraints: [],
    context: ""
  };
  renderWizard();
}

// web/agentflow/js/plan-gen.ts
import { confirmDialog as confirmDialog2, alertDialog as alertDialog3 } from "/shared/dist/shared.mjs";
var planData = null;
var reviewRound = 0;
var maxReviewRounds = 3;
async function generatePlan(frd) {
  try {
    const resp = await fetch("/plan", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ frd })
    });
    if (!resp.ok) throw new Error("Failed to generate plan");
    planData = await resp.json();
    planData.status = "draft";
    reviewRound = 0;
    renderPlan();
  } catch (err) {
    await alertDialog3("Failed to generate plan: " + err.message);
  }
}
async function reviewPlan() {
  if (!planData) throw new Error("No plan to review");
  const resp = await fetch("/plan/review", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ plan: planData })
  });
  if (!resp.ok) throw new Error("Review failed");
  return resp.json();
}
async function runReviewLoop() {
  while (reviewRound < maxReviewRounds) {
    reviewRound++;
    const result = await reviewPlan();
    if (result.verdict === "pass") {
      planData.status = "approved";
      planData.review_notes = result.issues.map((i) => ({
        description: i.description,
        severity: i.severity
      }));
      await alertDialog3(`Plan approved after ${reviewRound} review round(s)`);
      renderPlan();
      return;
    }
    if (result.verdict === "fail") {
      await alertDialog3("Plan review failed: " + result.summary);
      planData.status = "draft";
      renderPlan();
      return;
    }
    const issuesText = result.issues.map((i) => `[${i.severity}] ${i.description}`).join("\n");
    const proceed = await confirmDialog2(
      `Review round ${reviewRound} found issues:

${issuesText}

Repair prompt:
${result.repair_prompt}

Continue with repair?`,
      { title: "Plan Review - Repair Required", confirmLabel: "Repair & Continue" }
    );
    if (!proceed) {
      planData.status = "draft";
      renderPlan();
      return;
    }
    await regeneratePlan(result.repair_prompt);
  }
  await alertDialog3("Max review rounds reached. Plan needs manual intervention.");
  planData.status = "draft";
  renderPlan();
}
async function regeneratePlan(repairPrompt) {
  if (!planData) return;
  try {
    const resp = await fetch("/plan", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ...planData, repair_prompt: repairPrompt })
    });
    if (!resp.ok) throw new Error("Failed to regenerate plan");
    planData = await resp.json();
    renderPlan();
  } catch (err) {
    await alertDialog3("Failed to regenerate plan: " + err.message);
  }
}
async function compileTasks() {
  if (!planData || planData.status !== "approved") {
    await alertDialog3("Plan must be approved before compiling tasks");
    return;
  }
  try {
    const resp = await fetch("/tasks/compile", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ plan: planData })
    });
    if (!resp.ok) throw new Error("Failed to compile tasks");
    await alertDialog3("Tasks compiled successfully");
    location.hash = "#queue";
  } catch (err) {
    await alertDialog3("Failed to compile tasks: " + err.message);
  }
}
function renderPlan() {
  const host = document.getElementById("plan-host");
  if (!host) return;
  if (!planData) {
    host.innerHTML = `
      <div class="plan-empty">
        <h3>No Plan Yet</h3>
        <p>Complete the FRD wizard first, then generate a plan.</p>
        <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="gen-plan-btn">Generate Plan</button>
      </div>
    `;
    host.querySelector("#gen-plan-btn")?.addEventListener("click", async () => {
      try {
        const r = await fetch("/frd");
        if (!r.ok) {
          throw new Error("No FRD found. Please complete the FRD wizard first.");
        }
        const frd = await r.json();
        await generatePlan(frd);
      } catch (e) {
        alertDialog3("Error: " + e.message);
      }
    });
    return;
  }
  host.innerHTML = `
    <div class="plan-header">
      <div class="plan-meta">
        <div class="plan-status ${planData.status}">${planData.status.toUpperCase()}</div>
        <div class="plan-frd-id">FRD: ${planData.frd_id}</div>
        ${planData.review_notes && planData.review_notes.length > 0 ? `
          <div class="plan-review-notes">
            <strong>Review Notes:</strong>
            <ul>${planData.review_notes.map((n) => `<li>[${n.severity}] ${n.description}</li>`).join("")}</ul>
          </div>
        ` : ""}
      </div>
      <div class="plan-actions">
        ${planData.status === "draft" ? `
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="review-plan-btn">Start Review</button>
        ` : ""}
        ${planData.status === "approved" ? `
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="compile-tasks-btn">Compile Tasks</button>
        ` : ""}
        ${planData.status !== "approved" ? `
          <button type="button" class="ui-modal-btn" id="regen-plan-btn">Regenerate</button>
        ` : ""}
      </div>
    </div>

    <div class="plan-content">
      <section class="plan-section">
        <h4>Approach</h4>
        <pre>${planData.approach}</pre>
      </section>

      <section class="plan-section">
        <h4>Assumptions</h4>
        <ul>${planData.assumptions.map((a) => `<li>${a}</li>`).join("") || "<li>(none)</li>"}</ul>
      </section>

      <section class="plan-section">
        <h4>Risks</h4>
        ${planData.risks.length > 0 ? `
          <ul>${planData.risks.map((r) => `<li><strong>${r.risk}</strong> \u2014 ${r.mitigation}</li>`).join("")}</ul>
        ` : "<p>(none)</p>"}
      </section>

      <section class="plan-section">
        <h4>Phases (${planData.phases.length}, ~${planData.estimated_tasks} tasks)</h4>
        <div class="phase-list">
          ${planData.phases.map((p) => `
            <div class="phase-card">
              <div class="phase-header">
                <strong>${p.id}: ${p.name}</strong>
                ${p.depends_on.length > 0 ? `<span class="phase-deps">depends on: ${p.depends_on.join(", ")}</span>` : ""}
              </div>
              <div class="phase-desc">${p.tasks_description}</div>
            </div>
          `).join("")}
        </div>
      </section>
    </div>
  `;
  host.querySelector("#review-plan-btn")?.addEventListener("click", runReviewLoop);
  host.querySelector("#compile-tasks-btn")?.addEventListener("click", compileTasks);
  host.querySelector("#regen-plan-btn")?.addEventListener("click", () => {
    if (planData) generatePlan(planData);
  });
}
async function initPlanGen() {
  const container = document.getElementById("plan-gen");
  if (!container) return;
  container.innerHTML = `<div id="plan-host"></div>`;
  try {
    const resp = await fetch("/plan");
    if (resp.ok) {
      planData = await resp.json();
    }
  } catch {
    planData = null;
  }
  renderPlan();
}

// web/agentflow/js/task-compiler.ts
import { alertDialog as alertDialog4 } from "/shared/dist/shared.mjs";
function initTaskCompiler() {
  const container = document.getElementById("task-compiler");
  if (!container) return;
  container.innerHTML = `
    <div class="compiler-panel">
      <h3>Task Compilation</h3>
      <p>Convert the approved plan into atomic tasks. Each phase becomes one or more tasks with prompts, verify commands, and acceptance criteria.</p>
      <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="compile-btn">Compile Tasks</button>
      <div id="compile-output" class="compile-output"></div>
    </div>
  `;
  const btn = document.getElementById("compile-btn");
  btn?.addEventListener("click", async () => {
    const output = document.getElementById("compile-output");
    output.textContent = "Compiling...";
    try {
      const resp = await fetch("/tasks/compile", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({})
      });
      if (!resp.ok) {
        const err = await resp.json();
        throw new Error(err.error || "Compilation failed");
      }
      const result = await resp.json();
      let out = "\u2705 Compilation successful\n\n";
      out += `Tasks created: ${result.queue.tasks?.length || 0}
`;
      if (result.warnings.length > 0) {
        out += "\n\u26A0\uFE0F Warnings:\n" + result.warnings.map((w) => "  - " + w).join("\n");
      }
      output.textContent = out;
    } catch (err) {
      output.textContent = "\u274C " + err.message;
    }
  });
}

// web/agentflow/js/queue-dashboard.ts
function initQueueDashboard() {
  const panel = document.getElementById("panel-queue");
  if (panel && !panel.querySelector("#task-list")) {
    panel.innerHTML = `
      <div class="console-header" id="console-header">
        <div class="progress-bar-wrap">
          <div class="progress-bar" id="progress-fill"></div>
        </div>
        <span class="progress-pct" id="progress-pct">0%</span>
      </div>
      <div class="console-body">
        <div class="console-left">
          <h2 class="pane-title">Queue</h2>
          <div id="task-list"></div>
        </div>
        <aside class="console-right" id="detail-pane" hidden>
          <div class="detail-body">
            <div class="detail-header">
              <h3 id="detail-title"></h3>
              <span class="status-chip" id="detail-status"></span>
              <button class="icon-btn detail-close-btn" id="detail-close" title="Clear selection">
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 2l10 10M12 2L2 12"/></svg>
              </button>
            </div>
            <div class="detail-section">
              <h4>Status</h4>
              <span class="status-chip" id="detail-status-repeated"></span>
              <span class="detail-attempts" id="detail-attempts"></span>
            </div>
            <div class="detail-section" data-panid="actions">
              <h4>Actions</h4>
              <button class="action-btn ui-modal-btn ui-modal-btn-primary" id="action-approve" type="button">Approve</button>
              <button class="action-btn ui-modal-btn" id="action-repair" type="button">Repair</button>
              <button class="action-btn ui-modal-btn" id="action-fail" type="button">Fail</button>
              <button class="action-btn ui-modal-btn" id="action-run-next" type="button">Run next</button>
            </div>
            <div class="detail-section" data-panid="prompt">
              <h4>Prompt</h4>
              <pre class="detail-mono"><code id="detail-prompt"></code></pre>
            </div>
            <div class="detail-section" data-panid="acceptance">
              <h4>Acceptance Criteria</h4>
              <pre class="detail-mono"><code id="detail-acceptance"></code></pre>
            </div>
            <div class="detail-section" data-panid="verify">
              <h4>Verify Command</h4>
              <pre class="detail-mono"><code id="detail-verify"></code></pre>
            </div>
            <div class="detail-section" data-panid="last-review">
              <h4>Last Review</h4>
              <span class="status-chip" id="detail-verdict"></span>
              <p id="detail-review-summary"></p>
            </div>
          </div>
        </aside>
        <aside class="console-right" id="stream-panel" hidden></aside>
      </div>
    `;
  }
}

// web/agentflow/js/settings.ts
import { alertDialog as alertDialog5, confirmDialog as confirmDialog4 } from "/shared/dist/shared.mjs";
var settings = null;
async function loadSettings() {
  try {
    const resp = await fetch("/settings");
    if (resp.ok) {
      settings = await resp.json();
    }
  } catch {
    settings = {
      default_profile: "m5",
      profiles: [],
      prompt_dir: "./prompts",
      timeout_secs: 300
    };
  }
}
function renderSettings() {
  const host = document.getElementById("settings-host");
  if (!host) return;
  if (!settings) {
    host.innerHTML = "<p>Loading settings...</p>";
    return;
  }
  host.innerHTML = `
    <div class="settings-section">
      <h4>Default Profile</h4>
      <select id="default-profile" class="settings-select">
        ${settings.profiles.map((p) => `
          <option value="${p.name}" ${p.name === settings.default_profile ? "selected" : ""}>${p.name}</option>
        `).join("")}
      </select>
    </div>

    <div class="settings-section">
      <div class="section-header">
        <h4>Model Profiles</h4>
        <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="add-profile">Add Profile</button>
      </div>
      <div id="profile-list" class="profile-list">
        ${settings.profiles.map((p) => renderProfileCard(p)).join("")}
      </div>
    </div>

    <div class="settings-section">
      <h4>Prompt Directory</h4>
      <input type="text" id="prompt-dir" class="settings-input" value="${settings.prompt_dir || "./prompts"}">
    </div>

    <div class="settings-section">
      <h4>Default Timeout (seconds)</h4>
      <input type="number" id="timeout-secs" class="settings-input" value="${settings.timeout_secs || 300}" min="30" max="3600">
    </div>

    <div class="settings-actions">
      <button type="button" class="ui-modal-btn" id="reset-settings">Reset to Defaults</button>
      <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="save-settings">Save Settings</button>
    </div>
  `;
  const profileSelect = document.getElementById("default-profile");
  profileSelect?.addEventListener("change", () => {
    settings.default_profile = profileSelect.value;
  });
  const promptDir = document.getElementById("prompt-dir");
  promptDir?.addEventListener("input", () => {
    settings.prompt_dir = promptDir.value;
  });
  const timeoutSecs = document.getElementById("timeout-secs");
  timeoutSecs?.addEventListener("input", () => {
    settings.timeout_secs = parseInt(timeoutSecs.value, 10) || 300;
  });
  document.getElementById("add-profile")?.addEventListener("click", () => openProfileEditor(null));
  document.getElementById("save-settings")?.addEventListener("click", saveSettings);
  document.getElementById("reset-settings")?.addEventListener("click", () => confirmReset());
  renderProfileList();
}
function renderProfileCard(p) {
  return `
    <div class="profile-card" data-name="${p.name}">
      <div class="profile-header">
        <strong>${p.name}</strong>
        <div class="profile-actions">
          <button type="button" class="icon-btn edit-profile" data-name="${p.name}" title="Edit">\u270E</button>
          <button type="button" class="icon-btn delete-profile" data-name="${p.name}" title="Delete">\u2715</button>
        </div>
      </div>
      <div class="profile-fields">
        <div><label>Ollama Host</label><input type="text" class="field-ollama" value="${p.ollama_host}" disabled></div>
        <div><label>Worker Model</label><input type="text" class="field-worker" value="${p.worker_model}" disabled></div>
        <div><label>Planner Model</label><input type="text" class="field-planner" value="${p.planner_model}" disabled></div>
        <div><label>Review Model</label><input type="text" class="field-review" value="${p.review_model}" disabled></div>
        <div><label>Timeout (s)</label><input type="number" class="field-timeout" value="${p.timeout_secs}" disabled></div>
        <div><label>Prompt Dir</label><input type="text" class="field-promptdir" value="${p.prompt_dir || ""}" disabled></div>
      </div>
    </div>
  `;
}
function renderProfileList() {
  const host = document.getElementById("profile-list");
  if (!host || !settings) return;
  host.innerHTML = settings.profiles.map((p) => renderProfileCard(p)).join("");
  host.querySelectorAll(".edit-profile").forEach((btn) => {
    btn.addEventListener("click", () => openProfileEditor(btn.getAttribute("data-name")));
  });
  host.querySelectorAll(".delete-profile").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const name = btn.getAttribute("data-name");
      if (await confirmDialog4(`Delete profile "${name}"?`)) {
        settings.profiles = settings.profiles.filter((p) => p.name !== name);
        if (settings.default_profile === name) {
          settings.default_profile = settings.profiles[0]?.name || "m5";
        }
        renderSettings();
      }
    });
  });
}
function openProfileEditor(name) {
  const profile = name ? settings.profiles.find((p) => p.name === name) : null;
  const isNew = !profile;
  const form = document.createElement("div");
  form.className = "profile-editor-modal";
  form.innerHTML = `
    <div class="modal-overlay"></div>
    <div class="modal-content">
      <h3>${isNew ? "Add" : "Edit"} Profile</h3>
      <form id="profile-form">
        <div class="form-field">
          <label>Profile Name</label>
          <input type="text" name="name" value="${profile?.name || ""}" ${!isNew ? "readonly" : ""} required pattern="^[a-z0-9][a-z0-9_-]{0,31}$">
        </div>
        <div class="form-field">
          <label>Ollama Host URL</label>
          <input type="url" name="ollama_host" value="${profile?.ollama_host || "http://"}" required>
        </div>
        <div class="form-field">
          <label>Worker Model</label>
          <input type="text" name="worker_model" value="${profile?.worker_model || ""}" required>
        </div>
        <div class="form-field">
          <label>Planner Model</label>
          <input type="text" name="planner_model" value="${profile?.planner_model || ""}" required>
        </div>
        <div class="form-field">
          <label>Review Model</label>
          <input type="text" name="review_model" value="${profile?.review_model || ""}" required>
        </div>
        <div class="form-field">
          <label>Timeout (seconds)</label>
          <input type="number" name="timeout_secs" value="${profile?.timeout_secs || 300}" min="30" max="3600">
        </div>
        <div class="form-field">
          <label>Prompt Directory (optional)</label>
          <input type="text" name="prompt_dir" value="${profile?.prompt_dir || ""}">
        </div>
        <div class="modal-actions">
          <button type="button" class="ui-modal-btn" id="cancel-profile">Cancel</button>
          <button type="submit" class="ui-modal-btn ui-modal-btn-primary">${isNew ? "Add" : "Save"}</button>
        </div>
      </form>
    </div>
  `;
  document.body.appendChild(form);
  form.querySelector("#cancel-profile")?.addEventListener("click", () => form.remove());
  form.querySelector("#profile-form")?.addEventListener("submit", (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    const newProfile = {
      name: fd.get("name"),
      ollama_host: fd.get("ollama_host"),
      worker_model: fd.get("worker_model"),
      planner_model: fd.get("planner_model"),
      review_model: fd.get("review_model"),
      timeout_secs: parseInt(fd.get("timeout_secs"), 10) || 300,
      prompt_dir: fd.get("prompt_dir") || ""
    };
    if (isNew) {
      if (settings.profiles.some((p) => p.name === newProfile.name)) {
        alert("Profile name already exists");
        return;
      }
      settings.profiles.push(newProfile);
    } else {
      const idx = settings.profiles.findIndex((p) => p.name === profile.name);
      if (idx >= 0) settings.profiles[idx] = newProfile;
    }
    form.remove();
    renderSettings();
  });
}
async function saveSettings() {
  try {
    const resp = await fetch("/settings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(settings)
    });
    if (!resp.ok) throw new Error("Failed to save settings");
    await alertDialog5("Settings saved");
  } catch (err) {
    await alertDialog5("Failed to save settings: " + err.message);
  }
}
async function confirmReset() {
  if (await confirmDialog4("Reset all settings to defaults? This cannot be undone.")) {
    settings = {
      default_profile: "m5",
      profiles: [
        {
          name: "m5",
          ollama_host: "http://192.168.1.239:11434",
          worker_model: "qwen3.6:35b-a3b-mxfp8",
          planner_model: "qwen3.6:35b-a3b-mxfp8",
          review_model: "mistral-small3.2",
          timeout_secs: 300
        },
        {
          name: "m4",
          ollama_host: "http://192.168.1.11:11434",
          worker_model: "qwen3.6:27b-q4_K_M",
          planner_model: "qwen3.6:27b-q4_K_M",
          review_model: "qwen2.5-coder:14b",
          timeout_secs: 300
        },
        {
          name: "windows",
          ollama_host: "http://windows.cmdhome.net:11434",
          worker_model: "qwen3-coder:30b",
          planner_model: "qwen3-coder:30b",
          review_model: "mistral-small3.2",
          timeout_secs: 300
        }
      ],
      prompt_dir: "./prompts",
      timeout_secs: 300
    };
    renderSettings();
  }
}
async function initSettings() {
  const host = document.getElementById("settings-host");
  if (!host) return;
  await loadSettings();
  renderSettings();
}

// web/agentflow/js/main.ts
var themes = new ThemeManager({ module: "agentflow", default: "dark" });
themes.apply();
var TABS = [
  { id: "frd", label: "FRD Wizard", init: initFRDWizard },
  { id: "plan", label: "Plan Gen", init: initPlanGen },
  { id: "compile", label: "Compile", init: initTaskCompiler },
  { id: "queue", label: "Queue", init: initQueueDashboard },
  { id: "settings", label: "Settings", init: initSettings }
];
var activeTab = "frd";
var selectedTaskId = null;
var tasks = [];
var STATUS_CHIP_CLASSES = {
  ready: "status-ready",
  blocked: "status-blocked",
  "in-progress": "status-in-progress",
  review: "status-review",
  repair: "status-repair",
  passed: "status-passed",
  failed: "status-failed"
};
function chipClass(status) {
  return STATUS_CHIP_CLASSES[status] || "status-ready";
}
function buildHamburger() {
  const items = [
    { section: "AgentFlow" }
  ];
  return new HamburgerMenu({
    title: "AgentFlow",
    items,
    themePicker: true,
    themes,
    side: "right"
  });
}
function initHamburger() {
  const trigger = document.getElementById("hamburger-trigger");
  if (!trigger) return;
  const hamburger = buildHamburger();
  trigger.appendChild(hamburger.trigger);
}
function initLogout() {
  const btn = document.getElementById("logout-btn");
  if (!btn) return;
  btn.removeAttribute("hidden");
  btn.addEventListener("click", () => {
    window.location.href = "/logout";
  });
}
async function hasFRD() {
  try {
    const r = await fetch("/frd");
    return r.ok;
  } catch {
    return false;
  }
}
function renderTabs() {
  const host = document.getElementById("tab-bar");
  if (!host) return;
  host.innerHTML = TABS.map((t) => `
    <button type="button" class="tab-btn ${t.id === activeTab ? "active" : ""}" data-tab="${t.id}" data-tab-id="${t.id}">
      ${t.label}
    </button>
  `).join("");
  hasFRD().then((has) => {
    document.querySelectorAll('[data-tab-id="plan"], [data-tab-id="compile"]').forEach((el) => {
      const btn = el;
      btn.disabled = !has;
      if (!has) btn.style.opacity = "0.4";
    });
  });
  host.querySelectorAll(".tab-btn").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const tabId = btn.getAttribute("data-tab");
      if ((tabId === "plan" || tabId === "compile") && !await hasFRD()) {
        await alertDialog6("Please complete the FRD wizard first before accessing this tab.");
        return;
      }
      switchTab(tabId);
    });
  });
}
async function switchTab(tabId) {
  const panel0 = document.getElementById(`panel-${tabId}`);
  if (tabId === activeTab && panel0 && !panel0.hasAttribute("hidden")) return;
  const tabConfig = TABS.find((t) => t.id === tabId);
  if (!tabConfig) return;
  activeTab = tabId;
  renderTabs();
  document.querySelectorAll(".tab-panel").forEach((p) => p.setAttribute("hidden", ""));
  const panel = document.getElementById(`panel-${tabId}`);
  if (panel) panel.removeAttribute("hidden");
  try {
    await tabConfig.init();
  } catch (err) {
    console.error(`Tab ${tabId} init failed:`, err);
    await alertDialog6(`Failed to load ${tabConfig.label}: ${err.message}`);
  }
}
async function loadConsole() {
  try {
    const [queueData, progressData] = await Promise.all([
      api.queue(),
      api.progress()
    ]);
    tasks = queueData.tasks;
    renderProgress(progressData);
    renderTaskList();
  } catch {
  }
}
function renderProgress(counts) {
  const fill = document.getElementById("progress-fill");
  const pctEl = document.getElementById("progress-pct");
  if (!fill || !pctEl) return;
  const pct = counts.total > 0 ? Math.round(counts.passed / counts.total * 100) : 0;
  fill.style.width = pct + "%";
  pctEl.textContent = pct + "% \u2014 passed " + counts.passed + "/" + counts.total + " \xB7 ready " + counts.ready + " \xB7 blocked " + counts.blocked + " \xB7 review " + counts.review + " \xB7 repair " + counts.repair + " \xB7 failed " + counts.failed;
}
function renderTaskList() {
  const host = document.getElementById("task-list");
  if (!host) return;
  host.innerHTML = "";
  if (tasks.length === 0) {
    host.textContent = "No pending tasks.";
    return;
  }
  const ul = document.createElement("ul");
  ul.className = "task-list";
  tasks.forEach((task) => {
    const li = document.createElement("li");
    const selected = task.id === selectedTaskId;
    li.className = `task-item ${chipClass(task.status)}${selected ? " selected" : ""}`;
    li.innerHTML = `<div class="task-item-title">${escHtml(task.title)}</div><div class="task-item-meta"><span class="status-chip ${chipClass(task.status)}">${escHtml(task.status)}</span><span class="task-attempts">attempt${task.attempts !== 1 ? "s" : ""}: ${task.attempts}</span></div>`;
    li.addEventListener("click", () => {
      selectedTaskId = task.id;
      renderTaskList();
      renderDetail(task);
    });
    ul.appendChild(li);
  });
  host.replaceChildren(ul);
}
function renderDetail(task) {
  const pane = document.getElementById("detail-pane");
  if (!pane) return;
  const titleEl = document.getElementById("detail-title");
  const statusEl = document.getElementById("detail-status");
  const statusRepeatedEl = document.getElementById("detail-status-repeated");
  const attemptsEl = document.getElementById("detail-attempts");
  const promptEl = document.getElementById("detail-prompt");
  const acceptanceEl = document.getElementById("detail-acceptance");
  const verifyEl = document.getElementById("detail-verify");
  const verdictChip = document.getElementById("detail-verdict");
  const summaryP = document.getElementById("detail-review-summary");
  if (titleEl) titleEl.textContent = task.title;
  if (statusEl) {
    statusEl.className = `status-chip ${chipClass(task.status)}`;
    statusEl.textContent = task.status;
  }
  if (statusRepeatedEl) {
    statusRepeatedEl.className = `status-chip ${chipClass(task.status)}`;
    statusRepeatedEl.textContent = task.status;
  }
  if (attemptsEl) attemptsEl.textContent = "Attempt" + (task.attempts !== 1 ? "s" : "") + ": " + task.attempts;
  if (promptEl) {
    if (task.prompt) {
      promptEl.textContent = task.prompt;
      promptEl.closest(".detail-section")?.style.removeProperty("display");
    } else {
      promptEl.textContent = "";
      promptEl.closest(".detail-section").style.display = "none";
    }
  }
  if (acceptanceEl) {
    const acceptance = Array.isArray(task.acceptance) ? task.acceptance.join("\n") : task.acceptance ?? "";
    if (acceptance) {
      acceptanceEl.textContent = acceptance;
      acceptanceEl.closest(".detail-section")?.style.removeProperty("display");
    } else {
      acceptanceEl.closest(".detail-section").style.display = "none";
    }
  }
  if (verifyEl) {
    if (task.verify) {
      verifyEl.textContent = task.verify;
      verifyEl.closest(".detail-section")?.style.removeProperty("display");
    } else {
      verifyEl.closest(".detail-section").style.display = "none";
    }
  }
  if (verdictChip && summaryP) {
    const lastReview = task.last_review ?? null;
    if (lastReview) {
      verdictChip.className = `status-chip last-review-chip ${chipClass(lastReview.verdict)}`;
      verdictChip.textContent = lastReview.verdict;
      summaryP.textContent = lastReview.summary;
    } else {
      verdictChip.className = "status-chip last-review-chip";
      verdictChip.style.background = "var(--color-text-faint)";
      verdictChip.textContent = "no review yet";
      summaryP.textContent = "";
    }
  }
  wireActions(task.id);
  pane.removeAttribute("hidden");
}
function wireActions(taskId) {
  const wire = (id, url) => {
    const btn = document.getElementById(id);
    if (btn) btn.onclick = () => {
      void postAction(url);
    };
  };
  wire("action-approve", `/tasks/${taskId}/approve`);
  wire("action-repair", `/tasks/${taskId}/repair`);
  wire("action-fail", `/tasks/${taskId}/fail`);
  wire("action-run-next", "/run-next");
}
async function postAction(url) {
  try {
    await fetch(url, { method: "POST" });
  } catch {
  }
  await loadConsole();
  if (selectedTaskId) {
    const task = tasks.find((t) => t.id === selectedTaskId);
    if (task) renderDetail(task);
  }
}
function escHtml(s) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}
function initDetailClose() {
  const btn = document.getElementById("detail-close");
  if (!btn) return;
  btn.addEventListener("click", () => {
    selectedTaskId = null;
    const pane = document.getElementById("detail-pane");
    if (pane) pane.setAttribute("hidden", "");
    renderTaskList();
  });
}
function bootstrap() {
  initHamburger();
  initLogout();
  initDetailClose();
  renderTabs();
  switchTab("frd");
  setInterval(() => {
    void loadConsole();
  }, 3e3);
}
document.addEventListener("DOMContentLoaded", bootstrap);
export {
  loadConsole,
  postAction,
  renderDetail,
  renderProgress,
  renderTaskList,
  wireActions
};
