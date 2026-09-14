import { useState } from "react";
import { api } from "../api";
import { useData } from "../DataContext";
import {
  STATES,
  STATE_LABELS,
  PRIORITY_LABELS,
  type Issue,
  type State,
} from "../types";
import { Modal, TagPill } from "./common";

interface Props {
  onClose: () => void;
  onSaved: (issue: Issue) => void;
  initial?: Issue;
  defaultState?: State;
  defaultStoryId?: string | null;
}

export function IssueModal({
  onClose,
  onSaved,
  initial,
  defaultState,
  defaultStoryId,
}: Props) {
  const { teams, users, tags, userLabel, me } = useData();
  // The reporter is fixed server-side: the current actor on create, and
  // immutable on edit. There is no client override in any mode.
  const reporterUser = initial ? initial.reporter : me;
  const [title, setTitle] = useState(initial?.title ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [teamId, setTeamId] = useState(initial?.teamId ?? teams[0]?.id ?? "");
  const [kind, setKind] = useState(initial?.kind ?? "feature");
  const [state, setState] = useState<State>(
    initial?.state ?? defaultState ?? "backlog"
  );
  const [priority, setPriority] = useState(initial?.priority ?? 0);
  const [assigneeId, setAssigneeId] = useState(initial?.assigneeId ?? "");
  const [tagIds, setTagIds] = useState<string[]>(
    initial?.tags.map((t) => t.id) ?? []
  );
  const [saving, setSaving] = useState(false);

  const toggleTag = (id: string) =>
    setTagIds((cur) =>
      cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id]
    );

  const save = async () => {
    if (!title.trim() || !teamId) return;
    setSaving(true);
    try {
      const body = {
        teamId,
        title,
        description,
        kind,
        state,
        priority,
        assigneeId: assigneeId || "",
        tagIds,
        storyId: initial ? undefined : defaultStoryId ?? undefined,
      };
      const saved = initial
        ? await api.updateIssue(initial.id, body)
        : await api.createIssue(body);
      onSaved(saved);
      onClose();
    } catch (e) {
      alert(String(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal title={initial ? "Edit issue" : "New issue"} onClose={onClose}>
      <div className="field">
        <label>Title</label>
        <input
          autoFocus
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Issue title"
        />
      </div>
      <div className="field">
        <label>Description</label>
        <textarea
          rows={5}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Add a description… (markdown ok)"
        />
      </div>
      <div style={{ display: "flex", gap: 12 }}>
        <div className="field" style={{ flex: 1 }}>
          <label>Team</label>
          <select value={teamId} onChange={(e) => setTeamId(e.target.value)}>
            {teams.map((t) => (
              <option key={t.id} value={t.id}>
                {t.key} · {t.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>Type</label>
          <select value={kind} onChange={(e) => setKind(e.target.value)}>
            <option value="feature">Feature</option>
            <option value="bug">Bug</option>
            <option value="chore">Chore</option>
          </select>
        </div>
      </div>
      <div style={{ display: "flex", gap: 12 }}>
        <div className="field" style={{ flex: 1 }}>
          <label>State</label>
          <select
            value={state}
            onChange={(e) => setState(e.target.value as State)}
          >
            {STATES.map((s) => (
              <option key={s} value={s}>
                {STATE_LABELS[s]}
              </option>
            ))}
          </select>
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>Priority</label>
          <select
            value={priority}
            onChange={(e) => setPriority(Number(e.target.value))}
          >
            {PRIORITY_LABELS.map((p, i) => (
              <option key={i} value={i}>
                {p}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div style={{ display: "flex", gap: 12 }}>
        <div className="field" style={{ flex: 1 }}>
          <label>Assignee</label>
          <select
            value={assigneeId}
            onChange={(e) => setAssigneeId(e.target.value)}
          >
            <option value="">Unassigned</option>
            {users.map((u) => (
              <option key={u.id} value={u.id}>
                {userLabel(u)}
              </option>
            ))}
          </select>
        </div>
        <div className="field" style={{ flex: 1 }}>
          <label>Reporter</label>
          <input
            className="readonly"
            readOnly
            value={reporterUser ? userLabel(reporterUser) : "(set on create)"}
            title="The reporter is set from the current actor and cannot be changed."
          />
        </div>
      </div>
      <div className="field">
        <label>Tags</label>
        <div className="row-wrap">
          {tags.map((t) => (
            <span
              key={t.id}
              className={`chip-toggle ${tagIds.includes(t.id) ? "" : "off"}`}
              onClick={() => toggleTag(t.id)}
            >
              <TagPill tag={t} />
            </span>
          ))}
          {tags.length === 0 && (
            <span style={{ color: "var(--text-faint)" }}>
              No tags yet — create some from the Tags page.
            </span>
          )}
        </div>
      </div>
      <div className="actions">
        <button className="btn ghost" onClick={onClose}>
          Cancel
        </button>
        <button className="btn primary" onClick={save} disabled={saving}>
          {saving ? "Saving…" : initial ? "Save changes" : "Create issue"}
        </button>
      </div>
    </Modal>
  );
}
