package chat

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS rooms (
  id               TEXT PRIMARY KEY,
  name             TEXT NOT NULL,
  wave_id          TEXT NOT NULL DEFAULT '',
  moderator_key_id TEXT NOT NULL DEFAULT '',
  created_at       INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
  seq         INTEGER PRIMARY KEY AUTOINCREMENT,
  id          TEXT NOT NULL,
  room_id     TEXT NOT NULL,
  callsign    TEXT NOT NULL,
  to_callsign TEXT NOT NULL DEFAULT '',
  key_id      TEXT NOT NULL,
  content     TEXT NOT NULL,
  ts          INTEGER NOT NULL,
  kind        TEXT NOT NULL DEFAULT '',
  task_id     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room_id, seq);

CREATE TABLE IF NOT EXISTS callsigns (
  key_id           TEXT NOT NULL,
  name             TEXT NOT NULL,
  instance_id      TEXT NOT NULL DEFAULT '',
  created_at       INTEGER NOT NULL DEFAULT 0,
  last_activity_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (key_id, name)
);

CREATE TABLE IF NOT EXISTS tasks (
  num        INTEGER PRIMARY KEY AUTOINCREMENT,
  id         TEXT NOT NULL UNIQUE,
  room_id    TEXT NOT NULL,
  title      TEXT NOT NULL DEFAULT '',
  objective  TEXT NOT NULL DEFAULT '',
  assignee   TEXT NOT NULL DEFAULT '',
  creator    TEXT NOT NULL DEFAULT '',
  acceptance TEXT NOT NULL DEFAULT '',
  exclusions TEXT NOT NULL DEFAULT '',
  attempt    INTEGER NOT NULL DEFAULT 1,
  state      TEXT NOT NULL,
  waiting_on TEXT NOT NULL DEFAULT '',
  blocker    TEXT NOT NULL DEFAULT '',
  completed_portions TEXT NOT NULL DEFAULT '',
  changed_files      TEXT NOT NULL DEFAULT '',
  tests              TEXT NOT NULL DEFAULT '',
  deployment_target  TEXT NOT NULL DEFAULT '',
  service_health     TEXT NOT NULL DEFAULT '',
  evidence_ids       TEXT NOT NULL DEFAULT '',
  limitations        TEXT NOT NULL DEFAULT '',
  depends_on         TEXT NOT NULL DEFAULT '',
  capabilities       TEXT NOT NULL DEFAULT '',
  answer             TEXT NOT NULL DEFAULT '',
  answered_by        TEXT NOT NULL DEFAULT '',
  answered_at        INTEGER NOT NULL DEFAULT 0,
  answer_seq         INTEGER NOT NULL DEFAULT 0,
  version            INTEGER NOT NULL DEFAULT 1,
  spec_revision      INTEGER NOT NULL DEFAULT 1,
  acked_spec_revision INTEGER NOT NULL DEFAULT 0,
  summary            TEXT NOT NULL DEFAULT '',
  plan_id            TEXT NOT NULL DEFAULT '',
  claimed_by         TEXT NOT NULL DEFAULT '',
  claimed_at         INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_room ON tasks(room_id, num);

CREATE TABLE IF NOT EXISTS plans (
  seq                   INTEGER PRIMARY KEY AUTOINCREMENT,
  id                    TEXT NOT NULL UNIQUE,
  room_id               TEXT NOT NULL,
  title                 TEXT NOT NULL DEFAULT '',
  objective             TEXT NOT NULL DEFAULT '',
  planner               TEXT NOT NULL DEFAULT '',
  revision              INTEGER NOT NULL DEFAULT 1,
  status                TEXT NOT NULL DEFAULT 'active',
  task_ids              TEXT NOT NULL DEFAULT '',
  max_concurrent_active INTEGER NOT NULL DEFAULT 0,
  created_at            INTEGER NOT NULL,
  updated_at            INTEGER NOT NULL,
  closed_at             INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS inbox_acks (
  callsign    TEXT NOT NULL,
  instance_id TEXT NOT NULL,
  ack_seq     INTEGER NOT NULL DEFAULT 0,
  updated_at  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (callsign, instance_id)
);
`

// OpenStore opens (or creates) a SQLite-backed store at dbPath and hydrates its
// in-memory state from disk. retention sets the message TTL; messages older than
// retention are purged on open and by the background sweeper. retention <= 0
// disables the TTL (messages are kept indefinitely, subject to the per-room cap).
func OpenStore(dbPath string, retention time.Duration) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("chat: open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("chat: set pragmas: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("chat: init schema: %w", err)
	}
	if err := migrateCallsigns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateCallsignColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateMessageColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateTaskColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	s := &Store{
		rooms:        make(map[string]*Room),
		messages:     make(map[string][]Message),
		callsigns:    make(map[string]Callsign),
		tasks:        make(map[string]*Task),
		plans:        make(map[string]*Plan),
		ackCursors:   make(map[string]int64),
		TaskIDPrefix: defaultTaskPrefix,
		db:           db,
		retention:    retention,
	}

	if _, err := db.Exec(
		`INSERT OR IGNORE INTO rooms(id, name, wave_id, moderator_key_id, created_at) VALUES(?, 'Global', '', '', ?)`,
		globalRoomID, time.Now().UnixNano(),
	); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("chat: ensure global room: %w", err)
	}

	if err := s.load(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.PurgeExpired(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// migrateCallsigns upgrades legacy callsigns table shapes to the current
// composite-PK schema (key_id, name). Two legacy layouts are detected and
// recreated: (1) name PRIMARY KEY — the very first shape, which enforced global
// name uniqueness; (2) key_id PRIMARY KEY — the one-lease-per-key shape, which
// could not represent multiple agents sharing a key under different callsigns.
// Callsign rows are transient (agents re-register cheaply), so the table is
// simply recreated when a legacy layout is detected. No-op on the current shape.
func migrateCallsigns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(callsigns)`)
	if err != nil {
		return fmt.Errorf("chat: inspect callsigns: %w", err)
	}
	namePK, keyIDPK := false, false
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("chat: scan callsigns info: %w", err)
		}
		// SQLite reports pk as the 1-based position of the column within the
		// PRIMARY KEY (1 for the first, 2 for the second), or 0 if not part of
		// it. "Part of the PK" is therefore pk > 0, not pk == 1 — the composite
		// (key_id, name) PK reports key_id=1 and name=2.
		if name == "name" && pk > 0 {
			namePK = true
		}
		if name == "key_id" && pk > 0 {
			keyIDPK = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("chat: iterate callsigns info: %w", err)
	}
	_ = rows.Close()
	// Current composite-PK shape has BOTH key_id and name as part of the PK.
	// namePK alone  → the original name-unique shape (legacy).
	// keyIDPK alone → the one-lease-per-key shape (legacy, pre-attribution-fix).
	if !namePK && !keyIDPK {
		return nil
	}
	if namePK && keyIDPK {
		return nil
	}
	if _, err := db.Exec(`DROP TABLE callsigns;
CREATE TABLE IF NOT EXISTS callsigns (
  key_id           TEXT NOT NULL,
  name             TEXT NOT NULL,
  instance_id      TEXT NOT NULL DEFAULT '',
  created_at       INTEGER NOT NULL DEFAULT 0,
  last_activity_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (key_id, name)
);`); err != nil {
		return fmt.Errorf("chat: migrate callsigns: %w", err)
	}
	return nil
}

// migrateCallsignColumns adds the instance_id, created_at and last_activity_at
// columns to a callsigns table that predates callsign leases. Legacy rows are
// backfilled with a fresh instance ID and "now" timestamps so operators see a
// sensible baseline. New databases already have the columns via the schema; this
// is a no-op there.
func migrateCallsignColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(callsigns)`)
	if err != nil {
		return fmt.Errorf("chat: inspect callsigns cols: %w", err)
	}
	have := map[string]bool{}
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("chat: scan callsigns cols: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("chat: iterate callsigns cols: %w", err)
	}
	_ = rows.Close()
	if !have["instance_id"] {
		if _, err := db.Exec(`ALTER TABLE callsigns ADD COLUMN instance_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("chat: add callsigns.instance_id: %w", err)
		}
	}
	if !have["created_at"] {
		if _, err := db.Exec(`ALTER TABLE callsigns ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("chat: add callsigns.created_at: %w", err)
		}
	}
	if !have["last_activity_at"] {
		if _, err := db.Exec(`ALTER TABLE callsigns ADD COLUMN last_activity_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("chat: add callsigns.last_activity_at: %w", err)
		}
	}
	now := time.Now().UnixNano()
	if _, err := db.Exec(
		`UPDATE callsigns SET instance_id = 'agent_' || hex(randomblob(8)) WHERE instance_id = ''`,
	); err != nil {
		return fmt.Errorf("chat: backfill callsigns.instance_id: %w", err)
	}
	if _, err := db.Exec(`UPDATE callsigns SET created_at = ? WHERE created_at = 0`, now); err != nil {
		return fmt.Errorf("chat: backfill callsigns.created_at: %w", err)
	}
	if _, err := db.Exec(`UPDATE callsigns SET last_activity_at = ? WHERE last_activity_at = 0`, now); err != nil {
		return fmt.Errorf("chat: backfill callsigns.last_activity_at: %w", err)
	}
	return nil
}

// migrateMessageColumns adds the kind and task_id columns to a legacy messages
// table that predates first-class tasks. New databases already have them via the
// schema; this is a no-op there.
func migrateMessageColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return fmt.Errorf("chat: inspect messages: %w", err)
	}
	have := map[string]bool{}
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("chat: scan messages info: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("chat: iterate messages info: %w", err)
	}
	_ = rows.Close()
	if !have["kind"] {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN kind TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("chat: add messages.kind: %w", err)
		}
	}
	if !have["task_id"] {
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN task_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("chat: add messages.task_id: %w", err)
		}
	}
	return nil
}

// migrateTaskColumns adds the structured completion-contract columns to a tasks
// table that predates Slice 5. New databases already have them via the schema;
// this is a no-op there. All columns default to ” so legacy tasks load cleanly.
func migrateTaskColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(tasks)`)
	if err != nil {
		return fmt.Errorf("chat: inspect tasks: %w", err)
	}
	have := map[string]bool{}
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("chat: scan tasks info: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("chat: iterate tasks info: %w", err)
	}
	_ = rows.Close()
	cols := []string{"completed_portions", "changed_files", "tests", "deployment_target", "service_health", "evidence_ids", "limitations", "depends_on", "capabilities", "summary", "plan_id", "claimed_by"}
	for _, c := range cols {
		if have[c] {
			continue
		}
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE tasks ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, c)); err != nil {
			return fmt.Errorf("chat: add tasks.%s: %w", c, err)
		}
	}
	if !have["version"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN version INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("chat: add tasks.version: %w", err)
		}
	}
	if !have["claimed_at"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN claimed_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("chat: add tasks.claimed_at: %w", err)
		}
	}
	if !have["spec_revision"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN spec_revision INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("chat: add tasks.spec_revision: %w", err)
		}
	}
	if !have["acked_spec_revision"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN acked_spec_revision INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("chat: add tasks.acked_spec_revision: %w", err)
		}
	}
	if !have["answer"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN answer TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("chat: add tasks.answer: %w", err)
		}
	}
	if !have["answered_by"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN answered_by TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("chat: add tasks.answered_by: %w", err)
		}
	}
	if !have["answered_at"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN answered_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("chat: add tasks.answered_at: %w", err)
		}
	}
	if !have["answer_seq"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN answer_seq INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("chat: add tasks.answer_seq: %w", err)
		}
	}
	return nil
}

// Close releases the underlying database handle. Safe to call on an in-memory store.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) load() error {
	rows, err := s.db.Query(`SELECT id, name, wave_id, moderator_key_id, created_at FROM rooms`)
	if err != nil {
		return fmt.Errorf("chat: load rooms: %w", err)
	}
	for rows.Next() {
		var r Room
		var createdAt int64
		if err := rows.Scan(&r.ID, &r.Name, &r.WaveID, &r.ModeratorKeyID, &createdAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("chat: scan room: %w", err)
		}
		r.CreatedAt = time.Unix(0, createdAt)
		room := r
		s.rooms[room.ID] = &room
		s.messages[room.ID] = []Message{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("chat: iterate rooms: %w", err)
	}
	_ = rows.Close()

	mrows, err := s.db.Query(`SELECT seq, id, room_id, callsign, to_callsign, key_id, content, ts, kind, task_id FROM messages ORDER BY seq ASC`)
	if err != nil {
		return fmt.Errorf("chat: load messages: %w", err)
	}
	for mrows.Next() {
		var m Message
		var ts int64
		if err := mrows.Scan(&m.Seq, &m.ID, &m.RoomID, &m.Callsign, &m.To, &m.KeyID, &m.Content, &ts, &m.Kind, &m.TaskID); err != nil {
			_ = mrows.Close()
			return fmt.Errorf("chat: scan message: %w", err)
		}
		m.Timestamp = time.Unix(0, ts)
		if m.Seq > s.msgSeq {
			s.msgSeq = m.Seq
		}
		if _, ok := s.rooms[m.RoomID]; !ok {
			continue
		}
		s.messages[m.RoomID] = append(s.messages[m.RoomID], m)
	}
	if err := mrows.Err(); err != nil {
		_ = mrows.Close()
		return fmt.Errorf("chat: iterate messages: %w", err)
	}
	_ = mrows.Close()

	for id, msgs := range s.messages {
		if len(msgs) > maxMessagesPerRoom {
			s.messages[id] = msgs[len(msgs)-maxMessagesPerRoom:]
		}
		if r, ok := s.rooms[id]; ok {
			r.MessageCount = len(s.messages[id])
		}
	}

	crows, err := s.db.Query(`SELECT key_id, name, instance_id, created_at, last_activity_at FROM callsigns`)
	if err != nil {
		return fmt.Errorf("chat: load callsigns: %w", err)
	}
	for crows.Next() {
		var c Callsign
		var createdAt, lastActivityAt int64
		if err := crows.Scan(&c.KeyID, &c.Name, &c.InstanceID, &createdAt, &lastActivityAt); err != nil {
			_ = crows.Close()
			return fmt.Errorf("chat: scan callsign: %w", err)
		}
		if createdAt != 0 {
			c.CreatedAt = time.Unix(0, createdAt)
		}
		if lastActivityAt != 0 {
			c.LastActivityAt = time.Unix(0, lastActivityAt)
		}
		s.callsigns[leaseKey(c.KeyID, c.Name)] = c
	}
	if err := crows.Err(); err != nil {
		_ = crows.Close()
		return fmt.Errorf("chat: iterate callsigns: %w", err)
	}
	_ = crows.Close()

	arows, err := s.db.Query(`SELECT callsign, instance_id, ack_seq FROM inbox_acks`)
	if err != nil {
		return fmt.Errorf("chat: load inbox_acks: %w", err)
	}
	for arows.Next() {
		var callsign, instanceID string
		var ackSeq int64
		if err := arows.Scan(&callsign, &instanceID, &ackSeq); err != nil {
			_ = arows.Close()
			return fmt.Errorf("chat: scan inbox_ack: %w", err)
		}
		s.ackCursors[callsign+"\x00"+instanceID] = ackSeq
	}
	if err := arows.Err(); err != nil {
		_ = arows.Close()
		return fmt.Errorf("chat: iterate inbox_acks: %w", err)
	}
	_ = arows.Close()

	trows, err := s.db.Query(`SELECT num, id, room_id, title, objective, assignee, creator, acceptance, exclusions, attempt, state, waiting_on, blocker, completed_portions, changed_files, tests, deployment_target, service_health, evidence_ids, limitations, depends_on, capabilities, answer, answered_by, answered_at, answer_seq, version, spec_revision, acked_spec_revision, summary, plan_id, claimed_by, claimed_at, created_at, updated_at FROM tasks ORDER BY num ASC`)
	if err != nil {
		return fmt.Errorf("chat: load tasks: %w", err)
	}
	for trows.Next() {
		var t Task
		var state string
		var changedFiles, evidenceIDs, dependsOn, capabilities string
		var createdAt, updatedAt, claimedAt, answeredAt int64
		if err := trows.Scan(&t.Num, &t.ID, &t.RoomID, &t.Title, &t.Objective, &t.Assignee, &t.Creator,
			&t.AcceptanceCriteria, &t.Exclusions, &t.Attempt, &state, &t.WaitingOn, &t.Blocker,
			&t.CompletedPortions, &changedFiles, &t.Tests, &t.DeploymentTarget, &t.ServiceHealth,
			&evidenceIDs, &t.Limitations, &dependsOn, &capabilities,
			&t.Answer, &t.AnsweredBy, &answeredAt, &t.AnswerSeq,
			&t.Version, &t.SpecRevision, &t.AckedSpecRevision, &t.Summary, &t.PlanID, &t.ClaimedBy,
			&claimedAt, &createdAt, &updatedAt); err != nil {
			_ = trows.Close()
			return fmt.Errorf("chat: scan task: %w", err)
		}
		t.State = TaskState(state)
		t.ChangedFiles = decodeStringSlice(changedFiles)
		t.EvidenceIDs = decodeStringSlice(evidenceIDs)
		t.DependsOn = decodeStringSlice(dependsOn)
		t.Capabilities = decodeStringSlice(capabilities)
		t.CreatedAt = time.Unix(0, createdAt)
		t.UpdatedAt = time.Unix(0, updatedAt)
		if claimedAt != 0 {
			ca := time.Unix(0, claimedAt)
			t.ClaimedAt = &ca
		}
		if answeredAt != 0 {
			t.AnsweredAt = time.Unix(0, answeredAt)
		}
		task := t
		s.tasks[task.ID] = &task
		if task.Num > s.taskSeq {
			s.taskSeq = task.Num
		}
	}
	if err := trows.Err(); err != nil {
		_ = trows.Close()
		return fmt.Errorf("chat: iterate tasks: %w", err)
	}
	_ = trows.Close()

	if err := s.loadPlans(); err != nil {
		return err
	}

	return nil
}

// loadPlans hydrates the in-memory plan map from the plans table. Caller invoked
// from load() during OpenStore.
func (s *Store) loadPlans() error {
	prows, err := s.db.Query(`SELECT seq, id, room_id, title, objective, planner, revision, status, task_ids, max_concurrent_active, created_at, updated_at, closed_at FROM plans ORDER BY seq ASC`)
	if err != nil {
		return fmt.Errorf("chat: load plans: %w", err)
	}
	for prows.Next() {
		var p Plan
		var status, taskIDs string
		var seq int
		var createdAt, updatedAt, closedAt int64
		if err := prows.Scan(&seq, &p.ID, &p.RoomID, &p.Title, &p.Objective, &p.Planner, &p.Revision,
			&status, &taskIDs, &p.MaxConcurrentActive, &createdAt, &updatedAt, &closedAt); err != nil {
			_ = prows.Close()
			return fmt.Errorf("chat: scan plan: %w", err)
		}
		p.Status = PlanStatus(status)
		p.TaskIDs = decodeStringSlice(taskIDs)
		if p.TaskIDs == nil {
			p.TaskIDs = []string{}
		}
		p.CreatedAt = time.Unix(0, createdAt)
		p.UpdatedAt = time.Unix(0, updatedAt)
		if closedAt != 0 {
			ca := time.Unix(0, closedAt)
			p.ClosedAt = &ca
		}
		plan := p
		s.plans[plan.ID] = &plan
		if seq > s.planSeq {
			s.planSeq = seq
		}
	}
	if err := prows.Err(); err != nil {
		_ = prows.Close()
		return fmt.Errorf("chat: iterate plans: %w", err)
	}
	_ = prows.Close()
	return nil
}

// PurgeExpired removes messages older than the configured retention window from
// both memory and the database. It is a no-op when retention is disabled.
func (s *Store) PurgeExpired() error {
	if s.retention <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-s.retention)

	s.mu.Lock()
	defer s.mu.Unlock()

	for roomID, msgs := range s.messages {
		kept := make([]Message, 0, len(msgs))
		for _, m := range msgs {
			if m.Timestamp.After(cutoff) {
				kept = append(kept, m)
			}
		}
		if len(kept) != len(msgs) {
			s.messages[roomID] = kept
			if r, ok := s.rooms[roomID]; ok {
				r.MessageCount = len(kept)
			}
		}
	}

	if s.db != nil {
		if _, err := s.db.Exec(`DELETE FROM messages WHERE ts < ?`, cutoff.UnixNano()); err != nil {
			return fmt.Errorf("chat: purge expired: %w", err)
		}
	}
	return nil
}
