// Admin SPA (T5.5). Plain JS, no framework, no build step, zero external
// resources (L11) -- calls the T5.4 API under /api/ and renders its
// responses. The only auth logic here is calling the API and reacting to a
// 401: on any 401 the page reloads, which lets the platform gate serve the
// login page (NFR-1) -- everything else (who is signed in, what a session
// is) stays entirely server-side.
(() => {
  'use strict';

  const METHODS = ['pin', 'ldap', 'passkey', 'key'];

  const statusEl = document.getElementById('status');
  const panels = [
    'panel-matrix', 'panel-pins', 'panel-keys',
    'panel-ldap', 'panel-session', 'panel-passkeys', 'panel-operator-pin'
  ].map(id => document.getElementById(id));

  let authConfig = null; // last-fetched GET /api/config/auth response (redacted view)
  let matrix = {};       // editable copy of authConfig.modules, keyed by module name
  let passkeys = [];     // last-fetched GET /api/auth/passkeys "passkeys" array

  // ────────────────────────────────────────────────────────────────
  // API helper
  // ────────────────────────────────────────────────────────────────

  // api() never throws on an HTTP error -- callers read `ok`/`data.error`
  // and render it inline, per the matrix/pin/key/session panels' spec. The
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
  // Load + render
  // ────────────────────────────────────────────────────────────────

  async function loadAll() {
    const [authRes, passkeysRes] = await Promise.all([
      api('GET', '/api/config/auth'),
      api('GET', '/api/auth/passkeys')
    ]);
    if (!authRes.ok) {
      statusEl.textContent = 'Unable to load admin config.';
      return;
    }
    authConfig = authRes.data;
    // One row per routable module (known_modules), not just the already-
    // protected ones -- otherwise protection could never be turned ON here.
    matrix = {};
    (authConfig.known_modules || []).forEach(m => { matrix[m] = []; });
    Object.keys(authConfig.modules || {}).forEach(m => {
      matrix[m] = (authConfig.modules[m] || []).slice();
    });
    passkeys = (passkeysRes.ok && passkeysRes.data && passkeysRes.data.passkeys) || [];

    statusEl.classList.add('hidden');
    panels.forEach(p => p.classList.remove('hidden'));

    renderMatrix();
    renderPins();
    renderKeys();
    renderLdap();
    renderSession();
    renderPasskeys();
    renderOperatorPin();
  }

  // ── Matrix ──────────────────────────────────────────────────────

  function renderMatrix() {
    const container = document.getElementById('matrix-table');
    const modules = Object.keys(matrix).sort();
    if (modules.length === 0) {
      container.innerHTML = '<p class="hint">No modules in the matrix.</p>';
      return;
    }

    const table = document.createElement('table');
    table.className = 'matrix';

    const thead = document.createElement('thead');
    thead.innerHTML = '<tr><th>Module</th>' +
      METHODS.map(m => `<th>${esc(m)}</th>`).join('') + '</tr>';
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    modules.forEach(mod => {
      const tr = document.createElement('tr');
      let row = `<td>${esc(mod)}</td>`;
      METHODS.forEach(method => {
        const checked = matrix[mod].indexOf(method) !== -1;
        row += `<td><input type="checkbox" data-module="${esc(mod)}" data-method="${esc(method)}"${checked ? ' checked' : ''}></td>`;
      });
      tr.innerHTML = row;
      tbody.appendChild(tr);
    });
    table.appendChild(tbody);

    container.innerHTML = '';
    container.appendChild(table);

    container.querySelectorAll('input[type="checkbox"]').forEach(box => {
      box.addEventListener('change', () => {
        const mod = box.dataset.module;
        const method = box.dataset.method;
        const list = matrix[mod] || [];
        const idx = list.indexOf(method);
        if (box.checked && idx === -1) list.push(method);
        if (!box.checked && idx !== -1) list.splice(idx, 1);
        matrix[mod] = list;
      });
    });
  }

  document.getElementById('matrix-save').addEventListener('click', async () => {
    const errorEl = document.getElementById('matrix-error');
    const statusEl2 = document.getElementById('matrix-status');
    errorEl.textContent = '';
    statusEl2.textContent = '';
    // A matrix entry with zero methods would protect the module with no
    // way to log in, so unchecked rows are omitted, not sent empty.
    const toSave = {};
    Object.keys(matrix).forEach(m => {
      if (matrix[m].length > 0) toSave[m] = matrix[m];
    });
    const res = await api('PUT', '/api/config/modules', toSave);
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to save matrix.');
      return;
    }
    statusEl2.textContent = 'Saved.';
    setTimeout(() => { statusEl2.textContent = ''; }, 3000);
  });

  // ── PINs ────────────────────────────────────────────────────────

  function renderPins() {
    const list = document.getElementById('pins-list');
    list.innerHTML = '';
    const pins = (authConfig.pins || []);
    if (pins.length === 0) {
      list.innerHTML = '<li class="named-list-empty">No PINs configured.</li>';
      return;
    }
    pins.forEach(p => {
      const li = document.createElement('li');
      li.innerHTML = `<span class="named-list-name">${esc(p.name)}</span>` +
        `<span class="named-list-value">(set)</span>` +
        `<button type="button" class="btn-remove" data-name="${esc(p.name)}">Remove</button>`;
      list.appendChild(li);
    });
    list.querySelectorAll('.btn-remove').forEach(btn => {
      btn.addEventListener('click', () => removePin(btn.dataset.name));
    });
  }

  document.getElementById('pin-form').addEventListener('submit', async e => {
    e.preventDefault();
    const errorEl = document.getElementById('pin-error');
    errorEl.textContent = '';
    const nameInput = document.getElementById('pin-name');
    const pinInput = document.getElementById('pin-value');
    const name = nameInput.value.trim();
    const pin = pinInput.value;
    if (!name || !pin) return;
    const res = await api('POST', '/api/pins', { name, pin });
    if (!res.ok) {
      errorEl.textContent = errorText(res, 'Unable to add PIN.');
      return;
    }
    authConfig.pins = authConfig.pins || [];
    const idx = authConfig.pins.findIndex(p => p.name === name);
    const entry = { name, hash: '(set)' };
    if (idx !== -1) authConfig.pins[idx] = entry; else authConfig.pins.push(entry);
    nameInput.value = '';
    pinInput.value = '';
    renderPins();
  });

  async function removePin(name) {
    const errorEl = document.getElementById('pin-error');
    errorEl.textContent = '';
    const res = await api('DELETE', '/api/pins/' + encodeURIComponent(name));
    if (!res.ok && res.status !== 404) {
      errorEl.textContent = errorText(res, 'Unable to remove PIN.');
      return;
    }
    authConfig.pins = (authConfig.pins || []).filter(p => p.name !== name);
    renderPins();
  }

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
    const dl = document.getElementById('ldap-settings');
    const l = authConfig.ldap || {};
    const rows = [
      ['URL', l.url || '(not set)'],
      ['Start TLS', l.start_tls ? 'yes' : 'no'],
      ['Insecure TLS', l.insecure_tls ? 'yes' : 'no'],
      ['Bind DN', l.bind_dn || '(not set)'],
      ['Bind password', l.bind_password || '(not set)'],
      ['Base DN', l.base_dn || '(not set)'],
      ['User filter', l.user_filter || '(not set)'],
      ['Required groups', (l.required_groups || []).join(', ') || '(none)'],
      ['Timeout (s)', String(l.timeout_seconds || 0)]
    ];
    dl.innerHTML = rows.map(([k, v]) => `<dt>${esc(k)}</dt><dd>${esc(v)}</dd>`).join('');
  }

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

  document.getElementById('passkey-form').addEventListener('submit', async e => {
    e.preventDefault();
    const errorEl = document.getElementById('passkey-error');
    errorEl.textContent = '';
    if (!window.PublicKeyCredential || !navigator.credentials) {
      errorEl.textContent = 'Passkeys are not supported in this browser.';
      return;
    }
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

  // ── Operator PIN (read-only) ────────────────────────────────────

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
  }

  // ── Sign out ────────────────────────────────────────────────────

  document.getElementById('logout-btn').addEventListener('click', async () => {
    await api('POST', '/api/auth/logout');
    location.reload();
  });

  loadAll();
})();
