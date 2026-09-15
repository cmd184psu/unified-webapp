import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { api } from "../api";
import { IssueRow } from "../components/IssueRow";
import { IssueModal } from "../components/IssueModal";
import { STATE_LABELS, type Story } from "../types";

export function StoryDetailPage() {
  const { id } = useParams();
  const nav = useNavigate();
  const [story, setStory] = useState<Story | null>(null);
  const [adding, setAdding] = useState(false);

  const load = () => {
    if (id) api.getStory(id).then(setStory).catch(() => setStory(null));
  };
  useEffect(load, [id]);

  if (!story) return <div className="loading">Loading…</div>;

  return (
    <>
      <div className="topbar">
        <span className="btn ghost" onClick={() => nav("/stories")}>
          ←
        </span>
        <h1>🗂 {story.title}</h1>
        <span className="detail-ident">{story.identifier}</span>
        <div className="spacer" />
        <button className="btn primary" onClick={() => setAdding(true)}>
          + Add issue
        </button>
      </div>
      <div className="content">
        <div style={{ padding: "16px 18px", color: "var(--text-dim)" }}>
          <span className="detail-ident">{STATE_LABELS[story.state]}</span>
          {story.epicId && (
            <span
              style={{ marginLeft: 12, color: "var(--accent)", cursor: "pointer" }}
              onClick={() => nav(`/epics/${story.epicId}`)}
            >
              · In epic →
            </span>
          )}
          {story.description && (
            <div className="detail-desc" style={{ marginTop: 10 }}>
              {story.description}
            </div>
          )}
        </div>
        <div className="issue-group-header">
          Issues<span className="gcount">{story.issues?.length ?? 0}</span>
        </div>
        {(story.issues ?? []).length === 0 ? (
          <div className="empty">No issues in this story yet.</div>
        ) : (
          story.issues!.map((i) => <IssueRow key={i.id} issue={i} />)
        )}
      </div>
      {adding && (
        <IssueModal
          defaultStoryId={story.id}
          onClose={() => setAdding(false)}
          onSaved={load}
        />
      )}
    </>
  );
}
