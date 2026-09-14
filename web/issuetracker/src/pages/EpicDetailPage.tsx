import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "../api";
import { StoryModal } from "./StoriesPage";
import { STATE_LABELS, type Epic } from "../types";

export function EpicDetailPage() {
  const { id } = useParams();
  const nav = useNavigate();
  const [epic, setEpic] = useState<Epic | null>(null);
  const [adding, setAdding] = useState(false);

  const load = () => {
    if (id) api.getEpic(id).then(setEpic).catch(() => setEpic(null));
  };
  useEffect(load, [id]);

  if (!epic) return <div className="loading">Loading…</div>;

  return (
    <>
      <div className="topbar">
        <span className="btn ghost" onClick={() => nav("/epics")}>
          ←
        </span>
        <h1>📚 {epic.title}</h1>
        <span className="detail-ident">{epic.identifier}</span>
        <div className="spacer" />
        <button className="btn primary" onClick={() => setAdding(true)}>
          + Add story
        </button>
      </div>
      <div className="content">
        <div style={{ padding: "16px 18px", color: "var(--text-dim)" }}>
          <span className="detail-ident">{STATE_LABELS[epic.state]}</span>
          {epic.description && (
            <div className="detail-desc" style={{ marginTop: 10 }}>
              {epic.description}
            </div>
          )}
        </div>
        <div className="issue-group-header">
          Stories<span className="gcount">{epic.stories?.length ?? 0}</span>
        </div>
        {(epic.stories ?? []).length === 0 ? (
          <div className="empty">No stories in this epic yet.</div>
        ) : (
          epic.stories!.map((s) => (
            <div
              key={s.id}
              className="folder-row"
              onClick={() => nav(`/stories/${s.id}`)}
            >
              <span className="ficon">🗂</span>
              <div className="ftitle">
                <div>{s.title}</div>
                <div className="fsub">
                  {s.identifier} · {STATE_LABELS[s.state]}
                </div>
              </div>
              <span className="fsub">{s.issueCount} issues</span>
            </div>
          ))
        )}
      </div>
      {adding && (
        <StoryModal
          epicId={epic.id}
          onClose={() => setAdding(false)}
          onSaved={load}
        />
      )}
    </>
  );
}
