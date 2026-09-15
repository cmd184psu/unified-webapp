package models

// Issue states (Linear-like workflow).
const (
	StateBacklog    = "backlog"
	StateTodo       = "todo"
	StateInProgress = "in_progress"
	StateBlocked    = "blocked"
	StateInReview   = "in_review"
	StateCompleted  = "completed"
)

// AllStates is the ordered list of workflow states used by the board.
var AllStates = []string{
	StateBacklog,
	StateTodo,
	StateInProgress,
	StateBlocked,
	StateInReview,
	StateCompleted,
}

// Relation types between issues.
const (
	RelRelates    = "relates"
	RelBlocks     = "blocks"
	RelBlockedBy  = "blocked_by"
	RelDependsOn  = "depends_on"
	RelDuplicates = "duplicates"
)

type User struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Color     string `json:"color"`
	CreatedAt string `json:"createdAt"`
}

type Team struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Counter   int    `json:"-"`
	CreatedAt string `json:"createdAt"`
}

type Tag struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"createdAt"`
}

type Relation struct {
	ID         string `json:"id"`
	IssueID    string `json:"issueId"`
	RelatedID  string `json:"relatedId"`
	Type       string `json:"type"`
	Identifier string `json:"identifier,omitempty"` // related issue identifier, for display
	Title      string `json:"title,omitempty"`
	State      string `json:"state,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

type Issue struct {
	ID          string     `json:"id"`
	Identifier  string     `json:"identifier"` // TEAM-123
	TeamID      string     `json:"teamId"`
	TeamKey     string     `json:"teamKey"`
	StoryID     *string    `json:"storyId"`
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Kind        string     `json:"kind"`
	State       string     `json:"state"`
	Priority    int        `json:"priority"`
	AssigneeID  *string    `json:"assigneeId"`
	Assignee    *User      `json:"assignee"`
	ReporterID  *string    `json:"reporterId"`
	Reporter    *User      `json:"reporter"`
	SortOrder   float64    `json:"sortOrder"`
	Tags        []Tag      `json:"tags"`
	Relations   []Relation `json:"relations"`
	CreatedAt   string     `json:"createdAt"`
	UpdatedAt   string     `json:"updatedAt"`
}

type Story struct {
	ID          string  `json:"id"`
	Identifier  string  `json:"identifier"`
	TeamID      string  `json:"teamId"`
	TeamKey     string  `json:"teamKey"`
	EpicID      *string `json:"epicId"`
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	State       string  `json:"state"`
	IssueCount  int     `json:"issueCount"`
	Issues      []Issue `json:"issues,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

type Epic struct {
	ID          string  `json:"id"`
	Identifier  string  `json:"identifier"`
	TeamID      string  `json:"teamId"`
	TeamKey     string  `json:"teamKey"`
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	State       string  `json:"state"`
	StoryCount  int     `json:"storyCount"`
	Stories     []Story `json:"stories,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}
