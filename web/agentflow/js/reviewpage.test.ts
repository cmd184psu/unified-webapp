import { test, run, ok, installFakeDom, FakeEl, asEl } from './testkit';
import { ReviewsPage } from './reviewpage';
import type { Review } from './api';

installFakeDom();

const review = (over: Partial<Review> = {}): Review => ({
  id: 7, project: 'fs3', branch: 'feature/ai', task_id: 'TASK-9', state: 'done',
  verdict: 'PASS', findings: '1. No findings.', reviewer_model: 'deepseek-v4', created: 1, ...over,
});

test('the page lists reviews with their verdict and findings', async () => {
  const host = new FakeEl('DIV');
  const p = new ReviewsPage(asEl(host), { reviews: async () => [review(), review({ id: 8, task_id: 'TASK-10', verdict: 'CHANGES-REQUESTED', findings: '1. off by one' })] });
  await p.load();
  ok(host.textContent.includes('TASK-9') && host.textContent.includes('PASS'), host.textContent);
  ok(host.textContent.includes('off by one'), host.textContent);
});

test('the page says when no reviews exist', async () => {
  const host = new FakeEl('DIV');
  await new ReviewsPage(asEl(host), { reviews: async () => [] }).load();
  ok(host.textContent.includes('No reviews yet'), host.textContent);
});

void run('agentflow-reviewpage');
