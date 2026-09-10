package grocery

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Handler wires HTTP routes to the grocery store.
type Handler struct {
	store        *Store
	groups       []string
	progress     bool
	syncInterval int
	title        string
	broker       *broker.Broker
}

// NewHandler returns a Handler.
func NewHandler(s *Store, groups []string, progress bool, syncInterval int, title string, b *broker.Broker) *Handler {
	return &Handler{store: s, groups: groups, progress: progress, syncInterval: syncInterval, title: title, broker: b}
}

// Register mounts all grocery API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/config",                h.handleConfig)
	mux.HandleFunc("/api/config/title",          h.handleConfigTitle)
	mux.HandleFunc("/api/config/groups",         h.handleConfigGroupsAdd)
	mux.HandleFunc("/api/config/groups/remove",  h.handleConfigGroupsRemove)
	mux.HandleFunc("/api/config/groups/reorder", h.handleConfigGroupsReorder)

	mux.HandleFunc("/api/items",  h.handleItems)
	mux.HandleFunc("/api/items/", h.handleItem)

	mux.HandleFunc("/api/recipes",  h.handleRecipes)
	mux.HandleFunc("/api/recipes/", h.handleRecipePath)

	mux.HandleFunc("/api/move",     h.handleMove)
	mux.HandleFunc("/api/reorder",  h.handleReorder)
	mux.HandleFunc("/api/sync",     h.handleSync)
	mux.HandleFunc("/api/reset",    h.handleReset)
	mux.HandleFunc("/api/revision", h.handleRevision)
	mux.Handle("/api/events",       h.broker)
}

// GET /api/config
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	title := h.store.Title()
	if title == "" {
		title = h.title
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"groups":                h.groups,
		"progress":              h.progress,
		"sync_interval_seconds": h.syncInterval,
		"title":                 title,
	})
}

// POST /api/config/title
func (h *Handler) handleConfigTitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	title, ok := decodeName(w, r)
	if !ok {
		return
	}
	if err := h.store.SetTitle(title); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"title": title})
	h.broker.Notify()
}

// POST /api/config/groups
func (h *Handler) handleConfigGroupsAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name, ok := decodeName(w, r)
	if !ok {
		return
	}
	if name == NoGroup {
		response.WriteError(w, http.StatusBadRequest, `"No Group" is a reserved name`)
		return
	}
	for _, g := range h.groups {
		if g == name {
			response.WriteJSON(w, http.StatusOK, map[string]any{"groups": h.groups})
			return
		}
	}
	h.groups = append(h.groups, name)
	if err := h.store.SaveGroups(h.groups); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"groups": h.groups})
	h.broker.Notify()
}

// POST /api/config/groups/remove
func (h *Handler) handleConfigGroupsRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name, ok := decodeName(w, r)
	if !ok {
		return
	}
	newGroups := make([]string, 0, len(h.groups))
	for _, g := range h.groups {
		if g != name {
			newGroups = append(newGroups, g)
		}
	}
	h.groups = newGroups
	if err := h.store.SaveGroups(h.groups); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"groups": h.groups,
		"items":  h.store.List(),
	})
	h.broker.Notify()
}

// POST /api/config/groups/reorder
func (h *Handler) handleConfigGroupsReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Groups []string `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Groups) == 0 {
		response.WriteError(w, http.StatusBadRequest, "groups array required")
		return
	}
	h.groups = body.Groups
	if err := h.store.SaveGroups(h.groups); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"groups": h.groups})
	h.broker.Notify()
}

// GET /api/items  → list all items
// POST /api/items → {name, group} create item
func (h *Handler) handleItems(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response.WriteJSON(w, http.StatusOK, h.store.List())

	case http.MethodPost:
		var body struct {
			Name  string `json:"name"`
			Group string `json:"group"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
			strings.TrimSpace(body.Name) == "" {
			response.WriteError(w, http.StatusBadRequest, "name is required")
			return
		}
		if body.Group == "" {
			if len(h.groups) > 0 {
				body.Group = h.groups[0]
			} else {
				body.Group = NoGroup
			}
		}
		item, err := h.store.Add(strings.TrimSpace(body.Name), body.Group)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.WriteJSON(w, http.StatusCreated, item)
		h.broker.Notify()

	default:
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// PATCH  /api/items/:id → partial update
// DELETE /api/items/:id → remove item
func (h *Handler) handleItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/items/")
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "id required")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var p PatchPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			response.WriteError(w, http.StatusBadRequest, "invalid body")
			return
		}
		item, err := h.store.Patch(id, p)
		if err != nil {
			response.WriteError(w, http.StatusNotFound, err.Error())
			return
		}
		response.WriteJSON(w, http.StatusOK, item)
		h.broker.Notify()

	case http.MethodDelete:
		if err := h.store.Delete(id); err != nil {
			if errors.Is(err, ErrRecipeOwned) {
				response.WriteError(w, http.StatusConflict,
					"item belongs to a recipe; delete it from the recipe instead")
				return
			}
			response.WriteError(w, http.StatusNotFound, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		h.broker.Notify()

	default:
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// POST /api/move
func (h *Handler) handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		ID string `json:"id"`
		MovePayload
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		response.WriteError(w, http.StatusBadRequest, "id and group required")
		return
	}
	item, err := h.store.Move(body.ID, body.MovePayload)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, item)
	h.broker.Notify()
}

// POST /api/reorder
func (h *Handler) handleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Group string   `json:"group"`
		IDs   []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.store.Reorder(body.Group, body.IDs); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, h.store.List())
	h.broker.Notify()
}

// POST /api/sync
func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var items []*Item
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid body")
		return
	}
	merged, err := h.store.BulkSync(items)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, merged)
	h.broker.Notify()
}

// POST /api/reset
func (h *Handler) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.store.Reset()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
	h.broker.Notify()
}

// GET /api/revision
func (h *Handler) handleRevision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]int64{"revision": h.store.Revision()})
}

// writeRecipeError maps the store's sentinel errors onto status codes. The item
// routes collapse every store error to 404; the recipe routes have to
// distinguish 400, 404 and 409, which is what the sentinels exist for.
func writeRecipeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRecipeNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrDuplicateRecipe):
		// 409, not 400: the request is well-formed, it collides with existing
		// state. This is what separates ErrDuplicateRecipe from ErrInvalidName
		// — mapping both to 400 made the two sentinels indistinguishable to a
		// client and left this function unable to do the job its comment claims.
		response.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidName):
		response.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrRecipeOwned):
		response.WriteError(w, http.StatusConflict, err.Error())
	default:
		response.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

// GET  /api/recipes → list
// POST /api/recipes → create
func (h *Handler) handleRecipes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response.WriteJSON(w, http.StatusOK, h.store.Recipes())

	case http.MethodPost:
		name, ok := decodeName(w, r)
		if !ok {
			return
		}
		recipe, err := h.store.AddRecipe(name)
		if err != nil {
			writeRecipeError(w, err)
			return
		}
		response.WriteJSON(w, http.StatusCreated, recipe)
		h.broker.Notify()

	default:
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// POST   /api/recipes/reorder
// PATCH  /api/recipes/:id                        → rename and/or enable
// DELETE /api/recipes/:id                        → remove recipe + its items
// POST   /api/recipes/:id/ingredients            → add ingredient
// DELETE /api/recipes/:id/ingredients/:item_id   → remove ingredient
//
// DELETE /api/recipes/ trims to an empty id and answers 404 rather than
// handleItem's 400 "id required". Both are defensible; 404 is the store's own
// answer, so it is used rather than adding a check that disagrees with it.
func (h *Handler) handleRecipePath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/recipes/")
	seg := strings.Split(rest, "/")

	// The literal has to be matched before the :id case, or "reorder" is read
	// as a recipe id.
	if len(seg) == 1 && seg[0] == "reorder" {
		if r.Method != http.MethodPost {
			response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			response.WriteError(w, http.StatusBadRequest, "invalid body")
			return
		}
		recipes, err := h.store.ReorderRecipes(body.IDs)
		if err != nil {
			writeRecipeError(w, err)
			return
		}
		response.WriteJSON(w, http.StatusOK, recipes)
		h.broker.Notify()
		return
	}

	id := seg[0]

	switch {
	case len(seg) == 1:
		switch r.Method {
		case http.MethodPatch:
			var body struct {
				Name    *string `json:"name"`
				Enabled *bool   `json:"enabled"`
			}
			// Both fields are pointers, so a failed decode leaves both nil —
			// indistinguishable from a legitimate no-op patch. Without this
			// guard, PATCH {"enabled":"yes"} would answer 200 and tell the
			// client the write succeeded.
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				response.WriteError(w, http.StatusBadRequest, "invalid body")
				return
			}
			recipe, items, err := h.store.PatchRecipe(id, body.Name, body.Enabled)
			if err != nil {
				writeRecipeError(w, err)
				return
			}
			response.WriteJSON(w, http.StatusOK, map[string]any{"recipe": recipe, "items": items})
			// A patch carrying neither field wrote nothing, so there is
			// nothing to broadcast.
			if body.Name != nil || body.Enabled != nil {
				h.broker.Notify()
			}

		case http.MethodDelete:
			items, err := h.store.DeleteRecipe(id)
			if err != nil {
				writeRecipeError(w, err)
				return
			}
			response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
			h.broker.Notify()

		default:
			response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		}

	case len(seg) == 2 && seg[1] == "ingredients":
		if r.Method != http.MethodPost {
			response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// decodeName is what makes {"name":"  "} a 400 before the store is
		// reached, unlike the PATCH branch above.
		name, ok := decodeName(w, r)
		if !ok {
			return
		}
		item, err := h.store.AddIngredient(id, name)
		if err != nil {
			writeRecipeError(w, err)
			return
		}
		response.WriteJSON(w, http.StatusCreated, item)
		h.broker.Notify()

	case len(seg) == 3 && seg[1] == "ingredients":
		if r.Method != http.MethodDelete {
			response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := h.store.DeleteIngredient(id, seg[2]); err != nil {
			writeRecipeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		h.broker.Notify()

	default:
		response.WriteError(w, http.StatusNotFound, "not found")
	}
}

// decodeName reads {"name":"..."} from the request body.
func decodeName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return "", false
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		response.WriteError(w, http.StatusBadRequest, "name is required")
		return "", false
	}
	return name, true
}
