// designer.ts — the Task Designer modal (FRD §6, plan Phase F3).
//
// Primary fields: name + command + lane, plus Repeat (surfaced directly —
// it's a decision most tasks need, not an edge case) and, only when Repeat
// is on, a Cooldown-seconds field (defaults to 60). Advanced (collapsed by
// default) still holds output_file and sudo — sudo only shown when GET
// /api/capabilities reports allow_sudo. NOTE: the Task model's `command` is
// a single `sh -c` string — there is no workdir/env on this model, so those
// fields do not exist here.
//
// Below the form sit two collapsible, collapsed-by-default export panels
// (taskmasterctl / curl) that live-render from the current form state, each
// with a copy affordance and both copy-paste-runnable against this exact
// server: they use window.location.origin for the host and the shell env
// var $API_KEY (double-quoted so the shell expands it) for the credential —
// no placeholder host, no placeholder key. Primary action is "Add to lane"
// (POST /api/tasks).
//
// Every on/off control is a toggle (ui/toggle.ts), never a checkbox; the
// only dialog used for validation errors is ui/modal.ts's alertDialog.

import { api, Capabilities, LaneStatus } from './api.js';
import { openModal, alertDialog } from '@shared';
import { createToggleHandle } from './ui/toggle.js';

/** Shell-quotes a single argument the way a POSIX sh would need it quoted. */
function shQuote(s: string): string {
  if (s === '') return "''";
  if (/^[A-Za-z0-9_\-./:=@%,]+$/.test(s)) return s;
  return "'" + s.replace(/'/g, `'\\''`) + "'";
}

interface DesignerFormState {
  name: string;
  command: string;
  lane: string;
  repeat: boolean;
  cooldown: number;
  outputFile: string;
  sudo: boolean;
}

function buildCtlExport(f: DesignerFormState): string {
  const origin = window.location.origin;
  const parts = ['taskmasterctl', '-url', shQuote(origin), '-key', '"$API_KEY"', 'task', 'add'];
  parts.push('-name', shQuote(f.name || '<name>'));
  parts.push('-lane', shQuote(f.lane || '<lane>'));
  parts.push('-command', shQuote(f.command || '<command>'));
  if (f.repeat) parts.push('-repeat', '-cooldown', String(f.cooldown || 60));
  if (f.sudo) parts.push('-sudo');
  if (f.outputFile) parts.push('-output-file', shQuote(f.outputFile));
  return parts.join(' ');
}

function buildTaskBody(f: DesignerFormState): Record<string, unknown> {
  const body: Record<string, unknown> = {
    name: f.name || '<name>',
    lane_name: f.lane || '<lane>',
    command: f.command || '<command>',
    enabled: true,
    repeat: f.repeat,
    cooldown_seconds: f.repeat ? f.cooldown || 60 : 0,
    sudo: f.sudo,
  };
  if (f.outputFile) body.output_file = f.outputFile;
  return body;
}

function buildCurlExport(f: DesignerFormState): string {
  const body = JSON.stringify(buildTaskBody(f), null, 2);
  const origin = window.location.origin;
  return (
    `curl -X POST ${origin}/api/tasks \\\n` +
    `  -H 'Content-Type: application/json' \\\n` +
    `  -H "Authorization: Bearer $API_KEY" \\\n` +
    `  -d '${body.replace(/'/g, `'\\''`)}'`
  );
}

/** Builds a collapsible export panel (collapsed by default) with a copy button. */
function buildExportPanel(title: string, render: () => string): { el: HTMLElement; refresh: () => void } {
  const wrap = document.createElement('div');
  wrap.className = 'export-panel';

  const header = document.createElement('button');
  header.type = 'button';
  header.className = 'export-panel-header';
  const caret = document.createElement('span');
  caret.className = 'export-panel-caret';
  caret.textContent = '▸';
  const label = document.createElement('span');
  label.textContent = title;
  header.append(caret, label);

  const body = document.createElement('div');
  body.className = 'export-panel-body';
  body.hidden = true;

  const pre = document.createElement('pre');
  pre.className = 'export-panel-code';

  const copyBtn = document.createElement('button');
  copyBtn.type = 'button';
  copyBtn.className = 'btn btn-secondary btn-sm export-panel-copy';
  copyBtn.textContent = 'Copy';
  copyBtn.addEventListener('click', () => {
    void navigator.clipboard.writeText(pre.textContent ?? '').then(
      () => {
        copyBtn.textContent = 'Copied';
        setTimeout(() => (copyBtn.textContent = 'Copy'), 1200);
      },
      () => {
        copyBtn.textContent = 'Copy failed';
        setTimeout(() => (copyBtn.textContent = 'Copy'), 1200);
      }
    );
  });

  body.append(pre, copyBtn);
  wrap.append(header, body);

  header.addEventListener('click', () => {
    body.hidden = !body.hidden;
    caret.textContent = body.hidden ? '▸' : '▾';
  });

  function refresh(): void {
    pre.textContent = render();
  }
  refresh();

  return { el: wrap, refresh };
}

/**
 * Opens the Task Designer modal. `preselectLane` is preselected in the lane
 * dropdown (still changeable). Resolves once the modal closes (added or
 * canceled).
 */
export async function openTaskDesigner(lanes: LaneStatus[], preselectLane: string, caps: Capabilities): Promise<void> {
  const content = document.createElement('div');
  content.className = 'designer-form';

  // --- Primary fields ---
  const nameGroup = document.createElement('div');
  nameGroup.className = 'form-group';
  const nameLabel = document.createElement('label');
  nameLabel.textContent = 'Task name';
  const nameInput = document.createElement('input');
  nameInput.type = 'text';
  nameInput.placeholder = 'e.g. nightly-backup';
  nameGroup.append(nameLabel, nameInput);

  const cmdGroup = document.createElement('div');
  cmdGroup.className = 'form-group';
  const cmdLabel = document.createElement('label');
  cmdLabel.textContent = 'Command';
  const cmdHint = document.createElement('span');
  cmdHint.className = 'form-hint';
  cmdHint.textContent = ' — run as a single shell string (sh -c)';
  cmdLabel.appendChild(cmdHint);
  const cmdInput = document.createElement('textarea');
  cmdInput.rows = 3;
  cmdInput.placeholder = 'e.g. /usr/local/bin/backup.sh --quiet';
  cmdGroup.append(cmdLabel, cmdInput);

  const laneGroup = document.createElement('div');
  laneGroup.className = 'form-group';
  const laneLabel = document.createElement('label');
  laneLabel.textContent = 'Lane';
  const laneSelect = document.createElement('select');
  for (const lane of lanes) {
    const opt = document.createElement('option');
    opt.value = lane.name;
    opt.textContent = lane.name;
    if (lane.name === preselectLane) opt.selected = true;
    laneSelect.appendChild(opt);
  }
  laneGroup.append(laneLabel, laneSelect);

  content.append(nameGroup, cmdGroup, laneGroup);

  // --- Repeat + Cooldown: surfaced in the primary form, not buried in
  // Advanced — whether a task repeats is a primary decision. Cooldown only
  // appears once Repeat is on, defaulting to 60s. ---
  const repeatRow = document.createElement('div');
  repeatRow.className = 'menu-row form-toggle-row';
  const repeatLabel = document.createElement('span');
  repeatLabel.textContent = 'Repeat (re-enqueue after cooldown)';
  const repeatToggle = createToggleHandle({
    checked: false,
    onChange: (checked) => {
      cooldownGroup.hidden = !checked;
      refreshExports();
    },
  });
  repeatRow.append(repeatLabel, repeatToggle.el);

  const cooldownGroup = document.createElement('div');
  cooldownGroup.className = 'form-group';
  cooldownGroup.hidden = true;
  const cooldownLabel = document.createElement('label');
  cooldownLabel.textContent = 'Cooldown seconds (minimum rest between runs)';
  const cooldownInput = document.createElement('input');
  cooldownInput.type = 'number';
  cooldownInput.min = '0';
  cooldownInput.value = '60';
  cooldownGroup.append(cooldownLabel, cooldownInput);

  content.append(repeatRow, cooldownGroup);

  // --- Advanced (collapsible) ---
  const advToggleBtn = document.createElement('button');
  advToggleBtn.type = 'button';
  advToggleBtn.className = 'advanced-toggle';
  const advCaret = document.createElement('span');
  advCaret.className = 'export-panel-caret';
  advCaret.textContent = '▸';
  advToggleBtn.append(advCaret, document.createTextNode('Advanced'));

  const advBody = document.createElement('div');
  advBody.className = 'advanced-body';
  advBody.hidden = true;

  const outputGroup = document.createElement('div');
  outputGroup.className = 'form-group';
  const outputLabel = document.createElement('label');
  outputLabel.textContent = 'Output file (optional; tees stdout/stderr)';
  const outputInput = document.createElement('input');
  outputInput.type = 'text';
  outputInput.placeholder = 'e.g. /var/log/taskmaster/{task}-{exec_id}.log';
  outputGroup.append(outputLabel, outputInput);

  advBody.append(outputGroup);

  let sudoToggle: ReturnType<typeof createToggleHandle> | null = null;
  if (caps.allow_sudo) {
    const sudoRow = document.createElement('div');
    sudoRow.className = 'menu-row form-toggle-row';
    const sudoLabel = document.createElement('span');
    sudoLabel.textContent = 'Run with sudo';
    sudoToggle = createToggleHandle({ checked: false, onChange: () => refreshExports() });
    sudoRow.append(sudoLabel, sudoToggle.el);
    advBody.appendChild(sudoRow);
  }

  advToggleBtn.addEventListener('click', () => {
    advBody.hidden = !advBody.hidden;
    advCaret.textContent = advBody.hidden ? '▸' : '▾';
  });

  content.append(advToggleBtn, advBody);

  // --- Live export panels ---
  function currentForm(): DesignerFormState {
    return {
      name: nameInput.value.trim(),
      command: cmdInput.value.trim(),
      lane: laneSelect.value,
      repeat: repeatToggle.getChecked(),
      cooldown: parseInt(cooldownInput.value, 10) || 0,
      outputFile: outputInput.value.trim(),
      sudo: sudoToggle ? sudoToggle.getChecked() : false,
    };
  }

  const ctlPanel = buildExportPanel('taskmasterctl export', () => buildCtlExport(currentForm()));
  const curlPanel = buildExportPanel('curl export', () => buildCurlExport(currentForm()));
  content.append(ctlPanel.el, curlPanel.el);

  function refreshExports(): void {
    ctlPanel.refresh();
    curlPanel.refresh();
  }
  nameInput.addEventListener('input', refreshExports);
  cmdInput.addEventListener('input', refreshExports);
  laneSelect.addEventListener('change', refreshExports);
  cooldownInput.addEventListener('input', refreshExports);
  outputInput.addEventListener('input', refreshExports);

  // --- Actions ---
  const actions = document.createElement('div');
  actions.className = 'form-actions';
  const cancelBtn = document.createElement('button');
  cancelBtn.type = 'button';
  cancelBtn.className = 'btn btn-secondary';
  cancelBtn.textContent = 'Cancel';
  const addBtn = document.createElement('button');
  addBtn.type = 'button';
  addBtn.className = 'btn btn-primary';
  addBtn.textContent = 'Add to lane';
  actions.append(cancelBtn, addBtn);
  content.appendChild(actions);

  return new Promise<void>((resolve) => {
    const handle = openModal(content, { title: 'Add task', onClose: () => resolve() });
    cancelBtn.addEventListener('click', () => handle.close());
    addBtn.addEventListener('click', () => {
      void (async () => {
        const f = currentForm();
        if (!f.name || !f.command || !f.lane) {
          await alertDialog('Name, command, and lane are all required.');
          return;
        }
        try {
          await api.addTask(buildTaskBody(f) as never);
          handle.close();
        } catch {
          // api.ts already surfaced the error via alertDialog.
        }
      })();
    });
    nameInput.focus();
  });
}
