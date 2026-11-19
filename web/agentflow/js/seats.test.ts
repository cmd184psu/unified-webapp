import { test, run, eq, ok, installFakeDom, FakeEl, asEl } from './testkit';
import { SeatPanel, seatLabel } from './seats';
import type { SeatStage } from './api';

installFakeDom();

const seat = (over: Partial<SeatStage> = {}): SeatStage => ({
  stage: 42, project: 'demo', effort: 3, task: 'T001', role: 'worker', backend: 'fb', api_type: 'freebuff_seat',
  model: 'freebuff', attach: 'tmux attach -t af-demo-3-T001-42', ...over,
});

test('a terminal seat shows its attach command', async () => {
  const host = new FakeEl('DIV');
  const p = new SeatPanel(asEl(host), { seats: async () => [seat()], answerSeat: async () => ({ stage: 42, answered: true }), cancelSeat: async () => ({ stage: 42, cancelled: true }) });
  await p.load();
  ok(!host.hasAttribute('hidden'), 'shown');
  ok(host.textContent.includes('tmux attach -t af-demo-3-T001-42'), host.textContent);
  ok(host.textContent.includes('One seat is working'), host.textContent);
});

test('a waiting seat shows what it waits on; the panel hides with no seats', async () => {
  const host = new FakeEl('DIV');
  const list = [seat({ waiting: { stage: 42, kind: 'seat_question', message: 'The seat asks: tabs or spaces?', since: '' } })];
  const p = new SeatPanel(asEl(host), { seats: async () => list, answerSeat: async () => ({ stage: 42, answered: true }), cancelSeat: async () => ({ stage: 42, cancelled: true }) });
  await p.load();
  ok(host.textContent.includes('tabs or spaces?'), host.textContent);
  list.length = 0;
  await p.load();
  ok(host.hasAttribute('hidden'), 'hidden');
});

test('the label names where the stage sits', () => {
  eq(seatLabel(seat()), 'demo, effort 3, T001 — worker on fb (stage 42)');
});

void run('agentflow-seats');
