import { useNavigate } from "react-router-dom";
import type { Issue } from "../types";
import { Avatar, Priority, StateBadge, TagPill } from "./common";

export function IssueRow({ issue }: { issue: Issue }) {
  const nav = useNavigate();
  return (
    <div className="issue-row" onClick={() => nav(`/issue/${issue.identifier}`)}>
      <span className="ident">{issue.identifier}</span>
      <span className="meta-state">
        <StateBadge state={issue.state} />
      </span>
      <span className="title">{issue.title}</span>
      <Priority value={issue.priority} />
      <span className="tags">
        {issue.tags.map((t) => (
          <TagPill key={t.id} tag={t} />
        ))}
      </span>
      <Avatar user={issue.assignee} />
    </div>
  );
}
