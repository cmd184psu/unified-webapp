-- IssueTracker schema (SQLite)

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id          TEXT PRIMARY KEY,
    username    TEXT NOT NULL DEFAULT '',        -- LDAP login name, empty for demo users
    name        TEXT NOT NULL,
    email       TEXT NOT NULL DEFAULT '',
    color       TEXT NOT NULL DEFAULT '#6e79d6',
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- One account per LDAP username (demo users keep an empty username).
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username) WHERE username != '';

-- A team owns a 3-letter prefix and an incrementing counter for issue numbers.
CREATE TABLE IF NOT EXISTS teams (
    id          TEXT PRIMARY KEY,
    key         TEXT NOT NULL UNIQUE,            -- 3 letter prefix, e.g. ENG
    name        TEXT NOT NULL,
    color       TEXT NOT NULL DEFAULT '#6e79d6',
    counter     INTEGER NOT NULL DEFAULT 0,      -- last used issue number
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- User-defined colorful tags / labels.
CREATE TABLE IF NOT EXISTS tags (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    color       TEXT NOT NULL DEFAULT '#6e79d6',
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Epics are folders that contain stories.
CREATE TABLE IF NOT EXISTS epics (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL DEFAULT 'backlog',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Stories are folders that contain issues; may belong to an epic.
CREATE TABLE IF NOT EXISTS stories (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    epic_id     TEXT REFERENCES epics(id) ON DELETE SET NULL,
    number      INTEGER NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL DEFAULT 'backlog',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Issues are the atomic unit; may belong to a story.
CREATE TABLE IF NOT EXISTS issues (
    id          TEXT PRIMARY KEY,
    team_id     TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    story_id    TEXT REFERENCES stories(id) ON DELETE SET NULL,
    number      INTEGER NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL DEFAULT 'feature',   -- bug | feature | chore
    state       TEXT NOT NULL DEFAULT 'backlog',
    priority    INTEGER NOT NULL DEFAULT 0,         -- 0 none .. 4 urgent
    assignee_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    reporter_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    sort_order  REAL NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_issues_team   ON issues(team_id);
CREATE INDEX IF NOT EXISTS idx_issues_state  ON issues(state);
CREATE INDEX IF NOT EXISTS idx_issues_story  ON issues(story_id);

-- Many-to-many issue <-> tag.
CREATE TABLE IF NOT EXISTS issue_tags (
    issue_id    TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    tag_id      TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, tag_id)
);

-- Directed relationships between issues.
CREATE TABLE IF NOT EXISTS issue_relations (
    id          TEXT PRIMARY KEY,
    issue_id    TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    related_id  TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    type        TEXT NOT NULL DEFAULT 'relates',   -- relates | blocks | blocked_by | depends_on | duplicates
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (issue_id, related_id, type)
);

-- Access tokens for the Linear-compatible API.
CREATE TABLE IF NOT EXISTS api_tokens (
    id          TEXT PRIMARY KEY,
    token       TEXT NOT NULL UNIQUE,
    label       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
