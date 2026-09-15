import { useState } from "react";
import { api } from "../api";
import { useData } from "../DataContext";
import { TagPill } from "../components/common";

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

export function TagsPage() {
  const { tags, reloadTags } = useData();
  const [name, setName] = useState("");
  const [color, setColor] = useState(PALETTE[5]);

  const add = async () => {
    if (!name.trim()) return;
    await api.createTag(name.trim(), color);
    setName("");
    await reloadTags();
  };

  const remove = async (id: string) => {
    if (confirm("Delete this tag?")) {
      await api.deleteTag(id);
      await reloadTags();
    }
  };

  return (
    <>
      <div className="topbar">
        <h1>Tags</h1>
      </div>
      <div className="content" style={{ padding: 18, maxWidth: 640 }}>
        <div
          style={{
            display: "flex",
            gap: 8,
            alignItems: "center",
            marginBottom: 20,
            flexWrap: "wrap",
          }}
        >
          <input
            placeholder="New tag name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && add()}
          />
          <div style={{ display: "flex", gap: 6 }}>
            {PALETTE.map((c) => (
              <span
                key={c}
                onClick={() => setColor(c)}
                style={{
                  width: 22,
                  height: 22,
                  borderRadius: "50%",
                  background: c,
                  cursor: "pointer",
                  border:
                    color === c
                      ? "2px solid white"
                      : "2px solid transparent",
                }}
              />
            ))}
          </div>
          <button className="btn primary" onClick={add}>
            Add tag
          </button>
        </div>

        {tags.length === 0 ? (
          <div className="empty">No tags yet.</div>
        ) : (
          tags.map((t) => (
            <div
              key={t.id}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 12,
                padding: "10px 0",
                borderBottom: "1px solid var(--border)",
              }}
            >
              <TagPill tag={t} />
              <span className="detail-ident">{t.color}</span>
              <div style={{ flex: 1 }} />
              <button className="btn ghost danger" onClick={() => remove(t.id)}>
                Delete
              </button>
            </div>
          ))
        )}
      </div>
    </>
  );
}
