package graphql

import (
	"strings"

	"cmd184psu/unified-webapp/internal/issuetracker/models"
	"cmd184psu/unified-webapp/internal/issuetracker/store"
)

func userNode(u *models.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{
		"id": u.ID, "name": u.Name, "displayName": u.Name, "email": u.Email,
	}
}

func issueNode(i *models.Issue) map[string]any {
	labels := make([]map[string]any, 0, len(i.Tags))
	for _, t := range i.Tags {
		labels = append(labels, map[string]any{"id": t.ID, "name": t.Name, "color": t.Color})
	}
	node := map[string]any{
		"id":          i.ID,
		"identifier":  i.Identifier,
		"number":      i.Number,
		"title":       i.Title,
		"description": i.Description,
		"priority":    i.Priority,
		"url":         "/issue/" + i.Identifier,
		"createdAt":   i.CreatedAt,
		"updatedAt":   i.UpdatedAt,
		"state": map[string]any{
			"id": i.State, "name": stateName(i.State), "type": stateType(i.State), "color": stateColor(i.State),
		},
		"team":   map[string]any{"id": i.TeamID, "key": i.TeamKey},
		"labels": map[string]any{"nodes": labels},
	}
	node["assignee"] = userNode(i.Assignee)
	node["creator"] = userNode(i.Reporter)
	return node
}

func filterFromVars(filt map[string]any) store.IssueFilter {
	f := store.IssueFilter{}
	// { state: { name: { eq: "..." } } } or { state: { id: { eq } } }
	if st, ok := filt["state"].(map[string]any); ok {
		if name, ok := st["name"].(map[string]any); ok {
			if eq, ok := name["eq"].(string); ok {
				f.State = normalizeState(eq)
			}
		}
		if idf, ok := st["id"].(map[string]any); ok {
			if eq, ok := idf["eq"].(string); ok {
				f.State = normalizeState(eq)
			}
		}
	}
	if team, ok := filt["team"].(map[string]any); ok {
		if idf, ok := team["id"].(map[string]any); ok {
			if eq, ok := idf["eq"].(string); ok {
				f.TeamID = eq
			}
		}
	}
	return f
}

func toStringSlice(in []any) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// state display helpers map our snake_case states to Linear-like metadata.

func stateName(s string) string {
	switch s {
	case models.StateBacklog:
		return "Backlog"
	case models.StateTodo:
		return "Todo"
	case models.StateInProgress:
		return "In Progress"
	case models.StateBlocked:
		return "Blocked"
	case models.StateInReview:
		return "In Review"
	case models.StateCompleted:
		return "Completed"
	}
	return s
}

func stateType(s string) string {
	switch s {
	case models.StateBacklog:
		return "backlog"
	case models.StateTodo:
		return "unstarted"
	case models.StateInProgress, models.StateBlocked, models.StateInReview:
		return "started"
	case models.StateCompleted:
		return "completed"
	}
	return "unstarted"
}

func stateColor(s string) string {
	switch s {
	case models.StateBacklog:
		return "#bec2c8"
	case models.StateTodo:
		return "#e2e2e2"
	case models.StateInProgress:
		return "#f2c94c"
	case models.StateBlocked:
		return "#eb5757"
	case models.StateInReview:
		return "#5e6ad2"
	case models.StateCompleted:
		return "#5cb85c"
	}
	return "#bec2c8"
}

// normalizeState accepts either our ids, Linear display names, or "In Progress".
func normalizeState(s string) string {
	key := strings.ToLower(strings.TrimSpace(s))
	key = strings.ReplaceAll(key, " ", "_")
	switch key {
	case "backlog", "todo", "in_progress", "blocked", "in_review", "completed", "done":
		if key == "done" {
			return models.StateCompleted
		}
		return key
	}
	return s
}
