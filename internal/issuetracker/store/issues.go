package store

import (
	"database/sql"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/issuetracker/models"
)

// IssueFilter narrows issue listings.
type IssueFilter struct {
	State          string // single state filter
	TagID          string // issues having this tag
	TeamID         string
	AssigneeID     string // issues assigned to this user
	StoryID        string
	HasStoryFilter bool // when true, StoryID="" means "no story"
	Search         string
}

const issueSelect = `
SELECT i.id, i.team_id, t.key, i.story_id, i.number, i.title, i.description,
       i.kind, i.state, i.priority, i.assignee_id, i.reporter_id, i.sort_order,
       i.created_at, i.updated_at
FROM issues i JOIN teams t ON t.id = i.team_id`

func (s *Store) scanIssue(rows *sql.Rows) (*models.Issue, error) {
	var i models.Issue
	if err := rows.Scan(&i.ID, &i.TeamID, &i.TeamKey, &i.StoryID, &i.Number, &i.Title,
		&i.Description, &i.Kind, &i.State, &i.Priority, &i.AssigneeID, &i.ReporterID,
		&i.SortOrder, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	i.Identifier = identifier(i.TeamKey, i.Number)
	return &i, nil
}

// hydrate fills relational fields (assignee, reporter, tags, relations).
func (s *Store) hydrate(i *models.Issue) error {
	if i.AssigneeID != nil {
		if u, err := s.getUser(*i.AssigneeID); err == nil {
			i.Assignee = u
		}
	}
	if i.ReporterID != nil {
		if u, err := s.getUser(*i.ReporterID); err == nil {
			i.Reporter = u
		}
	}
	tags, err := s.tagsForIssue(i.ID)
	if err != nil {
		return err
	}
	i.Tags = tags
	rels, err := s.relationsForIssue(i.ID)
	if err != nil {
		return err
	}
	i.Relations = rels
	return nil
}

func (s *Store) ListIssues(f IssueFilter) ([]models.Issue, error) {
	q := issueSelect
	var where []string
	var args []any
	if f.State != "" {
		where = append(where, "i.state = ?")
		args = append(args, f.State)
	}
	if f.TeamID != "" {
		where = append(where, "i.team_id = ?")
		args = append(args, f.TeamID)
	}
	if f.AssigneeID != "" {
		where = append(where, "i.assignee_id = ?")
		args = append(args, f.AssigneeID)
	}
	if f.HasStoryFilter {
		if f.StoryID == "" {
			where = append(where, "i.story_id IS NULL")
		} else {
			where = append(where, "i.story_id = ?")
			args = append(args, f.StoryID)
		}
	}
	if f.TagID != "" {
		where = append(where, "i.id IN (SELECT issue_id FROM issue_tags WHERE tag_id = ?)")
		args = append(args, f.TagID)
	}
	if f.Search != "" {
		where = append(where, "(i.title LIKE ? OR i.description LIKE ?)")
		like := "%" + f.Search + "%"
		args = append(args, like, like)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY i.sort_order ASC, i.number DESC"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Issue{}
	for rows.Next() {
		i, err := s.scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for idx := range out {
		if err := s.hydrate(&out[idx]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) GetIssue(id string) (*models.Issue, error) {
	rows, err := s.db.Query(issueSelect+" WHERE i.id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	i, err := s.scanIssue(rows)
	if err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.hydrate(i); err != nil {
		return nil, err
	}
	return i, nil
}

// GetIssueByIdentifier looks up an issue by its TEAM-123 identifier.
func (s *Store) GetIssueByIdentifier(ident string) (*models.Issue, error) {
	parts := strings.SplitN(ident, "-", 2)
	if len(parts) != 2 {
		return nil, sql.ErrNoRows
	}
	var id string
	err := s.db.QueryRow(`SELECT i.id FROM issues i JOIN teams t ON t.id=i.team_id
		WHERE upper(t.key)=upper(?) AND i.number=?`, parts[0], parts[1]).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetIssue(id)
}

// IssueInput carries create/update fields. Pointers distinguish "unset".
type IssueInput struct {
	TeamID      *string
	StoryID     *string // empty string clears
	Title       *string
	Description *string
	Kind        *string
	State       *string
	Priority    *int
	AssigneeID  *string // empty string clears
	ReporterID  *string
	SortOrder   *float64
	TagIDs      *[]string
}

func (s *Store) CreateIssue(in IssueInput) (*models.Issue, error) {
	if in.TeamID == nil || *in.TeamID == "" {
		return nil, fmt.Errorf("teamId required")
	}
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return nil, fmt.Errorf("title required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	num, err := s.nextNumber(tx, *in.TeamID)
	if err != nil {
		return nil, err
	}
	id := NewID()
	kind := valOr(in.Kind, "feature")
	state := valOr(in.State, models.StateBacklog)
	desc := valOr(in.Description, "")
	prio := 0
	if in.Priority != nil {
		prio = *in.Priority
	}
	sort := 0.0
	if in.SortOrder != nil {
		sort = *in.SortOrder
	}
	_, err = tx.Exec(`INSERT INTO issues
		(id, team_id, story_id, number, title, description, kind, state, priority, assignee_id, reporter_id, sort_order)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, *in.TeamID, nullStr(in.StoryID), num, *in.Title, desc, kind, state, prio,
		nullStr(in.AssigneeID), nullStr(in.ReporterID), sort)
	if err != nil {
		return nil, err
	}
	if in.TagIDs != nil {
		if err := setIssueTags(tx, id, *in.TagIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetIssue(id)
}

func (s *Store) UpdateIssue(id string, in IssueInput) (*models.Issue, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var sets []string
	var args []any
	if in.Title != nil {
		sets = append(sets, "title=?")
		args = append(args, *in.Title)
	}
	if in.Description != nil {
		sets = append(sets, "description=?")
		args = append(args, *in.Description)
	}
	if in.Kind != nil {
		sets = append(sets, "kind=?")
		args = append(args, *in.Kind)
	}
	if in.State != nil {
		sets = append(sets, "state=?")
		args = append(args, *in.State)
	}
	if in.Priority != nil {
		sets = append(sets, "priority=?")
		args = append(args, *in.Priority)
	}
	if in.SortOrder != nil {
		sets = append(sets, "sort_order=?")
		args = append(args, *in.SortOrder)
	}
	if in.AssigneeID != nil {
		sets = append(sets, "assignee_id=?")
		args = append(args, nullStr(in.AssigneeID))
	}
	if in.ReporterID != nil {
		sets = append(sets, "reporter_id=?")
		args = append(args, nullStr(in.ReporterID))
	}
	if in.StoryID != nil {
		sets = append(sets, "story_id=?")
		args = append(args, nullStr(in.StoryID))
	}
	sets = append(sets, "updated_at=datetime('now')")
	args = append(args, id)
	if _, err := tx.Exec("UPDATE issues SET "+strings.Join(sets, ", ")+" WHERE id=?", args...); err != nil {
		return nil, err
	}
	if in.TagIDs != nil {
		if err := setIssueTags(tx, id, *in.TagIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetIssue(id)
}

func (s *Store) DeleteIssue(id string) error {
	_, err := s.db.Exec(`DELETE FROM issues WHERE id=?`, id)
	return err
}

func setIssueTags(tx *sql.Tx, issueID string, tagIDs []string) error {
	if _, err := tx.Exec(`DELETE FROM issue_tags WHERE issue_id=?`, issueID); err != nil {
		return err
	}
	for _, tid := range tagIDs {
		if tid == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO issue_tags (issue_id, tag_id) VALUES (?,?)`, issueID, tid); err != nil {
			return err
		}
	}
	return nil
}

// ----- Relations -----

func (s *Store) relationsForIssue(issueID string) ([]models.Relation, error) {
	rows, err := s.db.Query(`
		SELECT r.id, r.issue_id, r.related_id, r.type, r.created_at,
		       t.key, ri.number, ri.title, ri.state
		FROM issue_relations r
		JOIN issues ri ON ri.id = r.related_id
		JOIN teams t ON t.id = ri.team_id
		WHERE r.issue_id = ?
		ORDER BY r.created_at`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Relation{}
	for rows.Next() {
		var r models.Relation
		var key string
		var num int
		if err := rows.Scan(&r.ID, &r.IssueID, &r.RelatedID, &r.Type, &r.CreatedAt,
			&key, &num, &r.Title, &r.State); err != nil {
			return nil, err
		}
		r.Identifier = identifier(key, num)
		out = append(out, r)
	}
	return out, rows.Err()
}

var inverseRel = map[string]string{
	models.RelBlocks:    models.RelBlockedBy,
	models.RelBlockedBy: models.RelBlocks,
	models.RelDependsOn: models.RelRelates,
	models.RelRelates:   models.RelRelates,
}

// AddRelation creates a relation and a sensible inverse on the other issue.
func (s *Store) AddRelation(issueID, relatedID, relType string) (*models.Relation, error) {
	if issueID == relatedID {
		return nil, fmt.Errorf("cannot relate an issue to itself")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id := NewID()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO issue_relations (id, issue_id, related_id, type) VALUES (?,?,?,?)`,
		id, issueID, relatedID, relType); err != nil {
		return nil, err
	}
	if inv, ok := inverseRel[relType]; ok {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO issue_relations (id, issue_id, related_id, type) VALUES (?,?,?,?)`,
			NewID(), relatedID, issueID, inv); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	rels, err := s.relationsForIssue(issueID)
	if err != nil {
		return nil, err
	}
	for i := range rels {
		if rels[i].RelatedID == relatedID && rels[i].Type == relType {
			return &rels[i], nil
		}
	}
	return nil, nil
}

func (s *Store) DeleteRelation(id string) error {
	_, err := s.db.Exec(`DELETE FROM issue_relations WHERE id=?`, id)
	return err
}

func valOr(p *string, def string) string {
	if p != nil {
		return *p
	}
	return def
}

func nullStr(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}
