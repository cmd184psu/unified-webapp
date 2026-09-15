package api

import (
	"encoding/json"
	"net/http"

	"cmd184psu/unified-webapp/internal/issuetracker/store"
)

// API holds dependencies for HTTP handlers. Authentication is the platform
// gate's job; this module only reads the resolved principal (via the actor
// package) to attribute writes.
type API struct {
	Store        *store.Store
	DefaultName  string
	DefaultEmail string
}

// New returns an API. defaultName/defaultEmail configure the open-mode
// fallback user used to attribute writes when no principal is present.
func New(s *store.Store, defaultName, defaultEmail string) *API {
	return &API{Store: s, DefaultName: defaultName, DefaultEmail: defaultEmail}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// Routes registers REST routes on the given mux.
func (a *API) Routes(mux *http.ServeMux) {
	// Auth routes (/api/auth/*, /healthz, /api/auth/mode, /api/auth/whoami) and
	// caller identity are owned by the platform gate; this module registers none.
	mux.HandleFunc("GET /api/bootstrap", a.bootstrap)

	mux.HandleFunc("GET /api/teams", a.listTeams)
	mux.HandleFunc("POST /api/teams", a.createTeam)
	mux.HandleFunc("PATCH /api/teams/{id}", a.updateTeam)
	mux.HandleFunc("DELETE /api/teams/{id}", a.deleteTeam)

	mux.HandleFunc("GET /api/users", a.listUsers)
	mux.HandleFunc("POST /api/users", a.createUser)

	mux.HandleFunc("GET /api/tags", a.listTags)
	mux.HandleFunc("POST /api/tags", a.createTag)
	mux.HandleFunc("DELETE /api/tags/{id}", a.deleteTag)

	mux.HandleFunc("GET /api/issues", a.listIssues)
	mux.HandleFunc("POST /api/issues", a.createIssue)
	mux.HandleFunc("GET /api/issues/{id}", a.getIssue)
	mux.HandleFunc("PATCH /api/issues/{id}", a.updateIssue)
	mux.HandleFunc("DELETE /api/issues/{id}", a.deleteIssue)
	mux.HandleFunc("GET /api/issues/by-identifier/{ident}", a.getIssueByIdentifier)

	mux.HandleFunc("POST /api/issues/{id}/relations", a.addRelation)
	mux.HandleFunc("DELETE /api/relations/{id}", a.deleteRelation)

	mux.HandleFunc("GET /api/stories", a.listStories)
	mux.HandleFunc("POST /api/stories", a.createStory)
	mux.HandleFunc("GET /api/stories/{id}", a.getStory)
	mux.HandleFunc("PATCH /api/stories/{id}", a.updateStory)
	mux.HandleFunc("DELETE /api/stories/{id}", a.deleteStory)

	mux.HandleFunc("GET /api/epics", a.listEpics)
	mux.HandleFunc("POST /api/epics", a.createEpic)
	mux.HandleFunc("GET /api/epics/{id}", a.getEpic)
	mux.HandleFunc("PATCH /api/epics/{id}", a.updateEpic)
	mux.HandleFunc("DELETE /api/epics/{id}", a.deleteEpic)
}
