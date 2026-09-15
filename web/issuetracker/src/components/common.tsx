import type { ReactNode } from "react";
import {
  PRIORITY_LABELS,
  STATE_COLORS,
  STATE_LABELS,
  type State,
  type Tag,
  type User,
} from "../types";

export function TagPill({ tag }: { tag: Tag }) {
  return (
    <span
      className="tag"
      style={{
        background: tag.color + "22",
        borderColor: tag.color + "55",
        color: tag.color,
      }}
    >
      <span className="dot" style={{ background: tag.color }} />
      {tag.name}
    </span>
  );
}

export function StateBadge({ state }: { state: State }) {
  return (
    <span className="state" style={{ color: STATE_COLORS[state] }}>
      <span className="ring" />
      <span style={{ color: "var(--text-dim)" }}>{STATE_LABELS[state]}</span>
    </span>
  );
}

export function Avatar({ user }: { user: User | null }) {
  if (!user) {
    return (
      <span
        className="avatar"
        style={{ background: "transparent", border: "1px dashed var(--border-strong)" }}
        title="Unassigned"
      />
    );
  }
  const initials = user.name
    .split(" ")
    .map((p) => p[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();
  return (
    <span className="avatar" style={{ background: user.color }} title={user.name}>
      {initials}
    </span>
  );
}

export function Priority({ value }: { value: number }) {
  if (!value) return null;
  return <span className="prio">{PRIORITY_LABELS[value] ?? ""}</span>;
}

export function Modal({
  title,
  children,
  onClose,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
}) {
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h3>{title}</h3>
        {children}
      </div>
    </div>
  );
}
