import type {
  Bootstrap,
  Epic,
  Issue,
  Story,
  Tag,
  Team,
  User,
  Whoami,
} from "./types";

async function req<T>(
  path: string,
  method = "GET",
  body?: unknown
): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status}: ${text}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export interface IssueQuery {
  state?: string;
  tagId?: string;
  teamId?: string;
  storyId?: string;
  assignee?: string;
  assigneeId?: string;
  q?: string;
}

export const api = {
  bootstrap: () => req<Bootstrap>("/api/bootstrap"),

  listIssues: (query: IssueQuery = {}) => {
    const p = new URLSearchParams();
    Object.entries(query).forEach(([k, v]) => {
      if (v !== undefined && v !== "") p.set(k, v);
    });
    const qs = p.toString();
    return req<Issue[]>(`/api/issues${qs ? `?${qs}` : ""}`);
  },
  getIssue: (id: string) => req<Issue>(`/api/issues/${id}`),
  getIssueByIdentifier: (ident: string) =>
    req<Issue>(`/api/issues/by-identifier/${ident}`),
  createIssue: (body: Partial<Issue> & { tagIds?: string[] }) =>
    req<Issue>("/api/issues", "POST", body),
  updateIssue: (id: string, body: Partial<Issue> & { tagIds?: string[] }) =>
    req<Issue>(`/api/issues/${id}`, "PATCH", body),
  deleteIssue: (id: string) => req<void>(`/api/issues/${id}`, "DELETE"),

  addRelation: (issueId: string, relatedId: string, type: string) =>
    req(`/api/issues/${issueId}/relations`, "POST", { relatedId, type }),
  deleteRelation: (id: string) => req<void>(`/api/relations/${id}`, "DELETE"),

  listStories: (epicId?: string) => {
    const qs = epicId !== undefined ? `?epicId=${epicId}` : "";
    return req<Story[]>(`/api/stories${qs}`);
  },
  getStory: (id: string) => req<Story>(`/api/stories/${id}`),
  createStory: (body: Partial<Story>) => req<Story>("/api/stories", "POST", body),
  updateStory: (id: string, body: Partial<Story>) =>
    req<Story>(`/api/stories/${id}`, "PATCH", body),
  deleteStory: (id: string) => req<void>(`/api/stories/${id}`, "DELETE"),

  listEpics: () => req<Epic[]>("/api/epics"),
  getEpic: (id: string) => req<Epic>(`/api/epics/${id}`),
  createEpic: (body: Partial<Epic>) => req<Epic>("/api/epics", "POST", body),
  updateEpic: (id: string, body: Partial<Epic>) =>
    req<Epic>(`/api/epics/${id}`, "PATCH", body),
  deleteEpic: (id: string) => req<void>(`/api/epics/${id}`, "DELETE"),

  createTag: (name: string, color: string) =>
    req<Tag>("/api/tags", "POST", { name, color }),
  deleteTag: (id: string) => req<void>(`/api/tags/${id}`, "DELETE"),

  createTeam: (body: Partial<Team>) => req<Team>("/api/teams", "POST", body),
  updateTeam: (id: string, body: Partial<Team>) =>
    req<Team>(`/api/teams/${id}`, "PATCH", body),
  deleteTeam: (id: string) => req<void>(`/api/teams/${id}`, "DELETE"),
  createUser: (body: Partial<User>) => req<User>("/api/users", "POST", body),

  whoami: () => req<Whoami>("/api/auth/whoami"),
};
