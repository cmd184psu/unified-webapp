import { test, run, eq, ok, installFakeDom, FakeEl, descendants, asEl } from './testkit';
import { esc, h, replace, chip } from './dom';

installFakeDom();

const IMG = '<img src=x onerror=alert(1)>';
const SCRIPT = '<script>alert(1)</script>';

test('esc escapes the five HTML metacharacters and tolerates null', () => {
  eq(esc(`<>&"'`), '&lt;&gt;&amp;&quot;&#39;');
  eq(esc(null), '');
  eq(esc(undefined), '');
  eq(esc(7), '7');
});

test('AC-UI1: an <img onerror> payload renders as literal text, not an element', () => {
  const root = new FakeEl('DIV');
  replace(asEl(root), h('p', { text: IMG }), h('span', {}, IMG));
  eq(root.textContent, IMG + IMG);
  eq(descendants(root).map((e) => e.tagName), ['P', 'SPAN']);
});

test('AC-UI1: a <script> payload renders as literal text, not an element', () => {
  const root = new FakeEl('DIV');
  replace(asEl(root), h('li', { text: SCRIPT }), chip(SCRIPT));
  ok(root.textContent.includes(SCRIPT), 'script text is present literally');
  ok(descendants(root).every((e) => e.tagName !== 'SCRIPT'), 'no script element');
});

test('h refuses on* attributes but accepts listeners through props.on', () => {
  let threw = false;
  try {
    h('a', { attrs: { onclick: 'alert(1)' } });
  } catch {
    threw = true;
  }
  ok(threw, 'onclick attribute refused');
  let clicked = 0;
  const b = h('button', { on: { click: () => clicked++ } }) as unknown as FakeEl;
  b.click();
  eq(clicked, 1);
});

test('h skips null and false attributes and children, and sets class and attrs', () => {
  const el = h('div', { class: 'a b', attrs: { 'data-x': 'v', hidden: false, title: null } }, null, false, 't', 3) as unknown as FakeEl;
  eq(el.className, 'a b');
  eq(el.getAttribute('data-x'), 'v');
  eq(el.getAttribute('hidden'), null);
  eq(el.textContent, 't3');
});

test('chip maps statuses to classes, including the new ones', () => {
  const cls = (s: string): string => (chip(s) as unknown as FakeEl).className;
  eq(cls('needs_input'), 'status-chip status-needs-input');
  eq(cls('needs_attention'), 'status-chip status-needs-attention');
  eq(cls('stuck'), 'status-chip status-stuck');
  eq(cls('exhausted'), 'status-chip status-exhausted');
  eq(cls('overridden'), 'status-chip status-overridden');
  eq(cls('running'), 'status-chip status-in-progress');
});

void run('agentflow-dom');
