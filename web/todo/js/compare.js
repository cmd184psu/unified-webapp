// compare.js — side-by-side list compare/merge view.
// Self-contained: does not touch the globals used by todo.js / todo-utils.js.

var lists = [];  // subject catalog, shared by both side pickers: [{subject, entries:[...]}]

var left  = { subject: null, item: null, list: [], title: '' };
var right = { subject: null, item: null, list: [], title: '' };

var dupMatches = {}; // key "side:index" -> array of "side:index" partner keys

function otherSide(side) { return side === 'left' ? right : left; }
function stateFor(side)  { return side === 'left' ? left  : right; }

// ── Load / save ──────────────────────────────────────────────────────────

async function loadSide(side, subject, item) {
    var state = stateFor(side);
    var data = await ajaxGetJSON('items/' + subject + '/' + item);
    state.subject = subject;
    state.item = item;
    state.list = Array.isArray(data) ? data : (data.list || []);
    state.title = Array.isArray(data) ? '' : (data.title || '');
    dupMatches = {};
    renderSide(side);
    updatePickers(side);
    updateURL();
}

function saveSide(side) {
    var state = stateFor(side);
    if (!state.subject || !state.item) return;
    var body = { title: state.title || '', list: state.list };
    $.ajax({
        url: 'items/' + state.subject + '/' + state.item,
        type: 'post',
        dataType: 'json',
        contentType: 'application/json',
        data: JSON.stringify(body),
        error: function (err) {
            console.log(JSON.stringify(err, null, 3));
            alert('Failed to save ' + side + ' list.');
        }
    });
}

// ── Rendering ────────────────────────────────────────────────────────────

function renderSide(side) {
    var state = stateFor(side);
    var tbody = document.getElementById(side + '-table');
    tbody.innerHTML = '';

    document.getElementById(side + '-title').textContent =
        state.title || state.item || 'No list selected';
    document.getElementById(side + '-empty').style.display =
        state.list.length === 0 ? 'block' : 'none';

    state.list.forEach(function (it, idx) {
        tbody.appendChild(renderRow(side, it, idx));
    });

    applyDupHighlights();
}

function renderRow(side, item, idx) {
    var tr = document.createElement('tr');
    tr.className = 'compare-row' + (item.skip ? ' completeClass' : '');
    tr.dataset.side = side;
    tr.dataset.idx = idx;
    tr.draggable = true;

    var tdCheck = document.createElement('td');
    tdCheck.className = 'cmp-check';
    var cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.checked = !!item.skip;
    cb.onclick = function () { toggleSkip(side, idx); };
    tdCheck.appendChild(cb);

    var tdName = document.createElement('td');
    tdName.className = 'cmp-name';
    tdName.textContent = item.name;

    var tdBadge = document.createElement('td');
    tdBadge.className = 'cmp-badge';

    var tdSend = document.createElement('td');
    tdSend.className = 'cmp-send';
    var sendBtn = document.createElement('button');
    sendBtn.className = 'icon-btn cmp-send-btn';
    sendBtn.title = side === 'left' ? 'Send to right' : 'Send to left';
    sendBtn.innerHTML = side === 'left'
        ? '<i class="fas fa-arrow-right"></i>'
        : '<i class="fas fa-arrow-left"></i>';
    sendBtn.onclick = function () { sendItem(side, idx); };
    tdSend.appendChild(sendBtn);

    var tdDelete = document.createElement('td');
    tdDelete.className = 'cmp-delete';
    var delBtn = document.createElement('button');
    delBtn.className = 'icon-btn cmp-delete-btn';
    delBtn.title = 'Delete';
    delBtn.innerHTML = '<i class="fa fa-trash"></i>';
    delBtn.onclick = function () { deleteItem(side, idx); };
    tdDelete.appendChild(delBtn);

    if (side === 'left') {
        tr.appendChild(tdCheck); tr.appendChild(tdName);
        tr.appendChild(tdBadge); tr.appendChild(tdSend); tr.appendChild(tdDelete);
    } else {
        tr.appendChild(tdDelete); tr.appendChild(tdSend);
        tr.appendChild(tdBadge); tr.appendChild(tdCheck); tr.appendChild(tdName);
    }

    attachDragHandlers(tr, side, idx);
    return tr;
}

function toggleSkip(side, idx) {
    var state = stateFor(side);
    state.list[idx].skip = !state.list[idx].skip;
    renderSide(side);
    saveSide(side);
}

function deleteItem(side, idx) {
    var state = stateFor(side);
    state.list.splice(idx, 1);
    dupMatches = {};
    renderSide(side);
    saveSide(side);
}

// ── Send / move ──────────────────────────────────────────────────────────

function sendItem(side, idx) {
    var src = stateFor(side);
    var dst = otherSide(side);
    var item = src.list.splice(idx, 1)[0];
    dst.list.push(item);
    dupMatches = {};
    renderSide('left');
    renderSide('right');
    saveSide('left');
    saveSide('right');
}

function moveAllRightToLeft() {
    if (right.list.length === 0) return;
    left.list = left.list.concat(right.list);
    right.list = [];
    dupMatches = {};
    renderSide('left');
    renderSide('right');
    saveSide('left');
    saveSide('right');
}

function swapSides() {
    var tmp = left;
    left = right;
    right = tmp;
    dupMatches = {};
    renderSide('left');
    renderSide('right');
    updatePickers('left');
    updatePickers('right');
    updateURL();
}

// ── Drag and drop between panes ─────────────────────────────────────────

function attachDragHandlers(tr, side, idx) {
    tr.addEventListener('dragstart', function (e) {
        e.dataTransfer.setData('text/plain', side + ':' + idx);
        e.dataTransfer.effectAllowed = 'move';
    });
}

function attachDropZone(side) {
    var wrap = document.getElementById(side + '-table-wrap');
    wrap.addEventListener('dragover', function (e) { e.preventDefault(); });
    wrap.addEventListener('dragenter', function (e) {
        e.preventDefault();
        wrap.classList.add('cmp-drag-over');
    });
    wrap.addEventListener('dragleave', function (e) {
        if (!wrap.contains(e.relatedTarget)) wrap.classList.remove('cmp-drag-over');
    });
    wrap.addEventListener('drop', function (e) {
        e.preventDefault();
        wrap.classList.remove('cmp-drag-over');
        var data = e.dataTransfer.getData('text/plain');
        if (!data) return;
        var parts = data.split(':');
        var srcSide = parts[0], srcIdx = parseInt(parts[1], 10);
        if (srcSide === side) return; // no reordering within a pane here
        sendItem(srcSide, srcIdx);
    });
}

// ── Dedup (fuzzy flag, non-destructive) ─────────────────────────────────

function normalizeName(name) {
    return (name || '').toLowerCase().trim().replace(/\s+/g, ' ');
}

function levenshtein(a, b) {
    var m = a.length, n = b.length;
    var d = [];
    for (var i = 0; i <= m; i++) d.push([i]);
    for (var j = 0; j <= n; j++) d[0][j] = j;
    for (i = 1; i <= m; i++) {
        for (j = 1; j <= n; j++) {
            var cost = a[i - 1] === b[j - 1] ? 0 : 1;
            d[i][j] = Math.min(
                d[i - 1][j] + 1,
                d[i][j - 1] + 1,
                d[i - 1][j - 1] + cost
            );
        }
    }
    return d[m][n];
}

function similarity(a, b) {
    var maxLen = Math.max(a.length, b.length);
    if (maxLen === 0) return 1;
    return 1 - levenshtein(a, b) / maxLen;
}

var DUP_THRESHOLD = 0.85;

function flagDuplicates() {
    var entries = [];
    left.list.forEach(function (it, idx) { entries.push({ side: 'left', idx: idx, norm: normalizeName(it.name) }); });
    right.list.forEach(function (it, idx) { entries.push({ side: 'right', idx: idx, norm: normalizeName(it.name) }); });

    dupMatches = {};
    for (var i = 0; i < entries.length; i++) {
        for (var j = i + 1; j < entries.length; j++) {
            var a = entries[i], b = entries[j];
            if (a.side === b.side && a.idx === b.idx) continue;
            if (!a.norm || !b.norm) continue;
            if (similarity(a.norm, b.norm) >= DUP_THRESHOLD) {
                var keyA = a.side + ':' + a.idx;
                var keyB = b.side + ':' + b.idx;
                (dupMatches[keyA] = dupMatches[keyA] || []).push(keyB);
                (dupMatches[keyB] = dupMatches[keyB] || []).push(keyA);
            }
        }
    }
    applyDupHighlights();
}

function applyDupHighlights() {
    ['left', 'right'].forEach(function (side) {
        var tbody = document.getElementById(side + '-table');
        Array.prototype.forEach.call(tbody.children, function (tr) {
            var key = tr.dataset.side + ':' + tr.dataset.idx;
            var badge = tr.querySelector('.cmp-badge');
            var hasMatch = !!dupMatches[key];
            tr.classList.toggle('cmp-dup', hasMatch);
            badge.innerHTML = hasMatch ? '<i class="fas fa-exclamation-triangle" title="Possible duplicate"></i>' : '';
            badge.onclick = hasMatch ? function () { jumpToMatch(key); } : null;
        });
    });
}

function jumpToMatch(key) {
    var partners = dupMatches[key];
    if (!partners || !partners.length) return;
    var partnerKey = partners[0];
    var parts = partnerKey.split(':');
    var side = parts[0], idx = parts[1];
    var row = document.querySelector('#' + side + '-table tr[data-idx="' + idx + '"]');
    if (!row) return;
    row.scrollIntoView({ behavior: 'smooth', block: 'center' });
    row.classList.add('cmp-flash');
    setTimeout(function () { row.classList.remove('cmp-flash'); }, 900);
}

// ── List pickers ─────────────────────────────────────────────────────────

function populatePicker(side) {
    var subjSel = document.getElementById(side + '-subject-select');
    var itemSel = document.getElementById(side + '-item-select');

    subjSel.innerHTML = '';
    lists.forEach(function (s, i) {
        var opt = document.createElement('option');
        opt.value = s.subject;
        opt.textContent = s.subject;
        subjSel.appendChild(opt);
    });

    subjSel.onchange = function () { populateItemPicker(side); };
    itemSel.onchange = function () {
        var subject = subjSel.value;
        var item = itemSel.value;
        if (subject && item) loadSide(side, subject, item);
    };
}

function populateItemPicker(side, desiredItem) {
    var subjSel = document.getElementById(side + '-subject-select');
    var itemSel = document.getElementById(side + '-item-select');
    var subject = subjSel.value;
    var entry = lists.find(function (s) { return s.subject === subject; });

    itemSel.innerHTML = '';
    (entry ? entry.entries : []).forEach(function (fn) {
        var basename = fn.split('/').pop();
        var opt = document.createElement('option');
        opt.value = basename;
        opt.textContent = basename;
        itemSel.appendChild(opt);
    });

    if (desiredItem) itemSel.value = desiredItem.split('/').pop();
    if (itemSel.value && subject) loadSide(side, subject, itemSel.value);
}

// Syncs a side's pickers to its current state without rebuilding the
// <option> list (which would interrupt an open native dropdown / cause
// visible flicker) unless the subject itself has changed.
function updatePickers(side) {
    var state = stateFor(side);
    if (!state.subject) return;
    var subjSel = document.getElementById(side + '-subject-select');
    var itemSel = document.getElementById(side + '-item-select');

    if (subjSel.value !== state.subject) {
        subjSel.value = state.subject;
        populateItemPicker(side, state.item);
        return;
    }

    var desired = state.item ? state.item.split('/').pop() : '';
    if (desired && itemSel.value !== desired) {
        itemSel.value = desired;
    }
}

// ── URL sync ─────────────────────────────────────────────────────────────

function updateURL() {
    var params = [];
    if (left.subject && left.item)   params.push('leftSubject=' + encodeURIComponent(left.subject), 'leftItem=' + encodeURIComponent(left.item));
    if (right.subject && right.item) params.push('rightSubject=' + encodeURIComponent(right.subject), 'rightItem=' + encodeURIComponent(right.item));
    var url = window.location.pathname + (params.length ? '?' + params.join('&') : '');
    window.history.replaceState(null, '', url);
}

// ── Keyboard shortcuts ───────────────────────────────────────────────────

function isTypingTarget(el) {
    return el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable);
}

document.addEventListener('keydown', function (e) {
    if (isTypingTarget(document.activeElement)) return;
    if (!e.altKey) return;
    if (e.key === 's' || e.key === 'S') { e.preventDefault(); swapSides(); }
    else if (e.key === 'm' || e.key === 'M') { e.preventDefault(); moveAllRightToLeft(); }
    else if (e.key === 'd' || e.key === 'D') { e.preventDefault(); flagDuplicates(); }
});

// ── Init ─────────────────────────────────────────────────────────────────

async function startCompare(params) {
    lists = await ajaxGetJSON('items');

    populatePicker('left');
    populatePicker('right');
    attachDropZone('left');
    attachDropZone('right');

    var leftSubject  = params.leftSubject  || params.subject || (lists[0] && lists[0].subject);
    var leftItem     = params.leftItem     || params.item;
    var rightSubject = params.rightSubject || leftSubject;
    var rightItem    = params.rightItem;

    if (leftSubject) {
        document.getElementById('left-subject-select').value = leftSubject;
        populateItemPicker('left', leftItem);
    }
    if (rightSubject) {
        document.getElementById('right-subject-select').value = rightSubject;
        populateItemPicker('right', rightItem);
    }
}
