import { useState } from "react";

export function ApiPage() {
  const [copied, setCopied] = useState(false);
  const origin = window.location.origin;

  const copy = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  const curlExample = `curl -X POST ${origin}/graphql \\
  -H "Authorization: Bearer <api-key>" \\
  -H "Content-Type: application/json" \\
  -d '{"query":"query { issues { nodes { identifier title state { name } } } }"}'`;

  const box: React.CSSProperties = {
    background: "var(--bg-input)",
    border: "1px solid var(--border)",
    borderRadius: 6,
    padding: 12,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: 12,
    whiteSpace: "pre-wrap",
    wordBreak: "break-all",
    lineHeight: 1.6,
  };

  return (
    <>
      <div className="topbar">
        <h1>API</h1>
      </div>
      <div
        className="content"
        style={{ padding: 24, maxWidth: 760, lineHeight: 1.6 }}
      >
        <p style={{ color: "var(--text-dim)" }}>
          This module exposes a Linear-compatible GraphQL endpoint at{" "}
          <code>{origin}/graphql</code>. Access is controlled by the platform:
          a request must carry a platform API key, either as{" "}
          <code>Authorization: Bearer &lt;key&gt;</code> or{" "}
          <code>X-API-Key: &lt;key&gt;</code>. API keys are issued and managed
          outside this module — see the platform's admin settings. This is
          what zenflow / Linear API clients can point at.
        </p>

        <h3>Example request</h3>
        <div style={box}>{curlExample}</div>
        <button
          className="btn"
          style={{ marginTop: 10 }}
          onClick={() => copy(curlExample)}
        >
          {copied ? "Copied" : "Copy curl"}
        </button>

        <h3 style={{ marginTop: 24 }}>Supported operations</h3>
        <ul style={{ color: "var(--text-dim)" }}>
          <li>
            <code>viewer</code>, <code>teams</code>,{" "}
            <code>workflowStates</code>, <code>issueLabels</code>
          </li>
          <li>
            <code>issues(filter)</code> and <code>issue(id)</code> (id may be an
            internal id or a <code>TEAM-123</code> identifier)
          </li>
          <li>
            <code>issueCreate(input)</code> and{" "}
            <code>issueUpdate(id, input)</code>
          </li>
        </ul>

        <h3 style={{ marginTop: 24 }}>REST API</h3>
        <p style={{ color: "var(--text-dim)" }}>
          A plain REST API is also available under <code>{origin}/api</code>{" "}
          (e.g. <code>GET /api/issues?state=in_progress&amp;tagId=…</code>),
          subject to the same platform authentication.
        </p>
      </div>
    </>
  );
}
