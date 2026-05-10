data = {};
taggedcsv = [];
config={};
async function fetchStatus() {
  try {
    const response = await fetch("/status");
    data = await response.json();

    total = data.queued.length + data.running.length + data.completed.length;

    if (total == 0) {
      document.getElementById("progress").value = 0;
    } else {
      document.getElementById("progress").value =
        (data.completed.length * 100) / total;
    }
    updateTable("queuedTasks", data.queued);
    updateTable("runningTasks", data.running, true);
    updateTable("completedTasks", data.completed, false, true);
  } catch (error) {
    console.error("Error fetching status:", error);
  }
}

async function fetchTaggedCSV() {
  try {
    const response = await fetch("/taggedcsv", {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    taggedcsv = await response.json();
    updateTableCSV("taggedCSV", taggedcsv);
  } catch (error) {
    console.error("Fetch error:", error);
  }
}

async function fetchConfig() {
  try {
    const response = await fetch("/config", {});

    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    config = await response.json();
    
    //disabling taggecCSV fetch for now
    //fetchTaggedCSV();
    //setInterval(fetchTaggedCSV, config.refreshRate*1000);
  } catch (error) {
    console.error("Fetch error:", error);
  }
}

function openLightBox(content) {
  lightboxContent.innerHTML = "<pre>" + content + "</pre>";
  lightbox.style.display = "block";
}

function openLightBoxTask(t) {
  console.log("task id=" + t);
  console.log("len=" + data.completed.length);
  console.log("before for loop");

  for (let i = 0; i < data.completed.length; i++) {
    console.log(JSON.stringify(data.completed[i], null, 2));
    if (t == data.completed[i].id) {
      openLightBox(data.completed[i].output.join("\n"));
      return;
    }
  }
}

function GetLineId(bucket) {
  console.log("GetLineId(" + bucket + ")");
  console.log("length of taggedcsv is " + taggedcsv.length);
  for (var i = 0; i < taggedcsv.length; i++) {
    console.log("i=" + i);
    console.log("line=" + taggedcsv[i]);
    if (taggedcsv[i].tgtBucket == bucket) {
      return i;
    }
  }
}
function BucketToBucketRow(i) {
  return (
    "<tr><td>Transaction:</td><td>" +
    taggedcsv[i].srcBucket +
    ' <i class="arrow right"></i> ' +
    taggedcsv[i].action +
    ' <i class="arrow right"></i> ' +
    taggedcsv[i].tgtBucket +
    "</td></tr>"
  );
}

function HumanReadableCapacity(bytes) {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let size = bytes;
  let unitIndex = 0;

  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex++;
  }

  return `${size.toFixed(2)} ${units[unitIndex]}`;
}
function addCommasToNumber(number) {
  number = Math.floor(number);
  return number.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

function formatSeconds(totalSeconds) {
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  const formatNumber = (num) => num.toString().padStart(2, "0");

  let result = "";
  if (hours > 0) result += `${hours}h:`;
  result += `${formatNumber(minutes)}m:${formatNumber(seconds)}s`;

  return result;
}

function SecondsToTimestamp(seconds) {
  const date = new Date(seconds * 1000);

  const year = date.getFullYear();
  const month = (date.getMonth() + 1).toString().padStart(2, "0");
  const day = date.getDate().toString().padStart(2, "0");
  const hours = date.getHours().toString().padStart(2, "0");
  const minutes = date.getMinutes().toString().padStart(2, "0");
  const secs = date.getSeconds().toString().padStart(2, "0");

  return `${year}-${month}-${day} ${hours}:${minutes}:${secs}`;
}

function openLightBoxStatus(bucket) {
  const nowInSeconds = Math.floor(Date.now() / 1000);

  i = GetLineId(bucket);
  console.log("csv line id=" + i);
  // console.log("len="+data.completed.length)
  // console.log("before for loop")

  // for (let i = 0; i < data.completed.length; i++) {
  //     console.log(JSON.stringify(data.completed[i], null, 2))
  //     if (t==data.completed[i].id) {
  //         openLightBox(data.completed[i].output.join('\n'))
  //         return
  //     }
  //   }

  //    openLightBox("bucket="+ bucket+" line # "+t)

  content = "<table><tr><td>ID: </td><td>" + i + "</td></tr>";

  content += BucketToBucketRow(i);

  if (taggedcsv[i].stats.startTime == 0 || taggedcsv[i].stats.endTime != 0) {
    content += "<tr><td>Status: </td><td>idle</td></tr>";
  } else {
    content += "<tr><td>Status: </td><td>running</td></tr>";
  }

  if (taggedcsv[i].stats.startTime > 0) {
    console.log("get here");
    content += "<tr><td>Objects Processed: </td><td>";
    if (taggedcsv[i].stats.totalObjects == 0) {
      content +=
        addCommasToNumber(taggedcsv[i].stats.objectsProcessed) + " / ???";
      opsrate = 0;
    } else {
      content +=
        addCommasToNumber(taggedcsv[i].stats.objectsProcessed) +
        " / " +
        addCommasToNumber(taggedcsv[i].stats.totalObjects);
      if (taggedcsv[i].stats.endTime == 0) {
        opsrate =
          taggedcsv[i].stats.objectsProcessed /
          (nowInSeconds - taggedcsv[i].stats.startTime); //calc bytes per second
      } else {
        opsrate =
          taggedcsv[i].stats.objectsProcessed /
          (taggedcsv[i].stats.endTime - taggedcsv[i].stats.startTime); //calc bytes per second
      }
    }
    content += "</td></tr>";

    content += "<tr><td>Capacity Processed:</td><td>";
    if (taggedcsv[i].stats.totalBytes == 0) {
      content +=
        HumanReadableCapacity(taggedcsv[i].stats.bytesProcessed) + " / ???";
      byterate = 0;
    } else {
      content +=
        HumanReadableCapacity(taggedcsv[i].stats.bytesProcessed) +
        " / " +
        HumanReadableCapacity(taggedcsv[i].stats.totalBytes);
      if (taggedcsv[i].stats.endTime == 0) {
        byterate =
          taggedcsv[i].stats.bytesProcessed /
          (nowInSeconds - taggedcsv[i].stats.startTime); //calc bytes per second
      } else {
        byterate =
          taggedcsv[i].stats.bytesProcessed /
          (taggedcsv[i].stats.endTime - taggedcsv[i].stats.startTime); //calc bytes per second
      }
    }
    content += "</td></tr>";
    content += "<tr><td>Run Time:</td><td>";
    if (taggedcsv[i].stats.endTime == 0) {
      content += formatSeconds(nowInSeconds - taggedcsv[i].stats.startTime);
      content += "</td></tr><tr><td>Expected Completion Time:</td><td>";

      if (taggedcsv[i].stats.totalBytes == 0) {
        content += "???";
      } else {
        // content += SecondsToTimestamp(
        //   (taggedcsv[i].stats.totalBytes - taggedcsv[i].stats.bytesProcessed) /
        //     byterate +
        //     nowInSeconds
        // );
        content += formatSeconds(
          Math.floor(
            (taggedcsv[i].stats.totalBytes -
              taggedcsv[i].stats.bytesProcessed) /
              byterate
          )
        );
      }
      content += "</td></tr>";
    } else {
      content += formatSeconds(
        taggedcsv[i].stats.endTime - taggedcsv[i].stats.startTime
      );
      content +=
        "</td></tr><tr><td>Processed terminated at:</td><td>" +
        SecondsToTimestamp(taggedcsv[i].stats.endTime);
    }
    content += "</td></tr>";

    content += "<tr><td>Through put:</td>";
    content += "<td>" + HumanReadableCapacity(byterate) + "/s </td></tr>";
    content += "<tr><td>Processing Rate (Objects/s):</td>";

    if (opsrate < 1) {
      content += "<td>less than 1 object per second</td></tr>";
    } else {
      content += "<td>" + addCommasToNumber(opsrate) + "/s </td></tr>";
    }
  } else {
    console.log("got here because start time is 0?");
    console.log("start time = " + taggedcsv[i].stats.startTime);
    content += "<tr><td colspan=2>Process Not started</td></tr>";
  }
  lightboxContent.innerHTML = content;
  lightbox.style.display = "block";
}

function updateTable(tableId, tasks, isRunning = false, isCompleted = false) {
  const tableBody = document.getElementById(tableId);
  tableBody.innerHTML = "";

  tasks.forEach((task) => {
    const row = document.createElement("tr");

    if (!isRunning && !isCompleted) {
      row.innerHTML = `
            <td>${task.id}</td>
            <td>${task.command}</td>
        `;
    } else {
      row.innerHTML = `
            <td>${task.id}</td>
            <td>${task.command}</td>
            <td>${
              isRunning || isCompleted
                ? formatDuration(task.starttime, task.duration)
                : ""
            }</td>
            ${
              isCompleted
                ? `<td><button id="button"+${task.id}+'" onclick="openLightBoxTask(${task.id})">View Output</button></td>`
                : ""
            }
        `;
    }
    tableBody.appendChild(row);
  });

  // tasks.forEach(task => {
  //     const row = document.createElement('tr');

  //     row.innerHTML = `
  //       <td>${task.id}</td>
  //       <td>${task.command}</td>
  //       <td>${(isRunning || isCompleted) ? formatDuration(task.starttime, task.duration) : ''}</td>
  //       ${isCompleted ? `<td><button id="button${task.id}" onclick="openLightBoxTask(${task.id})">View Output</button></td>` : ''}
  //       ${isRunning && !isCompleted ? '<td><div>circle</div></td>' : ''}
  //     `;
  //     tableBody.appendChild(row);
  //   });
}

function createTableElement(ep, bucket, groupId, userId) {
  return (
    "<table><tr><td>EP: </td><td>" +
    ep +
    "/" +
    bucket +
    "</td></tr><tr><td>GroupId|UserId: </td><td>" +
    groupId +
    "|" +
    userId +
    "</td></tr></table>"
  );
}

function createButtonTable(bucket, prog, cmvs) {
  cdisabled = "";
  mdisabled = "";
  vdisabled = "";

  if (!cmvs.includes("c")) {
    cdisabled = "disabled";
  }
  if (!cmvs.includes("m")) {
    mdisabled = "disabled";
  }
  if (!cmvs.includes("v")) {
    vdisabled = "disabled";
  }

  return `<table><tr><td><form class="bucket-button-container" onsubmit="return false;">
    <button type="submit" 
            class="bucket-action-button" 
            onclick="handleAction('collect', '${bucket}', event)" ${cdisabled}>
      Collect
    </button>
    <button type="submit" 
            class="bucket-action-button" 
            onclick="handleAction('migrate', '${bucket}', event)" ${mdisabled}>
      Migrate
    </button>
    <button type="submit" 
            class="bucket-action-button" 
            onclick="handleAction('verify', '${bucket}', event)" ${vdisabled}>
      Verify
    </button>
    <button type="submit" 
            class="bucket-action-button" 
            onclick="handleAction('status', '${bucket}', event)">
      Status
    </button>
  </form></td></tr><tr><div id="progress-container-${bucket}">
                <label for="progress">Progress:</label>
                <progress id="progress-${bucket}" value="${prog}" max="100"></progress>
            </div>`;
}

function updateTableCSV(tableId, arr) {
  const tableBody = document.getElementById(tableId);
  tableBody.innerHTML =
    "<thead><tr><th colspan=2>Source</th><th>Target</th><th>Action</th></tr></thead>";

  console.log(JSON.stringify(arr, null, 3));

  for (var i = 0; i < arr.length; i++) {
    const row = document.createElement("tr");

    row.innerHTML = `
        <td>${createTableElement(
          arr[i].srcEP,
          arr[i].srcBucket,
          arr[i].srcGroupId,
          arr[i].srcUserId
        )}</td>
        <td><i class="arrow right"></i></td>
        <td>${createTableElement(
          arr[i].tgtEP,
          arr[i].tgtBucket,
          arr[i].tgtGroupId,
          arr[i].tgtUserId
        )}</td>
        <td>${createButtonTable(arr[i].tgtBucket, arr[i].progress, "")}</td>
        `;
    tableBody.appendChild(row);
  }
}

// function formatCell(starttime, duration) {
//     return `
//         <div style="display: flex; justify-content: space-between; align-items: center;">
//             <span>${formatDuration(starttime, duration * 1000000)}</span>
//             <div class="twirl"></div>
//         </div>
//     `;
// }

function formatDuration(starttime, duration) {
  if (starttime == undefined) {
    console.log("startime was undefined");
    starttime = 0;
  }
  if (duration == undefined) {
    // console.log("duration was undefined")
    // console.log("st="+starttime)
    // console.log("n ="+Date.now())
    duration = Date.now() - starttime;
    return formatDuration(starttime, duration * 1000000);
  }

  const seconds = Math.floor(duration / 1000000000);
  const minutes = Math.floor(seconds / 60);
  return `${minutes}m ${seconds % 60}s`;
}

function showOutput(output) {
  document.getElementById("modal-output").textContent = output;
  document.getElementById("modal").classList.remove("hidden");
}

function closeModal() {
  document.getElementById("modal").classList.add("hidden");
}

// async function addTask(event) {
//     event.preventDefault();
//     const commandInput = document.getElementById('cmd');
//     const command = commandInput.value;
//     commandInput.value = '';

//     const response = await fetch('/add', {
//         method: 'POST',
//         headers: {
//             'Content-Type': 'application/json',
//         },
//         body: JSON.stringify({ command }),
//     });

//     if (response.ok) {
//         fetchStatus();
//     } else {
//         alert('Failed to add task');
//     }
// }

function toggleTable(tableId) {
  const table = document.getElementById(tableId);
  if (table.classList.contains("hidden")) {
    table.classList.remove("hidden");
  } else {
    table.classList.add("hidden");
  }
}

/*** add task to queue ***/
async function addTask() {
  console.log("addTask()");
  const Command = document.getElementById("Command").value;
  const response = await fetch("/task", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ Command }),
  });

  if (response.ok) {
    fetchStatus();
  } else {
    alert("Failed to add task");
  }
}

async function addExistingProcess() {
  console.log("addExistingProcess()");
  const Pid = parseInt(document.getElementById("Pid").value, 10);
  const Command = "";
  const response = await fetch("/task", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ Command, Pid }),
  });

  if (response.ok) {
    fetchStatus();
  } else {
    alert("Failed to add task");
  }
}

function closeLightBox() {
  //document.getElementById('lightbox').classList.add('hidden');
  document.getElementById("lightbox").style = "display:none";
  //classList.add('hidden');
}

// closeButton.addEventListener('click', () => {
//     lightbox.style.display = 'none';
// });

// lightbox.addEventListener('click', (e) => {
//     if (e.target === lightbox) {
//         lightbox.style.display = 'none';
//     }
// });
// try {
//     await axios.put(`/task`, newTask);
//     alert('task sent successfully');
//     //getConfig(); // Refresh the displayed config
// } catch (error) {
//     console.error('Error sending task:', error);
//     alert('Error sending task');
// }

function openTab(evt, tabName) {
  // Declare all variables
  var i, tabcontent, tablinks;

  // Get all elements with class="tabcontent" and hide them
  tabcontent = document.getElementsByClassName("tabcontent");
  for (i = 0; i < tabcontent.length; i++) {
    tabcontent[i].style.display = "none";
  }

  // Get all elements with class="tablinks" and remove the class "active"
  tablinks = document.getElementsByClassName("tablinks");
  for (i = 0; i < tablinks.length; i++) {
    tablinks[i].className = tablinks[i].className.replace(" active", "");
  }

  // Show the current tab, and add an "active" class to the button that opened the tab
  document.getElementById(tabName).style.display = "block";
  evt.currentTarget.className += " active";
}
function handleAction(action, bucket, event) {
  event.preventDefault();
  console.log("Action:", action, "Bucket:", bucket);
  // Here you would handle the submission with both values

  switch (action) {
    case "collect":
      console.log("submit a request to run collect for bucket " + bucket);
      break;
    case "migrate":
      console.log("submit a request to run migrate for bucket " + bucket);
      break;
    case "verify":
      console.log("submit a request to run verify for bucket " + bucket);
      break;
    case "status":
      console.log("submit a request to run status for bucket " + bucket);

      openLightBoxStatus(bucket);
      break;
    default:
      console.log(
        "error: unknown action: ${action} for bucket ${bucket} requested"
      );
  }
  return false;
}

//fetchStatus();
fetchConfig();
//fetchTaggedCSV();

// if (config==null || config.refreshRate == 0 || config.refreshRate == undefined) {
//   console.log("error retrieving refresh rate from config");
//   console.log(JSON.stringify(config, null, 2));
// } else {
//   console.log("refresh rate is " + config.refreshRate);
//   setInterval(fetchTaggedCSV, config.refreshRate*1000);
// }

