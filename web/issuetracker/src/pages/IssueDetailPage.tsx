import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "../api";
import { useData } from "../DataContext";
import { IssueModal } from "../components/IssueModal";
import { Avatar, StateBadge, TagPill } from "../components/common";
import {
  RELATION_LABELS,
  RELATION_TYPES,
  STATES,
  STATE_LABELS,
  PRIORITY_LABELS,
  type Issue,
  type State,
} from "../types";

export function IssueDetailPage() {
  const { ident } = useParams();
  const nav = useNavigate();
  const { users, tags, userLabel } = useData();
  const [issue, setIssue] = useState<Issue | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [editing, setEditing] = useState(false);
  const [toast, setToast] = useState("");

  const load = () => {
    if (!ident) return;
    api
      .getIssueByIdentifier(ident)
      .then((i) => {
        setIssue(i);
        setNotFound(false);
      })
      .catch(() => setNotFound(true));
  };
  useEffect(load, [ident]);

  const patch = async (body: Partial<Issue> & { tagIds?: string[] }) => {
    if (!issue) return;
    const updated = await api.updateIssue(issue.id, body);
    setIssue(updated);
  };

  const flash = (msg: string) => {
    setToast(msg);
    setTimeout(() => setToast(""), 1800);
  };

  const copyLink = () => {
    navigator.clipboard.writeText(window.location.href);
    flash("Link copied to clipboard");
  };

  if (notFound) return <div className="empty">Issue “{ident}” not found.</div>;
  if (!issue) return <div className="loading">Loading…</div>;

  const toggleTag = (id: string) => {
    const cur = issue.tags.map((t) => t.id);
    const next = cur.includes(id)
      ? cur.filter((x) => x !== id)
      : [...cur, id];
    patch({ tagIds: next });
  };

  return (
    <>
      <div className="topbar">
        <span
          className="btn ghost"
          onClick={() => nav(-1)}
          style={{ cursor: "pointer" }}
        >
          ←
        </span>
        <h1>{issue.identifier}</h1>
        <div className="spacer" />
        <button className="btn" onClick={copyLink}>
          🔗 Copy link
        </button>
        <button className="btn" onClick={() => setEditing(true)}>
          Edit
        </button>
        <button
          className="btn danger"
          onClick={async () => {
            if (confirm("Delete this issue?")) {
              await api.deleteIssue(issue.id);
              nav("/issues");
            }
          }}
        >
          Delete
        </button>
      </div>

      <div className="detail">
        <div className="detail-main">
          <div className="detail-ident">
            {issue.teamKey} · {issue.kind}
          </div>
          <h2>{issue.title}</h2>
          {issue.description ? (
            <div className="detail-desc">{issue.description}</div>
          ) : (
            <div className="detail-desc" style={{ color: "var(--text-faint)" }}>
              No description.
            </div>
          )}

          <h3 style={{ marginTop: 32 }}>Relations</h3>
          <RelationsEditor issue={issue} onChange={load} />
        </div>

        <div className="detail-side">
          <div className="side-field">
            <label>State</label>
            <select
              value={issue.state}
              onChange={(e) => patch({ state: e.target.value as State })}
            >
              {STATES.map((s) => (
                <option key={s} value={s}>
                  {STATE_LABELS[s]}
                </option>
              ))}
            </select>
          </div>
          <div className="side-field">
            <label>Priority</label>
            <select
              value={issue.priority}
              onChange={(e) => patch({ priority: Number(e.target.value) })}
            >
              {PRIORITY_LABELS.map((p, i) => (
                <option key={i} value={i}>
                  {p}
                </option>
              ))}
            </select>
          </div>
          <div className="side-field">
            <label>Assignee</label>
            <select
              value={issue.assigneeId ?? ""}
              onChange={(e) => patch({ assigneeId: e.target.value })}
            >
              <option value="">Unassigned</option>
              {users.map((u) => (
                <option key={u.id} value={u.id}>
                  {userLabel(u)}
                </option>
              ))}
            </select>
          </div>
          <div className="side-field">
            <label>Reporter</label>
            <div className="row-wrap">
              <Avatar user={issue.reporter} />
              <span>{issue.reporter ? userLabel(issue.reporter) : "—"}</span>
            </div>
          </div>
          <div className="side-field">
            <label>Tags</label>
            <div className="row-wrap">
              {tags.map((t) => {
                const on = issue.tags.some((x) => x.id === t.id);
                return (
                  <span
                    key={t.id}
                    className={`chip-toggle ${on ? "" : "off"}`}
                    onClick={() => toggleTag(t.id)}
                  >
                    <TagPill tag={t} />
                  </span>
                );
              })}
            </div>
          </div>
          {issue.storyId && (
            <div className="side-field">
              <label>Story</label>
              <span
                style={{ color: "var(--accent)", cursor: "pointer" }}
                onClick={() => nav(`/stories/${issue.storyId}`)}
              >
                Open story →
              </span>
            </div>
          )}
        </div>
      </div>

      {editing && (
        <IssueModal
          initial={issue}
          onClose={() => setEditing(false)}
          onSaved={(i) => setIssue(i)}
        />
      )}
      {toast && <div className="toast">{toast}</div>}
    </>
  );
}

function RelationsEditor({
  issue,
  onChange,
}: {
  issue: Issue;
  onChange: () => void;
}) {
  const nav = useNavigate();
  const [adding, setAdding] = useState(false);
  const [type, setType] = useState("relates");
  const [target, setTarget] = useState("");
  const [options, setOptions] = useState<Issue[]>([]);

  useEffect(() => {
    if (adding) api.listIssues().then((all) =>
      setOptions(all.filter((i) => i.id !== issue.id))
    );
  }, [adding, issue.id]);

  const add = async () => {
    if (!target) return;
    await api.addRelation(issue.id, target, type);
    setAdding(false);
    setTarget("");
    onChange();
  };

  return (
    <div>
      {issue.relations.length === 0 && (
        <div style={{ color: "var(--text-faint)", padding: "6px 0" }}>
          No related issues.
        </div>
      )}
      {issue.relations.map((r) => (
        <div className="relation-row" key={r.id}>
          <span className="rtype">{RELATION_LABELS[r.type] ?? r.type}</span>
          <span
            style={{ color: "var(--text-faint)", cursor: "pointer" }}
            onClick={() => nav(`/issue/${r.identifier}`)}
          >
            {r.identifier}
          </span>
          <span style={{ flex: 1 }}>{r.title}</span>
          {r.state && <StateBadge state={r.state} />}
          <button
            className="btn ghost"
            onClick={async () => {
              await api.deleteRelation(r.id);
              onChange();
            }}
          >
            ✕
          </button>
        </div>
      ))}

      {adding ? (
        <div className="row-wrap" style={{ marginTop: 10 }}>
          <select value={type} onChange={(e) => setType(e.target.value)}>
            {RELATION_TYPES.map((t) => (
              <option key={t} value={t}>
                {RELATION_LABELS[t]}
              </option>
            ))}
          </select>
          <select
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            style={{ minWidth: 260 }}
          >
            <option value="">Select issue…</option>
            {options.map((o) => (
              <option key={o.id} value={o.id}>
                {o.identifier} · {o.title}
              </option>
            ))}
          </select>
          <button className="btn primary" onClick={add}>
            Add
          </button>
          <button className="btn ghost" onClick={() => setAdding(false)}>
            Cancel
          </button>
        </div>
      ) : (
        <button
          className="btn"
          style={{ marginTop: 10 }}
          onClick={() => setAdding(true)}
        >
          + Add relation
        </button>
      )}
    </div>
  );
}
