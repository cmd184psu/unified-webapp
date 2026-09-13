export function renderOutput(container: HTMLElement, execID: string): void {
  container.textContent = '';

  const h1 = document.createElement('h1');
  h1.textContent = 'Output — execution ' + execID;
  container.appendChild(h1);

  const statusDiv = document.createElement('div');
  statusDiv.style.marginBottom = '12px';
  container.appendChild(statusDiv);

  const pre = document.createElement('pre');
  pre.className = 'output-terminal';
  container.appendChild(pre);

  function appendLine(parsed: { stream: string; line: string }): void {
    const span = document.createElement('span');
    span.className = 'stream-' + parsed.stream;
    span.textContent = parsed.line + '\n';
    pre.appendChild(span);
    pre.scrollTop = pre.scrollHeight;
  }

  function showStatus(status: string): void {
    statusDiv.textContent = 'Status: ' + status;
  }

  function markDone(): void {
    const done = document.createElement('div');
    done.style.color = 'var(--text-muted)';
    done.style.marginTop = '8px';
    done.style.fontSize = '12px';
    done.textContent = '— execution complete —';
    container.appendChild(done);
  }

  const es = new EventSource('/api/executions/' + execID + '/output');

  es.addEventListener('output', (e) => {
    try {
      appendLine(JSON.parse((e as MessageEvent).data) as { stream: string; line: string });
    } catch { /* ignore malformed */ }
  });

  es.addEventListener('status', (e) => {
    showStatus((e as MessageEvent).data as string);
  });

  es.addEventListener('done', () => {
    es.close();
    markDone();
  });

  es.onerror = () => {
    es.close();
    const msg = document.createElement('div');
    msg.className = 'error-banner';
    msg.textContent = 'Connection lost.';
    container.appendChild(msg);
  };

  // Close the stream when the user navigates away.
  window.addEventListener('hashchange', () => es.close(), { once: true });
}
