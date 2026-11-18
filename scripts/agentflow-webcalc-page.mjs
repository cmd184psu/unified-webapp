// Drives the served webcalc page in headless Chromium and prints one JSON object:
//   {"checks":[{"name":..., "ok":true|false, "detail":...}]}
// scripts/agentflow-webcalc-probe.py runs this after its API checks and records the checks
// in its "page" group. Usage: node scripts/agentflow-webcalc-page.mjs http://127.0.0.1:7070/
//
// The page is judged as a person would use it: type an expression with the real keyboard,
// click buttons, and read what the display shows. It also records where the page sent its
// API requests, because a page that calls the wrong URL or another host reaches nothing.
import { spawn } from 'node:child_process';
import { readdirSync, existsSync } from 'node:fs';
import os from 'node:os';

const url = process.argv[2] || 'http://127.0.0.1:7070/';
const origin = new URL(url).origin;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function findChrome() {
  const root = os.homedir() + '/.cache/ms-playwright';
  if (!existsSync(root)) return null;
  const dirs = readdirSync(root).filter((d) => /^chromium-\d+$/.test(d)).sort().reverse();
  for (const d of dirs) {
    const p = `${root}/${d}/chrome-linux64/chrome`;
    if (existsSync(p)) return p;
  }
  return null;
}

const chrome = findChrome();
if (!chrome) {
  console.log(JSON.stringify({ checks: [{ name: 'page behaviour (browser available)', ok: false, detail: 'no Chromium under ~/.cache/ms-playwright' }] }));
  process.exit(0);
}
const port = 9400 + Math.floor(Math.random() * 300);
const proc = spawn(chrome, ['--headless=new', '--no-sandbox', '--disable-gpu', `--remote-debugging-port=${port}`,
  `--user-data-dir=/tmp/webcalc-page-${Date.now()}`, 'about:blank'], { stdio: 'ignore' });

let list;
for (let i = 0; i < 60; i++) { try { list = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json(); break; } catch { await sleep(250); } }
const target = list?.find((t) => t.type === 'page');
const ws = new WebSocket(target.webSocketDebuggerUrl);
await new Promise((r) => (ws.onopen = r));
let id = 0; const pending = new Map(); let requests = [];
ws.onmessage = (m) => {
  const d = JSON.parse(m.data);
  if (d.id && pending.has(d.id)) { pending.get(d.id)(d); pending.delete(d.id); }
  else if (d.method === 'Network.requestWillBeSent') requests.push(d.params.request.url);
};
const send = (method, params = {}) => new Promise((r) => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })); });
const evalJS = async (expr) => (await send('Runtime.evaluate', { expression: expr, returnByValue: true, awaitPromise: true })).result?.result?.value;
await send('Network.enable'); await send('Page.enable'); await send('Runtime.enable');

async function load() {
  requests = [];
  await send('Page.navigate', { url });
  await sleep(1200);
  // Focus the display field when there is an editable one, as a person would click into it.
  await evalJS(`(() => { const f = document.querySelector('input:not([readonly]):not([type=button]):not([type=submit]), textarea'); if (f) f.focus(); })()`);
}
async function typeText(text) {
  for (const ch of text) {
    const key = ch === ' ' ? ' ' : ch;
    await send('Input.dispatchKeyEvent', { type: 'keyDown', key, text: ch });
    await send('Input.dispatchKeyEvent', { type: 'keyUp', key });
  }
}
async function pressEnter() {
  await send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, text: '\r' });
  await send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 });
}
const display = () => evalJS(`(() => { const d = document.querySelector('input[readonly], input#display, #display, [class*=display], input, output'); return d ? (d.value ?? d.textContent) : ''; })()`);
const bodyText = () => evalJS('document.body.innerText + " " + Array.from(document.querySelectorAll("input,textarea")).map(e => e.value).join(" ")');
async function waitFor(fn, ms = 4000) {
  const end = Date.now() + ms;
  let v;
  while (Date.now() < end) { v = await fn(); if (v) return v; await sleep(150); }
  return v;
}
async function clickButton(labels) {
  const list = JSON.stringify(labels);
  return evalJS(`(() => { const want = ${list}; const b = Array.from(document.querySelectorAll('button')).find(b => want.includes(b.textContent.trim())); if (b) { b.click(); return true; } return false; })()`);
}
const compact = (s) => String(s ?? '').replace(/\s+/g, '');
const apiCalls = () => requests.filter((u) => u.includes('/api/'));

const checks = [];
const add = (name, ok, detail = '') => checks.push({ name, ok: !!ok, detail });

// 1. Typing on the keyboard: an expression with ^ and parentheses reaches the API and 25 is shown.
await load();
await typeText('(2+3)^2');
await sleep(200);
const typed = compact(await display());
await pressEnter();
const shown = compact(await waitFor(async () => { const d = compact(await display()); return d && d !== typed && d.includes('25') ? d : ''; }));
add('page: typing (2+3)^2 and pressing Enter shows 25', !!shown && !shown.includes('^'), `typed field showed ${JSON.stringify(typed)}, then ${JSON.stringify(shown)}`);
const calls1 = apiCalls();
add('page: the calculation request goes to the page\'s own origin under /api/',
  calls1.length > 0 && calls1.every((u) => u.startsWith(origin + '/api/')), JSON.stringify(calls1.slice(0, 3)));

// 2. Clicking: 2 + 3 x 4 = shows 14 (precedence is the API's, not the page's).
await load();
let clicked = true;
for (const label of [['2'], ['+'], ['3'], ['×', '*', 'x', '✕'], ['4'], ['=']]) clicked = (await clickButton(label)) && clicked;
const got = compact(await waitFor(async () => { const d = compact(await display()); return /^14(\.0+)?$/.test(d) ? d : ''; }));
add('page: clicking 2 + 3 × 4 = shows 14', clicked && !!got, `clicked all keys: ${clicked}, display ${JSON.stringify(compact(await display()))}`);

// 3. Buttons for what the spec lists beyond the four operations, for a person who does not type.
const rawLabels = await evalJS(`Array.from(document.querySelectorAll('button')).map(b => b.textContent.trim().toLowerCase())`);
const labels = [...rawLabels, ...rawLabels.map((l) => (l.length > 1 ? l.replace(/\(+$/, '') : l))]; // `sqrt(` counts as sqrt
const need = ['^', '(', ')', 'pi', 'sqrt', 'sin', 'cos', 'tan'];
const alias = { pi: ['pi', 'π'], sqrt: ['sqrt', '√'], '^': ['^', 'x^y', 'xʸ', 'x^n', 'pow'] };
const missing = need.filter((n) => !(alias[n] || [n]).some((a) => labels.includes(a)));
add('page: has buttons for ^ ( ) pi sqrt sin cos tan', missing.length === 0, missing.length ? `no button for ${missing.join(' ')}` : '');

// 4. An error from the API is shown as its message.
await load();
await typeText('1/0');
await pressEnter();
const err = await waitFor(async () => ((await bodyText()) || '').toLowerCase().includes('division by zero'));
add('page: 1/0 shows the API message "division by zero"', err, '');

ws.close(); proc.kill();
console.log(JSON.stringify({ checks }));
process.exit(0);
