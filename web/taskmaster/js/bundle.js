"use strict";
(() => {
  // web/taskmaster/js/api.ts
  async function apiFetch(path, options = {}) {
    const headers = { "Content-Type": "application/json" };
    const existingHeaders = options.headers;
    if (existingHeaders) {
      Object.assign(headers, existingHeaders);
    }
    const resp = await fetch(path, { ...options, headers });
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
      throw new Error(errMsg);
    }
    if (resp.status === 204) return void 0;
    return resp.json();
  }
  var api = {
    health() {
      return apiFetch("/api/health");
    },
    capabilities() {
      return apiFetch("/api/capabilities");
    },
    // Groups
    listGroups() {
      return apiFetch("/api/groups");
    },
    getGroup(name) {
      return apiFetch("/api/groups/" + name);
    },
    createGroup(g) {
      return apiFetch("/api/groups", { method: "POST", body: JSON.stringify(g) });
    },
    updateGroup(name, updates) {
      return apiFetch("/api/groups/" + name, { method: "PUT", body: JSON.stringify(updates) });
    },
    deleteGroup(name) {
      return apiFetch("/api/groups/" + name, { method: "DELETE" });
    },
    pauseGroup(name) {
      return apiFetch("/api/groups/" + name + "/pause", { method: "POST" });
    },
    resumeGroup(name) {
      return apiFetch("/api/groups/" + name + "/resume", { method: "POST" });
    },
    // Tasks
    listTasks(group) {
      const q = group ? "?group=" + encodeURIComponent(group) : "";
      return apiFetch("/api/tasks" + q);
    },
    getTask(name) {
      return apiFetch("/api/tasks/" + name);
    },
    addTask(task) {
      return apiFetch("/api/tasks", { method: "POST", body: JSON.stringify(task) });
    },
    updateTask(name, updates) {
      return apiFetch("/api/tasks/" + name, { method: "PUT", body: JSON.stringify(updates) });
    },
    deleteTask(name) {
      return apiFetch("/api/tasks/" + name, { method: "DELETE" });
    },
    pauseTask(name) {
      return apiFetch("/api/tasks/" + name + "/pause", { method: "POST" });
    },
    resumeTask(name) {
      return apiFetch("/api/tasks/" + name + "/resume", { method: "POST" });
    },
    enqueueTask(name) {
      return apiFetch("/api/tasks/" + name + "/enqueue", { method: "POST" });
    },
    // Executions
    listExecutions(taskName, limit = 50) {
      const params = new URLSearchParams({ limit: String(limit) });
      if (taskName) params.set("task", taskName);
      return apiFetch("/api/executions?" + params);
    },
    // Metrics
    getMetrics(group, task, hours = 24) {
      const params = new URLSearchParams({ hours: String(hours) });
      if (group) params.set("group", group);
      if (task) params.set("task", task);
      return apiFetch("/api/metrics?" + params);
    }
  };

  // web/taskmaster/js/groups.ts
  function statusBadge(paused) {
    const span = document.createElement("span");
    span.className = paused ? "badge badge-yellow" : "badge badge-green";
    span.textContent = paused ? "paused" : "active";
    return span;
  }
  function renderGroupRow(g, tbody, onRefresh) {
    const tr = document.createElement("tr");
    const tdName = document.createElement("td");
    tdName.textContent = g.name;
    const tdLimit = document.createElement("td");
    tdLimit.textContent = String(g.pool_limit);
    const tdRunning = document.createElement("td");
    tdRunning.textContent = String(g.running_count);
    const tdStatus = document.createElement("td");
    tdStatus.appendChild(statusBadge(g.paused));
    const tdActions = document.createElement("td");
    const btnGroup = document.createElement("div");
    btnGroup.className = "btn-group";
    if (g.paused) {
      const btnResume = document.createElement("button");
      btnResume.className = "btn btn-secondary btn-sm";
      btnResume.textContent = "Resume";
      btnResume.addEventListener("click", async () => {
        try {
          await api.resumeGroup(g.name);
          onRefresh();
        } catch (e) {
          showError(String(e));
        }
      });
      btnGroup.appendChild(btnResume);
    } else {
      const btnPause = document.createElement("button");
      btnPause.className = "btn btn-secondary btn-sm";
      btnPause.textContent = "Pause";
      btnPause.addEventListener("click", async () => {
        try {
          await api.pauseGroup(g.name);
          onRefresh();
        } catch (e) {
          showError(String(e));
        }
      });
      btnGroup.appendChild(btnPause);
    }
    const btnEdit = document.createElement("button");
    btnEdit.className = "btn btn-secondary btn-sm";
    btnEdit.textContent = "Edit";
    btnEdit.addEventListener("click", () => showEditModal(g, onRefresh));
    btnGroup.appendChild(btnEdit);
    const btnDel = document.createElement("button");
    btnDel.className = "btn btn-danger btn-sm";
    btnDel.textContent = "Delete";
    btnDel.addEventListener("click", async () => {
      if (!confirm('Delete group "' + g.name + '"? This fails if tasks exist.')) return;
      try {
        await api.deleteGroup(g.name);
        onRefresh();
      } catch (e) {
        showError(String(e));
      }
    });
    btnGroup.appendChild(btnDel);
    tdActions.appendChild(btnGroup);
    tr.appendChild(tdName);
    tr.appendChild(tdLimit);
    tr.appendChild(tdRunning);
    tr.appendChild(tdStatus);
    tr.appendChild(tdActions);
    tbody.appendChild(tr);
  }
  function showCreateModal(onRefresh) {
    const overlay = document.createElement("div");
    overlay.className = "modal-overlay";
    const modal = document.createElement("div");
    modal.className = "modal";
    const title = document.createElement("h2");
    title.textContent = "Create Group";
    modal.appendChild(title);
    const fgName = document.createElement("div");
    fgName.className = "form-group";
    const lblName = document.createElement("label");
    lblName.textContent = "Name";
    const inputName = document.createElement("input");
    inputName.type = "text";
    inputName.placeholder = "my-group";
    fgName.appendChild(lblName);
    fgName.appendChild(inputName);
    modal.appendChild(fgName);
    const fgLimit = document.createElement("div");
    fgLimit.className = "form-group";
    const lblLimit = document.createElement("label");
    lblLimit.textContent = "Pool Limit";
    const inputLimit = document.createElement("input");
    inputLimit.type = "number";
    inputLimit.value = "1";
    inputLimit.min = "1";
    fgLimit.appendChild(lblLimit);
    fgLimit.appendChild(inputLimit);
    modal.appendChild(fgLimit);
    const actions = document.createElement("div");
    actions.className = "form-actions";
    const btnCreate = document.createElement("button");
    btnCreate.className = "btn btn-primary";
    btnCreate.textContent = "Create";
    btnCreate.addEventListener("click", async () => {
      try {
        await api.createGroup({ name: inputName.value.trim(), pool_limit: Number(inputLimit.value), allowed_types: [] });
        overlay.remove();
        onRefresh();
      } catch (e) {
        showError(String(e));
      }
    });
    const btnCancel = document.createElement("button");
    btnCancel.className = "btn btn-secondary";
    btnCancel.textContent = "Cancel";
    btnCancel.addEventListener("click", () => overlay.remove());
    actions.appendChild(btnCreate);
    actions.appendChild(btnCancel);
    modal.appendChild(actions);
    overlay.appendChild(modal);
    overlay.addEventListener("click", (e) => {
      if (e.target === overlay) overlay.remove();
    });
    document.body.appendChild(overlay);
  }
  function showEditModal(g, onRefresh) {
    const overlay = document.createElement("div");
    overlay.className = "modal-overlay";
    const modal = document.createElement("div");
    modal.className = "modal";
    const title = document.createElement("h2");
    title.textContent = "Edit Group: " + g.name;
    modal.appendChild(title);
    const fgLimit = document.createElement("div");
    fgLimit.className = "form-group";
    const lblLimit = document.createElement("label");
    lblLimit.textContent = "Pool Limit";
    const inputLimit = document.createElement("input");
    inputLimit.type = "number";
    inputLimit.value = String(g.pool_limit);
    inputLimit.min = "1";
    fgLimit.appendChild(lblLimit);
    fgLimit.appendChild(inputLimit);
    modal.appendChild(fgLimit);
    const actions = document.createElement("div");
    actions.className = "form-actions";
    const btnSave = document.createElement("button");
    btnSave.className = "btn btn-primary";
    btnSave.textContent = "Save";
    btnSave.addEventListener("click", async () => {
      try {
        await api.updateGroup(g.name, { pool_limit: Number(inputLimit.value) });
        overlay.remove();
        onRefresh();
      } catch (e) {
        showError(String(e));
      }
    });
    const btnCancel = document.createElement("button");
    btnCancel.className = "btn btn-secondary";
    btnCancel.textContent = "Cancel";
    btnCancel.addEventListener("click", () => overlay.remove());
    actions.appendChild(btnSave);
    actions.appendChild(btnCancel);
    modal.appendChild(actions);
    overlay.appendChild(modal);
    overlay.addEventListener("click", (e) => {
      if (e.target === overlay) overlay.remove();
    });
    document.body.appendChild(overlay);
  }
  var errorTimer = null;
  function showError(msg) {
    let banner = document.getElementById("global-error");
    if (!banner) {
      banner = document.createElement("div");
      banner.id = "global-error";
      banner.className = "error-banner";
      const app = document.getElementById("app");
      if (app) app.prepend(banner);
    }
    banner.textContent = msg;
    if (errorTimer) clearTimeout(errorTimer);
    errorTimer = setTimeout(() => banner?.remove(), 5e3);
  }
  function renderGroups(container) {
    container.textContent = "";
    const toolbar = document.createElement("div");
    toolbar.className = "toolbar";
    const h1 = document.createElement("h1");
    h1.textContent = "Groups";
    toolbar.appendChild(h1);
    const spacer = document.createElement("div");
    spacer.className = "toolbar-spacer";
    toolbar.appendChild(spacer);
    const refresh = () => {
      renderGroups(container);
    };
    const btnNew = document.createElement("button");
    btnNew.className = "btn btn-primary";
    btnNew.textContent = "+ New Group";
    btnNew.addEventListener("click", () => showCreateModal(refresh));
    toolbar.appendChild(btnNew);
    const btnRefresh = document.createElement("button");
    btnRefresh.className = "btn btn-secondary";
    btnRefresh.textContent = "Refresh";
    btnRefresh.addEventListener("click", refresh);
    toolbar.appendChild(btnRefresh);
    container.appendChild(toolbar);
    api.listGroups().then((groups) => {
      if (groups.length === 0) {
        const empty = document.createElement("div");
        empty.className = "empty-state";
        empty.textContent = "No groups yet. Create one to get started.";
        container.appendChild(empty);
        return;
      }
      const card = document.createElement("div");
      card.className = "card";
      const table = document.createElement("table");
      const thead = document.createElement("thead");
      const headerRow = document.createElement("tr");
      ["Name", "Pool Limit", "Running", "Status", "Actions"].forEach((h) => {
        const th = document.createElement("th");
        th.textContent = h;
        headerRow.appendChild(th);
      });
      thead.appendChild(headerRow);
      table.appendChild(thead);
      const tbody = document.createElement("tbody");
      groups.forEach((g) => renderGroupRow(g, tbody, refresh));
      table.appendChild(tbody);
      card.appendChild(table);
      container.appendChild(card);
    }).catch((e) => showError(e.message));
  }

  // web/taskmaster/js/tasks.ts
  function taskStatusBadge(task) {
    const span = document.createElement("span");
    if (!task.enabled) {
      span.className = "badge badge-muted";
      span.textContent = "disabled";
    } else if (task.paused) {
      span.className = "badge badge-yellow";
      span.textContent = "paused";
    } else {
      span.className = "badge badge-green";
      span.textContent = "active";
    }
    return span;
  }
  function renderTaskRow(task, tbody, onRefresh) {
    const tr = document.createElement("tr");
    const cells = [];
    const tdName = document.createElement("td");
    tdName.textContent = task.name;
    cells.push(tdName);
    const tdGroup = document.createElement("td");
    tdGroup.textContent = task.group_name;
    cells.push(tdGroup);
    const tdType = document.createElement("td");
    const typeSpan = document.createElement("code");
    typeSpan.textContent = task.task_type;
    tdType.appendChild(typeSpan);
    cells.push(tdType);
    const tdStatus = document.createElement("td");
    tdStatus.appendChild(taskStatusBadge(task));
    cells.push(tdStatus);
    const tdRepeat = document.createElement("td");
    tdRepeat.textContent = task.repeat ? task.cooldown_seconds + "s" : "once";
    cells.push(tdRepeat);
    const tdActions = document.createElement("td");
    const btnGroup = document.createElement("div");
    btnGroup.className = "btn-group";
    const btnEnqueue = document.createElement("button");
    btnEnqueue.className = "btn btn-primary btn-sm";
    btnEnqueue.textContent = "Run now";
    btnEnqueue.addEventListener("click", async () => {
      try {
        const res = await api.enqueueTask(task.name);
        window.location.hash = "#output/" + res.execution_id;
      } catch (e) {
        alert(String(e));
      }
    });
    btnGroup.appendChild(btnEnqueue);
    const btnTogglePause = document.createElement("button");
    btnTogglePause.className = "btn btn-secondary btn-sm";
    btnTogglePause.textContent = task.paused ? "Resume" : "Pause";
    btnTogglePause.addEventListener("click", async () => {
      try {
        if (task.paused) await api.resumeTask(task.name);
        else await api.pauseTask(task.name);
        onRefresh();
      } catch (e) {
        alert(String(e));
      }
    });
    btnGroup.appendChild(btnTogglePause);
    const btnDel = document.createElement("button");
    btnDel.className = "btn btn-danger btn-sm";
    btnDel.textContent = "Delete";
    btnDel.addEventListener("click", async () => {
      if (!confirm('Delete task "' + task.name + '"?')) return;
      try {
        await api.deleteTask(task.name);
        onRefresh();
      } catch (e) {
        alert(String(e));
      }
    });
    btnGroup.appendChild(btnDel);
    tdActions.appendChild(btnGroup);
    cells.push(tdActions);
    cells.forEach((c) => tr.appendChild(c));
    tbody.appendChild(tr);
  }
  function showAddTaskModal(groups, onRefresh) {
    const overlay = document.createElement("div");
    overlay.className = "modal-overlay";
    const modal = document.createElement("div");
    modal.className = "modal";
    const title = document.createElement("h2");
    title.textContent = "Add Task";
    modal.appendChild(title);
    function formGroup(label, input) {
      const fg = document.createElement("div");
      fg.className = "form-group";
      const lbl = document.createElement("label");
      lbl.textContent = label;
      fg.appendChild(lbl);
      fg.appendChild(input);
      return fg;
    }
    const inputName = document.createElement("input");
    inputName.type = "text";
    inputName.placeholder = "my-task";
    modal.appendChild(formGroup("Name *", inputName));
    const selectGroup = document.createElement("select");
    groups.forEach((g) => {
      const opt = document.createElement("option");
      opt.value = g.name;
      opt.textContent = g.name;
      selectGroup.appendChild(opt);
    });
    modal.appendChild(formGroup("Group *", selectGroup));
    const argsTemplates = {
      shell: '{"shell":"echo hello"}',
      exec: '{"command":"ls","args":["-la"],"workdir":"/tmp"}',
      script: '{"path":"/opt/scripts/run.sh","args":["--dry"]}',
      migration: '{"name":"0001_example"}'
    };
    const selectType = document.createElement("select");
    ["shell", "exec", "script", "migration"].forEach((t) => {
      const opt = document.createElement("option");
      opt.value = t;
      opt.textContent = t;
      selectType.appendChild(opt);
    });
    modal.appendChild(formGroup("Task Type", selectType));
    const textArgs = document.createElement("textarea");
    textArgs.rows = 3;
    textArgs.value = argsTemplates["shell"];
    modal.appendChild(formGroup("Args (JSON)", textArgs));
    selectType.addEventListener("change", () => {
      textArgs.value = argsTemplates[selectType.value] ?? "{}";
    });
    const row = document.createElement("div");
    row.className = "form-row";
    const inputPriority = document.createElement("input");
    inputPriority.type = "number";
    inputPriority.value = "50";
    row.appendChild(formGroup("Priority", inputPriority));
    const inputCooldown = document.createElement("input");
    inputCooldown.type = "number";
    inputCooldown.value = "0";
    row.appendChild(formGroup("Cooldown (sec)", inputCooldown));
    modal.appendChild(row);
    const checkRepeat = document.createElement("input");
    checkRepeat.type = "checkbox";
    checkRepeat.id = "task-repeat";
    const lblRepeat = document.createElement("label");
    lblRepeat.className = "checkbox-label form-group";
    lblRepeat.appendChild(checkRepeat);
    const lblText = document.createElement("span");
    lblText.textContent = "Repeat";
    lblRepeat.appendChild(lblText);
    modal.appendChild(lblRepeat);
    let checkSudo = null;
    if (caps.allow_sudo) {
      checkSudo = document.createElement("input");
      checkSudo.type = "checkbox";
      const lblSudo = document.createElement("label");
      lblSudo.className = "checkbox-label form-group";
      lblSudo.appendChild(checkSudo);
      const lblSudoText = document.createElement("span");
      lblSudoText.textContent = "Run with sudo";
      lblSudo.appendChild(lblSudoText);
      modal.appendChild(lblSudo);
    }
    const inputOutputFile = document.createElement("input");
    inputOutputFile.type = "text";
    inputOutputFile.placeholder = "/var/log/taskmaster/{task}.log  (optional; {exec_id} also supported)";
    modal.appendChild(formGroup("Output File", inputOutputFile));
    const actions = document.createElement("div");
    actions.className = "form-actions";
    const errDiv = document.createElement("div");
    errDiv.className = "error-banner";
    errDiv.style.display = "none";
    const btnAdd = document.createElement("button");
    btnAdd.className = "btn btn-primary";
    btnAdd.textContent = "Add Task";
    btnAdd.addEventListener("click", async () => {
      errDiv.style.display = "none";
      try {
        const newTask = {
          name: inputName.value.trim(),
          group_name: selectGroup.value,
          task_type: selectType.value,
          args: textArgs.value.trim() || "{}",
          priority: Number(inputPriority.value),
          cooldown_seconds: Number(inputCooldown.value),
          repeat: checkRepeat.checked,
          enabled: true
        };
        if (checkSudo && checkSudo.checked) newTask.sudo = true;
        const outFile = inputOutputFile.value.trim();
        if (outFile) newTask.output_file = outFile;
        await api.addTask(newTask);
        overlay.remove();
        onRefresh();
      } catch (e) {
        errDiv.textContent = String(e);
        errDiv.style.display = "block";
      }
    });
    const btnCancel = document.createElement("button");
    btnCancel.className = "btn btn-secondary";
    btnCancel.textContent = "Cancel";
    btnCancel.addEventListener("click", () => overlay.remove());
    actions.appendChild(btnAdd);
    actions.appendChild(btnCancel);
    modal.appendChild(errDiv);
    modal.appendChild(actions);
    overlay.appendChild(modal);
    overlay.addEventListener("click", (e) => {
      if (e.target === overlay) overlay.remove();
    });
    document.body.appendChild(overlay);
  }
  function renderTasks(container, groupFilter) {
    container.textContent = "";
    const refresh = () => {
      renderTasks(container, groupFilter);
    };
    const toolbar = document.createElement("div");
    toolbar.className = "toolbar";
    const h1 = document.createElement("h1");
    h1.textContent = groupFilter ? "Tasks \u2014 " + groupFilter : "Tasks";
    toolbar.appendChild(h1);
    const spacer = document.createElement("div");
    spacer.className = "toolbar-spacer";
    toolbar.appendChild(spacer);
    const btnRefresh = document.createElement("button");
    btnRefresh.className = "btn btn-secondary";
    btnRefresh.textContent = "Refresh";
    btnRefresh.addEventListener("click", refresh);
    toolbar.appendChild(btnRefresh);
    container.appendChild(toolbar);
    Promise.all([api.listTasks(groupFilter), api.listGroups()]).then(([tasks, groups]) => {
      const btnNew = document.createElement("button");
      btnNew.className = "btn btn-primary";
      btnNew.textContent = "+ New Task";
      btnNew.addEventListener("click", () => showAddTaskModal(groups, refresh));
      toolbar.insertBefore(btnNew, btnRefresh);
      if (tasks.length === 0) {
        const empty = document.createElement("div");
        empty.className = "empty-state";
        empty.textContent = "No tasks yet.";
        container.appendChild(empty);
        return;
      }
      const card = document.createElement("div");
      card.className = "card";
      const table = document.createElement("table");
      const thead = document.createElement("thead");
      const headerRow = document.createElement("tr");
      ["Name", "Group", "Type", "Status", "Schedule", "Actions"].forEach((h) => {
        const th = document.createElement("th");
        th.textContent = h;
        headerRow.appendChild(th);
      });
      thead.appendChild(headerRow);
      table.appendChild(thead);
      const tbody = document.createElement("tbody");
      tasks.forEach((t) => renderTaskRow(t, tbody, refresh));
      table.appendChild(tbody);
      card.appendChild(table);
      container.appendChild(card);
    }).catch((e) => {
      const err = document.createElement("div");
      err.className = "error-banner";
      err.textContent = e.message;
      container.appendChild(err);
    });
  }

  // web/taskmaster/js/executions.ts
  function statusBadge2(status) {
    const span = document.createElement("span");
    const map = {
      success: "badge-green",
      failed: "badge-red",
      running: "badge-blue",
      pending: "badge-yellow"
    };
    span.className = "badge " + (map[status] ?? "badge-muted");
    span.textContent = status;
    return span;
  }
  function renderExecRow(exec, tbody) {
    const tr = document.createElement("tr");
    const tdId = document.createElement("td");
    const link = document.createElement("a");
    link.href = "#output/" + exec.id;
    link.textContent = String(exec.id);
    link.style.color = "var(--text-accent)";
    tdId.appendChild(link);
    tr.appendChild(tdId);
    const tdTask = document.createElement("td");
    tdTask.textContent = exec.task_name ?? String(exec.task_id);
    tr.appendChild(tdTask);
    const tdStatus = document.createElement("td");
    tdStatus.appendChild(statusBadge2(exec.status));
    tr.appendChild(tdStatus);
    const tdDuration = document.createElement("td");
    tdDuration.textContent = exec.duration_ms != null ? exec.duration_ms + "ms" : "-";
    tr.appendChild(tdDuration);
    const tdDelay = document.createElement("td");
    tdDelay.textContent = exec.schedule_delay_ms != null ? exec.schedule_delay_ms + "ms" : "-";
    tr.appendChild(tdDelay);
    const tdStarted = document.createElement("td");
    tdStarted.textContent = exec.started_at ? new Date(exec.started_at).toLocaleString() : "-";
    tr.appendChild(tdStarted);
    const tdError = document.createElement("td");
    if (exec.error_message) {
      const code = document.createElement("code");
      code.textContent = exec.error_message.slice(0, 80);
      code.title = exec.error_message;
      tdError.appendChild(code);
    } else {
      tdError.textContent = "-";
    }
    tr.appendChild(tdError);
    tbody.appendChild(tr);
  }
  function renderExecutions(container, taskFilter) {
    container.textContent = "";
    const toolbar = document.createElement("div");
    toolbar.className = "toolbar";
    const h1 = document.createElement("h1");
    h1.textContent = taskFilter ? "Executions \u2014 " + taskFilter : "Executions";
    toolbar.appendChild(h1);
    const spacer = document.createElement("div");
    spacer.className = "toolbar-spacer";
    toolbar.appendChild(spacer);
    const btnRefresh = document.createElement("button");
    btnRefresh.className = "btn btn-secondary";
    btnRefresh.textContent = "Refresh";
    btnRefresh.addEventListener("click", () => renderExecutions(container, taskFilter));
    toolbar.appendChild(btnRefresh);
    container.appendChild(toolbar);
    api.listExecutions(taskFilter, 50).then((execs) => {
      if (execs.length === 0) {
        const empty = document.createElement("div");
        empty.className = "empty-state";
        empty.textContent = "No executions yet.";
        container.appendChild(empty);
        return;
      }
      const card = document.createElement("div");
      card.className = "card";
      const table = document.createElement("table");
      const thead = document.createElement("thead");
      const headerRow = document.createElement("tr");
      ["ID", "Task", "Status", "Duration", "Delay", "Started", "Error"].forEach((h) => {
        const th = document.createElement("th");
        th.textContent = h;
        headerRow.appendChild(th);
      });
      thead.appendChild(headerRow);
      table.appendChild(thead);
      const tbody = document.createElement("tbody");
      execs.forEach((e) => renderExecRow(e, tbody));
      table.appendChild(tbody);
      card.appendChild(table);
      container.appendChild(card);
    }).catch((e) => {
      const err = document.createElement("div");
      err.className = "error-banner";
      err.textContent = e.message;
      container.appendChild(err);
    });
  }

  // web/taskmaster/js/metrics.ts
  function tile(label, value) {
    const div = document.createElement("div");
    div.className = "metric-tile";
    const lbl = document.createElement("div");
    lbl.className = "metric-label";
    lbl.textContent = label;
    const val = document.createElement("div");
    val.className = "metric-value";
    val.textContent = value;
    div.appendChild(lbl);
    div.appendChild(val);
    return div;
  }
  function renderMetricCard(s, container) {
    const card = document.createElement("div");
    card.className = "card";
    const header = document.createElement("div");
    header.className = "card-header";
    const title = document.createElement("h2");
    title.textContent = s.task_name;
    header.appendChild(title);
    const groupBadge = document.createElement("span");
    groupBadge.className = "badge badge-muted";
    groupBadge.textContent = s.group_name;
    header.appendChild(groupBadge);
    card.appendChild(header);
    const grid = document.createElement("div");
    grid.className = "metrics-grid";
    const total = s.success_count + s.failed_count;
    const successPct = total > 0 ? Math.round(s.success_count / total * 100) : 0;
    grid.appendChild(tile("Success", String(s.success_count)));
    grid.appendChild(tile("Failed", String(s.failed_count)));
    grid.appendChild(tile("Success rate", successPct + "%"));
    grid.appendChild(tile("Avg duration", s.avg_duration_ms != null ? Math.round(s.avg_duration_ms) + "ms" : "\u2014"));
    grid.appendChild(tile("Min duration", s.min_duration_ms != null ? s.min_duration_ms + "ms" : "\u2014"));
    grid.appendChild(tile("Max duration", s.max_duration_ms != null ? s.max_duration_ms + "ms" : "\u2014"));
    grid.appendChild(tile("Avg delay", s.avg_schedule_delay_ms != null ? Math.round(s.avg_schedule_delay_ms) + "ms" : "\u2014"));
    grid.appendChild(tile("Last run", s.last_execution ? new Date(s.last_execution).toLocaleString() : "\u2014"));
    card.appendChild(grid);
    container.appendChild(card);
  }
  function renderMetrics(container, groupFilter) {
    container.textContent = "";
    const toolbar = document.createElement("div");
    toolbar.className = "toolbar";
    const h1 = document.createElement("h1");
    h1.textContent = "Metrics";
    toolbar.appendChild(h1);
    const spacer = document.createElement("div");
    spacer.className = "toolbar-spacer";
    toolbar.appendChild(spacer);
    const btnRefresh = document.createElement("button");
    btnRefresh.className = "btn btn-secondary";
    btnRefresh.textContent = "Refresh";
    btnRefresh.addEventListener("click", () => renderMetrics(container, groupFilter));
    toolbar.appendChild(btnRefresh);
    container.appendChild(toolbar);
    api.getMetrics(groupFilter, void 0, 24).then((summaries) => {
      if (summaries.length === 0) {
        const empty = document.createElement("div");
        empty.className = "empty-state";
        empty.textContent = "No metrics yet. Run some tasks first.";
        container.appendChild(empty);
        return;
      }
      summaries.forEach((s) => renderMetricCard(s, container));
    }).catch((e) => {
      const err = document.createElement("div");
      err.className = "error-banner";
      err.textContent = e.message;
      container.appendChild(err);
    });
  }

  // web/taskmaster/js/output.ts
  function renderOutput(container, execID) {
    container.textContent = "";
    const h1 = document.createElement("h1");
    h1.textContent = "Output \u2014 execution " + execID;
    container.appendChild(h1);
    const statusDiv = document.createElement("div");
    statusDiv.style.marginBottom = "12px";
    container.appendChild(statusDiv);
    const pre = document.createElement("pre");
    pre.className = "output-terminal";
    container.appendChild(pre);
    function appendLine(parsed) {
      const span = document.createElement("span");
      span.className = "stream-" + parsed.stream;
      span.textContent = parsed.line + "\n";
      pre.appendChild(span);
      pre.scrollTop = pre.scrollHeight;
    }
    function showStatus(status) {
      statusDiv.textContent = "Status: " + status;
    }
    function markDone() {
      const done = document.createElement("div");
      done.style.color = "var(--text-muted)";
      done.style.marginTop = "8px";
      done.style.fontSize = "12px";
      done.textContent = "\u2014 execution complete \u2014";
      container.appendChild(done);
    }
    const es = new EventSource("/api/executions/" + execID + "/output");
    es.addEventListener("output", (e) => {
      try {
        appendLine(JSON.parse(e.data));
      } catch {
      }
    });
    es.addEventListener("status", (e) => {
      showStatus(e.data);
    });
    es.addEventListener("done", () => {
      es.close();
      markDone();
    });
    es.onerror = () => {
      es.close();
      const msg = document.createElement("div");
      msg.className = "error-banner";
      msg.textContent = "Connection lost.";
      container.appendChild(msg);
    };
    window.addEventListener("hashchange", () => es.close(), { once: true });
  }

  // web/taskmaster/js/main.ts
  var caps = { allow_sudo: false };
  function buildNav() {
    const nav = document.getElementById("nav");
    if (!nav) return;
    nav.textContent = "";
    const brand = document.createElement("a");
    brand.className = "nav-brand";
    brand.textContent = "taskmaster";
    brand.href = "#groups";
    nav.appendChild(brand);
    const links = [
      { label: "Groups", hash: "#groups" },
      { label: "Tasks", hash: "#tasks" },
      { label: "Executions", hash: "#executions" },
      { label: "Metrics", hash: "#metrics" }
    ];
    links.forEach(({ label, hash }) => {
      const a = document.createElement("a");
      a.className = "nav-link" + (window.location.hash === hash ? " active" : "");
      a.textContent = label;
      a.href = hash;
      nav.appendChild(a);
    });
    const spacer = document.createElement("div");
    spacer.className = "nav-spacer";
    nav.appendChild(spacer);
    const btnLogout = document.createElement("button");
    btnLogout.className = "nav-link";
    btnLogout.textContent = "Logout";
    btnLogout.addEventListener("click", async () => {
      await fetch("/api/auth/logout", { method: "POST" });
      window.location.reload();
    });
    nav.appendChild(btnLogout);
  }
  function route() {
    const container = document.getElementById("app");
    if (!container) return;
    buildNav();
    const hash = window.location.hash || "#groups";
    const [page, param] = hash.slice(1).split("/");
    switch (page) {
      case "groups":
        renderGroups(container);
        break;
      case "tasks":
        renderTasks(container, param);
        break;
      case "executions":
        renderExecutions(container, param);
        break;
      case "metrics":
        renderMetrics(container, param);
        break;
      case "output":
        if (param) renderOutput(container, param);
        break;
      default:
        renderGroups(container);
    }
  }
  async function bootstrap() {
    try {
      caps = await api.capabilities();
    } catch {
      caps = { allow_sudo: false };
    }
    window.addEventListener("hashchange", route);
    route();
  }
  document.addEventListener("DOMContentLoaded", () => {
    void bootstrap();
  });
})();
