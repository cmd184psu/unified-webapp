// queue-dashboard.ts — Queue tab initialization (re-exports from main.ts)

import { loadConsole, renderProgress, renderTaskList, renderDetail, wireActions, postAction } from './main.js';

// Re-export for main.ts
export { loadConsole, renderProgress, renderTaskList, renderDetail, wireActions, postAction };

export function initQueueDashboard(): void {
  // The queue dashboard is initialized in main.ts bootstrap
  // This module just ensures the queue tab panel exists
  const panel = document.getElementById('panel-queue');
  if (panel && !panel.querySelector('#task-list')) {
    panel.innerHTML = `
      <div class="console-header" id="console-header">
        <div class="progress-bar-wrap">
          <div class="progress-bar" id="progress-fill"></div>
        </div>
        <span class="progress-pct" id="progress-pct">0%</span>
      </div>
      <div class="console-body">
        <div class="console-left">
          <h2 class="pane-title">Queue</h2>
          <div id="task-list"></div>
        </div>
        <aside class="console-right" id="detail-pane" hidden>
          <div class="detail-body">
            <div class="detail-header">
              <h3 id="detail-title"></h3>
              <span class="status-chip" id="detail-status"></span>
              <button class="icon-btn detail-close-btn" id="detail-close" title="Clear selection">
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M2 2l10 10M12 2L2 12"/></svg>
              </button>
            </div>
            <div class="detail-section">
              <h4>Status</h4>
              <span class="status-chip" id="detail-status-repeated"></span>
              <span class="detail-attempts" id="detail-attempts"></span>
            </div>
            <div class="detail-section" data-panid="actions">
              <h4>Actions</h4>
              <button class="action-btn ui-modal-btn ui-modal-btn-primary" id="action-approve" type="button">Approve</button>
              <button class="action-btn ui-modal-btn" id="action-repair" type="button">Repair</button>
              <button class="action-btn ui-modal-btn" id="action-fail" type="button">Fail</button>
              <button class="action-btn ui-modal-btn" id="action-run-next" type="button">Run next</button>
            </div>
            <div class="detail-section" data-panid="prompt">
              <h4>Prompt</h4>
              <pre class="detail-mono"><code id="detail-prompt"></code></pre>
            </div>
            <div class="detail-section" data-panid="acceptance">
              <h4>Acceptance Criteria</h4>
              <pre class="detail-mono"><code id="detail-acceptance"></code></pre>
            </div>
            <div class="detail-section" data-panid="verify">
              <h4>Verify Command</h4>
              <pre class="detail-mono"><code id="detail-verify"></code></pre>
            </div>
            <div class="detail-section" data-panid="last-review">
              <h4>Last Review</h4>
              <span class="status-chip" id="detail-verdict"></span>
              <p id="detail-review-summary"></p>
            </div>
          </div>
        </aside>
        <aside class="console-right" id="stream-panel" hidden></aside>
      </div>
    `;
  }
}