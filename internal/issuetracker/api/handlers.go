package api

import (
	"database/sql"
	"errors"
	"net/http"

	"cmd184psu/unified-webapp/internal/issuetracker/actor"
	"cmd184psu/unified-webapp/internal/issuetracker/models"
	"cmd184psu/unified-webapp/internal/issuetracker/store"
)

// bootstrap returns everything the frontend needs for an initial render.
func (a *API) bootstrap(w http.ResponseWriter, r *http.Request) {
	teams, err := a.Store.ListTeams()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	users, _ := a.Store.ListUsers()
	tags, _ := a.Store.ListTags()
	writeJSON(w, 200, map[string]any{
		"teams":  teams,
		"users":  users,
		"tags":   tags,
		"states": models.AllStates,
	})
}

// ----- Teams -----

func (a *API) listTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := a.Store.ListTeams()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, teams)
}

func (a *API) createTeam(w http.ResponseWriter, r *http.Request) {
	var in struct{ Key, Name, Color string }
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t, err := a.Store.CreateTeam(in.Key, in.Name, in.Color)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, t)
}

func (a *API) updateTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key   *string `json:"key"`
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t, err := a.Store.UpdateTeam(r.PathValue("id"), in.Key, in.Name, in.Color)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, t)
}

func (a *API) deleteTeam(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteTeam(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ----- Users -----

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Store.ListUsers()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, users)
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Email, Color string }
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	u, err := a.Store.CreateUser(in.Name, in.Email, in.Color)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, u)
}

// ----- Tags -----

func (a *API) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := a.Store.ListTags()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, tags)
}

func (a *API) createTag(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Color string }
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t, err := a.Store.CreateTag(in.Name, in.Color)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, t)
}

func (a *API) deleteTag(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteTag(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ----- Issues -----

func (a *API) listIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.IssueFilter{
		State:      q.Get("state"),
		TagID:      q.Get("tagId"),
		TeamID:     q.Get("teamId"),
		AssigneeID: q.Get("assigneeId"),
		Search:     q.Get("q"),
	}
	if q.Get("assignee") == "me" {
		if u := a.currentUser(r); u != nil {
			f.AssigneeID = u.ID
		} else {
			// No logged-in user: "my issues" is empty.
			writeJSON(w, 200, []models.Issue{})
			return
		}
	}
	if q.Has("storyId") {
		f.HasStoryFilter = true
		f.StoryID = q.Get("storyId")
	}
	issues, err := a.Store.ListIssues(f)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, issues)
}

func (a *API) getIssue(w http.ResponseWriter, r *http.Request) {
	i, err := a.Store.GetIssue(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "issue not found")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, i)
}

func (a *API) getIssueByIdentifier(w http.ResponseWriter, r *http.Request) {
	i, err := a.Store.GetIssueByIdentifier(r.PathValue("ident"))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "issue not found")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, i)
}

// issueBody is the JSON payload for create/update.
type issueBody struct {
	TeamID      *string   `json:"teamId"`
	StoryID     *string   `json:"storyId"`
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Kind        *string   `json:"kind"`
	State       *string   `json:"state"`
	Priority    *int      `json:"priority"`
	AssigneeID  *string   `json:"assigneeId"`
	ReporterID  *string   `json:"reporterId"`
	SortOrder   *float64  `json:"sortOrder"`
	TagIDs      *[]string `json:"tagIds"`
}

func (b issueBody) toInput() store.IssueInput {
	return store.IssueInput{
		TeamID: b.TeamID, StoryID: b.StoryID, Title: b.Title, Description: b.Description,
		Kind: b.Kind, State: b.State, Priority: b.Priority, AssigneeID: b.AssigneeID,
		ReporterID: b.ReporterID, SortOrder: b.SortOrder, TagIDs: b.TagIDs,
	}
}

// currentUser resolves the request's actor (API-key "api" user, LDAP/passkey
// session user, or the configured default user) to a stored user, or nil on a
// lookup error.
func (a *API) currentUser(r *http.Request) *models.User {
	u, err := actor.Resolve(r, a.Store, a.DefaultName, a.DefaultEmail)
	if err != nil {
		return nil
	}
	return u
}

func (a *API) createIssue(w http.ResponseWriter, r *http.Request) {
	var b issueBody
	if err := decode(r, &b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// The reporter is always the resolved actor; clients cannot set it.
	b.ReporterID = nil
	if u := a.currentUser(r); u != nil {
		b.ReporterID = &u.ID
	}
	i, err := a.Store.CreateIssue(b.toInput())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, i)
}

func (a *API) updateIssue(w http.ResponseWriter, r *http.Request) {
	var b issueBody
	if err := decode(r, &b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// Reporter is immutable once an issue is created.
	b.ReporterID = nil
	i, err := a.Store.UpdateIssue(r.PathValue("id"), b.toInput())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, i)
}

func (a *API) deleteIssue(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteIssue(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ----- Relations -----

func (a *API) addRelation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RelatedID string `json:"relatedId"`
		Type      string `json:"type"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rel, err := a.Store.AddRelation(r.PathValue("id"), in.RelatedID, in.Type)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, rel)
}

func (a *API) deleteRelation(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteRelation(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ----- Stories -----

func (a *API) listStories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var epic string
	has := q.Has("epicId")
	if has {
		epic = q.Get("epicId")
	}
	stories, err := a.Store.ListStories(epic, has)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, stories)
}

func (a *API) getStory(w http.ResponseWriter, r *http.Request) {
	st, err := a.Store.GetStory(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "story not found")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, st)
}

type storyBody struct {
	TeamID      *string `json:"teamId"`
	EpicID      *string `json:"epicId"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	State       *string `json:"state"`
}

func (a *API) createStory(w http.ResponseWriter, r *http.Request) {
	var b storyBody
	if err := decode(r, &b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	st, err := a.Store.CreateStory(store.StoryInput(b))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, st)
}

func (a *API) updateStory(w http.ResponseWriter, r *http.Request) {
	var b storyBody
	if err := decode(r, &b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	st, err := a.Store.UpdateStory(r.PathValue("id"), store.StoryInput(b))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (a *API) deleteStory(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteStory(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}

// ----- Epics -----

func (a *API) listEpics(w http.ResponseWriter, r *http.Request) {
	epics, err := a.Store.ListEpics()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, epics)
}

func (a *API) getEpic(w http.ResponseWriter, r *http.Request) {
	e, err := a.Store.GetEpic(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "epic not found")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, e)
}

type epicBody struct {
	TeamID      *string `json:"teamId"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	State       *string `json:"state"`
}

func (a *API) createEpic(w http.ResponseWriter, r *http.Request) {
	var b epicBody
	if err := decode(r, &b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	e, err := a.Store.CreateEpic(store.EpicInput(b))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, e)
}

func (a *API) updateEpic(w http.ResponseWriter, r *http.Request) {
	var b epicBody
	if err := decode(r, &b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	e, err := a.Store.UpdateEpic(r.PathValue("id"), store.EpicInput(b))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, e)
}

func (a *API) deleteEpic(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeleteEpic(r.PathValue("id")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}
