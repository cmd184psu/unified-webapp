// frd-wizard.ts — Step-through FRD creation wizard.

export { initFRDWizard };

import { api } from './api.js';
import { confirmDialog, alertDialog } from '@shared';

interface FRDStep {
  id: string;
  title: string;
  render: (host: HTMLElement, data: FRDData) => void;
  validate: (data: FRDData) => string | null;
  next?: string;
}

interface FRDData {
  id: string;
  title: string;
  objective: string;
  requirements: FRDRequirement[];
  acceptance_criteria: FRDAC[];
  constraints: string[];
  context: string;
}

interface FRDRequirement {
  id: string;
  description: string;
  priority: 'must' | 'should' | 'could';
}

interface FRDAC {
  id: string;
  criterion: string;
}

let frdData: FRDData = {
  id: 'frd-001',
  title: '',
  objective: '',
  requirements: [],
  acceptance_criteria: [],
  constraints: [],
  context: '',
};

let currentStepIndex = 0;

const steps: FRDStep[] = [
  {
    id: 'objective',
    title: 'Objective',
    render: (host, data) => {
      host.innerHTML = `
        <div class="wizard-field">
          <label for="frd-id">FRD ID</label>
          <input type="text" id="frd-id" value="${data.id}" pattern="^frd-\d{3,}$" required>
          <span class="field-hint">Format: frd-001, frd-100, etc.</span>
        </div>
        <div class="wizard-field">
          <label for="frd-title">Title</label>
          <input type="text" id="frd-title" value="${data.title}" maxlength="120" required placeholder="One-line summary">
        </div>
        <div class="wizard-field">
          <label for="frd-objective">Objective</label>
          <textarea id="frd-objective" rows="4" required placeholder="One paragraph: what and why">${data.objective}</textarea>
        </div>
        <div class="wizard-field">
          <label for="frd-context">Context (optional)</label>
          <textarea id="frd-context" rows="3" placeholder="Background info, links, prior art">${data.context}</textarea>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-next">Next</button>
        </div>
      `;
      const titleEl = host.querySelector('#frd-title') as HTMLInputElement;
      const objEl = host.querySelector('#frd-objective') as HTMLTextAreaElement;
      const ctxEl = host.querySelector('#frd-context') as HTMLTextAreaElement;
      const idEl = host.querySelector('#frd-id') as HTMLInputElement;
      titleEl.addEventListener('input', () => data.title = titleEl.value);
      objEl.addEventListener('input', () => data.objective = objEl.value);
      ctxEl.addEventListener('input', () => data.context = ctxEl.value);
      idEl.addEventListener('input', () => data.id = idEl.value);
      host.querySelector('#wizard-next')?.addEventListener('click', () => {
        const err = steps[currentStepIndex].validate(data);
        if (err) { alertDialog(err); return; }
        if (steps[currentStepIndex].next) goToStep(steps[currentStepIndex].next!);
      });
    },
    validate: (data) => {
      if (!data.id.match(/^frd-\d{3,}$/)) return 'FRD ID must match frd-NNN format';
      if (!data.title.trim()) return 'Title is required';
      if (!data.objective.trim()) return 'Objective is required';
      return null;
    },
    next: 'requirements',
  },
  {
    id: 'requirements',
    title: 'Requirements',
    render: (host, data) => {
      host.innerHTML = `
        <div class="wizard-list">
          <div class="list-header">
            <h4>Requirements</h4>
            <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="add-req">Add Requirement</button>
          </div>
          <div id="req-list" class="req-list"></div>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn" id="wizard-back">Back</button>
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-next">Next</button>
        </div>
      `;
      renderRequirementList(host, data);
      host.querySelector('#add-req')?.addEventListener('click', () => {
        data.requirements.push({ id: nextReqId(data.requirements), description: '', priority: 'must' });
        renderRequirementList(host, data);
      });
      host.querySelector('#wizard-back')?.addEventListener('click', () => goToStep('objective'));
      host.querySelector('#wizard-next')?.addEventListener('click', () => {
        const err = steps[currentStepIndex].validate(data);
        if (err) { alertDialog(err); return; }
        if (steps[currentStepIndex].next) goToStep(steps[currentStepIndex].next!);
      });
    },
    validate: (data) => {
      if (data.requirements.length === 0) return 'At least one requirement is required';
      for (const r of data.requirements) {
        if (!r.description.trim()) return 'All requirements must have a description';
        if (!r.id.match(/^R\d+$/)) return `Requirement ID ${r.id} must match R1, R2... format`;
      }
      return null;
    },
    next: 'acceptance',
  },
  {
    id: 'acceptance',
    title: 'Acceptance Criteria',
    render: (host, data) => {
      host.innerHTML = `
        <div class="wizard-list">
          <div class="list-header">
            <h4>Acceptance Criteria</h4>
            <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="add-ac">Add Criterion</button>
          </div>
          <div id="ac-list" class="ac-list"></div>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn" id="wizard-back">Back</button>
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-next">Next</button>
        </div>
      `;
      renderACList(host, data);
      host.querySelector('#add-ac')?.addEventListener('click', () => {
        data.acceptance_criteria.push({ id: nextACId(data.acceptance_criteria), criterion: '' });
        renderACList(host, data);
      });
      host.querySelector('#wizard-back')?.addEventListener('click', () => goToStep('requirements'));
      host.querySelector('#wizard-next')?.addEventListener('click', () => {
        const err = steps[currentStepIndex].validate(data);
        if (err) { alertDialog(err); return; }
        if (steps[currentStepIndex].next) goToStep(steps[currentStepIndex].next!);
      });
    },
    validate: (data) => {
      if (data.acceptance_criteria.length === 0) return 'At least one acceptance criterion is required';
      for (const ac of data.acceptance_criteria) {
        if (!ac.criterion.trim()) return 'All criteria must have a description';
        if (!ac.id.match(/^AC\d+$/)) return `Criterion ID ${ac.id} must match AC1, AC2... format`;
      }
      return null;
    },
    next: 'constraints',
  },
  {
    id: 'constraints',
    title: 'Constraints',
    render: (host, data) => {
      host.innerHTML = `
        <div class="wizard-field">
          <label>Constraints (one per line)</label>
          <textarea id="frd-constraints" rows="6" placeholder="Go standard library only; no new external dependencies.&#10;Queue and plan JSON formats are byte-compatible...">${data.constraints.join('\n')}</textarea>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn" id="wizard-back">Back</button>
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-next">Next</button>
        </div>
      `;
      const ta = host.querySelector('#frd-constraints') as HTMLTextAreaElement;
      ta.addEventListener('input', () => {
        data.constraints = ta.value.split('\n').map(s => s.trim()).filter(s => s);
      });
      host.querySelector('#wizard-back')?.addEventListener('click', () => goToStep('acceptance'));
      host.querySelector('#wizard-next')?.addEventListener('click', () => {
        const err = steps[currentStepIndex].validate(data);
        if (err) { alertDialog(err); return; }
        if (steps[currentStepIndex].next) goToStep(steps[currentStepIndex].next!);
      });
    },
    validate: () => null,
    next: 'review',
  },
  {
    id: 'review',
    title: 'Review & Submit',
    render: (host, data) => {
      host.innerHTML = `
        <div class="review-section">
          <h4>FRD ID</h4>
          <pre>${data.id}</pre>
        </div>
        <div class="review-section">
          <h4>Title</h4>
          <pre>${data.title}</pre>
        </div>
        <div class="review-section">
          <h4>Objective</h4>
          <pre>${data.objective}</pre>
        </div>
        <div class="review-section">
          <h4>Requirements (${data.requirements.length})</h4>
          <pre>${data.requirements.map(r => `${r.id}: ${r.description} [${r.priority}]`).join('\n')}</pre>
        </div>
        <div class="review-section">
          <h4>Acceptance Criteria (${data.acceptance_criteria.length})</h4>
          <pre>${data.acceptance_criteria.map(ac => `${ac.id}: ${ac.criterion}`).join('\n')}</pre>
        </div>
        <div class="review-section">
          <h4>Constraints (${data.constraints.length})</h4>
          <pre>${data.constraints.join('\n')}</pre>
        </div>
        <div class="review-section">
          <h4>Context</h4>
          <pre>${data.context || '(none)'}</pre>
        </div>
        <div class="wizard-actions">
          <button type="button" class="ui-modal-btn" id="wizard-back">Back</button>
          <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="wizard-submit">Submit FRD</button>
        </div>
      `;
      host.querySelector('#wizard-back')?.addEventListener('click', () => goToStep('constraints'));
      host.querySelector('#wizard-submit')?.addEventListener('click', () => submitFRD(data));
    },
    validate: () => null,
  },
];

function nextReqId(arr: FRDRequirement[]): string {
  const nums = arr.map(r => parseInt(r.id.slice(1))).filter(n => !isNaN(n));
  return 'R' + (Math.max(0, ...nums) + 1);
}

function nextACId(arr: FRDAC[]): string {
  const nums = arr.map(ac => parseInt(ac.id.slice(2))).filter(n => !isNaN(n));
  return 'AC' + (Math.max(0, ...nums) + 1);
}

function renderRequirementList(host: HTMLElement, data: FRDData): void {
  const list = host.querySelector('#req-list')!;
  list.innerHTML = data.requirements.map((r, i) => `
    <div class="req-item" data-index="${i}">
      <div class="req-header">
        <input type="text" class="req-id" value="${r.id}" readonly>
        <select class="req-priority">
          <option value="must" ${r.priority === 'must' ? 'selected' : ''}>Must</option>
          <option value="should" ${r.priority === 'should' ? 'selected' : ''}>Should</option>
          <option value="could" ${r.priority === 'could' ? 'selected' : ''}>Could</option>
        </select>
        <button type="button" class="icon-btn req-delete" title="Delete">✕</button>
      </div>
      <textarea class="req-desc" rows="2" placeholder="Description">${r.description}</textarea>
    </div>
  `).join('');

  list.querySelectorAll('.req-item').forEach((item, i) => {
    const desc = item.querySelector('.req-desc') as HTMLTextAreaElement;
    const priority = item.querySelector('.req-priority') as HTMLSelectElement;
    const del = item.querySelector('.req-delete') as HTMLButtonElement;
    desc.addEventListener('input', () => data.requirements[i].description = desc.value);
    priority.addEventListener('change', () => data.requirements[i].priority = priority.value as any);
    del.addEventListener('click', () => {
      data.requirements.splice(i, 1);
      renderRequirementList(host, data);
    });
  });
}

function renderACList(host: HTMLElement, data: FRDData): void {
  const list = host.querySelector('#ac-list')!;
  list.innerHTML = data.acceptance_criteria.map((ac, i) => `
    <div class="ac-item" data-index="${i}">
      <div class="ac-header">
        <input type="text" class="ac-id" value="${ac.id}" readonly>
        <button type="button" class="icon-btn ac-delete" title="Delete">✕</button>
      </div>
      <textarea class="ac-criterion" rows="2" placeholder="Criterion">${ac.criterion}</textarea>
    </div>
  `).join('');

  list.querySelectorAll('.ac-item').forEach((item, i) => {
    const criterion = item.querySelector('.ac-criterion') as HTMLTextAreaElement;
    const del = item.querySelector('.ac-delete') as HTMLButtonElement;
    criterion.addEventListener('input', () => data.acceptance_criteria[i].criterion = criterion.value);
    del.addEventListener('click', () => {
      data.acceptance_criteria.splice(i, 1);
      renderACList(host, data);
    });
  });
}

function goToStep(stepId: string): void {
  const index = steps.findIndex(s => s.id === stepId);
  if (index >= 0) {
    currentStepIndex = index;
    renderWizard();
  }
}

function renderWizard(): void {
  const host = document.getElementById('wizard-host');
  const step = steps[currentStepIndex];
  if (!host || !step) return;

  host.innerHTML = `
    <div class="wizard-progress">
      ${steps.map((s, i) => `<span class="wizard-step ${i === currentStepIndex ? 'active' : ''} ${i < currentStepIndex ? 'done' : ''}">${s.title}</span>`).join('')}
    </div>
    <div id="wizard-content"></div>
  `;
  step.render(document.getElementById('wizard-content')!, frdData);
}

async function submitFRD(data: FRDData): Promise<void> {
  try {
    const resp = await fetch('/frd', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    });
    if (!resp.ok) {
      const err = await resp.json();
      throw new Error(err.error || 'Failed to save FRD');
    }
    await alertDialog('FRD saved successfully');
    // Switch to plan tab
    location.hash = '#plan';
  } catch (err) {
    await alertDialog('Failed to save FRD: ' + (err as Error).message);
  }
}
 
function initFRDWizard(): void {
  const container = document.getElementById('frd-wizard');
  if (!container) return;

  container.innerHTML = `
    <div id="wizard-host"></div>
  `;
  currentStepIndex = 0;
  frdData = {
    id: 'frd-001',
    title: '',
    objective: '',
    requirements: [],
    acceptance_criteria: [],
    constraints: [],
    context: '',
  };
  renderWizard();
}