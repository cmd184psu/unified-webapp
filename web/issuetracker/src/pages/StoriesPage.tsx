import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import { useData } from "../DataContext";
import { Modal } from "../components/common";
import { STATE_LABELS, type Story } from "../types";

export function StoriesPage() {
  const nav = useNavigate();
  const [stories, setStories] = useState<Story[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);

  const load = () => {
    setLoading(true);
    api.listStories().then(setStories).finally(() => setLoading(false));
  };
  useEffect(load, []);

  return (
    <>
      <div className="topbar">
        <h1>Stories</h1>
        <span style={{ color: "var(--text-faint)" }}>{stories.length}</span>
        <div className="spacer" />
        <button className="btn primary" onClick={() => setCreating(true)}>
          + New story
        </button>
      </div>
      <div className="content">
        {loading ? (
          <div className="loading">Loading…</div>
        ) : stories.length === 0 ? (
          <div className="empty">No stories yet.</div>
        ) : (
          stories.map((s) => (
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
      {creating && (
        <StoryModal onClose={() => setCreating(false)} onSaved={load} />
      )}
    </>
  );
}

export function StoryModal({
  onClose,
  onSaved,
  epicId,
}: {
  onClose: () => void;
  onSaved: () => void;
  epicId?: string;
}) {
  const { teams } = useData();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [teamId, setTeamId] = useState(teams[0]?.id ?? "");

  const save = async () => {
    if (!title.trim()) return;
    await api.createStory({ teamId, title, description, epicId });
    onSaved();
    onClose();
  };

  return (
    <Modal title="New story" onClose={onClose}>
      <div className="field">
        <label>Title</label>
        <input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
      </div>
      <div className="field">
        <label>Description</label>
        <textarea
          rows={4}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </div>
      <div className="field">
        <label>Team</label>
        <select value={teamId} onChange={(e) => setTeamId(e.target.value)}>
          {teams.map((t) => (
            <option key={t.id} value={t.id}>
              {t.key} · {t.name}
            </option>
          ))}
        </select>
      </div>
      <div className="actions">
        <button className="btn ghost" onClick={onClose}>
          Cancel
        </button>
        <button className="btn primary" onClick={save}>
          Create story
        </button>
      </div>
    </Modal>
  );
}
