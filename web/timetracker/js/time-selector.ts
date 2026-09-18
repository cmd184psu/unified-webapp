import './time-selector.css';

interface TimeSelectorOptions {
  onChange?: (info: { count: number; totalMinutes: number; slots: TimeSlot[] }) => void;
}

interface TimeSlot {
  hour: number;
  quarter: number;
}

const SVG_COPY = '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';

export default class TimeSelector {
  private _rawContainer: string | Element;
  private onChange: TimeSelectorOptions['onChange'];
  private selectedBlocks: HTMLElement[] = [];
  private isDragging = false;
  private selector?: string;
  private container?: Element;
  private table?: HTMLElement;
  private timeBlocks: HTMLElement[] = [];
  private totalTimeInput?: HTMLInputElement;
  private clearBtn?: HTMLElement;
  private copyBtn?: HTMLElement;
  private _domReadyHandler?: () => void;

  constructor(container: string | Element, options: TimeSelectorOptions = {}) {
    this._rawContainer = container;
    this.onChange = options.onChange || undefined;

    if (container instanceof Element) {
      this.container = container;
      this._init();
      return;
    }

    if (typeof container === 'string') {
      this.selector = container.startsWith('#') ? container : `#${container}`;
      this.container = document.querySelector(this.selector) ?? undefined;

      if (this.container) {
        this._init();
        return;
      }

      this._domReadyHandler = () => {
        this.container = document.querySelector(this.selector!) ?? undefined;
        if (this.container) {
          this._init();
          document.removeEventListener('DOMContentLoaded', this._domReadyHandler!);
        } else {
          console.error(`TimeSelector: container ${this.selector} not found after DOMContentLoaded.`);
        }
      };
      document.addEventListener('DOMContentLoaded', this._domReadyHandler);
      return;
    }

    console.error('TimeSelector: invalid container (must be DOM element or selector string).');
  }

  initManual(): void {
    if (!this.container && typeof this.selector === 'string') {
      this.container = document.querySelector(this.selector) ?? undefined;
    }
    if (this.container) this._init();
    else console.error('TimeSelector: initManual failed — container not found.');
  }

  private _init(): void {
    if (!this.container) {
      console.error('TimeSelector: container not set in _init().');
      return;
    }
    this._render();
    this._attachEvents();
    this._updateTotalTime();
  }

  private _render(): void {
    const html = this._generateTimeTable();
    this.container!.innerHTML = `
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
          ${SVG_COPY}
        </button>
      </div>
    `;

    this.table = this.container!.querySelector('#timeSelectorTable') as HTMLElement;
    this.timeBlocks = Array.from(this.container!.querySelectorAll('.time-block'));
    this.totalTimeInput = this.container!.querySelector('#totalTimeInput') as HTMLInputElement;
    this.clearBtn = this.container!.querySelector('#clearTimeSelectionBtn') as HTMLElement;
    this.copyBtn = this.container!.querySelector('#copyTimeSelectionBtn') as HTMLElement;
  }

  private _generateTimeTable(): string {
    let html = '';
    for (let hour = 8; hour <= 22; hour++) {
      const displayHour = hour > 12 ? hour - 12 : hour;
      const ampm = hour >= 12 ? 'PM' : 'AM';
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

  private _attachEvents(): void {
    this.timeBlocks.forEach(block => {
      block.addEventListener('mousedown', e => this._startSelection(e, block));
      block.addEventListener('mouseenter', _e => this._dragSelection(block));
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

    document.addEventListener('mouseup', () => {
      if (this.isDragging) this._endSelection();
    });
  }

  private _startSelection(event: MouseEvent, block: HTMLElement): void {
    event.preventDefault();
    this.isDragging = true;
    this._toggleBlock(block);
    this._notifyChange();
  }

  private _dragSelection(block: HTMLElement): void {
    if (this.isDragging) {
      if (!block.classList.contains('selected')) {
        block.classList.add('selected');
        this.selectedBlocks.push(block);
      }
      this._updateTotalTime();
      this._notifyChange();
    }
  }

  private _endSelection(): void {
    this.isDragging = false;
    this._updateTotalTime();
    this._notifyChange();
  }

  private _toggleBlock(block: HTMLElement): void {
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

  private _updateTotalTime(): void {
    const totalMinutes = this.selectedBlocks.length * 15;
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    const text = `${hours}h ${minutes}m`;
    if (this.totalTimeInput) this.totalTimeInput.value = text;
  }

  private _notifyChange(): void {
    if (typeof this.onChange === 'function') {
      const slots = this.getSelectedTimeBlocks();
      this.onChange({
        count: slots.length,
        totalMinutes: slots.length * 15,
        slots
      });
    }
  }

  setSelection(slots: TimeSlot[], notify = false): void {
    this.selectedBlocks.forEach(b => {
      b.classList.remove('selected');
      b.setAttribute('aria-pressed', 'false');
    });
    this.selectedBlocks = [];
    (slots || []).forEach(({ hour, quarter }) => {
      const block = this.container!.querySelector(
        `.time-block[data-hour="${hour}"][data-quarter="${quarter}"]`) as HTMLElement | null;
      if (block) {
        block.classList.add('selected');
        block.setAttribute('aria-pressed', 'true');
        this.selectedBlocks.push(block);
      }
    });
    this._updateTotalTime();
    if (notify) this._notifyChange();
  }

  clearSelection(): void {
    this.selectedBlocks.forEach(b => {
      b.classList.remove('selected');
      b.setAttribute('aria-pressed', 'false');
    });
    this.selectedBlocks = [];
    this._updateTotalTime();
    this._notifyChange();
  }

  copySelection(): void {
    const text = this.totalTimeInput?.value || '';
    if (navigator.clipboard && text) {
      navigator.clipboard.writeText(text).catch(err => {
        console.warn('TimeSelector: copy failed', err);
      });
    }
  }

  getSelectedTimeBlocks(): TimeSlot[] {
    return this.selectedBlocks.map(b => ({
      hour: parseInt(b.dataset.hour!, 10),
      quarter: parseInt(b.dataset.quarter!, 10)
    }));
  }
}
