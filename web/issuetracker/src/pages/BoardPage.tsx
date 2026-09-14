import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import { useData } from "../DataContext";
import { IssueModal } from "../components/IssueModal";
import { Avatar, TagPill } from "../components/common";
import {
  STATES,
  STATE_COLORS,
  STATE_LABELS,
  type Issue,
  type State,
} from "../types";

export function BoardPage({
  mine = false,
  title = "Board",
}: {
  mine?: boolean;
  title?: string;
}) {
  const { tags } = useData();
  const [issues, setIssues] = useState<Issue[]>([]);
  const [loading, setLoading] = useState(true);
  const [tagFilter, setTagFilter] = useState("");
  const [dragId, setDragId] = useState<string | null>(null);
  const [dropState, setDropState] = useState<State | null>(null);
  const [newFor, setNewFor] = useState<State | null>(null);

  const load = () => {
    setLoading(true);
    api
      .listIssues({ tagId: tagFilter, assignee: mine ? "me" : undefined })
      .then(setIssues)
      .finally(() => setLoading(false));
  };
  useEffect(load, [tagFilter, mine]);

  const byState = useMemo(() => {
    const m = new Map<State, Issue[]>();
    STATES.forEach((s) => m.set(s, []));
    for (const i of issues) m.get(i.state)?.push(i);
    return m;
  }, [issues]);

  const onDrop = async (state: State) => {
    setDropState(null);
    const id = dragId;
    setDragId(null);
    if (!id) return;
    const issue = issues.find((i) => i.id === id);
    if (!issue || issue.state === state) return;
    // optimistic update
    setIssues((cur) =>
      cur.map((i) => (i.id === id ? { ...i, state } : i))
    );
    try {
      await api.updateIssue(id, { state });
    } catch {
      load();
    }
  };

  return (
    <>
      <div className="topbar">
        <h1>{title}</h1>
        <div className="spacer" />
        <select value={tagFilter} onChange={(e) => setTagFilter(e.target.value)}>
          <option value="">All tags</option>
          {tags.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
      </div>

      {loading ? (
        <div className="loading">Loading…</div>
      ) : (
        <div className="board">
          {STATES.map((s) => {
            const list = byState.get(s) ?? [];
            return (
              <div
                key={s}
                className={`board-col ${dropState === s ? "drop" : ""}`}
                onDragOver={(e) => {
                  e.preventDefault();
                  setDropState(s);
                }}
                onDragLeave={() =>
                  setDropState((cur) => (cur === s ? null : cur))
                }
                onDrop={() => onDrop(s)}
              >
                <div className="board-col-header">
                  <span
                    className="ring state"
                    style={{
                      color: STATE_COLORS[s],
                      width: 12,
                      height: 12,
                      borderRadius: "50%",
                      border: `2px solid ${STATE_COLORS[s]}`,
                    }}
                  />
                  {STATE_LABELS[s]}
                  <span className="gcount">{list.length}</span>
                </div>
                <div className="board-col-body">
                  {list.map((i) => (
                    <BoardCard
                      key={i.id}
                      issue={i}
                      dragging={dragId === i.id}
                      onDragStart={() => setDragId(i.id)}
                      onDragEnd={() => setDragId(null)}
                    />
                  ))}
                  <button
                    className="btn ghost"
                    style={{ justifyContent: "flex-start" }}
                    onClick={() => setNewFor(s)}
                  >
                    + Add
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {newFor && (
        <IssueModal
          defaultState={newFor}
          onClose={() => setNewFor(null)}
          onSaved={() => load()}
        />
      )}
    </>
  );
}

function BoardCard({
  issue,
  dragging,
  onDragStart,
  onDragEnd,
}: {
  issue: Issue;
  dragging: boolean;
  onDragStart: () => void;
  onDragEnd: () => void;
}) {
  const nav = useNavigate();
  return (
    <div
      className={`card ${dragging ? "dragging" : ""}`}
      draggable
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onClick={() => nav(`/issue/${issue.identifier}`)}
    >
      <div className="card-top">
        <span>{issue.identifier}</span>
      </div>
      <div className="card-title">{issue.title}</div>
      {issue.tags.length > 0 && (
        <div className="card-tags">
          {issue.tags.map((t) => (
            <TagPill key={t.id} tag={t} />
          ))}
        </div>
      )}
      <div className="card-bottom">
        <Avatar user={issue.assignee} />
      </div>
    </div>
  );
}
