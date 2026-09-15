package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/issuetracker/models"
)

// Store wraps the database and exposes domain operations.
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

// NewID returns a random hex id.
func NewID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ----- Users -----

func (s *Store) ListUsers() ([]models.User, error) {
	rows, err := s.db.Query(`SELECT id, username, name, email, color, created_at FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.Color, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(name, email, color string) (*models.User, error) {
	id := NewID()
	if color == "" {
		color = "#6e79d6"
	}
	_, err := s.db.Exec(`INSERT INTO users (id, name, email, color) VALUES (?,?,?,?)`, id, name, email, color)
	if err != nil {
		return nil, err
	}
	return s.getUser(id)
}

func (s *Store) getUser(id string) (*models.User, error) {
	var u models.User
	err := s.db.QueryRow(`SELECT id, username, name, email, color, created_at FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.Color, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// userColors is a stable palette assigned to new sign-in users.
var userColors = []string{"#5e6ad2", "#e2a03f", "#26a69a", "#ec4899", "#bb6bd9", "#f2994a", "#eb5757", "#27ae60"}

// EnsureUserByUsername returns the account for an LDAP username, creating it on
// first sign-in so the user joins the assignee pool. displayName updates an
// existing record when provided.
func (s *Store) EnsureUserByUsername(username, displayName string) (*models.User, error) {
	if username == "" {
		return nil, fmt.Errorf("username required")
	}
	name := displayName
	if name == "" {
		name = username
	}
	var id string
	err := s.db.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id)
	if err == nil {
		if displayName != "" {
			_, _ = s.db.Exec(`UPDATE users SET name=? WHERE id=? AND name=username`, displayName, id)
		}
		return s.getUser(id)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	id = NewID()
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n)
	color := userColors[n%len(userColors)]
	if _, err := s.db.Exec(`INSERT INTO users (id, username, name, email, color) VALUES (?,?,?,?,?)`,
		id, username, name, "", color); err != nil {
		return nil, err
	}
	return s.getUser(id)
}

// ----- Teams -----

func (s *Store) ListTeams() ([]models.Team, error) {
	rows, err := s.db.Query(`SELECT id, key, name, color, counter, created_at FROM teams ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Team
	for rows.Next() {
		var t models.Team
		if err := rows.Scan(&t.ID, &t.Key, &t.Name, &t.Color, &t.Counter, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// validKey enforces a 3-4 character all-caps project prefix (e.g. ENG, PLAT).
func validKey(key string) (string, error) {
	k := strings.ToUpper(strings.TrimSpace(key))
	if len(k) < 3 || len(k) > 4 {
		return "", fmt.Errorf("prefix must be 3-4 characters")
	}
	for _, c := range k {
		if c < 'A' || c > 'Z' {
			return "", fmt.Errorf("prefix must contain only letters")
		}
	}
	return k, nil
}

func (s *Store) CreateTeam(key, name, color string) (*models.Team, error) {
	k, err := validKey(key)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("project name required")
	}
	id := NewID()
	if color == "" {
		color = "#6e79d6"
	}
	_, err = s.db.Exec(`INSERT INTO teams (id, key, name, color) VALUES (?,?,?,?)`, id, k, name, color)
	if err != nil {
		return nil, err
	}
	return s.getTeam(id)
}

func (s *Store) getTeam(id string) (*models.Team, error) {
	var t models.Team
	err := s.db.QueryRow(`SELECT id, key, name, color, counter, created_at FROM teams WHERE id=?`, id).
		Scan(&t.ID, &t.Key, &t.Name, &t.Color, &t.Counter, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateTeam edits a project's prefix, name and/or color.
func (s *Store) UpdateTeam(id string, key, name, color *string) (*models.Team, error) {
	var sets []string
	var args []any
	if key != nil {
		k, err := validKey(*key)
		if err != nil {
			return nil, err
		}
		sets = append(sets, "key=?")
		args = append(args, k)
	}
	if name != nil {
		if strings.TrimSpace(*name) == "" {
			return nil, fmt.Errorf("project name required")
		}
		sets = append(sets, "name=?")
		args = append(args, *name)
	}
	if color != nil && *color != "" {
		sets = append(sets, "color=?")
		args = append(args, *color)
	}
	if len(sets) == 0 {
		return s.getTeam(id)
	}
	args = append(args, id)
	if _, err := s.db.Exec(`UPDATE teams SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...); err != nil {
		return nil, err
	}
	return s.getTeam(id)
}

// DeleteTeam removes a project and cascades to its issues/stories/epics.
func (s *Store) DeleteTeam(id string) error {
	_, err := s.db.Exec(`DELETE FROM teams WHERE id=?`, id)
	return err
}

func (s *Store) teamKey(id string) (string, error) {
	var key string
	err := s.db.QueryRow(`SELECT key FROM teams WHERE id=?`, id).Scan(&key)
	return key, err
}

// nextNumber atomically increments and returns the team's issue counter.
func (s *Store) nextNumber(tx *sql.Tx, teamID string) (int, error) {
	if _, err := tx.Exec(`UPDATE teams SET counter = counter + 1 WHERE id=?`, teamID); err != nil {
		return 0, err
	}
	var n int
	err := tx.QueryRow(`SELECT counter FROM teams WHERE id=?`, teamID).Scan(&n)
	return n, err
}

// ----- Tags -----

func (s *Store) ListTags() ([]models.Tag, error) {
	rows, err := s.db.Query(`SELECT id, name, color, created_at FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Tag
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateTag(name, color string) (*models.Tag, error) {
	id := NewID()
	if color == "" {
		color = "#6e79d6"
	}
	_, err := s.db.Exec(`INSERT INTO tags (id, name, color) VALUES (?,?,?)`, id, name, color)
	if err != nil {
		return nil, err
	}
	var t models.Tag
	err = s.db.QueryRow(`SELECT id, name, color, created_at FROM tags WHERE id=?`, id).
		Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt)
	return &t, err
}

func (s *Store) DeleteTag(id string) error {
	_, err := s.db.Exec(`DELETE FROM tags WHERE id=?`, id)
	return err
}

func (s *Store) tagsForIssue(issueID string) ([]models.Tag, error) {
	rows, err := s.db.Query(`SELECT t.id, t.name, t.color, t.created_at
		FROM tags t JOIN issue_tags it ON it.tag_id = t.id
		WHERE it.issue_id = ? ORDER BY t.name`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Tag{}
	for rows.Next() {
		var t models.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func identifier(key string, n int) string { return fmt.Sprintf("%s-%d", key, n) }
