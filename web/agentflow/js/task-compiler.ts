// task-compiler.ts — Compile approved plan into atomic task queue.

import { api } from './api.js';
import { confirmDialog, alertDialog } from '@shared';

interface CompileResult {
  queue: any;
  warnings: string[];
}

async function compileTasks(): Promise<void> {
  try {
    const resp = await fetch('/tasks/compile', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}), // plan is server-side
    });
    if (!resp.ok) {
      const err = await resp.json();
      throw new Error(err.error || 'Compilation failed');
    }
    const result: CompileResult = await resp.json();

    if (result.warnings.length > 0) {
      await alertDialog('Compilation completed with warnings:\n' + result.warnings.join('\n'));
    } else {
      await alertDialog('Tasks compiled successfully');
    }

    // Reload queue view
    location.hash = '#queue';
    location.reload();
  } catch (err) {
    await alertDialog('Compilation failed: ' + (err as Error).message);
  }
}

export function initTaskCompiler(): void {
  const container = document.getElementById('task-compiler');
  if (!container) return;

  container.innerHTML = `
    <div class="compiler-panel">
      <h3>Task Compilation</h3>
      <p>Convert the approved plan into atomic tasks. Each phase becomes one or more tasks with prompts, verify commands, and acceptance criteria.</p>
      <button type="button" class="ui-modal-btn ui-modal-btn-primary" id="compile-btn">Compile Tasks</button>
      <div id="compile-output" class="compile-output"></div>
    </div>
  `;

  const btn = document.getElementById('compile-btn');
  btn?.addEventListener('click', async () => {
    const output = document.getElementById('compile-output')!;
    output.textContent = 'Compiling...';

    try {
      const resp = await fetch('/tasks/compile', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      });
      if (!resp.ok) {
        const err = await resp.json();
        throw new Error(err.error || 'Compilation failed');
      }
      const result: { queue: any; warnings: string[] } = await resp.json();

      let out = '✅ Compilation successful\n\n';
      out += `Tasks created: ${result.queue.tasks?.length || 0}\n`;
      if (result.warnings.length > 0) {
        out += '\n⚠️ Warnings:\n' + result.warnings.map(w => '  - ' + w).join('\n');
      }
      output.textContent = out;
    } catch (err) {
      output.textContent = '❌ ' + (err as Error).message;
    }
  });
}