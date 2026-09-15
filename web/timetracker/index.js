import TimeSelector from './TimeSelector.js';  // Changed to default import

document.addEventListener('DOMContentLoaded', function() {
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
    timeSelectorContainer.className = 'time-selector-container hidden';  // Start hidden
    timeSelectorContainer.id = 'timeSelectorContainer';  // Keep ID for selector, but use class for styling
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

    // Adding a customer asks for the name first: the server rejects blank
    // names, and creating an empty record up front (the reference behavior)
    // left an unnamed row in the list that then had to be edited into shape.
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
        const nameInput = addModal.querySelector('#newCustomerNameInput');
        nameInput.value = '';
        addModal.classList.remove('hidden');
        nameInput.focus();
    };

    function submitNewCustomer() {
        const nameInput = addModal.querySelector('#newCustomerNameInput');
        const name = nameInput.value.trim();
        if (!name) {
            nameInput.focus();
            return;
        }

        const requestData = {
            // The server appends regardless of index for newCustomer; any
            // value other than -1 (the author path) works.
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
            // Jump straight to the new customer's form after the reload.
            sessionStorage.setItem('tt-select-customer', name);
            location.reload();
        })
        .catch(error => {
            console.error('Error adding customer:', error);
        });
    }

    addModal.querySelector('#confirmAddBtn').onclick = submitNewCustomer;
    addModal.querySelector('#cancelAddBtn').onclick = function() {
        addModal.classList.add('hidden');
    };
    addModal.querySelector('#newCustomerNameInput').addEventListener('keydown', function(e) {
        if (e.key === 'Enter') submitNewCustomer();
    });

    document.body.appendChild(addModal);
    document.body.appendChild(modal);

    document.body.appendChild(footer);

    function showMessage(message) {
        messageBar.textContent = message;
        messageBar.classList.remove('hidden');
        setTimeout(() => {
            messageBar.classList.add('hidden');
        }, 10000);
    }

    let tooltips = {};

    fetch('/tooltips.json')
        .then(response => response.json())
        .then(data => {
            tooltips = data;
            initializeTooltips();
        });

    function initializeTooltips() {
        document.querySelectorAll('[data-tooltip]').forEach(element => {
            const tooltipText = tooltips[element.getAttribute('data-tooltip')];
            if (tooltipText) {
                element.setAttribute('title', tooltipText);
            }
        });
    }

    // Initialize TimeSelector once, outside of showCustomerDetails
    let timeSelector = null;
    
    fetch('/data')
        .then(response => response.json())
        .then(data => {
            header.innerHTML = `
                <h1>${data.projectName}</h1>
                <div class="form-group">
                    <label data-tooltip="author">Author:</label>
                    <input type="text" id="authorInput" value="${data.author}" readonly data-tooltip="author">
                    <i class="fas fa-edit edit-btn" data-target="author"></i>
                    <button class="submit-btn hidden" data-target="author">Submit</button>
                    <button id="exportBtn" class="export-btn" data-tooltip="exportData">
                        <i class="fas fa-download"></i>
                    </button>
                </div>
            `;

            document.getElementById('exportBtn').addEventListener('click', function() {
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

            // Hooks the active customer view installs so the shared time
            // selector and page-level events can reach its report state:
            // refreshReport re-renders the preview, notifyTimeChanged queues
            // an auto-save, flushActiveReport saves pending edits immediately.
            let refreshReport = null;
            let notifyTimeChanged = null;
            let flushActiveReport = null;

            window.addEventListener('beforeunload', () => {
                if (flushActiveReport) flushActiveReport();
            });

            // Initialize TimeSelector once after data is loaded
            if (!timeSelector) {
                timeSelector = new TimeSelector('timeSelectorContainer', {
                    onChange: () => {
                        if (refreshReport) refreshReport();
                        if (notifyTimeChanged) notifyTimeChanged();
                    }
                });
                window.timeSelector = timeSelector;
            }

            // After a reload triggered by add-customer or rename, jump
            // straight to that customer's form.
            const pendingSelect = sessionStorage.getItem('tt-select-customer');
            if (pendingSelect) {
                sessionStorage.removeItem('tt-select-customer');
                const idx = data.customers.findIndex(c => c.customerName === pendingSelect);
                if (idx !== -1) {
                    const item = customerList.children[idx];
                    item.click();
                    item.scrollIntoView({ block: 'nearest' });
                }
            }

            function showCustomerDetails(customer, index) {
                // Save the outgoing customer's pending report edits, then
                // detach the hooks so nothing fires mid-rebuild.
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
                        <i class="fas fa-edit edit-btn" data-target="customerName"></i>
                        <button class="submit-btn hidden" data-target="customerName" data-index="${index}">Submit</button>
                    </div>
                    <div class="form-group">
                        <label data-tooltip="slackChannel">Slack Channel:</label>
                        <a href="slack://channel?team=T12DX4MJR&id=${customer.slackChannelId}" id="slackChannel" data-tooltip="slackChannel">${customer.slackChannel}</a>
                        <i class="fas fa-edit edit-btn" data-target="slackChannel"></i>
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
                        <i class="fas fa-edit edit-btn" data-target="cmsUrl"></i>
                        <input type="text" id="cmsUrlInput" class="hidden" value="${customer.cmsUrl}" data-tooltip="cmsUrl">
                        <button class="submit-btn hidden" data-target="cmsUrl" data-index="${index}">Submit</button>
                    </div>
                    <div class="form-group">
                        <label data-tooltip="supportBucket">Support Bucket:</label>
                        <input type="text" id="supportBucketInput" value="${customer.supportBucket}" readonly data-tooltip="supportBucket">
                        <i class="fas fa-edit edit-btn" data-target="supportBucket"></i>
                        <button class="submit-btn hidden" data-target="supportBucket" data-index="${index}">Submit</button>
                    </div>
                    <div class="form-group">
                        <label data-tooltip="jira">JIRA #:</label>
                        <a href="https://cloudian.atlassian.net/browse/PS-${customer.jira}" id="jiraUrl" data-tooltip="jira">${customer.jira}</a>
                        <i class="fas fa-edit edit-btn" data-target="jira"></i>
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
                            <button id="copyReportBtn" data-tooltip="copyReportBtn"><i class="fas fa-copy"></i></button>
                        </div>
                    </div>
                `;

                rightPanel.appendChild(deleteButton);

                const reportInput = document.getElementById('reportInput');
                const processedReport = document.getElementById('processedReport');
                const copyReportBtn = document.getElementById('copyReportBtn');
                const reportDate = document.getElementById('reportDate');
                const prevReportBtn = document.getElementById('prevReportBtn');
                const nextReportBtn = document.getElementById('nextReportBtn');
                const todayReportBtn = document.getElementById('todayReportBtn');

                // Reports persist server-side per (customer, date). Edits
                // auto-save after a short pause; the arrows rewind through
                // the dates that actually have a stored report.
                let currentDate = localToday();
                let prevDate = '';
                let nextDate = '';
                let saveTimer = null;
                let dirty = false;
                let applyingRemote = false;

                function localToday() {
                    const d = new Date();
                    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
                }

                function updateNavState(state) {
                    prevDate = state.prevDate || '';
                    nextDate = state.nextDate || '';
                    prevReportBtn.disabled = !prevDate;
                    nextReportBtn.disabled = !nextDate;
                }

                function scheduleSave() {
                    if (applyingRemote) return;
                    dirty = true;
                    clearTimeout(saveTimer);
                    saveTimer = setTimeout(saveReportNow, 600);
                }

                function saveReportNow() {
                    clearTimeout(saveTimer);
                    saveTimer = null;
                    if (!dirty) return Promise.resolve();
                    dirty = false;
                    return fetch(`/report`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        // keepalive lets the flush on page unload complete
                        keepalive: true,
                        body: JSON.stringify({
                            customerName: customer.customerName,
                            date: currentDate,
                            body: reportInput.value,
                            timeBlocks: timeSelector.getSelectedTimeBlocks()
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
                        dirty = true;  // retry on the next edit or flush
                        console.error('Error saving report:', error);
                    });
                }

                function loadReport(date) {
                    // Flush pending edits for the current date before the
                    // view swings to another one.
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
                            timeSelector.setSelection(state.timeBlocks || []);
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
                    btn.addEventListener('click', function() {
                        const target = this.getAttribute('data-target');
                        const input = document.getElementById(`${target}Input`);
                        input.removeAttribute('readonly');
                        input.classList.remove('hidden');
                        document.querySelector(`button[data-target="${target}"]`).classList.remove('hidden');
                    });
                });

                document.querySelectorAll('.submit-btn').forEach(btn => {
                    btn.addEventListener('click', function() {
                        const target = this.getAttribute('data-target');
                        const input = document.getElementById(`${target}Input`);
                        const link = document.getElementById(target);
                        const index = parseInt(this.getAttribute('data-index'), 10);
                        const updatedValue = input.value;

                        const requestData = {
                            index: index,
                            field: target,
                            value: updatedValue
                        };

                        console.log('Sending request data:', requestData);

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
                        .then(updatedCustomer => {
                            console.log('Received updated customer:', updatedCustomer);
                            console.log('Target:', target);
                            if (target === 'jira') {
                                console.log("Updating JIRA link");
                                const existingLink = document.getElementById('jiraUrl');
                                if (existingLink) existingLink.remove();
                                
                                const jiraLink = document.createElement('a');
                                jiraLink.id = 'jiraUrl';
                                // The /update response is the full Data envelope, not a
                                // customer, so render the value that was just submitted.
                                jiraLink.href = `https://cloudian.atlassian.net/browse/PS-${input.value}`;
                                jiraLink.textContent = input.value;
                                jiraLink.setAttribute('data-tooltip', 'jira');
                                
                                const parentDiv = input.parentNode;
                                const label = parentDiv.querySelector('label');
                                parentDiv.insertBefore(jiraLink, label.nextSibling);
                                
                                input.classList.add('hidden');
                            } else if (target === 'customerName') {
                                // Renaming can move every customer's sorted
                                // position, so reload to resync the list and
                                // all data-index attributes — then jump back
                                // to this customer under its new name.
                                sessionStorage.setItem('tt-select-customer', input.value.trim());
                                location.reload();
                            } else if (target === 'supportBucket') {
                                input.setAttribute('readonly', true);
                                input.classList.remove('hidden');
                            } else {
                                if (link) {
                                    link.textContent = input.value;
                                    if (target === 'cmsUrl') {
                                        link.href = input.value;
                                    }
                                    input.classList.add('hidden');
                                }
                            }
                            this.classList.add('hidden');
                        })
                        .catch(error => {
                            console.error('Error updating customer:', error);
                        });

                        if (target === 'customerName' && flushActiveReport) {
                            // Save pending report edits under the old name
                            // first so the server-side rename migrates them
                            // along with the rest of the report history.
                            flushActiveReport().then(sendUpdate);
                        } else {
                            sendUpdate();
                        }
                    });
                });

                document.getElementById('workLoadTypeSelect').addEventListener('change', function() {
                    const index = parseInt(this.getAttribute('data-index'), 10);
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
                    const totalTime = document.getElementById('totalTimeInput')?.value || '0h 0m';
                    
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
                    const totalTime = document.getElementById('totalTimeInput')?.value || '0h 0m';
                    
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

                function updateCustomerData(index, field, value) {
                    const requestData = {
                        index: index,
                        field: field,
                        value: value
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
                    .then(updatedCustomer => {
                        console.log('Updated customer data:', updatedCustomer);
                    })
                    .catch(error => {
                        console.error('Error updating customer data:', error);
                    });
                }

                deleteButton.onclick = function() {
                    modal.classList.remove('hidden');
                };

                document.getElementById('confirmDeleteBtn').onclick = function() {
                    deleteCustomer(index);
                    modal.classList.add('hidden');
                };

                document.getElementById('cancelDeleteBtn').onclick = function() {
                    modal.classList.add('hidden');
                };

                function deleteCustomer(index) {
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

            document.querySelector('.edit-btn[data-target="author"]').addEventListener('click', function() {
                const input = document.getElementById('authorInput');
                input.removeAttribute('readonly');
                input.classList.remove('hidden');
                document.querySelector('button[data-target="author"]').classList.remove('hidden');
            });

            document.querySelector('button[data-target="author"]').addEventListener('click', function() {
                const input = document.getElementById('authorInput');
                const updatedValue = input.value;

                const requestData = {
                    index: -1,
                    field: 'author',
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
                .then(updatedData => {
                    console.log('Received updated data:', updatedData);
                    input.setAttribute('readonly', true);
                    input.classList.remove('hidden');
                    this.classList.add('hidden');
                    showMessage('Author updated');
                })
                .catch(error => {
                    console.error('Error updating author:', error);
                });
            });
        });
});