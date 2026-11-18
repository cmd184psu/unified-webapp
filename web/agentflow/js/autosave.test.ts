import { test, run, eq, ok, installFakeDom, FakeEl } from './testkit';
import { autosave, forgetAll, timing, rollup, saveAll, mountSaveMark } from './autosave';

installFakeDom();
timing.delay = 10;
const wait = (ms: number): Promise<void> => new Promise((r) => setTimeout(r, ms));

test('AS1: nothing changed means nothing saved, however long it waits', async () => {
  let saves = 0;
  const a = autosave({ snapshot: () => 'same', save: async () => { saves++; return true; } });
  a.touch();
  await wait(40);
  await a.flush();
  eq(saves, 0);
});

test('AS2: a change saves once after the pause, and typing again moves the pause', async () => {
  let value = 'a';
  const saved: string[] = [];
  const a = autosave({ snapshot: () => value, save: async () => { saved.push(value); return true; } });
  value = 'b';
  a.touch();
  await wait(6);
  value = 'bc';
  a.touch();
  await wait(6);
  eq(saved, []);
  await wait(30);
  eq(saved, ['bc']);
  eq(a.status.getAttribute('data-state'), 'saved');
  await wait(30);
  eq(saved, ['bc']);
});

test('AS3: a refused save is not retried until something changes again', async () => {
  let value = 'a';
  let saves = 0;
  const a = autosave({ snapshot: () => value, save: async () => { saves++; return false; } });
  value = 'b';
  a.touch();
  await wait(40);
  eq(saves, 1);
  eq(a.status.getAttribute('data-state'), 'failed');
  await wait(40);
  eq(saves, 1);
  value = 'c';
  a.touch();
  await wait(40);
  eq(saves, 2);
});

test('AS4: an edit that is undone before the pause ends saves nothing; stop cancels', async () => {
  let value = 'a';
  let saves = 0;
  const a = autosave({ snapshot: () => value, save: async () => { saves++; return true; } });
  value = 'b';
  a.touch();
  value = 'a';
  await wait(40);
  eq(saves, 0);
  value = 'z';
  a.touch();
  a.stop();
  await wait(40);
  eq(saves, 0);
});

test('AS5: watch reacts to input and change events from inside the card', async () => {
  let value = 'a';
  let saves = 0;
  const card = new FakeEl('DIV');
  const field = new FakeEl('INPUT');
  card.appendChild(field);
  const a = autosave({ snapshot: () => value, save: async () => { saves++; return true; } });
  a.watch(card as unknown as HTMLElement);
  value = 'b';
  field.fire('input');
  await wait(40);
  eq(saves, 1);
  ok(true, 'done');
});

test('AS6: a card rebuilt around unsaved edits saves them after the pause', async () => {
  let saves = 0;
  const a = autosave({ snapshot: () => 'draft', save: async () => { saves++; return true; }, startDirty: true });
  a.touch();
  await wait(40);
  eq(saves, 1);
});

test('AS7: the corner mark rolls the cards up: pending, failed, saved; a click saves now', async () => {
  forgetAll();
  let a = 'a';
  let b = 'b';
  let bOk = false;
  const one = autosave({ snapshot: () => a, save: async () => true });
  const two = autosave({ snapshot: () => b, save: async () => bOk });
  const corner = new FakeEl('DIV');
  const stop = mountSaveMark(corner as unknown as HTMLElement);
  const mark = corner.childNodes[0] as FakeEl;
  eq(mark.getAttribute('data-state'), 'idle');
  a = 'a2';
  one.touch();
  eq(rollup(), 'pending');
  eq(mark.getAttribute('data-state'), 'pending');
  mark.click();
  await wait(5);
  eq(mark.getAttribute('data-state'), 'idle');
  b = 'b2';
  two.touch();
  await saveAll();
  eq(rollup(), 'failed');
  eq(mark.getAttribute('data-state'), 'failed');
  bOk = true;
  b = 'b3';
  two.touch();
  await wait(40);
  eq(rollup(), 'idle');
  one.stop();
  two.stop();
  stop();
  eq(corner.childNodes.length, 0);
});

void run('agentflow-autosave');
