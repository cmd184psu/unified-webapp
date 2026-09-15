import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import { useData } from "../DataContext";
import { Modal } from "../components/common";
import { STATE_LABELS, type Epic } from "../types";

export function EpicsPage() {
  const nav = useNavigate();
  const [epics, setEpics] = useState<Epic[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);

  const load = () => {
    setLoading(true);
    api.listEpics().then(setEpics).finally(() => setLoading(false));
  };
  useEffect(load, []);

  return (
    <>
      <div className="topbar">
        <h1>Epics</h1>
        <span style={{ color: "var(--text-faint)" }}>{epics.length}</span>
        <div className="spacer" />
        <button className="btn primary" onClick={() => setCreating(true)}>
          + New epic
        </button>
      </div>
      <div className="content">
        {loading ? (
          <div className="loading">Loading…</div>
        ) : epics.length === 0 ? (
          <div className="empty">No epics yet.</div>
        ) : (
          epics.map((e) => (
            <div
              key={e.id}
              className="folder-row"
              onClick={() => nav(`/epics/${e.id}`)}
            >
              <span className="ficon">📚</span>
              <div className="ftitle">
                <div>{e.title}</div>
                <div className="fsub">
                  {e.identifier} · {STATE_LABELS[e.state]}
                </div>
              </div>
              <span className="fsub">{e.storyCount} stories</span>
            </div>
          ))
        )}
      </div>
      {creating && <EpicModal onClose={() => setCreating(false)} onSaved={load} />}
    </>
  );
}

function EpicModal({
  onClose,
  onSaved,
}: {
  onClose: () => void;
  onSaved: () => void;
}) {
  const { teams } = useData();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [teamId, setTeamId] = useState(teams[0]?.id ?? "");

  const save = async () => {
    if (!title.trim()) return;
    await api.createEpic({ teamId, title, description });
    onSaved();
    onClose();
  };

  return (
    <Modal title="New epic" onClose={onClose}>
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
          Create epic
        </button>
      </div>
    </Modal>
  );
}
