// plan-gen.ts — Plan generation, review loop, and approval.

export { initPlanGen };

import { api } from './api.js';
import { confirmDialog, alertDialog } from '@shared';

interface PlanData {
  frd_id: string;
  status: 'draft' | 'approved' | 'in_progress' | 'completed';
  created?: string;
  approved_date?: string;
  approach: string;
  assumptions: string[];
  risks: PlanRisk[];
  phases: PlanPhase[];
  estimated_tasks: number;
  review_notes?: PlanReviewNote[];
}

interface PlanRisk {
  risk: string;
  mitigation: string;
}

interface PlanPhase {
  id: string;
  name: string;
  tasks_description: string;
  depends_on: string[];
}

interface PlanReviewNote {
  description: string;
  severity: 'blocker' | 'major' | 'minor';
}

interface PlanReviewResult {
  verdict: 'pass' | 'repair' | 'fail';
  summary: string;
  issues: Array<{ description: string; severity: 'blocker' | 'major' | 'minor' }>;
  repair_prompt: string;
}

let planData: PlanData | null = null;
let reviewRound = 0;
let maxReviewRounds = 3;

async function generatePlan(frd: any): Promise<void> {
  try {
    const resp = await fetch('/plan', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ frd }),
    });
    if (!resp.ok) throw new Error('Failed to generate plan');
    planData = await resp.json();
    planData!.status = 'draft';
    reviewRound = 0;
    renderPlan();
  } catch (err) {
    await alertDialog('Failed to generate plan: ' + (err as Error).message);
  }
}

async function reviewPlan(): Promise<PlanReviewResult> {
  if (!planData) throw new Error('No plan to review');

  const resp = await fetch('/plan/review', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ plan: planData }),
  });
  if (!resp.ok) throw new Error('Review failed');
  return resp.json();
}

async function runReviewLoop(): Promise<void> {
  while (reviewRound < maxReviewRounds) {
    reviewRound++;
    const result = await reviewPlan();

    if (result.verdict === 'pass') {
      planData!.status = 'approved';
      planData!.review_notes = result.issues.map(i => ({
        description: i.description,
        severity: i.severity,
      }));
      await alertDialog(`Plan approved after ${reviewRound} review round(s)`);
      renderPlan();
      return;
    }

    if (result.verdict === 'fail') {
      await alertDialog('Plan review failed: ' + result.summary);
      planData!.status = 'draft';
      renderPlan();
      return;
    }

    // repair - show issues and let user decide
    const issuesText = result.issues.map(i => `[${i.severity}] ${i.description}`).join('\n');
    const proceed = await confirmDialog(
      `Review round ${reviewRound} found issues:\n\n${issuesText}\n\nRepair prompt:\n${result.repair_prompt}\n\nContinue with repair?`,
      { title: 'Plan Review - Repair Required', confirmLabel: 'Repair & Continue' }
    );

    if (!proceed) {
      planData!.status = 'draft';
      renderPlan();
      return;
    }

    // Regenerate plan with repair prompt
    await regeneratePlan(result.repair_prompt);
  }

  await alertDialog('Max review rounds reached. Plan needs manual intervention.');
  planData!.status = 'draft';
  renderPlan();
}

async function regeneratePlan(repairPrompt: string): Promise<void> {
  if (!planData) return;
  try {
    const resp = await fetch('/plan', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...planData, repair_prompt: repairPrompt }),
    });
    if (!resp.ok) throw new Error('Failed to regenerate plan');
    planData = await resp.json();
    renderPlan();
  } catch (err) {
    await alertDialog('Failed to regenerate plan: ' + (err as Error).message);
  }
}

async function compileTasks(): Promise<void> {
  if (!planData || planData.status !== 'approved') {
    await alertDialog('Plan must be approved before compiling tasks');
    return;
  }

  try {
    const resp = await fetch('/tasks/compile', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ plan: planData }),
    });
    if (!resp.ok) throw new Error('Failed to compile tasks');
    await alertDialog('Tasks compiled successfully');
    location.hash = '#queue';
  } catch (err) {
    await alertDialog('Failed to compile tasks: ' + (err as Error).message);
  }
}

function renderPlan(): void {
  const host = document.getElementById('plan-host');
  if (!host) return;

  if (!planData) {
    host.innerHTML = `
      <div class="plan-empty">
        <h3>No Plan Yet</h3>
        <p>Complete the FRD wizard first, then generate a plan.</p>
        <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="gen-plan-btn">Generate Plan</button>
      </div>
    `;
    host.querySelector('#gen-plan-btn')?.addEventListener('click', async () => {
      try {
        const r = await fetch('/frd');
        if (!r.ok) {
          throw new Error('No FRD found. Please complete the FRD wizard first.');
        }
        const frd = await r.json();
        await generatePlan(frd);
      } catch (e) {
        alertDialog('Error: ' + (e as Error).message);
      }
    });
    return;
  }

  host.innerHTML = `
    <div class="plan-header">
      <div class="plan-meta">
        <div class="plan-status ${planData.status}">${planData.status.toUpperCase()}</div>
        <div class="plan-frd-id">FRD: ${planData.frd_id}</div>
        ${planData.review_notes && planData.review_notes.length > 0 ? `
          <div class="plan-review-notes">
            <strong>Review Notes:</strong>
            <ul>${planData.review_notes.map(n => `<li>[${n.severity}] ${n.description}</li>`).join('')}</ul>
          </div>
        ` : ''}
      </div>
      <div class="plan-actions">
        ${planData.status === 'draft' ? `
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="review-plan-btn">Start Review</button>
        ` : ''}
        ${planData.status === 'approved' ? `
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="compile-tasks-btn">Compile Tasks</button>
        ` : ''}
        ${planData.status !== 'approved' ? `
          <button type="button" class="ui-modal-btn" id="regen-plan-btn">Regenerate</button>
        ` : ''}
      </div>
    </div>

    <div class="plan-content">
      <section class="plan-section">
        <h4>Approach</h4>
        <pre>${planData.approach}</pre>
      </section>

      <section class="plan-section">
        <h4>Assumptions</h4>
        <ul>${planData.assumptions.map(a => `<li>${a}</li>`).join('') || '<li>(none)</li>'}</ul>
      </section>

      <section class="plan-section">
        <h4>Risks</h4>
        ${planData.risks.length > 0 ? `
          <ul>${planData.risks.map(r => `<li><strong>${r.risk}</strong> — ${r.mitigation}</li>`).join('')}</ul>
        ` : '<p>(none)</p>'}
      </section>

      <section class="plan-section">
        <h4>Phases (${planData.phases.length}, ~${planData.estimated_tasks} tasks)</h4>
        <div class="phase-list">
          ${planData.phases.map(p => `
            <div class="phase-card">
              <div class="phase-header">
                <strong>${p.id}: ${p.name}</strong>
                ${p.depends_on.length > 0 ? `<span class="phase-deps">depends on: ${p.depends_on.join(', ')}</span>` : ''}
              </div>
              <div class="phase-desc">${p.tasks_description}</div>
            </div>
          `).join('')}
        </div>
      </section>
    </div>
  `;

  host.querySelector('#review-plan-btn')?.addEventListener('click', runReviewLoop);
  host.querySelector('#compile-tasks-btn')?.addEventListener('click', compileTasks);
  host.querySelector('#regen-plan-btn')?.addEventListener('click', () => {
    if (planData) generatePlan(planData);
  });
}

async function initPlanGen(): Promise<void> {
  const container = document.getElementById('plan-gen');
  if (!container) return;

  container.innerHTML = `<div id="plan-host"></div>`;

  // Load existing plan if any
  try {
    const resp = await fetch('/plan');
    if (resp.ok) {
      planData = await resp.json();
    }
  } catch {
    planData = null;
  }

  renderPlan();
}