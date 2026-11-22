import { test, run, eq, ok, installFakeDom, FakeEl, asEl, findAll } from './testkit';
import { SeatsPage, policyLine, seatState } from './seatspage';
import type { CoordStatus, ExternalSeat, SeatInput } from './api';

installFakeDom();

const seat = (over: Partial<ExternalSeat> = {}): ExternalSeat => ({
  callsign: 'deepseek4',
  session: 'deepseek4',
  running: true,
  since: '2026-11-20T08:00:00Z',
  ready: true,
  feed: true,
  nudge: false,
  queue_file: '/q/deepseek4.md',
  current: 'TASK-444',
  kind: 'freebuff',
  where: 'tmux deepseek4',
  policy: { enabled: false, trigger: 'idle', idle_minutes: 60, min_gap_minutes: 60, max_per_hour: 1 },
  suggested: { status: 'TASK-724  text view', message: 'Read your queue file /q/deepseek4.md (callsign deepseek4)' },
  ...over,
});
const status: CoordStatus = {
  enabled: true, dry_run: true, me: 'coord-fs3', base: 'feature/x', watch: [],
  last_run: { started: '2026-11-20T08:00:00Z', finished: '2026-11-20T08:00:05Z', dry: true, decisions: [{ kind: 'fed', task: 'TASK-445', seat: 'deepseek4', detail: 'next' }] },
};

const deps = (seats: ExternalSeat[], over: Record<string, unknown> = {}) => ({
  coordStatus: async () => status,
  externalSeats: async () => seats,
  seatQueue: async (cs: string) => ({ callsign: cs, text: '# Queue' }),
  nudgeSeat: async (cs: string) => ({ nudged: cs }),
  saveSeat: async (s: SeatInput) => seat({ callsign: s.callsign, kind: s.kind ?? 'other', where: s.where ?? '' }),
  deleteSeat: async (cs: string) => ({ deleted: cs }),
  pauseSeat: async (cs: string, paused: boolean) => ({ callsign: cs, paused }),
  copy: async () => true,
  ...over,
});

test('every seat gets a card with its paste message and status', async () => {
  const host = new FakeEl('DIV');
  const p = new SeatsPage(asEl(host), deps([seat(), seat({ callsign: 'opus-gui', kind: 'gui', where: 'freemilk', nudge: true, suggested: { status: 'current: TASK-654', message: 'Read the GUI queue again' } })]));
  await p.load();
  eq(findAll(host, (e) => e.getAttribute('data-seat') !== null).length >= 2, true, 'both seats rendered');
  ok(host.textContent.includes('Read your queue file /q/deepseek4.md'), host.textContent);
  ok(host.textContent.includes('Read the GUI queue again'), host.textContent);
  ok(host.textContent.includes('status: TASK-724  text view'), host.textContent);
  ok(host.textContent.includes('state unknown (no terminal)'), host.textContent);
});

test('Copy writes exactly the suggested message', async () => {
  const host = new FakeEl('DIV');
  const copied: string[] = [];
  const p = new SeatsPage(asEl(host), deps([seat()], { copy: async (m: string) => { copied.push(m); return true; } }));
  await p.load();
  const copy = findAll(host, (e) => e.getAttribute('data-role') === 'seat-copy')[0];
  copy.click();
  await new Promise((r) => setTimeout(r, 0));
  eq(copied.join('|'), 'Read your queue file /q/deepseek4.md (callsign deepseek4)');
});

test('an empty queue says nothing to send and offers no copy button', async () => {
  const host = new FakeEl('DIV');
  const p = new SeatsPage(asEl(host), deps([seat({ suggested: { status: 'queue empty: nothing to send', message: '' } })]));
  await p.load();
  ok(host.textContent.includes('nothing to send'), host.textContent);
  eq(findAll(host, (e) => e.getAttribute('data-role') === 'seat-copy').length, 0);
});

test('Pause nudges posts the opposite of the current state', async () => {
  const host = new FakeEl('DIV');
  const calls: string[] = [];
  const p = new SeatsPage(asEl(host), deps([seat()], {
    pauseSeat: async (cs: string, paused: boolean) => { calls.push(`${cs}:${paused}`); return {}; },
  }));
  await p.load();
  findAll(host, (e) => e.getAttribute('data-role') === 'seat-pause')[0].click();
  await new Promise((r) => setTimeout(r, 0));
  eq(calls.join(','), 'deepseek4:true');
});

test('the form registers a seat with its policy', async () => {
  const host = new FakeEl('DIV');
  const saved: SeatInput[] = [];
  const p = new SeatsPage(asEl(host), deps([], { saveSeat: async (s: SeatInput) => { saved.push(s); return seat({ callsign: s.callsign }); } }));
  await p.load();
  const field = (name: string) => findAll(host, (e) => e.getAttribute('data-field') === name)[0];
  field('callsign').value = 'muse';
  field('kind').value = 'opencode';
  field('where').value = 'tmux muse';
  findAll(host, (e) => e.getAttribute('data-role') === 'seat-add')[0].click();
  await new Promise((r) => setTimeout(r, 0));
  eq(saved.length, 1, 'one seat saved');
  eq(saved[0].callsign, 'muse');
  eq(saved[0].kind, 'opencode');
  eq(saved[0].policy?.enabled, false);
});

test('policy and state lines read the owner settings', () => {
  eq(policyLine({ enabled: false, trigger: 'idle', idle_minutes: 60, min_gap_minutes: 60, max_per_hour: 1 }), 'nudges off');
  eq(policyLine({ enabled: true, paused: true, trigger: 'idle', idle_minutes: 60, min_gap_minutes: 60, max_per_hour: 1 }), 'nudges paused');
  eq(policyLine({ enabled: true, trigger: 'both', idle_minutes: 30, min_gap_minutes: 60, max_per_hour: 2, quiet_hours: '22:00-07:00' }),
    'nudges on (both, after 30m idle, gap 60m, max 2/h, quiet 22:00-07:00)');
  eq(seatState(seat({ halt: 'in use elsewhere' })), 'waits for the owner: in use elsewhere');
  eq(seatState(seat({ running: false })), 'no session');
});

test('a server without the module says it is off', async () => {
  const host = new FakeEl('DIV');
  const p = new SeatsPage(asEl(host), deps([], { coordStatus: async () => { throw new Error('the coordinator module is off'); } }));
  await p.load();
  ok(host.textContent.includes('module is off'), host.textContent);
});

void run('agentflow-seatspage');
