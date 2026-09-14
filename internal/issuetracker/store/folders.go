package store

import (
	"database/sql"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/issuetracker/models"
)

// ----- Stories -----

func (s *Store) ListStories(epicFilter string, hasEpicFilter bool) ([]models.Story, error) {
	q := `SELECT s.id, s.team_id, t.key, s.epic_id, s.number, s.title, s.description, s.state,
		s.created_at, s.updated_at,
		(SELECT COUNT(1) FROM issues i WHERE i.story_id = s.id) AS issue_count
		FROM stories s JOIN teams t ON t.id = s.team_id`
	var args []any
	if hasEpicFilter {
		if epicFilter == "" {
			q += " WHERE s.epic_id IS NULL"
		} else {
			q += " WHERE s.epic_id = ?"
			args = append(args, epicFilter)
		}
	}
	q += " ORDER BY s.number DESC"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Story{}
	for rows.Next() {
		var st models.Story
		if err := rows.Scan(&st.ID, &st.TeamID, &st.TeamKey, &st.EpicID, &st.Number, &st.Title,
			&st.Description, &st.State, &st.CreatedAt, &st.UpdatedAt, &st.IssueCount); err != nil {
			return nil, err
		}
		st.Identifier = identifier(st.TeamKey, st.Number)
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) GetStory(id string) (*models.Story, error) {
	var st models.Story
	err := s.db.QueryRow(`SELECT s.id, s.team_id, t.key, s.epic_id, s.number, s.title, s.description, s.state,
		s.created_at, s.updated_at,
		(SELECT COUNT(1) FROM issues i WHERE i.story_id = s.id)
		FROM stories s JOIN teams t ON t.id = s.team_id WHERE s.id = ?`, id).
		Scan(&st.ID, &st.TeamID, &st.TeamKey, &st.EpicID, &st.Number, &st.Title,
			&st.Description, &st.State, &st.CreatedAt, &st.UpdatedAt, &st.IssueCount)
	if err != nil {
		return nil, err
	}
	st.Identifier = identifier(st.TeamKey, st.Number)
	issues, err := s.ListIssues(IssueFilter{StoryID: id, HasStoryFilter: true})
	if err != nil {
		return nil, err
	}
	st.Issues = issues
	return &st, nil
}

type StoryInput struct {
	TeamID      *string
	EpicID      *string
	Title       *string
	Description *string
	State       *string
}

func (s *Store) CreateStory(in StoryInput) (*models.Story, error) {
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
	_, err = tx.Exec(`INSERT INTO stories (id, team_id, epic_id, number, title, description, state)
		VALUES (?,?,?,?,?,?,?)`,
		id, *in.TeamID, nullStr(in.EpicID), num, *in.Title, valOr(in.Description, ""), valOr(in.State, models.StateBacklog))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetStory(id)
}

func (s *Store) UpdateStory(id string, in StoryInput) (*models.Story, error) {
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
	if in.State != nil {
		sets = append(sets, "state=?")
		args = append(args, *in.State)
	}
	if in.EpicID != nil {
		sets = append(sets, "epic_id=?")
		args = append(args, nullStr(in.EpicID))
	}
	if len(sets) == 0 {
		return s.GetStory(id)
	}
	sets = append(sets, "updated_at=datetime('now')")
	args = append(args, id)
	if _, err := s.db.Exec("UPDATE stories SET "+strings.Join(sets, ", ")+" WHERE id=?", args...); err != nil {
		return nil, err
	}
	return s.GetStory(id)
}

func (s *Store) DeleteStory(id string) error {
	_, err := s.db.Exec(`DELETE FROM stories WHERE id=?`, id)
	return err
}

// ----- Epics -----

func (s *Store) ListEpics() ([]models.Epic, error) {
	rows, err := s.db.Query(`SELECT e.id, e.team_id, t.key, e.number, e.title, e.description, e.state,
		e.created_at, e.updated_at,
		(SELECT COUNT(1) FROM stories st WHERE st.epic_id = e.id)
		FROM epics e JOIN teams t ON t.id = e.team_id ORDER BY e.number DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Epic{}
	for rows.Next() {
		var e models.Epic
		if err := rows.Scan(&e.ID, &e.TeamID, &e.TeamKey, &e.Number, &e.Title, &e.Description,
			&e.State, &e.CreatedAt, &e.UpdatedAt, &e.StoryCount); err != nil {
			return nil, err
		}
		e.Identifier = identifier(e.TeamKey, e.Number)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEpic(id string) (*models.Epic, error) {
	var e models.Epic
	err := s.db.QueryRow(`SELECT e.id, e.team_id, t.key, e.number, e.title, e.description, e.state,
		e.created_at, e.updated_at,
		(SELECT COUNT(1) FROM stories st WHERE st.epic_id = e.id)
		FROM epics e JOIN teams t ON t.id = e.team_id WHERE e.id = ?`, id).
		Scan(&e.ID, &e.TeamID, &e.TeamKey, &e.Number, &e.Title, &e.Description,
			&e.State, &e.CreatedAt, &e.UpdatedAt, &e.StoryCount)
	if err != nil {
		return nil, err
	}
	e.Identifier = identifier(e.TeamKey, e.Number)
	stories, err := s.ListStories(id, true)
	if err != nil {
		return nil, err
	}
	e.Stories = stories
	return &e, nil
}

type EpicInput struct {
	TeamID      *string
	Title       *string
	Description *string
	State       *string
}

func (s *Store) CreateEpic(in EpicInput) (*models.Epic, error) {
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
	_, err = tx.Exec(`INSERT INTO epics (id, team_id, number, title, description, state)
		VALUES (?,?,?,?,?,?)`,
		id, *in.TeamID, num, *in.Title, valOr(in.Description, ""), valOr(in.State, models.StateBacklog))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetEpic(id)
}

func (s *Store) UpdateEpic(id string, in EpicInput) (*models.Epic, error) {
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
	if in.State != nil {
		sets = append(sets, "state=?")
		args = append(args, *in.State)
	}
	if len(sets) == 0 {
		return s.GetEpic(id)
	}
	sets = append(sets, "updated_at=datetime('now')")
	args = append(args, id)
	if _, err := s.db.Exec("UPDATE epics SET "+strings.Join(sets, ", ")+" WHERE id=?", args...); err != nil {
		return nil, err
	}
	return s.GetEpic(id)
}

func (s *Store) DeleteEpic(id string) error {
	_, err := s.db.Exec(`DELETE FROM epics WHERE id=?`, id)
	return err
}

var _ = sql.ErrNoRows
