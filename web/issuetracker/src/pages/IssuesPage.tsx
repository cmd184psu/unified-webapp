import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { useData } from "../DataContext";
import { IssueRow } from "../components/IssueRow";
import { IssueModal } from "../components/IssueModal";
import {
  STATES,
  STATE_LABELS,
  type Issue,
  type State,
} from "../types";

export function IssuesPage({
  mine = false,
  title = "Issues",
}: {
  mine?: boolean;
  title?: string;
}) {
  const { tags } = useData();
  const [issues, setIssues] = useState<Issue[]>([]);
  const [loading, setLoading] = useState(true);
  const [stateFilter, setStateFilter] = useState<string>("");
  const [tagFilter, setTagFilter] = useState<string>("");
  const [search, setSearch] = useState("");
  const [showModal, setShowModal] = useState(false);

  const load = () => {
    setLoading(true);
    api
      .listIssues({
        state: stateFilter,
        tagId: tagFilter,
        q: search,
        assignee: mine ? "me" : undefined,
      })
      .then(setIssues)
      .finally(() => setLoading(false));
  };

  useEffect(load, [stateFilter, tagFilter, search, mine]);

  const grouped = useMemo(() => {
    const m = new Map<State, Issue[]>();
    STATES.forEach((s) => m.set(s, []));
    for (const i of issues) m.get(i.state)?.push(i);
    return m;
  }, [issues]);

  return (
    <>
      <div className="topbar">
        <h1>{title}</h1>
        <span style={{ color: "var(--text-faint)" }}>{issues.length}</span>
        <div className="spacer" />
        <button className="btn primary" onClick={() => setShowModal(true)}>
          + New issue
        </button>
      </div>

      <div className="filterbar">
        <input
          className="search"
          placeholder="Search issues…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          value={stateFilter}
          onChange={(e) => setStateFilter(e.target.value)}
        >
          <option value="">All states</option>
          {STATES.map((s) => (
            <option key={s} value={s}>
              {STATE_LABELS[s]}
            </option>
          ))}
        </select>
        <select value={tagFilter} onChange={(e) => setTagFilter(e.target.value)}>
          <option value="">All tags</option>
          {tags.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
        {(stateFilter || tagFilter || search) && (
          <button
            className="btn ghost"
            onClick={() => {
              setStateFilter("");
              setTagFilter("");
              setSearch("");
            }}
          >
            Clear
          </button>
        )}
      </div>

      <div className="content">
        {loading ? (
          <div className="loading">Loading…</div>
        ) : issues.length === 0 ? (
          <div className="empty">No issues match these filters.</div>
        ) : (
          STATES.map((s) => {
            const list = grouped.get(s) ?? [];
            if (list.length === 0) return null;
            return (
              <div key={s}>
                <div className="issue-group-header">
                  {STATE_LABELS[s]}
                  <span className="gcount">{list.length}</span>
                </div>
                {list.map((i) => (
                  <IssueRow key={i.id} issue={i} />
                ))}
              </div>
            );
          })
        )}
      </div>

      {showModal && (
        <IssueModal
          onClose={() => setShowModal(false)}
          onSaved={() => load()}
        />
      )}
    </>
  );
}
