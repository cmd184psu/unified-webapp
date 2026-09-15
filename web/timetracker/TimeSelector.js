// TimeSelector.js
export default class TimeSelector {
  /**
   * container: element or selector string (e.g. 'timeSelectorContainer' or '#timeSelectorContainer')
   * options: { onChange: fn }
   */
  constructor(container, options = {}) {
    this._rawContainer = container; // keep original param
    this.onChange = options.onChange || null;
    this.selectedBlocks = [];
    this.isDragging = false;

    // If user passed an Element directly, use it and init immediately.
    if (container instanceof Element) {
      this.container = container;
      this._init();
      return;
    }

    // If user passed an id without '#', normalize to an id selector
    if (typeof container === 'string') {
      // Accept 'timeSelectorContainer' or '#timeSelectorContainer'
      this.selector = container.startsWith('#') ? container : `#${container}`;
      this.container = document.querySelector(this.selector);

      if (this.container) {
        this._init();
        return;
      }

      // If container isn't present yet, wait for DOM ready and try init then.
      this._domReadyHandler = () => {
        this.container = document.querySelector(this.selector);
        if (this.container) {
          this._init();
          document.removeEventListener('DOMContentLoaded', this._domReadyHandler);
        } else {
          // still not found — log helpful error
          console.error(`TimeSelector: container ${this.selector} not found after DOMContentLoaded.`);
        }
      };
      document.addEventListener('DOMContentLoaded', this._domReadyHandler);
      return;
    }

    // Invalid container param
    console.error('TimeSelector: invalid container (must be DOM element or selector string).');
  }

  // Public: call init manually if you constructed early and want to control timing.
  initManual() {
    if (!this.container && typeof this.selector === 'string') {
      this.container = document.querySelector(this.selector);
    }
    if (this.container) this._init();
    else console.error('TimeSelector: initManual failed — container not found.');
  }

  _init() {
    if (!this.container) {
      console.error('TimeSelector: container not set in _init().');
      return;
    }
    this._render();
    this._attachEvents();
    // initial total update
    this._updateTotalTime();
  }

  _render() {
    const html = this._generateTimeTable();
    this.container.innerHTML = `
      <table id="timeSelectorTable" class="time-selector-table" aria-label="time selector">
        ${html}
      </table>
      <div class="form-group">
        <label for="totalTimeInput">Total Time:</label>
        <input type="text" id="totalTimeInput" readonly>
      </div>
      <div class="time-buttons">
        <button type="button" id="clearTimeSelectionBtn">Clear</button>
        <button type="button" id="copyTimeSelectionBtn" title="Copy total time">
          <i class="fas fa-copy" aria-hidden="true"></i>
        </button>
      </div>
    `;

    this.table = this.container.querySelector('#timeSelectorTable');
    this.timeBlocks = Array.from(this.container.querySelectorAll('.time-block'));
    this.totalTimeInput = this.container.querySelector('#totalTimeInput');
    this.clearBtn = this.container.querySelector('#clearTimeSelectionBtn');
    this.copyBtn = this.container.querySelector('#copyTimeSelectionBtn');
  }

  _generateTimeTable() {
    // Build from 8:00 to 22:45 as in your original (4 quarters per hour)
    let html = '';
    for (let hour = 8; hour <= 22; hour++) {
      const displayHour = hour > 12 ? hour - 12 : hour;
      const ampm = hour >= 12 ? 'PM' : 'AM';
      // create 4 rows per hour, put the hour label in first row with rowspan=4
      for (let quarter = 0; quarter < 4; quarter++) {
        html += '<tr>';
        if (quarter === 0) {
          html += `<th rowspan="4" scope="row">${displayHour} ${ampm}</th>`;
        }
        html += `<td class="time-block" data-hour="${hour}" data-quarter="${quarter}" tabindex="0" role="button" aria-pressed="false"></td>`;
        html += '</tr>';
      }
    }
    return html;
  }

  _attachEvents() {
    this.timeBlocks.forEach(block => {
      // mouse
      block.addEventListener('mousedown', e => this._startSelection(e, block));
      block.addEventListener('mouseenter', e => this._dragSelection(e, block));
      // keyboard accessibility: space/enter to toggle
      block.addEventListener('keydown', e => {
        if (e.key === ' ' || e.key === 'Enter') {
          e.preventDefault();
          this._toggleBlock(block);
          this._notifyChange();
        }
      });
      block.addEventListener('mouseup', () => this._endSelection());
    });

    this.clearBtn?.addEventListener('click', () => this.clearSelection());
    this.copyBtn?.addEventListener('click', () => this.copySelection());

    // global mouseup to end dragging if user releases outside table —
    // routed through _endSelection so onChange still fires for that drag
    document.addEventListener('mouseup', () => {
      if (this.isDragging) this._endSelection();
    });
  }

  _startSelection(event, block) {
    event.preventDefault();
    this.isDragging = true;
    this._toggleBlock(block);
    this._notifyChange();
  }

  _dragSelection(event, block) {
    if (this.isDragging) {
      // Only toggle when entering if not already selected (so dragging selects many)
      if (!block.classList.contains('selected')) {
        block.classList.add('selected');
        this.selectedBlocks.push(block);
      }
      this._updateTotalTime();
      this._notifyChange();
    }
  }

  _endSelection() {
    this.isDragging = false;
    this._updateTotalTime();
    this._notifyChange();
  }

  _toggleBlock(block) {
    const idx = this.selectedBlocks.indexOf(block);
    if (idx === -1) {
      block.classList.add('selected');
      block.setAttribute('aria-pressed', 'true');
      this.selectedBlocks.push(block);
    } else {
      block.classList.remove('selected');
      block.setAttribute('aria-pressed', 'false');
      this.selectedBlocks.splice(idx, 1);
    }
    this._updateTotalTime();
  }

  _updateTotalTime() {
    const totalMinutes = this.selectedBlocks.length * 15;
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    const text = `${hours}h ${minutes}m`;
    if (this.totalTimeInput) this.totalTimeInput.value = text;
  }

  _notifyChange() {
    if (typeof this.onChange === 'function') {
      const slots = this.getSelectedTimeBlocks();
      this.onChange({
        count: slots.length,
        totalMinutes: slots.length * 15,
        slots
      });
    }
  }

  // Replace the current selection with the given {hour, quarter} slots.
  // Silent by default so restoring a saved report doesn't re-trigger the
  // auto-save/refresh hooks that user interaction fires.
  setSelection(slots, notify = false) {
    this.selectedBlocks.forEach(b => {
      b.classList.remove('selected');
      b.setAttribute('aria-pressed', 'false');
    });
    this.selectedBlocks = [];
    (slots || []).forEach(({ hour, quarter }) => {
      const block = this.container.querySelector(
        `.time-block[data-hour="${hour}"][data-quarter="${quarter}"]`);
      if (block) {
        block.classList.add('selected');
        block.setAttribute('aria-pressed', 'true');
        this.selectedBlocks.push(block);
      }
    });
    this._updateTotalTime();
    if (notify) this._notifyChange();
  }

  clearSelection() {
    this.selectedBlocks.forEach(b => {
      b.classList.remove('selected');
      b.setAttribute('aria-pressed', 'false');
    });
    this.selectedBlocks = [];
    this._updateTotalTime();
    this._notifyChange();
  }

  copySelection() {
    const text = this.totalTimeInput?.value || '';
    if (navigator.clipboard && text) {
      navigator.clipboard.writeText(text).catch(err => {
        console.warn('TimeSelector: copy failed', err);
      });
    }
  }

  getSelectedTimeBlocks() {
    return this.selectedBlocks.map(b => ({
      hour: parseInt(b.dataset.hour, 10),
      quarter: parseInt(b.dataset.quarter, 10)
    }));
  }
}
