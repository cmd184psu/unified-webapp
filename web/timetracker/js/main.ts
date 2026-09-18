import { ThemeManager, HamburgerMenu } from '@shared';
import type { MenuItem } from '@shared';
import TimeSelector from './time-selector.js';
import './main.css';

declare const marked: { parse(s: string): string };

const SVG_EDIT = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"/></svg>';
const SVG_DOWNLOAD = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>';
const SVG_COPY = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';

const themes = new ThemeManager({ module: 'timetracker', default: 'dark' });
themes.apply();

function buildHamburger(): void {
  const items: MenuItem[] = [];
  new HamburgerMenu({ title: 'TimeTracker', items, themePicker: true, themes });
}

document.addEventListener('DOMContentLoaded', function() {
    buildHamburger();

    const container = document.createElement('div');
    container.className = 'container';

    const header = document.createElement('div');
    header.className = 'header';
    container.appendChild(header);

    const content = document.createElement('div');
    content.className = 'content';
    container.appendChild(content);

    const leftPanel = document.createElement('div');
    leftPanel.className = 'left-panel';
    content.appendChild(leftPanel);

    const rightPanel = document.createElement('div');
    rightPanel.className = 'right-panel';
    content.appendChild(rightPanel);

    const timeSelectorContainer = document.createElement('div');
    timeSelectorContainer.className = 'time-selector-container hidden';
    timeSelectorContainer.id = 'timeSelectorContainer';
    content.appendChild(timeSelectorContainer);

    document.body.appendChild(container);

    const footer = document.createElement('div');
    footer.className = 'footer';
    const messageBar = document.createElement('div');
    messageBar.className = 'message-bar hidden';
    footer.appendChild(messageBar);

    const deleteButton = document.createElement('button');
    deleteButton.textContent = 'Delete Customer';
    deleteButton.className = 'delete-btn';
    deleteButton.style.backgroundColor = 'red';

    const modal = document.createElement('div');
    modal.className = 'modal hidden';
    modal.innerHTML = `
        <div class="modal-content">
            <p>Are you sure you want to delete this customer?</p>
            <div class="modal-buttons">
                <button id="confirmDeleteBtn">YES</button>
                <button id="cancelDeleteBtn">NO</button>
            </div>
        </div>
    `;

    const addButton = document.createElement('button');
    addButton.textContent = 'Add Customer';
    addButton.className = 'add-btn';
    addButton.style.backgroundColor = 'darkgreen';
    addButton.style.color = 'white';
    addButton.style.minWidth = '250px';
    addButton.style.padding = '10px 20px';
    addButton.style.border = 'none';
    addButton.style.cursor = 'pointer';
    leftPanel.appendChild(addButton);

    const addModal = document.createElement('div');
    addModal.className = 'modal hidden';
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
        const nameInput = addModal.querySelector('#newCustomerNameInput') as HTMLInputElement;
        nameInput.value = '';
        addModal.classList.remove('hidden');
        nameInput.focus();
    };

    function submitNewCustomer() {
        const nameInput = addModal.querySelector('#newCustomerNameInput') as HTMLInputElement;
        const name = nameInput.value.trim();
        if (!name) {
            nameInput.focus();
            return;
        }

        const requestData = {
            index: 0,
            field: 'newCustomer',
            value: {
                customerName: name,
                slackChannel: '',
                slackChannelId: '',
                workLoadType: '',
                cmsUrl: '',
                supportBucket: '',
                jira: ''
            }
        };

        fetch(`/update`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(requestData)
        })
        .then(response => {
            if (!response.ok) {
                return response.text().then(text => { throw new Error(text) });
            }
            return response.json();
        })
        .then(updatedData => {
            console.log('Customer added:', updatedData);
            sessionStorage.setItem('tt-select-customer', name);
            location.reload();
        })
        .catch(error => {
            console.error('Error adding customer:', error);
        });
    }

    (addModal.querySelector('#confirmAddBtn') as HTMLElement).onclick = submitNewCustomer;
    (addModal.querySelector('#cancelAddBtn') as HTMLElement).onclick = function() {
        addModal.classList.add('hidden');
    };
    (addModal.querySelector('#newCustomerNameInput') as HTMLElement).addEventListener('keydown', function(e) {
        if ((e as KeyboardEvent).key === 'Enter') submitNewCustomer();
    });

    document.body.appendChild(addModal);
    document.body.appendChild(modal);

    document.body.appendChild(footer);

    function showMessage(message: string) {
        messageBar.textContent = message;
        messageBar.classList.remove('hidden');
        setTimeout(() => {
            messageBar.classList.add('hidden');
        }, 10000);
    }

    let tooltips: Record<string, string> = {};

    fetch('/tooltips.json')
        .then(response => response.json())
        .then(data => {
            tooltips = data;
            initializeTooltips();
        });

    function initializeTooltips() {
        document.querySelectorAll('[data-tooltip]').forEach(element => {
            const tooltipText = tooltips[element.getAttribute('data-tooltip')!];
            if (tooltipText) {
                element.setAttribute('title', tooltipText);
            }
        });
    }

    let timeSelector: TimeSelector | null = null;

    interface CustomerData {
        customerName: string;
        slackChannel: string;
        slackChannelId: string;
        workLoadType: string;
        cmsUrl: string;
        supportBucket: string;
        jira: string;
    }

    interface AppData {
        projectName: string;
        author: string;
        customers: CustomerData[];
    }

    fetch('/data')
        .then(response => response.json())
        .then((data: AppData) => {
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

            document.getElementById('exportBtn')!.addEventListener('click', function() {
                window.location.href = '/export-csv';
            });

            const customerList = document.createElement('ul');
            customerList.className = 'customer-list';
            leftPanel.appendChild(customerList);

            data.customers.forEach((customer, index) => {
                const li = document.createElement('li');
                li.className = 'customer-item';
                li.textContent = customer.customerName;
                li.style.cursor = 'pointer';
                li.onclick = () => {
                    document.querySelectorAll('.customer-item').forEach(item => item.classList.remove('selected'));
                    li.classList.add('selected');
                    showCustomerDetails(customer, index);
                };
                customerList.appendChild(li);
            });

            let refreshReport: (() => void) | null = null;
            let notifyTimeChanged: (() => void) | null = null;
            let flushActiveReport: (() => Promise<void>) | null = null;

            window.addEventListener('beforeunload', () => {
                if (flushActiveReport) flushActiveReport();
            });

            if (!timeSelector) {
                timeSelector = new TimeSelector('timeSelectorContainer', {
                    onChange: () => {
                        if (refreshReport) refreshReport();
                        if (notifyTimeChanged) notifyTimeChanged();
                    }
                });
                (window as unknown as Record<string, unknown>).timeSelector = timeSelector;
            }

            const pendingSelect = sessionStorage.getItem('tt-select-customer');
            if (pendingSelect) {
                sessionStorage.removeItem('tt-select-customer');
                const idx = data.customers.findIndex(c => c.customerName === pendingSelect);
                if (idx !== -1) {
                    const item = customerList.children[idx] as HTMLElement;
                    item.click();
                    item.scrollIntoView({ block: 'nearest' });
                }
            }

            function showCustomerDetails(customer: CustomerData, index: number) {
                if (flushActiveReport) flushActiveReport();
                refreshReport = null;
                notifyTimeChanged = null;
                flushActiveReport = null;

                timeSelectorContainer.classList.remove('hidden');
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
                            <option ${customer.workLoadType === 'Bucket Migration' ? 'selected' : ''}>Bucket Migration</option>
                            <option ${customer.workLoadType === 'OS Migration' ? 'selected' : ''}>OS Migration</option>
                            <option ${customer.workLoadType === 'HS Upgrade' ? 'selected' : ''}>HS Upgrade</option>
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
                            <button id="copyReportBtn" data-tooltip="copyReportBtn">${SVG_COPY}</button>
                        </div>
                    </div>
                `;

                rightPanel.appendChild(deleteButton);

                const reportInput = document.getElementById('reportInput') as HTMLTextAreaElement;
                const processedReport = document.getElementById('processedReport')!;
                const copyReportBtn = document.getElementById('copyReportBtn')!;
                const reportDate = document.getElementById('reportDate') as HTMLInputElement;
                const prevReportBtn = document.getElementById('prevReportBtn') as HTMLButtonElement;
                const nextReportBtn = document.getElementById('nextReportBtn') as HTMLButtonElement;
                const todayReportBtn = document.getElementById('todayReportBtn')!;

                let currentDate = localToday();
                let prevDate = '';
                let nextDate = '';
                let saveTimer: ReturnType<typeof setTimeout> | null = null;
                let dirty = false;
                let applyingRemote = false;

                function localToday() {
                    const d = new Date();
                    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
                }

                function updateNavState(state: { prevDate?: string; nextDate?: string }) {
                    prevDate = state.prevDate || '';
                    nextDate = state.nextDate || '';
                    prevReportBtn.disabled = !prevDate;
                    nextReportBtn.disabled = !nextDate;
                }

                function scheduleSave() {
                    if (applyingRemote) return;
                    dirty = true;
                    if (saveTimer) clearTimeout(saveTimer);
                    saveTimer = setTimeout(saveReportNow, 600);
                }

                function saveReportNow(): Promise<void> {
                    if (saveTimer) clearTimeout(saveTimer);
                    saveTimer = null;
                    if (!dirty) return Promise.resolve();
                    dirty = false;
                    return fetch(`/report`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        keepalive: true,
                        body: JSON.stringify({
                            customerName: customer.customerName,
                            date: currentDate,
                            body: reportInput.value,
                            timeBlocks: timeSelector!.getSelectedTimeBlocks()
                        })
                    })
                    .then(response => {
                        if (!response.ok) {
                            return response.text().then(text => { throw new Error(text) });
                        }
                        return response.json();
                    })
                    .then(state => updateNavState(state))
                    .catch(error => {
                        dirty = true;
                        console.error('Error saving report:', error);
                    });
                }

                function loadReport(date: string) {
                    saveReportNow()
                        .then(() => fetch(`/report?customer=${encodeURIComponent(customer.customerName)}&date=${encodeURIComponent(date)}`))
                        .then(response => {
                            if (!response.ok) {
                                return response.text().then(text => { throw new Error(text) });
                            }
                            return response.json();
                        })
                        .then(state => {
                            applyingRemote = true;
                            currentDate = state.date;
                            reportDate.value = state.date;
                            reportInput.value = state.body || '';
                            timeSelector!.setSelection(state.timeBlocks || []);
                            updateNavState(state);
                            processReport();
                            applyingRemote = false;
                        })
                        .catch(error => {
                            console.error('Error loading report:', error);
                        });
                }

                reportInput.addEventListener('input', () => {
                    processReport();
                    scheduleSave();
                });
                reportDate.addEventListener('change', () => {
                    if (reportDate.value) loadReport(reportDate.value);
                });
                prevReportBtn.addEventListener('click', () => { if (prevDate) loadReport(prevDate); });
                nextReportBtn.addEventListener('click', () => { if (nextDate) loadReport(nextDate); });
                todayReportBtn.addEventListener('click', () => loadReport(localToday()));
                copyReportBtn.addEventListener('click', copyReportToClipboard);

                refreshReport = processReport;
                notifyTimeChanged = scheduleSave;
                flushActiveReport = saveReportNow;
                loadReport(currentDate);

                document.querySelectorAll('.edit-btn').forEach(btn => {
                    btn.addEventListener('click', function(this: HTMLElement) {
                        const target = this.getAttribute('data-target')!;
                        const input = document.getElementById(`${target}Input`) as HTMLInputElement;
                        input.removeAttribute('readonly');
                        input.classList.remove('hidden');
                        document.querySelector(`button[data-target="${target}"]`)!.classList.remove('hidden');
                    });
                });

                document.querySelectorAll('.submit-btn').forEach(btn => {
                    btn.addEventListener('click', function(this: HTMLElement) {
                        const target = this.getAttribute('data-target')!;
                        const input = document.getElementById(`${target}Input`) as HTMLInputElement;
                        const link = document.getElementById(target);
                        const index = parseInt(this.getAttribute('data-index')!, 10);
                        const updatedValue = input.value;

                        const requestData = {
                            index: index,
                            field: target,
                            value: updatedValue
                        };

                        console.log('Sending request data:', requestData);

                        const self = this;
                        const sendUpdate = () => fetch(`/update`, {
                            method: 'POST',
                            headers: {
                                'Content-Type': 'application/json'
                            },
                            body: JSON.stringify(requestData)
                        })
                        .then(response => {
                            if (!response.ok) {
                                return response.text().then(text => { throw new Error(text) });
                            }
                            return response.json();
                        })
                        .then(_updatedCustomer => {
                            console.log('Received updated customer:', _updatedCustomer);
                            console.log('Target:', target);
                            if (target === 'jira') {
                                console.log("Updating JIRA link");
                                const existingLink = document.getElementById('jiraUrl');
                                if (existingLink) existingLink.remove();

                                const jiraLink = document.createElement('a');
                                jiraLink.id = 'jiraUrl';
                                jiraLink.href = `https://cloudian.atlassian.net/browse/PS-${input.value}`;
                                jiraLink.textContent = input.value;
                                jiraLink.setAttribute('data-tooltip', 'jira');

                                const parentDiv = input.parentNode as HTMLElement;
                                const label = parentDiv.querySelector('label')!;
                                parentDiv.insertBefore(jiraLink, label.nextSibling);

                                input.classList.add('hidden');
                            } else if (target === 'customerName') {
                                sessionStorage.setItem('tt-select-customer', input.value.trim());
                                location.reload();
                            } else if (target === 'supportBucket') {
                                input.setAttribute('readonly', 'true');
                                input.classList.remove('hidden');
                            } else {
                                if (link) {
                                    link.textContent = input.value;
                                    if (target === 'cmsUrl') {
                                        (link as HTMLAnchorElement).href = input.value;
                                    }
                                    input.classList.add('hidden');
                                }
                            }
                            self.classList.add('hidden');
                        })
                        .catch(error => {
                            console.error('Error updating customer:', error);
                        });

                        if (target === 'customerName' && flushActiveReport) {
                            flushActiveReport().then(sendUpdate);
                        } else {
                            sendUpdate();
                        }
                    });
                });

                document.getElementById('workLoadTypeSelect')!.addEventListener('change', function(this: HTMLSelectElement) {
                    const index = parseInt(this.getAttribute('data-index')!, 10);
                    const updatedValue = this.value;

                    const requestData = {
                        index: index,
                        field: 'workLoadType',
                        value: updatedValue
                    };

                    console.log('Sending request data:', requestData);

                    fetch(`/update`, {
                        method: 'POST',
                        headers: {
                            'Content-Type': 'application/json'
                        },
                        body: JSON.stringify(requestData)
                    })
                    .then(response => {
                        if (!response.ok) {
                            return response.text().then(text => { throw new Error(text) });
                        }
                        return response.json();
                    })
                    .then(updatedCustomer => {
                        console.log('Received updated customer:', updatedCustomer);
                    })
                    .catch(error => {
                        console.error('Error updating customer:', error);
                    });
                });

                function processReport() {
                    const report = reportInput.value;
                    const selectedDate = new Date(reportDate.value);
                    selectedDate.setMinutes(selectedDate.getMinutes() + selectedDate.getTimezoneOffset());
                    const formattedDate = selectedDate.toLocaleDateString('en-US', {
                        month: 'long',
                        day: 'numeric',
                        year: 'numeric'
                    });
                    const totalTime = (document.getElementById('totalTimeInput') as HTMLInputElement)?.value || '0h 0m';

                    const reportContent = report.trim() ? marked.parse(report.trim()) : '<p></p>';

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
                    const formattedDate = selectedDate.toLocaleDateString('en-US', {
                        month: 'long',
                        day: 'numeric',
                        year: 'numeric'
                    });
                    const totalTime = (document.getElementById('totalTimeInput') as HTMLInputElement)?.value || '0h 0m';

                    const markdownReport = `### Date: ${formattedDate}

Customer: ${customer.customerName}
Author: ${data.author}

${reportInput.value.trim()}

Time related to update ${formattedDate}
Hours worked: ${totalTime}`;

                    const tempElement = document.createElement('textarea');
                    tempElement.value = markdownReport;
                    document.body.appendChild(tempElement);
                    tempElement.select();
                    document.execCommand('copy');
                    document.body.removeChild(tempElement);
                    showMessage('Report copied.');
                }

                deleteButton.onclick = function() {
                    modal.classList.remove('hidden');
                };

                document.getElementById('confirmDeleteBtn')!.onclick = function() {
                    deleteCustomer(index);
                    modal.classList.add('hidden');
                };

                document.getElementById('cancelDeleteBtn')!.onclick = function() {
                    modal.classList.add('hidden');
                };

                function deleteCustomer(index: number) {
                    const requestData = {
                        index: index
                    };

                    fetch(`/delete`, {
                        method: 'POST',
                        headers: {
                            'Content-Type': 'application/json'
                        },
                        body: JSON.stringify(requestData)
                    })
                    .then(response => {
                        if (!response.ok) {
                            return response.text().then(text => { throw new Error(text) });
                        }
                        return response.json();
                    })
                    .then(updatedData => {
                        console.log('Customer deleted:', updatedData);
                        showMessage('Customer deleted');
                        location.reload();
                    })
                    .catch(error => {
                        console.error('Error deleting customer:', error);
                    });
                }

                initializeTooltips();
            }

            document.querySelector('.edit-btn[data-target="author"]')!.addEventListener('click', function(this: HTMLElement) {
                const input = document.getElementById('authorInput') as HTMLInputElement;
                input.removeAttribute('readonly');
                input.classList.remove('hidden');
                document.querySelector('button[data-target="author"]')!.classList.remove('hidden');
            });

            document.querySelector('button[data-target="author"]')!.addEventListener('click', function(this: HTMLElement) {
                const input = document.getElementById('authorInput') as HTMLInputElement;
                const updatedValue = input.value;

                const requestData = {
                    index: -1,
                    field: 'author',
                    value: updatedValue
                };

                console.log('Sending request data:', requestData);

                const self = this;
                fetch(`/update`, {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify(requestData)
                })
                .then(response => {
                    if (!response.ok) {
                        return response.text().then(text => { throw new Error(text) });
                    }
                    return response.json();
                })
                .then(_updatedData => {
                    console.log('Received updated data:', _updatedData);
                    input.setAttribute('readonly', 'true');
                    input.classList.remove('hidden');
                    self.classList.add('hidden');
                    showMessage('Author updated');
                })
                .catch(error => {
                    console.error('Error updating author:', error);
                });
            });
        });
});
