import { useState } from "react";
import { api } from "../api";
import { useData } from "../DataContext";
import type { Team } from "../types";

const PALETTE = [
  "#eb5757",
  "#f2994a",
  "#f2c94c",
  "#5cb85c",
  "#26a69a",
  "#5e6ad2",
  "#bb6bd9",
  "#ec4899",
  "#95a2b3",
];

function ColorDots({
  color,
  onPick,
}: {
  color: string;
  onPick: (c: string) => void;
}) {
  return (
    <div style={{ display: "flex", gap: 6 }}>
      {PALETTE.map((c) => (
        <span
          key={c}
          onClick={() => onPick(c)}
          style={{
            width: 22,
            height: 22,
            borderRadius: "50%",
            background: c,
            cursor: "pointer",
            border: color === c ? "2px solid white" : "2px solid transparent",
          }}
        />
      ))}
    </div>
  );
}

export function ProjectsPage() {
  const { teams, reloadTeams } = useData();
  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [color, setColor] = useState(PALETTE[5]);
  const [error, setError] = useState("");
  const [editId, setEditId] = useState<string | null>(null);

  const add = async () => {
    setError("");
    if (!name.trim() || !key.trim()) {
      setError("Both a name and a 3–4 letter prefix are required.");
      return;
    }
    try {
      await api.createTeam({ name: name.trim(), key: key.trim(), color });
      setName("");
      setKey("");
      await reloadTeams();
    } catch (e) {
      setError(String(e));
    }
  };

  const remove = async (t: Team) => {
    if (
      confirm(
        `Delete project "${t.name}" (${t.key})? This removes all of its issues, stories and epics.`
      )
    ) {
      await api.deleteTeam(t.id);
      await reloadTeams();
    }
  };

  return (
    <>
      <div className="topbar">
        <h1>Projects</h1>
      </div>
      <div className="content" style={{ padding: 18, maxWidth: 720 }}>
        <div
          style={{
            display: "flex",
            gap: 8,
            alignItems: "center",
            marginBottom: 8,
            flexWrap: "wrap",
          }}
        >
          <input
            placeholder="Project name (e.g. Engineering Tools)"
            value={name}
            onChange={(e) => setName(e.target.value)}
            style={{ flex: 1, minWidth: 220 }}
          />
          <input
            placeholder="PREFIX"
            value={key}
            maxLength={4}
            onChange={(e) => setKey(e.target.value.toUpperCase())}
            onKeyDown={(e) => e.key === "Enter" && add()}
            style={{ width: 110, textTransform: "uppercase" }}
          />
          <ColorDots color={color} onPick={setColor} />
          <button className="btn primary" onClick={add}>
            Add project
          </button>
        </div>
        {error && (
          <div className="login-error" style={{ marginBottom: 12 }}>
            {error}
          </div>
        )}

        {teams.length === 0 ? (
          <div className="empty">No projects yet.</div>
        ) : (
          teams.map((t) =>
            editId === t.id ? (
              <ProjectEditRow
                key={t.id}
                team={t}
                onDone={async () => {
                  setEditId(null);
                  await reloadTeams();
                }}
                onCancel={() => setEditId(null)}
              />
            ) : (
              <div
                key={t.id}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 12,
                  padding: "12px 0",
                  borderBottom: "1px solid var(--border)",
                }}
              >
                <span
                  className="tag"
                  style={{
                    background: t.color + "22",
                    borderColor: t.color + "55",
                    color: t.color,
                    fontWeight: 600,
                  }}
                >
                  {t.key}
                </span>
                <span style={{ flex: 1 }}>{t.name}</span>
                <button className="btn ghost" onClick={() => setEditId(t.id)}>
                  Edit
                </button>
                <button
                  className="btn ghost danger"
                  onClick={() => remove(t)}
                >
                  Delete
                </button>
              </div>
            )
          )
        )}
      </div>
    </>
  );
}

function ProjectEditRow({
  team,
  onDone,
  onCancel,
}: {
  team: Team;
  onDone: () => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(team.name);
  const [key, setKey] = useState(team.key);
  const [color, setColor] = useState(team.color);
  const [error, setError] = useState("");

  const save = async () => {
    setError("");
    try {
      await api.updateTeam(team.id, { name, key, color });
      onDone();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <div
      style={{
        display: "flex",
        gap: 8,
        alignItems: "center",
        padding: "12px 0",
        borderBottom: "1px solid var(--border)",
        flexWrap: "wrap",
      }}
    >
      <input
        value={name}
        onChange={(e) => setName(e.target.value)}
        style={{ flex: 1, minWidth: 200 }}
      />
      <input
        value={key}
        maxLength={4}
        onChange={(e) => setKey(e.target.value.toUpperCase())}
        style={{ width: 110, textTransform: "uppercase" }}
      />
      <ColorDots color={color} onPick={setColor} />
      <button className="btn primary" onClick={save}>
        Save
      </button>
      <button className="btn ghost" onClick={onCancel}>
        Cancel
      </button>
      {error && <span className="login-error">{error}</span>}
    </div>
  );
}
