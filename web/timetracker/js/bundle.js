// web/timetracker/js/main.ts
import { ThemeManager, HamburgerMenu } from "/shared/dist/shared.mjs";

// web/timetracker/js/time-selector.ts
var SVG_COPY = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
var TimeSelector = class {
  constructor(container, options = {}) {
    this.selectedBlocks = [];
    this.isDragging = false;
    this.timeBlocks = [];
    this._rawContainer = container;
    this.onChange = options.onChange || void 0;
    if (container instanceof Element) {
      this.container = container;
      this._init();
      return;
    }
    if (typeof container === "string") {
      this.selector = container.startsWith("#") ? container : `#${container}`;
      this.container = document.querySelector(this.selector) ?? void 0;
      if (this.container) {
        this._init();
        return;
      }
      this._domReadyHandler = () => {
        this.container = document.querySelector(this.selector) ?? void 0;
        if (this.container) {
          this._init();
          document.removeEventListener("DOMContentLoaded", this._domReadyHandler);
        } else {
          console.error(`TimeSelector: container ${this.selector} not found after DOMContentLoaded.`);
        }
      };
      document.addEventListener("DOMContentLoaded", this._domReadyHandler);
      return;
    }
    console.error("TimeSelector: invalid container (must be DOM element or selector string).");
  }
  initManual() {
    if (!this.container && typeof this.selector === "string") {
      this.container = document.querySelector(this.selector) ?? void 0;
    }
    if (this.container) this._init();
    else console.error("TimeSelector: initManual failed \u2014 container not found.");
  }
  _init() {
    if (!this.container) {
      console.error("TimeSelector: container not set in _init().");
      return;
    }
    this._render();
    this._attachEvents();
    this._updateTotalTime();
  }
  _render() {
    const html = this._generateTimeTable();
    this.container.innerHTML = `
      <table id="timeSelectorTable" class="time-selector-table" aria-label="time selector">
        ${html}
      </table>
      <div class="form-group">
        <label for="totalTimeInput">Total Time:</label>
        <input type="text" id="totalTimeInput" readonly>
      </div>
      <div class="time-buttons">
        <button type="button" id="clearTimeSelectionBtn">Clear</button>
        <button type="button" id="copyTimeSelectionBtn" title="Copy total time">
          ${SVG_COPY}
        </button>
      </div>
    `;
    this.table = this.container.querySelector("#timeSelectorTable");
    this.timeBlocks = Array.from(this.container.querySelectorAll(".time-block"));
    this.totalTimeInput = this.container.querySelector("#totalTimeInput");
    this.clearBtn = this.container.querySelector("#clearTimeSelectionBtn");
    this.copyBtn = this.container.querySelector("#copyTimeSelectionBtn");
  }
  _generateTimeTable() {
    let html = "";
    for (let hour = 8; hour <= 22; hour++) {
      const displayHour = hour > 12 ? hour - 12 : hour;
      const ampm = hour >= 12 ? "PM" : "AM";
      for (let quarter = 0; quarter < 4; quarter++) {
        html += "<tr>";
        if (quarter === 0) {
          html += `<th rowspan="4" scope="row">${displayHour} ${ampm}</th>`;
        }
        html += `<td class="time-block" data-hour="${hour}" data-quarter="${quarter}" tabindex="0" role="button" aria-pressed="false"></td>`;
        html += "</tr>";
      }
    }
    return html;
  }
  _attachEvents() {
    this.timeBlocks.forEach((block) => {
      block.addEventListener("mousedown", (e) => this._startSelection(e, block));
      block.addEventListener("mouseenter", (_e) => this._dragSelection(block));
      block.addEventListener("keydown", (e) => {
        if (e.key === " " || e.key === "Enter") {
          e.preventDefault();
          this._toggleBlock(block);
          this._notifyChange();
        }
      });
      block.addEventListener("mouseup", () => this._endSelection());
    });
    this.clearBtn?.addEventListener("click", () => this.clearSelection());
    this.copyBtn?.addEventListener("click", () => this.copySelection());
    document.addEventListener("mouseup", () => {
      if (this.isDragging) this._endSelection();
    });
  }
  _startSelection(event, block) {
    event.preventDefault();
    this.isDragging = true;
    this._toggleBlock(block);
    this._notifyChange();
  }
  _dragSelection(block) {
    if (this.isDragging) {
      if (!block.classList.contains("selected")) {
        block.classList.add("selected");
        this.selectedBlocks.push(block);
      }
      this._updateTotalTime();
      this._notifyChange();
    }
  }
  _endSelection() {
    this.isDragging = false;
    this._updateTotalTime();
    this._notifyChange();
  }
  _toggleBlock(block) {
    const idx = this.selectedBlocks.indexOf(block);
    if (idx === -1) {
      block.classList.add("selected");
      block.setAttribute("aria-pressed", "true");
      this.selectedBlocks.push(block);
    } else {
      block.classList.remove("selected");
      block.setAttribute("aria-pressed", "false");
      this.selectedBlocks.splice(idx, 1);
    }
    this._updateTotalTime();
  }
  _updateTotalTime() {
    const totalMinutes = this.selectedBlocks.length * 15;
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    const text = `${hours}h ${minutes}m`;
    if (this.totalTimeInput) this.totalTimeInput.value = text;
  }
  _notifyChange() {
    if (typeof this.onChange === "function") {
      const slots = this.getSelectedTimeBlocks();
      this.onChange({
        count: slots.length,
        totalMinutes: slots.length * 15,
        slots
      });
    }
  }
  setSelection(slots, notify = false) {
    this.selectedBlocks.forEach((b) => {
      b.classList.remove("selected");
      b.setAttribute("aria-pressed", "false");
    });
    this.selectedBlocks = [];
    (slots || []).forEach(({ hour, quarter }) => {
      const block = this.container.querySelector(
        `.time-block[data-hour="${hour}"][data-quarter="${quarter}"]`
      );
      if (block) {
        block.classList.add("selected");
        block.setAttribute("aria-pressed", "true");
        this.selectedBlocks.push(block);
      }
    });
    this._updateTotalTime();
    if (notify) this._notifyChange();
  }
  clearSelection() {
    this.selectedBlocks.forEach((b) => {
      b.classList.remove("selected");
      b.setAttribute("aria-pressed", "false");
    });
    this.selectedBlocks = [];
    this._updateTotalTime();
    this._notifyChange();
  }
  copySelection() {
    const text = this.totalTimeInput?.value || "";
    if (navigator.clipboard && text) {
      navigator.clipboard.writeText(text).catch((err) => {
        console.warn("TimeSelector: copy failed", err);
      });
    }
  }
  getSelectedTimeBlocks() {
    return this.selectedBlocks.map((b) => ({
      hour: parseInt(b.dataset.hour, 10),
      quarter: parseInt(b.dataset.quarter, 10)
    }));
  }
};

// web/timetracker/js/main.ts
var SVG_EDIT = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/></svg>';
var SVG_DOWNLOAD = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>';
var SVG_COPY2 = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
var themes = new ThemeManager({ module: "timetracker", default: "dark" });
themes.apply();
function buildHamburger() {
  const items = [];
  new HamburgerMenu({ title: "TimeTracker", items, themePicker: true, themes });
}
document.addEventListener("DOMContentLoaded", function() {
  buildHamburger();
  const container = document.createElement("div");
  container.className = "container";
  const header = document.createElement("div");
  header.className = "header";
  container.appendChild(header);
  const content = document.createElement("div");
  content.className = "content";
  container.appendChild(content);
  const leftPanel = document.createElement("div");
  leftPanel.className = "left-panel";
  content.appendChild(leftPanel);
  const rightPanel = document.createElement("div");
  rightPanel.className = "right-panel";
  content.appendChild(rightPanel);
  const timeSelectorContainer = document.createElement("div");
  timeSelectorContainer.className = "time-selector-container hidden";
  timeSelectorContainer.id = "timeSelectorContainer";
  content.appendChild(timeSelectorContainer);
  document.body.appendChild(container);
  const footer = document.createElement("div");
  footer.className = "footer";
  const messageBar = document.createElement("div");
  messageBar.className = "message-bar hidden";
  footer.appendChild(messageBar);
  const deleteButton = document.createElement("button");
  deleteButton.textContent = "Delete Customer";
  deleteButton.className = "delete-btn";
  deleteButton.style.backgroundColor = "red";
  const modal = document.createElement("div");
  modal.className = "modal hidden";
  modal.innerHTML = `
        <div class="modal-content">
            <p>Are you sure you want to delete this customer?</p>
            <div class="modal-buttons">
                <button id="confirmDeleteBtn">YES</button>
                <button id="cancelDeleteBtn">NO</button>
            </div>
        </div>
    `;
  const addButton = document.createElement("button");
  addButton.textContent = "Add Customer";
  addButton.className = "add-btn";
  addButton.style.backgroundColor = "darkgreen";
  addButton.style.color = "white";
  addButton.style.minWidth = "250px";
  addButton.style.padding = "10px 20px";
  addButton.style.border = "none";
  addButton.style.cursor = "pointer";
  leftPanel.appendChild(addButton);
  const addModal = document.createElement("div");
  addModal.className = "modal hidden";
  addModal.innerHTML = `
        <div class="modal-content">
            <p>New customer name:</p>
            <input type="text" id="newCustomerNameInput" style="width: 90%; margin-bottom: 10px;">
            <div class="modal-buttons">
                <button id="confirmAddBtn">Add</button>
                <button id="cancelAddBtn">Cancel</button>
            </div>
        </div>
    `;
  addButton.onclick = function() {
    const nameInput = addModal.querySelector("#newCustomerNameInput");
    nameInput.value = "";
    addModal.classList.remove("hidden");
    nameInput.focus();
  };
  function submitNewCustomer() {
    const nameInput = addModal.querySelector("#newCustomerNameInput");
    const name = nameInput.value.trim();
    if (!name) {
      nameInput.focus();
      return;
    }
    const requestData = {
      index: 0,
      field: "newCustomer",
      value: {
        customerName: name,
        slackChannel: "",
        slackChannelId: "",
        workLoadType: "",
        cmsUrl: "",
        supportBucket: "",
        jira: ""
      }
    };
    fetch(`/update`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json"
      },
      body: JSON.stringify(requestData)
    }).then((response) => {
      if (!response.ok) {
        return response.text().then((text) => {
          throw new Error(text);
        });
      }
      return response.json();
    }).then((updatedData) => {
      console.log("Customer added:", updatedData);
      sessionStorage.setItem("tt-select-customer", name);
      location.reload();
    }).catch((error) => {
      console.error("Error adding customer:", error);
    });
  }
  addModal.querySelector("#confirmAddBtn").onclick = submitNewCustomer;
  addModal.querySelector("#cancelAddBtn").onclick = function() {
    addModal.classList.add("hidden");
  };
  addModal.querySelector("#newCustomerNameInput").addEventListener("keydown", function(e) {
    if (e.key === "Enter") submitNewCustomer();
  });
  document.body.appendChild(addModal);
  document.body.appendChild(modal);
  document.body.appendChild(footer);
  function showMessage(message) {
    messageBar.textContent = message;
    messageBar.classList.remove("hidden");
    setTimeout(() => {
      messageBar.classList.add("hidden");
    }, 1e4);
  }
  let tooltips = {};
  fetch("/tooltips.json").then((response) => response.json()).then((data) => {
    tooltips = data;
    initializeTooltips();
  });
  function initializeTooltips() {
    document.querySelectorAll("[data-tooltip]").forEach((element) => {
      const tooltipText = tooltips[element.getAttribute("data-tooltip")];
      if (tooltipText) {
        element.setAttribute("title", tooltipText);
      }
    });
  }
  let timeSelector = null;
  fetch("/data").then((response) => response.json()).then((data) => {
    header.innerHTML = `
                <h1>${data.projectName}</h1>
                <div class="form-group">
                    <label data-tooltip="author">Author:</label>
                    <input type="text" id="authorInput" value="${data.author}" readonly data-tooltip="author">
                    <span class="edit-btn" data-target="author">${SVG_EDIT}</span>
                    <button class="submit-btn hidden" data-target="author">Submit</button>
                    <button id="exportBtn" class="export-btn" data-tooltip="exportData">
                        ${SVG_DOWNLOAD}
                    </button>
                </div>
            `;
    document.getElementById("exportBtn").addEventListener("click", function() {
      window.location.href = "/export-csv";
    });
    const customerList = document.createElement("ul");
    customerList.className = "customer-list";
    leftPanel.appendChild(customerList);
    data.customers.forEach((customer, index) => {
      const li = document.createElement("li");
      li.className = "customer-item";
      li.textContent = customer.customerName;
      li.style.cursor = "pointer";
      li.onclick = () => {
        document.querySelectorAll(".customer-item").forEach((item) => item.classList.remove("selected"));
        li.classList.add("selected");
        showCustomerDetails(customer, index);
      };
      customerList.appendChild(li);
    });
    let refreshReport = null;
    let notifyTimeChanged = null;
    let flushActiveReport = null;
    window.addEventListener("beforeunload", () => {
      if (flushActiveReport) flushActiveReport();
    });
    if (!timeSelector) {
      timeSelector = new TimeSelector("timeSelectorContainer", {
        onChange: () => {
          if (refreshReport) refreshReport();
          if (notifyTimeChanged) notifyTimeChanged();
        }
      });
      window.timeSelector = timeSelector;
    }
    const pendingSelect = sessionStorage.getItem("tt-select-customer");
    if (pendingSelect) {
      sessionStorage.removeItem("tt-select-customer");
      const idx = data.customers.findIndex((c) => c.customerName === pendingSelect);
      if (idx !== -1) {
        const item = customerList.children[idx];
        item.click();
        item.scrollIntoView({ block: "nearest" });
      }
    }
    function showCustomerDetails(customer, index) {
      if (flushActiveReport) flushActiveReport();
      refreshReport = null;
      notifyTimeChanged = null;
      flushActiveReport = null;
      timeSelectorContainer.classList.remove("hidden");
      if (timeSelector) {
        timeSelector.setSelection([]);
      }
      rightPanel.innerHTML = `
                    <div class="form-group">
                        <label data-tooltip="customerName">Customer Name:</label>
                        <input type="text" id="customerNameInput" value="${customer.customerName}" readonly data-tooltip="customerName">
                        <span class="edit-btn" data-target="customerName">${SVG_EDIT}</span>
                        <button class="submit-btn hidden" data-target="customerName" data-index="${index}">Submit</button>
                    </div>
                    <div class="form-group">
                        <label data-tooltip="slackChannel">Slack Channel:</label>
                        <a href="slack://channel?team=T12DX4MJR&id=${customer.slackChannelId}" id="slackChannel" data-tooltip="slackChannel">${customer.slackChannel}</a>
                        <span class="edit-btn" data-target="slackChannel">${SVG_EDIT}</span>
                        <input type="text" id="slackChannelInput" class="hidden" value="${customer.slackChannel}" data-tooltip="slackChannel">
                        <button class="submit-btn hidden" data-target="slackChannel" data-index="${index}">Submit</button>
                    </div>
                    <div class="form-group">
                        <label data-tooltip="workLoadType">Work Load Type:</label>
                        <select id="workLoadTypeSelect" data-index="${index}" data-tooltip="workLoadType">
                            <option ${customer.workLoadType === "Bucket Migration" ? "selected" : ""}>Bucket Migration</option>
                            <option ${customer.workLoadType === "OS Migration" ? "selected" : ""}>OS Migration</option>
                            <option ${customer.workLoadType === "HS Upgrade" ? "selected" : ""}>HS Upgrade</option>
                        </select>
                    </div>
                    <div class="form-group">
                        <label data-tooltip="cmsUrl">CMS URL:</label>
                        <a href="${customer.cmsUrl}" id="cmsUrl" data-tooltip="cmsUrl">${customer.cmsUrl}</a>
                        <span class="edit-btn" data-target="cmsUrl">${SVG_EDIT}</span>
                        <input type="text" id="cmsUrlInput" class="hidden" value="${customer.cmsUrl}" data-tooltip="cmsUrl">
                        <button class="submit-btn hidden" data-target="cmsUrl" data-index="${index}">Submit</button>
                    </div>
                    <div class="form-group">
                        <label data-tooltip="supportBucket">Support Bucket:</label>
                        <input type="text" id="supportBucketInput" value="${customer.supportBucket}" readonly data-tooltip="supportBucket">
                        <span class="edit-btn" data-target="supportBucket">${SVG_EDIT}</span>
                        <button class="submit-btn hidden" data-target="supportBucket" data-index="${index}">Submit</button>
                    </div>
                    <div class="form-group">
                        <label data-tooltip="jira">JIRA #:</label>
                        <a href="https://cloudian.atlassian.net/browse/PS-${customer.jira}" id="jiraUrl" data-tooltip="jira">${customer.jira}</a>
                        <span class="edit-btn" data-target="jira">${SVG_EDIT}</span>
                        <input type="text" id="jiraInput" class="hidden" value="${customer.jira}" data-tooltip="jira">
                        <button class="submit-btn hidden" data-target="jira" data-index="${index}">Submit</button>
                    </div>
                    <div class="form-group report-container">
                        <div class="report-editor">
                            <div class="date-selector">
                                <label for="reportDate" data-tooltip="reportDate">Report Date:</label>
                                <button type="button" id="prevReportBtn" title="Previous saved report" disabled>&#9664;</button>
                                <input type="date" id="reportDate" data-tooltip="reportDate">
                                <button type="button" id="nextReportBtn" title="Next saved report" disabled>&#9654;</button>
                                <button type="button" id="todayReportBtn" title="Jump to today's report">Today</button>
                            </div>
                            <label data-tooltip="reportInput">Report:</label>
                            <textarea id="reportInput" data-tooltip="reportInput" placeholder="Type your report here..."></textarea>
                        </div>
                        <div class="report-preview">
                            <label>Processed Report:</label>
                            <div id="processedReport"></div>
                            <button id="copyReportBtn" data-tooltip="copyReportBtn">${SVG_COPY2}</button>
                        </div>
                    </div>
                `;
      rightPanel.appendChild(deleteButton);
      const reportInput = document.getElementById("reportInput");
      const processedReport = document.getElementById("processedReport");
      const copyReportBtn = document.getElementById("copyReportBtn");
      const reportDate = document.getElementById("reportDate");
      const prevReportBtn = document.getElementById("prevReportBtn");
      const nextReportBtn = document.getElementById("nextReportBtn");
      const todayReportBtn = document.getElementById("todayReportBtn");
      let currentDate = localToday();
      let prevDate = "";
      let nextDate = "";
      let saveTimer = null;
      let dirty = false;
      let applyingRemote = false;
      function localToday() {
        const d = /* @__PURE__ */ new Date();
        return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
      }
      function updateNavState(state) {
        prevDate = state.prevDate || "";
        nextDate = state.nextDate || "";
        prevReportBtn.disabled = !prevDate;
        nextReportBtn.disabled = !nextDate;
      }
      function scheduleSave() {
        if (applyingRemote) return;
        dirty = true;
        if (saveTimer) clearTimeout(saveTimer);
        saveTimer = setTimeout(saveReportNow, 600);
      }
      function saveReportNow() {
        if (saveTimer) clearTimeout(saveTimer);
        saveTimer = null;
        if (!dirty) return Promise.resolve();
        dirty = false;
        return fetch(`/report`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          keepalive: true,
          body: JSON.stringify({
            customerName: customer.customerName,
            date: currentDate,
            body: reportInput.value,
            timeBlocks: timeSelector.getSelectedTimeBlocks()
          })
        }).then((response) => {
          if (!response.ok) {
            return response.text().then((text) => {
              throw new Error(text);
            });
          }
          return response.json();
        }).then((state) => updateNavState(state)).catch((error) => {
          dirty = true;
          console.error("Error saving report:", error);
        });
      }
      function loadReport(date) {
        saveReportNow().then(() => fetch(`/report?customer=${encodeURIComponent(customer.customerName)}&date=${encodeURIComponent(date)}`)).then((response) => {
          if (!response.ok) {
            return response.text().then((text) => {
              throw new Error(text);
            });
          }
          return response.json();
        }).then((state) => {
          applyingRemote = true;
          currentDate = state.date;
          reportDate.value = state.date;
          reportInput.value = state.body || "";
          timeSelector.setSelection(state.timeBlocks || []);
          updateNavState(state);
          processReport();
          applyingRemote = false;
        }).catch((error) => {
          console.error("Error loading report:", error);
        });
      }
      reportInput.addEventListener("input", () => {
        processReport();
        scheduleSave();
      });
      reportDate.addEventListener("change", () => {
        if (reportDate.value) loadReport(reportDate.value);
      });
      prevReportBtn.addEventListener("click", () => {
        if (prevDate) loadReport(prevDate);
      });
      nextReportBtn.addEventListener("click", () => {
        if (nextDate) loadReport(nextDate);
      });
      todayReportBtn.addEventListener("click", () => loadReport(localToday()));
      copyReportBtn.addEventListener("click", copyReportToClipboard);
      refreshReport = processReport;
      notifyTimeChanged = scheduleSave;
      flushActiveReport = saveReportNow;
      loadReport(currentDate);
      document.querySelectorAll(".edit-btn").forEach((btn) => {
        btn.addEventListener("click", function() {
          const target = this.getAttribute("data-target");
          const input = document.getElementById(`${target}Input`);
          input.removeAttribute("readonly");
          input.classList.remove("hidden");
          document.querySelector(`button[data-target="${target}"]`).classList.remove("hidden");
        });
      });
      document.querySelectorAll(".submit-btn").forEach((btn) => {
        btn.addEventListener("click", function() {
          const target = this.getAttribute("data-target");
          const input = document.getElementById(`${target}Input`);
          const link = document.getElementById(target);
          const index2 = parseInt(this.getAttribute("data-index"), 10);
          const updatedValue = input.value;
          const requestData = {
            index: index2,
            field: target,
            value: updatedValue
          };
          console.log("Sending request data:", requestData);
          const self = this;
          const sendUpdate = () => fetch(`/update`, {
            method: "POST",
            headers: {
              "Content-Type": "application/json"
            },
            body: JSON.stringify(requestData)
          }).then((response) => {
            if (!response.ok) {
              return response.text().then((text) => {
                throw new Error(text);
              });
            }
            return response.json();
          }).then((_updatedCustomer) => {
            console.log("Received updated customer:", _updatedCustomer);
            console.log("Target:", target);
            if (target === "jira") {
              console.log("Updating JIRA link");
              const existingLink = document.getElementById("jiraUrl");
              if (existingLink) existingLink.remove();
              const jiraLink = document.createElement("a");
              jiraLink.id = "jiraUrl";
              jiraLink.href = `https://cloudian.atlassian.net/browse/PS-${input.value}`;
              jiraLink.textContent = input.value;
              jiraLink.setAttribute("data-tooltip", "jira");
              const parentDiv = input.parentNode;
              const label = parentDiv.querySelector("label");
              parentDiv.insertBefore(jiraLink, label.nextSibling);
              input.classList.add("hidden");
            } else if (target === "customerName") {
              sessionStorage.setItem("tt-select-customer", input.value.trim());
              location.reload();
            } else if (target === "supportBucket") {
              input.setAttribute("readonly", "true");
              input.classList.remove("hidden");
            } else {
              if (link) {
                link.textContent = input.value;
                if (target === "cmsUrl") {
                  link.href = input.value;
                }
                input.classList.add("hidden");
              }
            }
            self.classList.add("hidden");
          }).catch((error) => {
            console.error("Error updating customer:", error);
          });
          if (target === "customerName" && flushActiveReport) {
            flushActiveReport().then(sendUpdate);
          } else {
            sendUpdate();
          }
        });
      });
      document.getElementById("workLoadTypeSelect").addEventListener("change", function() {
        const index2 = parseInt(this.getAttribute("data-index"), 10);
        const updatedValue = this.value;
        const requestData = {
          index: index2,
          field: "workLoadType",
          value: updatedValue
        };
        console.log("Sending request data:", requestData);
        fetch(`/update`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json"
          },
          body: JSON.stringify(requestData)
        }).then((response) => {
          if (!response.ok) {
            return response.text().then((text) => {
              throw new Error(text);
            });
          }
          return response.json();
        }).then((updatedCustomer) => {
          console.log("Received updated customer:", updatedCustomer);
        }).catch((error) => {
          console.error("Error updating customer:", error);
        });
      });
      function processReport() {
        const report = reportInput.value;
        const selectedDate = new Date(reportDate.value);
        selectedDate.setMinutes(selectedDate.getMinutes() + selectedDate.getTimezoneOffset());
        const formattedDate = selectedDate.toLocaleDateString("en-US", {
          month: "long",
          day: "numeric",
          year: "numeric"
        });
        const totalTime = document.getElementById("totalTimeInput")?.value || "0h 0m";
        const reportContent = report.trim() ? marked.parse(report.trim()) : "<p></p>";
        const processed = `
                        <div style="text-align: center; font-weight: bold; text-decoration: underline;">Date: ${formattedDate}</div>
                        <div>Customer: ${customer.customerName}</div>
                        <div>Author: ${data.author}</div>
                        <div>${reportContent}</div>
                        <div>Time related to update ${formattedDate}</div>
                        <div>Hours worked: ${totalTime}</div>
                    `;
        if (processedReport) {
          processedReport.innerHTML = processed;
        }
      }
      function copyReportToClipboard() {
        const selectedDate = new Date(reportDate.value);
        selectedDate.setMinutes(selectedDate.getMinutes() + selectedDate.getTimezoneOffset());
        const formattedDate = selectedDate.toLocaleDateString("en-US", {
          month: "long",
          day: "numeric",
          year: "numeric"
        });
        const totalTime = document.getElementById("totalTimeInput")?.value || "0h 0m";
        const markdownReport = `### Date: ${formattedDate}

Customer: ${customer.customerName}
Author: ${data.author}

${reportInput.value.trim()}

Time related to update ${formattedDate}
Hours worked: ${totalTime}`;
        const tempElement = document.createElement("textarea");
        tempElement.value = markdownReport;
        document.body.appendChild(tempElement);
        tempElement.select();
        document.execCommand("copy");
        document.body.removeChild(tempElement);
        showMessage("Report copied.");
      }
      deleteButton.onclick = function() {
        modal.classList.remove("hidden");
      };
      document.getElementById("confirmDeleteBtn").onclick = function() {
        deleteCustomer(index);
        modal.classList.add("hidden");
      };
      document.getElementById("cancelDeleteBtn").onclick = function() {
        modal.classList.add("hidden");
      };
      function deleteCustomer(index2) {
        const requestData = {
          index: index2
        };
        fetch(`/delete`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json"
          },
          body: JSON.stringify(requestData)
        }).then((response) => {
          if (!response.ok) {
            return response.text().then((text) => {
              throw new Error(text);
            });
          }
          return response.json();
        }).then((updatedData) => {
          console.log("Customer deleted:", updatedData);
          showMessage("Customer deleted");
          location.reload();
        }).catch((error) => {
          console.error("Error deleting customer:", error);
        });
      }
      initializeTooltips();
    }
    document.querySelector('.edit-btn[data-target="author"]').addEventListener("click", function() {
      const input = document.getElementById("authorInput");
      input.removeAttribute("readonly");
      input.classList.remove("hidden");
      document.querySelector('button[data-target="author"]').classList.remove("hidden");
    });
    document.querySelector('button[data-target="author"]').addEventListener("click", function() {
      const input = document.getElementById("authorInput");
      const updatedValue = input.value;
      const requestData = {
        index: -1,
        field: "author",
        value: updatedValue
      };
      console.log("Sending request data:", requestData);
      const self = this;
      fetch(`/update`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify(requestData)
      }).then((response) => {
        if (!response.ok) {
          return response.text().then((text) => {
            throw new Error(text);
          });
        }
        return response.json();
      }).then((_updatedData) => {
        console.log("Received updated data:", _updatedData);
        input.setAttribute("readonly", "true");
        input.classList.remove("hidden");
        self.classList.add("hidden");
        showMessage("Author updated");
      }).catch((error) => {
        console.error("Error updating author:", error);
      });
    });
  });
});
