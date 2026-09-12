// Admin SPA (T5.5). Plain JS, no framework, no build step, zero external
// resources (L11) -- calls the T5.4 API under /api/ and renders its
// responses. The only auth logic here is calling the API and reacting to a
// 401: on any 401 the page reloads, which lets the platform gate serve the
// login page (NFR-1) -- everything else (who is signed in, what a session
// is) stays entirely server-side.
(() => {
  'use strict';

  const statusEl = document.getElementById('status');
  const panels = [
    'panel-matrix', 'panel-keys',
    'panel-ldap', 'panel-session', 'panel-passkeys', 'panel-operator-pin'
  ].map(id => document.getElementById(id));

  let authConfig = null; // last-fetched GET /api/config/auth response (redacted view)
  // Editable copy of authConfig.modules, keyed by module name -- excludes
  // "admin" (shown read-only, never edited from this table; see
  // renderMatrix/adminSourceDisplay). { [module]: { protected: bool, pinFile: string } }.
  let matrix = {};
  let passkeys = [];     // last-fetched GET /api/auth/passkeys "passkeys" array
  let pinFiles = [];     // last-fetched GET /api/config/pin-files "files" array

  // ────────────────────────────────────────────────────────────────
  // API helper
  // ────────────────────────────────────────────────────────────────

  // api() never throws on an HTTP error -- callers read `ok`/`data.error`
  // and render it inline, per the matrix/key/session panels' spec. The
  // one exception is 401: the gate has decided this session is no longer
  // valid, and the only correct move anywhere in this app is to reload so
  // the gate can serve its own login page.
  async function api(method, path, body) {
    const opts = { method, headers: {} };
    if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    const res = await fetch(path, opts);
    if (res.status === 401) {
      location.reload();
      return new Promise(() => {}); // reloading; never resolve
    }
    let data = null;
    if (res.status !== 204) {
      data = await res.json().catch(() => null);
    }
    return { ok: res.ok, status: res.status, data };
  }

  function esc(str) {
    return String(str)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;')
      .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function errorText(res, fallback) {
    return (res.data && res.data.error) || fallback;
  }

  // ────────────────────────────────────────────────────────────────
  // Pin files (GET /api/config/pin-files) -- shared between the matrix's
  // per-row pin_file picker and the Operator PIN panel's file picker.
  // ────────────────────────────────────────────────────────────────

  async function loadPinFiles() {
    const res = await api('GET', '/api/config/pin-files');
    if (res.ok && res.data) {
      pinFiles = res.data.files || [];
    }
  }

  // Builds <option> markup for a pin-file <select>: an empty "(no pin
  // file)" option, one option per known pin file (label name, value path),
  // and -- if currentValue isn't among them -- one extra option preserving
  // it verbatim, so a pin_file value that predates this listing (or was
  // just set moments ago, before the listing refreshed) is never silently
  // dropped from the dropdown.
  function pinFileOptions(currentValue) {
    const opts = [`<option value=""${currentValue ? '' : ' selected'}>(no pin file)</option>`];
    let found = !currentValue;
    pinFiles.forEach(f => {
      if (f.path === currentValue) found = true;
      opts.push(`<option value="${esc(f.path)}"${f.path === currentValue ? ' selected' : ''}>${esc(f.name)}</option>`);
    });
    if (currentValue && !found) {
      opts.push(`<option value="${esc(currentValue)}" selected>${esc(currentValue)}</option>`);
    }
    return opts.join('');
  }

  // Shared payload builder for PUT /api/config/modules: turns the matrix
  // state into the module map the endpoint expects. adminOverride, when
  // given, replaces modules.admin (used by the Operator PIN panel's file
  // picker and Set PIN form); otherwise admin is re-injected verbatim from
  // the last-fetched authConfig, since the PUT replaces the whole map and
  // the matrix table never edits admin itself.
  function buildModulesPayload(adminOverride) {
    const toSave = {};
    Object.keys(matrix).forEach(m => {
      if (!matrix[m].protected) return;
      const pinFile = matrix[m].pinFile.trim();
      toSave[m] = pinFile ? { pin_file: pinFile } : {};
    });
    if (adminOverride !== undefined) {
      toSave.admin = adminOverride;
    } else if (authConfig.modules && authConfig.modules.admin) {
      toSave.admin = authConfig.modules.admin;
    }
    return toSave;
  }

  // currentAdminPinFilePath mirrors adminSourceDisplay's precedence (see
  // below) but returns the bare path, for seeding the Operator PIN panel's
  // file picker and Set PIN form.
  function currentAdminPinFilePath() {
    const adminModule = (authConfig.modules || {}).admin;
    if (adminModule && adminModule.pin_file) return adminModule.pin_file;
    const adminPin = authConfig.admin_pin || {};
    if (adminPin.defined_by === 'file') return adminPin.path || '';
    return '';
  }

  // ────────────────────────────────────────────────────────────────
  // Load + render
  // ────────────────────────────────────────────────────────────────

  async function loadAll() {
    const [authRes, passkeysRes] = await Promise.all([
      api('GET', '/api/config/auth'),
      api('GET', '/api/auth/passkeys'),
      loadPinFiles()
    ]);
    if (!authRes.ok) {
      statusEl.textContent = 'Unable to load admin config.';
      return;
    }
    authConfig = authRes.data;
    // One row per routable module (known_modules), not just the already-
    // protected ones -- otherwise protection could never be turned ON here.
    // "admin" is excluded: it is shown read-only (renderMatrix) and never
    // edited through this table (see the matrix-save handler).
    //
    // Seeding is deliberately asymmetric: known_modules seeds every row as
    // *unprotected*, and only actual entries in authConfig.modules flip a
    // row to protected. Seeding every known module as protected here would
    // silently protect everything on the very next save (present === protected
    // under the two-state model) -- the opposite of what an unprotected
    // module's absence from authConfig.modules means.
    matrix = {};
    (authConfig.known_modules || []).forEach(m => {
      if (m === 'admin') return;
      matrix[m] = { protected: false, pinFile: '' };
    });
    Object.keys(authConfig.modules || {}).forEach(m => {
      if (m === 'admin') return;
      const entry = authConfig.modules[m] || {};
      matrix[m] = { protected: true, pinFile: entry.pin_file || '' };
    });
    passkeys = (passkeysRes.ok && passkeysRes.data && passkeysRes.data.passkeys) || [];

    statusEl.classList.add('hidden');
    panels.forEach(p => p.classList.remove('hidden'));

    renderMatrix();
    renderKeys();
    renderLdap();
    renderSession();
    renderPasskeys();
    renderOperatorPin();
  }

  // ── Matrix ──────────────────────────────────────────────────────

  // adminSourceDisplay describes where the admin PIN comes from, for the
  // matrix table's read-only admin row: modules.admin.pin_file (the new
  // spelling) takes priority when set, otherwise the legacy top-level
  // admin_pin/admin_pin_file source authConfig.admin_pin already reports.
  function adminSourceDisplay() {
    const adminModule = (authConfig.modules || {}).admin;
    if (adminModule && adminModule.pin_file) {
      return adminModule.pin_file + ' (modules.admin.pin_file)';
    }
    const adminPin = authConfig.admin_pin || {};
    if (adminPin.defined_by === 'file') return adminPin.path + ' (admin_pin_file)';
    if (adminPin.defined_by === 'config') return '(inline config hash)';
    return '(not configured)';
  }

  function renderMatrix() {
    const container = document.getElementById('matrix-table');
    const modules = Object.keys(matrix).sort();

    const table = document.createElement('table');
    table.className = 'matrix';

    const thead = document.createElement('thead');
    thead.innerHTML = '<tr><th>Module</th><th>Protected</th><th>Pin file</th></tr>';
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    modules.forEach(mod => {
      const entry = matrix[mod];
      const tr = document.createElement('tr');
      tr.innerHTML =
        `<td>${esc(mod)}</td>` +
        `<td><input type="checkbox" class="matrix-protected" data-module="${esc(mod)}"${entry.protected ? ' checked' : ''}></td>` +
        `<td>` +
        `<select class="matrix-pinfile" data-module="${esc(mod)}"${entry.protected ? '' : ' disabled'}>${pinFileOptions(entry.pinFile)}</select> ` +
        `<button type="button" class="matrix-setpin-btn" data-module="${esc(mod)}"${entry.protected ? '' : ' disabled'}>Set PIN&hellip;</button>` +
        `<div class="matrix-setpin-form inline-form hidden" data-module="${esc(mod)}">` +
        `<input type="password" class="matrix-pin-input" placeholder="new PIN" autocomplete="off">` +
        `<button type="button" class="matrix-pin-save" data-module="${esc(mod)}">Save</button>` +
        `<button type="button" class="matrix-pin-cancel" data-module="${esc(mod)}">Cancel</button>` +
        `</div>` +
        `<span class="matrix-pin-status status" data-module="${esc(mod)}"></span>` +
        `<p class="matrix-pin-error error" data-module="${esc(mod)}"></p>` +
        `</td>`;
      tbody.appendChild(tr);
    });

    // Admin is always shown for visibility, but read-only here: its source
    // is one of three config keys (auth.admin_pin, auth.admin_pin_file,
    // auth.modules.admin.pin_file) with its own exactly-one-of-three rule,
    // not a toggle-and-door-code pair like every other module -- see the
    // Operator PIN panel below for the authoritative, unredacted view.
    const adminTr = document.createElement('tr');
    adminTr.innerHTML =
      `<td>admin</td>` +
      `<td class="hint">n/a</td>` +
      `<td class="hint">${esc(adminSourceDisplay())}</td>`;
    tbody.appendChild(adminTr);

    table.appendChild(tbody);

    container.innerHTML = '';
    container.appendChild(table);

    container.querySelectorAll('input.matrix-protected').forEach(box => {
      box.addEventListener('change', () => {
        const mod = box.dataset.module;
        matrix[mod].protected = box.checked;
        const select = container.querySelector(`select.matrix-pinfile[data-module="${CSS.escape(mod)}"]`);
        if (select) select.disabled = !box.checked;
        const setBtn = container.querySelector(`.matrix-setpin-btn[data-module="${CSS.escape(mod)}"]`);
        if (setBtn) setBtn.disabled = !box.checked;
      });
    });
    container.querySelectorAll('select.matrix-pinfile').forEach(select => {
      select.addEventListener('change', () => {
        matrix[select.dataset.module].pinFile = select.value;
      });
    });
    container.querySelectorAll('.matrix-setpin-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const mod = btn.dataset.module;
        const form = container.querySelector(`.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`);
        form.classList.toggle('hidden');
        const input = form.querySelector('.matrix-pin-input');
        input.value = '';
        if (!form.classList.contains('hidden')) input.focus();
      });
    });
    container.querySelectorAll('.matrix-pin-cancel').forEach(btn => {
      btn.addEventListener('click', () => {
        const mod = btn.dataset.module;
        const form = container.querySelector(`.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`);
        form.classList.add('hidden');
        form.querySelector('.matrix-pin-input').value = '';
      });
    });
    container.querySelectorAll('.matrix-pin-save').forEach(btn => {
      btn.addEventListener('click', () => matrixSetPinSubmit(btn.dataset.module));
    });
  }

  // matrixSetPinSubmit pushes a new PIN to a module's pin file via POST
  // /api/config/pin-files, from the matrix row's inline "Set PIN..." form.
  // Targets the file currently selected in that row's dropdown, or lets
  // the server pick a default path by module name when none is selected
  // yet. The typed PIN is cleared from the input immediately after
  // submission, success or failure, and is never logged or redisplayed.
  async function matrixSetPinSubmit(mod) {
    const container = document.getElementById('matrix-table');
    const form = container.querySelector(`.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`);
    const select = container.querySelector(`select.matrix-pinfile[data-module="${CSS.escape(mod)}"]`);
    const errorEl = container.querySelector(`.matrix-pin-error[data-module="${CSS.escape(mod)}"]`);
    const input = form.querySelector('.matrix-pin-input');
    const pin = input.value;
    errorEl.textContent = '';
    const selectedPath = select.value;
    const payload = selectedPath ? { path: selectedPath, pin } : { name: mod + '.pin', pin };
    const res = await api('POST', '/api/config/pin-files', payload);
    input.value = '';
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to set PIN.');
      return;
    }
    matrix[mod].pinFile = (res.data && res.data.path) || '';
    await loadPinFiles();
    renderMatrix();
    const statusEl2 = document.getElementById('matrix-table')
      .querySelector(`.matrix-pin-status[data-module="${CSS.escape(mod)}"]`);
    if (statusEl2) {
      statusEl2.textContent = 'Saved.';
      setTimeout(() => { statusEl2.textContent = ''; }, 3000);
    }
  }

  document.getElementById('matrix-save').addEventListener('click', async () => {
    const errorEl = document.getElementById('matrix-error');
    const statusEl2 = document.getElementById('matrix-status');
    errorEl.textContent = '';
    statusEl2.textContent = '';
    // Only rows the operator checked "Protected" are sent, as {} (LDAP/
    // passkey only) or {pin_file: ...} (also accepts a per-module door
    // code) -- an unchecked row is simply omitted, leaving it open. This is
    // the two-state model's contract: present (even {}) means protected,
    // absent means open. admin is never edited from this table -- see
    // buildModulesPayload, which resends its existing entry (if any)
    // unchanged so a save doesn't silently drop modules.admin.pin_file
    // (the PUT replaces the whole map).
    const toSave = buildModulesPayload();
    const res = await api('PUT', '/api/config/modules', toSave);
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to save matrix.');
      return;
    }
    authConfig.modules = (res.data && res.data.modules) || toSave;
    Object.keys(matrix).forEach(m => {
      matrix[m].pinFile = (authConfig.modules[m] && authConfig.modules[m].pin_file) || '';
    });
    renderMatrix();
    statusEl2.textContent = 'Saved.';
    setTimeout(() => { statusEl2.textContent = ''; }, 3000);
  });

  // ── API keys ────────────────────────────────────────────────────

  function renderKeys() {
    const list = document.getElementById('keys-list');
    list.innerHTML = '';
    const keys = (authConfig.api_keys || []);
    if (keys.length === 0) {
      list.innerHTML = '<li class="named-list-empty">No API keys configured.</li>';
      return;
    }
    keys.forEach(k => {
      const li = document.createElement('li');
      li.innerHTML = `<span class="named-list-name">${esc(k.name)}</span>` +
        `<span class="named-list-value">(set)</span>` +
        `<button type="button" class="btn-remove" data-name="${esc(k.name)}">Revoke</button>`;
      list.appendChild(li);
    });
    list.querySelectorAll('.btn-remove').forEach(btn => {
      btn.addEventListener('click', () => revokeKey(btn.dataset.name));
    });
  }

  document.getElementById('key-form').addEventListener('submit', async e => {
    e.preventDefault();
    const errorEl = document.getElementById('key-error');
    errorEl.textContent = '';
    const nameInput = document.getElementById('key-name');
    const name = nameInput.value.trim();
    if (!name) return;
    const res = await api('POST', '/api/keys', { name });
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to generate key.');
      return;
    }
    authConfig.api_keys = authConfig.api_keys || [];
    const idx = authConfig.api_keys.findIndex(k => k.name === name);
    const entry = { name, hash: '(set)' };
    if (idx !== -1) authConfig.api_keys[idx] = entry; else authConfig.api_keys.push(entry);
    nameInput.value = '';
    renderKeys();
    showKeyModal(res.data.key);
  });

  async function revokeKey(name) {
    const errorEl = document.getElementById('key-error');
    errorEl.textContent = '';
    const res = await api('DELETE', '/api/keys/' + encodeURIComponent(name));
    if (!res.ok && res.status !== 404) {
      errorEl.textContent = errorText(res, 'Unable to revoke key.');
      return;
    }
    authConfig.api_keys = (authConfig.api_keys || []).filter(k => k.name !== name);
    renderKeys();
  }

  // ── Show-once key modal ─────────────────────────────────────────

  const keyModal = document.getElementById('key-modal');
  const keyModalValue = document.getElementById('key-modal-value');
  const keyModalCopied = document.getElementById('key-modal-copied');

  function showKeyModal(key) {
    keyModalValue.textContent = key;
    keyModalCopied.textContent = '';
    keyModal.classList.remove('hidden');
  }

  function closeKeyModal() {
    keyModal.classList.add('hidden');
    keyModalValue.textContent = '';
  }

  document.getElementById('key-modal-copy').addEventListener('click', () => {
    const value = keyModalValue.textContent;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(value)
        .then(() => { keyModalCopied.textContent = 'Copied.'; })
        .catch(() => { keyModalCopied.textContent = 'Copy failed -- select and copy manually.'; });
    } else {
      keyModalCopied.textContent = 'Copy not supported -- select and copy manually.';
    }
  });
  document.getElementById('key-modal-close').addEventListener('click', closeKeyModal);
  keyModal.addEventListener('click', e => { if (e.target === keyModal) closeKeyModal(); });

  // ── LDAP ────────────────────────────────────────────────────────

  function renderLdap() {
    const l = authConfig.ldap || {};
    document.getElementById('ldap-url').value = l.url || '';
    document.getElementById('ldap-base-dn').value = l.base_dn || '';
    document.getElementById('ldap-bind-dn').value = l.bind_dn || '';
    const pwInput = document.getElementById('ldap-bind-password');
    pwInput.value = '';
    pwInput.placeholder = l.bind_password === '(set)' ? '(unchanged)' : '(none)';
    document.getElementById('ldap-user-filter').value = l.user_filter || '';
    document.getElementById('ldap-required-groups').value = (l.required_groups || []).join(', ');
    document.getElementById('ldap-timeout').value = l.timeout_seconds || 0;
    document.getElementById('ldap-start-tls').checked = !!l.start_tls;
    document.getElementById('ldap-insecure-tls').checked = !!l.insecure_tls;
  }

  document.getElementById('ldap-form').addEventListener('submit', async e => {
    e.preventDefault();
    const errorEl = document.getElementById('ldap-error');
    const statusEl2 = document.getElementById('ldap-status');
    errorEl.textContent = '';
    statusEl2.textContent = '';
    statusEl2.className = 'status';
    const url = document.getElementById('ldap-url').value.trim();
    const start_tls = document.getElementById('ldap-start-tls').checked;
    const insecure_tls = document.getElementById('ldap-insecure-tls').checked;
    const bind_dn = document.getElementById('ldap-bind-dn').value.trim();
    const bind_password = document.getElementById('ldap-bind-password').value;
    const base_dn = document.getElementById('ldap-base-dn').value.trim();
    const user_filter = document.getElementById('ldap-user-filter').value.trim();
    const required_groups = document.getElementById('ldap-required-groups').value
      .split(',').map(s => s.trim()).filter(Boolean);
    const timeout_seconds = parseInt(document.getElementById('ldap-timeout').value, 10) || 0;
    const res = await api('PUT', '/api/config/ldap', {
      url, start_tls, insecure_tls, bind_dn, bind_password,
      base_dn, user_filter, required_groups, timeout_seconds
    });
    document.getElementById('ldap-bind-password').value = '';
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to save LDAP settings.');
      return;
    }
    authConfig.ldap = res.data;
    renderLdap();
    statusEl2.textContent = 'Saved.';
    statusEl2.className = 'status status-good';
    setTimeout(() => { statusEl2.textContent = ''; statusEl2.className = 'status'; }, 3000);
  });

  document.getElementById('ldap-test-form').addEventListener('submit', async e => {
    e.preventDefault();
    const resultEl = document.getElementById('ldap-test-result');
    resultEl.textContent = 'Testing…';
    resultEl.className = 'status';
    const username = document.getElementById('ldap-test-username').value;
    const password = document.getElementById('ldap-test-password').value;
    const res = await api('POST', '/api/ldap/test', { username, password });
    document.getElementById('ldap-test-password').value = '';
    if (!res.ok) {
      resultEl.textContent = errorText(res, 'Test failed.');
      resultEl.className = 'status status-bad';
      return;
    }
    const labels = {
      ok: 'Bind succeeded.',
      bad_credentials: 'Bad credentials.',
      unreachable: 'LDAP server unreachable.',
      tls_error: 'TLS error.'
    };
    const cls = res.data.result === 'ok' ? 'status-good' : 'status-bad';
    resultEl.textContent = labels[res.data.result] || res.data.result;
    resultEl.className = 'status ' + cls;
  });

  // ── Session ─────────────────────────────────────────────────────

  function renderSession() {
    const s = authConfig.session || {};
    document.getElementById('session-ttl').value = s.ttl_hours || 0;
    document.getElementById('session-cookie-domain').value = authConfig.cookie_domain || '';
    document.getElementById('session-cookie-secure').checked = !!authConfig.cookie_secure;
  }

  document.getElementById('session-form').addEventListener('submit', async e => {
    e.preventDefault();
    const statusEl2 = document.getElementById('session-status');
    statusEl2.textContent = '';
    statusEl2.className = 'status';
    const ttl_hours = parseInt(document.getElementById('session-ttl').value, 10) || 0;
    const cookie_domain = document.getElementById('session-cookie-domain').value;
    const cookie_secure = document.getElementById('session-cookie-secure').checked;
    const res = await api('PUT', '/api/config/session', { ttl_hours, cookie_domain, cookie_secure });
    if (!res.ok) {
      statusEl2.textContent = errorText(res, 'Unable to save session settings.');
      statusEl2.className = 'status status-bad';
      return;
    }
    authConfig.session = authConfig.session || {};
    authConfig.session.ttl_hours = res.data.ttl_hours;
    authConfig.cookie_domain = res.data.cookie_domain;
    authConfig.cookie_secure = res.data.cookie_secure;
    statusEl2.textContent = 'Saved.';
    statusEl2.className = 'status status-good';
  });

  // ── Passkeys ────────────────────────────────────────────────────

  function formatTimestamp(sec) {
    if (!sec) return '';
    return new Date(sec * 1000).toLocaleString();
  }

  function renderPasskeys() {
    const list = document.getElementById('passkeys-list');
    list.innerHTML = '';
    if (passkeys.length === 0) {
      list.innerHTML = '<li class="named-list-empty">No passkeys registered.</li>';
      return;
    }
    passkeys.forEach(p => {
      const li = document.createElement('li');
      const name = p.friendlyName || '(unnamed)';
      const created = formatTimestamp(p.createdAt);
      const lastUsed = formatTimestamp(p.lastUsedAt);
      li.innerHTML = `<span class="named-list-name">${esc(name)}</span>` +
        `<span class="named-list-value">${esc(created ? 'added ' + created : '')}${lastUsed ? esc(', last used ' + lastUsed) : ''}</span>` +
        `<button type="button" class="btn-remove" data-id="${esc(p.id)}">Delete</button>`;
      list.appendChild(li);
    });
    list.querySelectorAll('.btn-remove').forEach(btn => {
      btn.addEventListener('click', () => deletePasskey(btn.dataset.id));
    });
  }

  async function deletePasskey(id) {
    const errorEl = document.getElementById('passkey-error');
    errorEl.textContent = '';
    const res = await api('DELETE', '/api/auth/passkeys/' + encodeURIComponent(id));
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to delete passkey.');
      return;
    }
    passkeys = passkeys.filter(p => p.id !== id);
    renderPasskeys();
  }

  // --- base64url helpers (WebAuthn ceremony encode/decode), mirroring
  // internal/platform/auth/login.html's helpers exactly, extended here to
  // also cover the registration (attestation) ceremony that login.html
  // never needs. ---

  function b64urlToBuffer(value) {
    const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
    const padded = normalized + '==='.slice((normalized.length + 3) % 4);
    const binary = atob(padded);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes.buffer;
  }

  function bufferToB64url(value) {
    if (!value) return null;
    const bytes = new Uint8Array(value);
    let binary = '';
    for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
    return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
  }

  function creationOptions(options) {
    const publicKey = Object.assign({}, options.publicKey);
    publicKey.challenge = b64urlToBuffer(publicKey.challenge);
    if (publicKey.user) {
      publicKey.user = Object.assign({}, publicKey.user, { id: b64urlToBuffer(publicKey.user.id) });
    }
    if (Array.isArray(publicKey.excludeCredentials)) {
      publicKey.excludeCredentials = publicKey.excludeCredentials.map(c =>
        Object.assign({}, c, { id: b64urlToBuffer(c.id) }));
    }
    return publicKey;
  }

  function attestationToJSON(credential) {
    const response = credential.response;
    return {
      id: credential.id,
      rawId: bufferToB64url(credential.rawId),
      type: credential.type,
      authenticatorAttachment: credential.authenticatorAttachment,
      response: {
        attestationObject: bufferToB64url(response.attestationObject),
        clientDataJSON: bufferToB64url(response.clientDataJSON)
      },
      clientExtensionResults: credential.getClientExtensionResults()
    };
  }

  // Passkeys need a secure context: browsers expose WebAuthn only over
  // HTTPS or on localhost, so on a plain-http host the register button is
  // disabled up front with a tooltip explaining why, rather than erroring
  // on click.
  const passkeySupported = window.isSecureContext &&
    !!window.PublicKeyCredential && !!navigator.credentials;
  if (!passkeySupported) {
    const form = document.getElementById('passkey-form');
    const btn = form.querySelector('button');
    btn.disabled = true;
    const why = 'Disabled: this page is served over plain HTTP. Browsers only ' +
      'allow passkeys (WebAuthn) over HTTPS or on localhost.';
    btn.title = why;
    form.title = why;
  }

  document.getElementById('passkey-form').addEventListener('submit', async e => {
    e.preventDefault();
    if (!passkeySupported) return;
    const errorEl = document.getElementById('passkey-error');
    errorEl.textContent = '';
    const nameInput = document.getElementById('passkey-name');
    const friendlyName = nameInput.value.trim();

    const beginRes = await api('POST', '/api/auth/passkey/register/begin', { friendlyName });
    if (!beginRes.ok) {
      errorEl.textContent = errorText(beginRes, 'Unable to begin passkey registration.');
      return;
    }
    const ceremony = beginRes.data;

    let credential;
    try {
      credential = await navigator.credentials.create({ publicKey: creationOptions(ceremony.options) });
    } catch (err) {
      errorEl.textContent = 'Passkey registration cancelled.';
      return;
    }
    if (!credential) {
      errorEl.textContent = 'Passkey registration cancelled.';
      return;
    }

    const finishRes = await api('POST', '/api/auth/passkey/register/finish', {
      challengeId: ceremony.challengeId,
      friendlyName,
      credential: attestationToJSON(credential)
    });
    if (!finishRes.ok) {
      errorEl.textContent = errorText(finishRes, 'Passkey registration failed.');
      return;
    }
    passkeys.push(finishRes.data);
    nameInput.value = '';
    renderPasskeys();
  });

  // ── Operator PIN ────────────────────────────────────────────────
  // The door-code that unlocks this admin panel. Read-only status text
  // (source: config / file / none) plus a pin-file picker and a Set PIN
  // form that push changes via the same modules-map save path the matrix
  // uses (see buildModulesPayload) and the shared pin-files endpoint.

  function renderOperatorPin() {
    const el = document.getElementById('operator-pin-status');
    const adminPin = authConfig.admin_pin || {};
    if (adminPin.defined_by === 'config') {
      el.textContent = 'Defined by config.';
    } else if (adminPin.defined_by === 'file') {
      el.textContent = 'Defined by file (' + adminPin.path + ').';
    } else {
      el.textContent = 'Not configured.';
    }
    document.getElementById('operator-pin-file-select').innerHTML =
      pinFileOptions(currentAdminPinFilePath());
  }

  document.getElementById('operator-pin-change-file').addEventListener('click', async () => {
    const statusEl2 = document.getElementById('operator-pin-file-status');
    const errorEl = document.getElementById('operator-pin-error');
    statusEl2.textContent = '';
    errorEl.textContent = '';
    const selectedPath = document.getElementById('operator-pin-file-select').value;
    const toSave = buildModulesPayload({ pin_file: selectedPath });
    const res = await api('PUT', '/api/config/modules', toSave);
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to change PIN file.');
      return;
    }
    authConfig.modules = (res.data && res.data.modules) || toSave;
    Object.keys(matrix).forEach(m => {
      matrix[m].pinFile = (authConfig.modules[m] && authConfig.modules[m].pin_file) || '';
    });
    renderMatrix();
    renderOperatorPin();
    statusEl2.textContent = 'Saved.';
    setTimeout(() => { statusEl2.textContent = ''; }, 3000);
  });

  document.getElementById('operator-pin-set-btn').addEventListener('click', () => {
    const form = document.getElementById('operator-pin-form');
    const input = document.getElementById('operator-pin-value');
    form.classList.toggle('hidden');
    input.value = '';
    if (!form.classList.contains('hidden')) input.focus();
  });

  document.getElementById('operator-pin-cancel').addEventListener('click', () => {
    document.getElementById('operator-pin-form').classList.add('hidden');
    document.getElementById('operator-pin-value').value = '';
  });

  document.getElementById('operator-pin-form').addEventListener('submit', async e => {
    e.preventDefault();
    const errorEl = document.getElementById('operator-pin-error');
    const statusEl2 = document.getElementById('operator-pin-file-status');
    errorEl.textContent = '';
    statusEl2.textContent = '';
    const input = document.getElementById('operator-pin-value');
    const pin = input.value;
    const currentPath = currentAdminPinFilePath();
    const payload = currentPath ? { path: currentPath, pin } : { name: 'admin.pin', pin };
    const res = await api('POST', '/api/config/pin-files', payload);
    input.value = '';
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to set PIN.');
      return;
    }
    // Also point modules.admin.pin_file at the returned path, via the same
    // shared modules-map save path the matrix and "Change file" use.
    const toSave = buildModulesPayload({ pin_file: res.data.path });
    const modRes = await api('PUT', '/api/config/modules', toSave);
    if (!modRes.ok) {
      errorEl.textContent = errorText(modRes, 'PIN set, but unable to update the admin pin file reference.');
      return;
    }
    authConfig.modules = (modRes.data && modRes.data.modules) || toSave;
    Object.keys(matrix).forEach(m => {
      matrix[m].pinFile = (authConfig.modules[m] && authConfig.modules[m].pin_file) || '';
    });
    await loadPinFiles();
    renderMatrix();
    renderOperatorPin();
    document.getElementById('operator-pin-form').classList.add('hidden');
    statusEl2.textContent = 'Saved.';
    setTimeout(() => { statusEl2.textContent = ''; }, 3000);
  });

  // ── Sign out ────────────────────────────────────────────────────

  document.getElementById('logout-btn').addEventListener('click', async () => {
    await api('POST', '/api/auth/logout');
    location.reload();
  });

  loadAll();
})();
