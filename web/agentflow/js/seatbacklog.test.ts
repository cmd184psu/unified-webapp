import { test, run, eq, ok, installFakeDom, FakeEl, asEl, findAll, byAttr, byField, flush } from './testkit';
import { SeatTaskBacklog, nextState, type SeatTaskBacklogDeps } from './seatbacklog';
import type { SeatTask, SeatTaskInput } from './api';

installFakeDom();

const task = (over: Partial<SeatTask>): SeatTask => ({ id: 1, title: 'Wire the backlog', objective: '', eligible_seats: ['solar'], priority: 1, state: 'ready', ...over });

/** fixture is a server holding three tasks, one in each state, and what was sent to it. */
function fixture() {
  const list: SeatTask[] = [
    task({}),
    task({ id: 2, title: 'Review the panel', state: 'fed', eligible_seats: ['beam'], priority: 2 }),
    task({ id: 3, title: 'Old task', state: 'done', priority: 0 }),
  ];
  const posted: SeatTaskInput[] = [];
  const patched: string[] = [];
  let next = list.length;
  const deps: SeatTaskBacklogDeps = {
    seatTasks: async (state = '', seat = '') =>
      list.filter((t) => (!state || t.state === state) && (!seat || t.eligible_seats.includes(seat))),
    createSeatTask: async (input: SeatTaskInput) => {
      posted.push(input);
      const created = task({ ...input, id: ++next, eligible_seats: input.eligible_seats ?? [] });
      list.push(created);
      return created;
    },
    setSeatTaskState: async (id: number, state: string) => {
      patched.push(`${id}:${state}`);
      const t = list.find((x) => x.id === id);
      if (!t) throw new Error(`no task ${id}`);
      t.state = state;
      return t;
    },
  };
  return { posted, patched, deps };
}

const rows = (host: FakeEl): FakeEl[] => findAll(host, (e) => e.tagName === 'LI');

const mounted = () => {
  const { posted, patched, deps } = fixture();
  const host = new FakeEl('DIV');
  new SeatTaskBacklog(asEl(host), deps).mount();
  return { posted, patched, host };
};

test('lists every seat task with its state, seats and priority', async () => {
  const { host } = mounted();
  await flush();
  eq(rows(host).length, 3, 'three rows');
  ok(host.textContent.includes('Wire the backlog'), host.textContent);
  ok(host.textContent.includes('fed · beam · priority 2'), host.textContent);
  ok(host.textContent.includes('any seat'), host.textContent);
  eq(findAll(host, (e) => e.getAttribute('data-role') === 'backlog-advance').length, 2, 'done offers no move');
});

test('the state and seat filters narrow the list', async () => {
  const { host } = mounted();
  await flush();
  const state = byField(host, 'backlog-state');
  state.value = 'ready';
  state.fire('change', { target: state });
  await flush();
  eq(rows(host).length, 1, 'only the ready one');
  ok(host.textContent.includes('Wire the backlog'), host.textContent);
  // The seat filter is built from the unfiltered backlog: beam's only task is fed,
  // so a dropdown built from the ready rows would have lost it.
  const options = findAll(byField(host, 'backlog-seat'), (e) => e.tagName === 'OPTION').map((e) => e.textContent);
  ok(options.includes('beam'), options.join(','));
  ok(options.includes('solar'), options.join(','));
  state.value = '';
  state.fire('change', { target: state });
  await flush();
  const seat = byField(host, 'backlog-seat');
  seat.value = 'beam';
  seat.fire('change', { target: seat });
  await flush();
  eq(rows(host).length, 1, 'only beam’s task');
  ok(host.textContent.includes('Review the panel'), host.textContent);
});

test('the form posts what was typed and the new row appears', async () => {
  const { host, posted } = mounted();
  await flush();
  byField(host, 'backlog-title').value = 'Feed the backlog to a seat';
  byField(host, 'backlog-objective').value = 'wire it up';
  byField(host, 'backlog-seats').value = 'solar, beam';
  byField(host, 'backlog-priority').value = '3';
  byAttr(host, 'data-role', 'backlog-add')[0].click();
  await flush();
  await flush();
  eq(posted.length, 1, 'one POST');
  eq(posted[0].title, 'Feed the backlog to a seat');
  eq(posted[0].objective, 'wire it up');
  eq(posted[0].eligible_seats, ['solar', 'beam']);
  eq(posted[0].priority, 3);
  eq(rows(host).length, 4, 'the new row is there');
  ok(host.textContent.includes('Feed the backlog to a seat'), host.textContent);
});

test('a seat filter with no task left is dropped instead of shown', async () => {
  const { host } = mounted();
  await flush();
  const seat = byField(host, 'backlog-seat');
  seat.value = 'ghost';
  seat.fire('change', { target: seat });
  await flush();
  await flush();
  eq(rows(host).length, 3, 'the stale filter is gone, so every row is back');
  eq((byField(host, 'backlog-seat') as FakeEl).value, '', 'the select shows every seat again');
});

test('a message belongs to the change that made it', async () => {
  const { host } = mounted();
  await flush();
  byField(host, 'backlog-title').value = 'Feed the backlog to a seat';
  byAttr(host, 'data-role', 'backlog-add')[0].click();
  await flush();
  await flush();
  ok(host.textContent.includes('added #4'), host.textContent);
  const state = byField(host, 'backlog-state');
  state.value = 'done';
  state.fire('change', { target: state });
  await flush();
  ok(!host.textContent.includes('added #4'), `a stale message survived a filter change: ${host.textContent}`);
});

test('a task moves ready -> fed -> done and the row shows it', async () => {
  const { host, patched } = mounted();
  await flush();
  const first = () => byAttr(host, 'data-task', '1')[0];
  ok(first().textContent.includes('ready'), first().textContent);
  byAttr(host, 'data-role', 'backlog-advance')[0].click();
  await flush();
  await flush();
  eq(patched.join(','), '1:fed');
  ok(first().textContent.includes('fed · solar'), first().textContent);
  byAttr(host, 'data-task', '1')[1].click();
  await flush();
  await flush();
  eq(patched.join(','), '1:fed,1:done');
  ok(first().textContent.includes('done · solar'), first().textContent);
  eq(nextState('done'), '', 'done is the end');
});

void run('agentflow-seatbacklog');
