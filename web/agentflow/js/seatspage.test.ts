import { test, run, eq, ok, installFakeDom, FakeEl, asEl, findAll } from './testkit';
import { SeatsPage } from './seatspage';
import type { CoordStatus, ExternalSeat } from './api';

installFakeDom();

const seat = (over: Partial<ExternalSeat> = {}): ExternalSeat => ({ callsign: 'deepseek4', session: 'deepseek4', running: true, since: '2026-11-20T08:00:00Z', ready: true, feed: true, nudge: true, queue_file: '/q/deepseek4.md', current: 'TASK-444', ...over });
const status: CoordStatus = { enabled: true, dry_run: true, me: 'coord-fs3', base: 'feature/x', watch: [],
  last_run: { started: '2026-11-20T08:00:00Z', finished: '2026-11-20T08:00:05Z', dry: true, decisions: [{ kind: 'fed', task: 'TASK-445', seat: 'deepseek4', detail: 'next' }] } };

test('the page lists the seats, their state and the last pass, and Nudge nudges', async () => {
  const host = new FakeEl('DIV');
  const nudged: string[] = [];
  const p = new SeatsPage(asEl(host), {
    coordStatus: async () => status,
    externalSeats: async () => [seat(), seat({ callsign: 'fb-beam', session: 'freebuff-beam', halt: 'in use elsewhere', nudge: true })],
    seatQueue: async (cs: string) => ({ callsign: cs, text: '# Queue' }),
    nudgeSeat: async (cs: string) => { nudged.push(cs); return { nudged: cs }; },
    coordRun: async () => status.last_run!,
  });
  await p.load();
  ok(host.textContent.includes('deepseek4') && host.textContent.includes('task TASK-444'), host.textContent);
  ok(host.textContent.includes('waits for the owner: in use elsewhere'), host.textContent);
  ok(host.textContent.includes('fed TASK-445'), host.textContent);
  const btns = findAll(host, (e) => e.getAttribute('data-role') === 'seat-nudge');
  eq(btns.length, 2);
  btns[0].click();
  await new Promise((r) => setTimeout(r, 0));
  eq(nudged.join(','), 'deepseek4');
});

test('a server without the module says it is off', async () => {
  const host = new FakeEl('DIV');
  const p = new SeatsPage(asEl(host), {
    coordStatus: async () => { throw new Error('the coordinator module is off'); },
    externalSeats: async () => [], seatQueue: async () => ({ callsign: '', text: '' }), nudgeSeat: async () => ({ nudged: '' }), coordRun: async () => status.last_run!,
  });
  await p.load();
  ok(host.textContent.includes('module is off'), host.textContent);
});

void run('agentflow-seatspage');
