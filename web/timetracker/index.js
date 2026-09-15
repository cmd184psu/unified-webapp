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

    addButton.onclick = function() {
        const newCustomer = {
            customerName: '',
            slackChannel: '',
            slackChannelId: '',
            workLoadType: '',
            cmsUrl: '',
            supportBucket: '',
            jira: ''
        };

        const requestData = {
            // The server appends regardless of index for newCustomer; any
            // value other than -1 (the author path) works. The reference
            // read `data.customers.length` here, but `data` is not in scope
            // at this point in a module script, so the click threw a
            // ReferenceError and the button did nothing.
            index: 0,
            field: 'newCustomer',
            value: newCustomer
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
            showMessage('Customer added');
            location.reload();
        })
        .catch(error => {
            console.error('Error adding customer:', error);
        });
    };
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
                <h2>Version: ${data.version}</h2>
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

            // Re-render the currently shown report when the time selection
            // changes; showCustomerDetails points this at its processReport.
            let refreshReport = null;

            // Initialize TimeSelector once after data is loaded
            if (!timeSelector) {
                timeSelector = new TimeSelector('timeSelectorContainer', {
                    onChange: () => { if (refreshReport) refreshReport(); }
                });
                window.timeSelector = timeSelector;
            }

            function showCustomerDetails(customer, index) {
                // Show the time selector and clear it when switching customers
                timeSelectorContainer.classList.remove('hidden');
                if (timeSelector) {
                    timeSelector.clearSelection();
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
                                <input type="date" id="reportDate" data-tooltip="reportDate">
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
                reportInput.value = "Type your report here...";
                const processedReport = document.getElementById('processedReport');
                const copyReportBtn = document.getElementById('copyReportBtn');
                const reportDate = document.getElementById('reportDate');

                const today = new Date();
                reportDate.value = today.toISOString().split('T')[0];

                reportInput.addEventListener('input', processReport);
                reportDate.addEventListener('change', processReport);
                copyReportBtn.addEventListener('click', copyReportToClipboard);
                refreshReport = processReport;

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
                            } else if (target === 'customerName' || target === 'supportBucket') {
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