// Package graphql implements a pragmatic, Linear-compatible GraphQL endpoint
// so tools that speak Linear's API (e.g. zenflow) can read and write issues.
//
// It is NOT a full GraphQL engine. It parses the {query, variables} envelope
// and dispatches on the operation by inspecting the query text. The response
// shapes mirror the fields Linear clients commonly request.
package graphql

import (
	"encoding/json"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/issuetracker/actor"
	"cmd184psu/unified-webapp/internal/issuetracker/models"
	"cmd184psu/unified-webapp/internal/issuetracker/store"
)

type Handler struct {
	Store        *store.Store
	DefaultName  string
	DefaultEmail string
}

func New(s *store.Store, defaultName, defaultEmail string) *Handler {
	return &Handler{Store: s, DefaultName: defaultName, DefaultEmail: defaultEmail}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	var req gqlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeData(w, gqlError("invalid request body"))
		return
	}
	defer r.Body.Close()

	q := req.Query
	vars := req.Variables
	if vars == nil {
		vars = map[string]any{}
	}

	switch {
	case strings.Contains(q, "issueCreate"):
		h.issueCreate(w, r, vars)
	case strings.Contains(q, "issueUpdate"):
		h.issueUpdate(w, vars)
	case strings.Contains(q, "commentCreate"):
		writeData(w, map[string]any{"data": map[string]any{"commentCreate": map[string]any{"success": true}}})
	case strings.Contains(q, "viewer"):
		h.viewer(w, r)
	case strings.Contains(q, "workflowStates") || strings.Contains(q, "WorkflowStates"):
		h.workflowStates(w)
	case strings.Contains(q, "issueLabels") || strings.Contains(q, "labels"):
		h.labels(w)
	case strings.Contains(q, "teams") || strings.Contains(q, "Teams"):
		h.teams(w)
	case h.looksLikeSingleIssue(q, vars):
		h.issue(w, vars)
	case strings.Contains(q, "issues") || strings.Contains(q, "Issues"):
		h.issues(w, vars)
	default:
		writeData(w, gqlError("unsupported operation"))
	}
}

func (h *Handler) looksLikeSingleIssue(q string, vars map[string]any) bool {
	if _, ok := vars["id"]; ok && strings.Contains(q, "issue(") {
		return true
	}
	return strings.Contains(q, "issue(id")
}

// ----- query resolvers -----

func (h *Handler) viewer(w http.ResponseWriter, r *http.Request) {
	v := map[string]any{"id": "viewer", "name": "IssueTracker", "email": ""}
	if u, err := actor.Resolve(r, h.Store, h.DefaultName, h.DefaultEmail); err == nil && u != nil {
		v = userNode(u)
	}
	writeData(w, map[string]any{"data": map[string]any{"viewer": v}})
}

func (h *Handler) teams(w http.ResponseWriter) {
	teams, _ := h.Store.ListTeams()
	nodes := make([]map[string]any, 0, len(teams))
	for i := range teams {
		nodes = append(nodes, map[string]any{
			"id": teams[i].ID, "key": teams[i].Key, "name": teams[i].Name, "color": teams[i].Color,
		})
	}
	writeData(w, map[string]any{"data": map[string]any{"teams": map[string]any{"nodes": nodes}}})
}

func (h *Handler) workflowStates(w http.ResponseWriter) {
	nodes := make([]map[string]any, 0, len(models.AllStates))
	for _, st := range models.AllStates {
		nodes = append(nodes, map[string]any{
			"id": st, "name": stateName(st), "type": stateType(st), "color": stateColor(st),
		})
	}
	writeData(w, map[string]any{"data": map[string]any{"workflowStates": map[string]any{"nodes": nodes}}})
}

func (h *Handler) labels(w http.ResponseWriter) {
	tags, _ := h.Store.ListTags()
	nodes := make([]map[string]any, 0, len(tags))
	for i := range tags {
		nodes = append(nodes, map[string]any{"id": tags[i].ID, "name": tags[i].Name, "color": tags[i].Color})
	}
	writeData(w, map[string]any{"data": map[string]any{"issueLabels": map[string]any{"nodes": nodes}}})
}

func (h *Handler) issues(w http.ResponseWriter, vars map[string]any) {
	f := store.IssueFilter{}
	if filt, ok := vars["filter"].(map[string]any); ok {
		f = filterFromVars(filt)
	}
	issues, _ := h.Store.ListIssues(f)
	nodes := make([]map[string]any, 0, len(issues))
	for i := range issues {
		nodes = append(nodes, issueNode(&issues[i]))
	}
	writeData(w, map[string]any{"data": map[string]any{"issues": map[string]any{"nodes": nodes}}})
}

func (h *Handler) issue(w http.ResponseWriter, vars map[string]any) {
	id, _ := vars["id"].(string)
	var iss *models.Issue
	var err error
	if strings.Contains(id, "-") {
		iss, err = h.Store.GetIssueByIdentifier(id)
	} else {
		iss, err = h.Store.GetIssue(id)
	}
	if err != nil || iss == nil {
		writeData(w, map[string]any{"data": map[string]any{"issue": nil}})
		return
	}
	writeData(w, map[string]any{"data": map[string]any{"issue": issueNode(iss)}})
}

// ----- mutations -----

func (h *Handler) issueCreate(w http.ResponseWriter, r *http.Request, vars map[string]any) {
	input, _ := vars["input"].(map[string]any)
	if input == nil {
		writeData(w, gqlError("input required"))
		return
	}
	in := store.IssueInput{}
	if v, ok := input["teamId"].(string); ok {
		in.TeamID = &v
	} else if teams, _ := h.Store.ListTeams(); len(teams) > 0 {
		in.TeamID = &teams[0].ID
	}
	if v, ok := input["title"].(string); ok {
		in.Title = &v
	}
	if v, ok := input["description"].(string); ok {
		in.Description = &v
	}
	if v, ok := input["stateId"].(string); ok {
		s := normalizeState(v)
		in.State = &s
	}
	if v, ok := input["assigneeId"].(string); ok {
		in.AssigneeID = &v
	}
	if v, ok := input["priority"].(float64); ok {
		p := int(v)
		in.Priority = &p
	}
	if v, ok := input["labelIds"].([]any); ok {
		ids := toStringSlice(v)
		in.TagIDs = &ids
	}
	// Reporter is the resolved actor (FR5) — reference GraphQL set none.
	if u, err := actor.Resolve(r, h.Store, h.DefaultName, h.DefaultEmail); err == nil && u != nil {
		in.ReporterID = &u.ID
	}
	iss, err := h.Store.CreateIssue(in)
	if err != nil {
		writeData(w, gqlError(err.Error()))
		return
	}
	writeData(w, map[string]any{"data": map[string]any{"issueCreate": map[string]any{
		"success": true, "issue": issueNode(iss),
	}}})
}

func (h *Handler) issueUpdate(w http.ResponseWriter, vars map[string]any) {
	id, _ := vars["id"].(string)
	input, _ := vars["input"].(map[string]any)
	if id == "" || input == nil {
		writeData(w, gqlError("id and input required"))
		return
	}
	if strings.Contains(id, "-") {
		if iss, err := h.Store.GetIssueByIdentifier(id); err == nil && iss != nil {
			id = iss.ID
		}
	}
	in := store.IssueInput{}
	if v, ok := input["title"].(string); ok {
		in.Title = &v
	}
	if v, ok := input["description"].(string); ok {
		in.Description = &v
	}
	if v, ok := input["stateId"].(string); ok {
		s := normalizeState(v)
		in.State = &s
	}
	if v, ok := input["assigneeId"].(string); ok {
		in.AssigneeID = &v
	}
	if v, ok := input["priority"].(float64); ok {
		p := int(v)
		in.Priority = &p
	}
	if v, ok := input["labelIds"].([]any); ok {
		ids := toStringSlice(v)
		in.TagIDs = &ids
	}
	iss, err := h.Store.UpdateIssue(id, in)
	if err != nil {
		writeData(w, gqlError(err.Error()))
		return
	}
	writeData(w, map[string]any{"data": map[string]any{"issueUpdate": map[string]any{
		"success": true, "issue": issueNode(iss),
	}}})
}

func writeData(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_ = json.NewEncoder(w).Encode(v)
}

func gqlError(msg string) map[string]any {
	return map[string]any{"data": nil, "errors": []map[string]any{{"message": msg}}}
}
