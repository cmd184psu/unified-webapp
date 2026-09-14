export type State =
  | "backlog"
  | "todo"
  | "in_progress"
  | "blocked"
  | "in_review"
  | "completed";

export interface User {
  id: string;
  username?: string;
  name: string;
  email: string;
  color: string;
}

export interface Whoami {
  authenticated: boolean;
  method: string;
  identity: string;
}

export interface Team {
  id: string;
  key: string;
  name: string;
  color: string;
}

export interface Tag {
  id: string;
  name: string;
  color: string;
}

export interface Relation {
  id: string;
  issueId: string;
  relatedId: string;
  type: string;
  identifier?: string;
  title?: string;
  state?: State;
}

export interface Issue {
  id: string;
  identifier: string;
  teamId: string;
  teamKey: string;
  storyId: string | null;
  number: number;
  title: string;
  description: string;
  kind: string;
  state: State;
  priority: number;
  assigneeId: string | null;
  assignee: User | null;
  reporterId: string | null;
  reporter: User | null;
  sortOrder: number;
  tags: Tag[];
  relations: Relation[];
  createdAt: string;
  updatedAt: string;
}

export interface Story {
  id: string;
  identifier: string;
  teamId: string;
  teamKey: string;
  epicId: string | null;
  number: number;
  title: string;
  description: string;
  state: State;
  issueCount: number;
  issues?: Issue[];
  createdAt: string;
  updatedAt: string;
}

export interface Epic {
  id: string;
  identifier: string;
  teamId: string;
  teamKey: string;
  number: number;
  title: string;
  description: string;
  state: State;
  storyCount: number;
  stories?: Story[];
  createdAt: string;
  updatedAt: string;
}

export interface Bootstrap {
  teams: Team[];
  users: User[];
  tags: Tag[];
  states: State[];
}

export const STATES: State[] = [
  "backlog",
  "todo",
  "in_progress",
  "blocked",
  "in_review",
  "completed",
];

export const STATE_LABELS: Record<State, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  blocked: "Blocked",
  in_review: "In Review",
  completed: "Completed",
};

export const STATE_COLORS: Record<State, string> = {
  backlog: "#95a2b3",
  todo: "#e2e2e2",
  in_progress: "#f2c94c",
  blocked: "#eb5757",
  in_review: "#5e6ad2",
  completed: "#5cb85c",
};

export const PRIORITY_LABELS = ["No priority", "Low", "Medium", "High", "Urgent"];

export const RELATION_TYPES = [
  "relates",
  "blocks",
  "blocked_by",
  "depends_on",
  "duplicates",
];

export const RELATION_LABELS: Record<string, string> = {
  relates: "Relates to",
  blocks: "Blocks",
  blocked_by: "Blocked by",
  depends_on: "Depends on",
  duplicates: "Duplicates",
};
