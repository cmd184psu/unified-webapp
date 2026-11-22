package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"cmd184psu/unified-webapp/internal/botchat/apikey"
	"cmd184psu/unified-webapp/internal/botchat/chat"
)

type authModeDTO struct {
	Mode string `json:"mode"`
}

type apiKeyDTO struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt int64  `json:"lastUsedAt,omitempty"`
	RevokedAt  int64  `json:"revokedAt,omitempty"`
}

func (s *Server) handleAuthMode(w http.ResponseWriter, r *http.Request) {
	mode := "none"
	if s.auth != nil {
		mode = s.auth.Mode()
	}
	writeJSON(w, http.StatusOK, authModeDTO{Mode: mode})
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	if s.keySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "api keys unavailable")
		return
	}
	keys, err := s.keySvc.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]apiKeyDTO, 0, len(keys))
	for _, k := range keys {
		out = append(out, toAPIKeyDTO(k))
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func (s *Server) handleMintKey(w http.ResponseWriter, r *http.Request) {
	if s.keySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "api keys unavailable")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	result, err := s.keySvc.Mint(r.Context(), in.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":   toAPIKeyDTO(result.Key),
		"token": result.Token,
	})
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	if s.keySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "api keys unavailable")
		return
	}
	if err := s.keySvc.Revoke(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "api key not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRegisterCallsign(w http.ResponseWriter, r *http.Request) {
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		Callsign string `json:"callsign"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Callsign == "" {
		writeError(w, http.StatusBadRequest, "callsign is required")
		return
	}
	suggestion, taken := s.store.SuggestIfTaken(in.Callsign, k.ID)
	c, err := s.store.RegisterCallsign(in.Callsign, k.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	resp := map[string]any{
		"callsign":   c.Name,
		"instanceId": c.InstanceID,
	}
	if taken {
		resp["suggestion"] = suggestion
		resp["note"] = "callsign is already in use by another agent; it is yours regardless (impersonation is allowed), but you may prefer the suggested distinct name"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUnregisterCallsign(w http.ResponseWriter, r *http.Request) {
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	if err := s.store.UnregisterCallsign(k.ID); err != nil {
		writeError(w, http.StatusNotFound, "no callsign registered for this key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListCallsigns(w http.ResponseWriter, r *http.Request) {
	out := make([]callsignEntry, 0)
	for _, c := range s.store.ListCallsignInfo() {
		out = append(out, toCallsignEntry(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"callsigns": out})
}

type callsignEntry struct {
	Callsign       string    `json:"callsign"`
	InstanceID     string    `json:"instanceId"`
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
	Stale          bool      `json:"stale"`
	Presence       string    `json:"presence"`
	WaitingOn      string    `json:"waitingOn,omitempty"`
	CurrentTask    string    `json:"currentTask,omitempty"`
}

func toCallsignEntry(c chat.CallsignInfo) callsignEntry {
	return callsignEntry{
		Callsign:       c.Callsign,
		InstanceID:     c.InstanceID,
		CreatedAt:      c.CreatedAt,
		LastActivityAt: c.LastActivityAt,
		Stale:          c.Stale,
		Presence:       c.Presence,
		WaitingOn:      c.WaitingOn,
		CurrentTask:    c.CurrentTask,
	}
}

// handleHeartbeat lets an agent report liveness without posting chatter. It
// bumps the (key, callsign) lease's last-activity timestamp and echoes back the
// agent's computed presence. The shared API key authorizes the call; the body's
// "callsign" (your identity) is the lease that gets touched — so a heartbeat
// from BrainStorm on a key registered to HarnessBuilder bumps BrainStorm, not
// HarnessBuilder.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		Callsign string `json:"callsign"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Callsign == "" {
		writeError(w, http.StatusBadRequest, `"callsign" is required on a heartbeat — the API key is shared authorization, not your identity`)
		return
	}
	if _, ok := s.store.GetCallsign(k.ID, in.Callsign); !ok {
		writeError(w, http.StatusNotFound, "no lease for that callsign under this key — POST /api/v1/callsign first")
		return
	}
	s.store.TouchCallsign(k.ID, in.Callsign)
	info, ok := s.store.CallsignInfoForKey(k.ID, in.Callsign)
	if !ok {
		writeError(w, http.StatusNotFound, "no lease for that callsign under this key")
		return
	}
	writeJSON(w, http.StatusOK, toCallsignEntry(info))
}

// handleReleaseCallsign is an operator control to reclaim a callsign lease. It
// accepts either an instanceId (preferred — releases exactly one lease) or a
// callsign name (releases every lease using that name). Releasing never affects
// historical messages, which retain their original display callsign.
func (s *Server) handleReleaseCallsign(w http.ResponseWriter, r *http.Request) {
	var in struct {
		InstanceID string `json:"instanceId"`
		Callsign   string `json:"callsign"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (in.InstanceID == "" && in.Callsign == "") {
		writeError(w, http.StatusBadRequest, "instanceId or callsign is required")
		return
	}
	if in.InstanceID != "" {
		c, ok := s.store.ReleaseCallsignByInstance(in.InstanceID)
		if !ok {
			writeError(w, http.StatusNotFound, "no lease with that instanceId")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"released": 1, "callsign": c.Name, "instanceId": c.InstanceID})
		return
	}
	n := s.store.ReleaseCallsignByName(in.Callsign)
	if n == 0 {
		writeError(w, http.StatusNotFound, "no lease with that callsign")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"released": n, "callsign": in.Callsign})
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	s.writeRoomMessages(w, r, r.PathValue("id"))
}

// writeRoomMessages writes a room's history as {messages,cursor}, honouring the
// optional after=<seq> cursor and limit=<n> query params. Shared by the
// API-key message read (agents) and the session-authenticated operator read
// (the web UI, which does not hold the shared key).
func (s *Server) writeRoomMessages(w http.ResponseWriter, r *http.Request, roomID string) {
	var after int64
	if a := r.URL.Query().Get("after"); a != "" {
		if _, err := fmt.Sscanf(a, "%d", &after); err != nil || after < 0 {
			writeError(w, http.StatusBadRequest, `"after" must be a non-negative message seq cursor`)
			return
		}
	}
	msgs, err := s.store.MessagesAfter(roomID, after)
	if err != nil {
		if errors.Is(err, chat.ErrRoomNotFound) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if msgs == nil {
		msgs = []chat.Message{}
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		var limit int
		if _, err := fmt.Sscanf(limitStr, "%d", &limit); err == nil && limit > 0 && limit < len(msgs) {
			msgs = msgs[len(msgs)-limit:]
		}
	}
	var cursor int64
	if n := len(msgs); n > 0 {
		cursor = msgs[n-1].Seq
	} else {
		cursor = after
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "cursor": cursor})
}

// handleAdminRoomMessages serves a room's history to a logged-in operator over
// session auth, so the web UI can do an authoritative REST load and cursor
// reconciliation (and recover from a silently dropped WebSocket) without
// holding the shared API key. Read-only.
func (s *Server) handleAdminRoomMessages(w http.ResponseWriter, r *http.Request) {
	s.writeRoomMessages(w, r, r.PathValue("id"))
}

// handleAdminDeleteRoom lets a logged-in operator drop a leftover wave room from
// the UI. Unlike the agent-facing DELETE (restricted to the moderator key), this
// is a human-operator action gated by the session middleware; the global room is
// protected by the store.
func (s *Server) handleAdminDeleteRoom(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetRoom(id); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if err := s.store.DeleteRoom(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastRoomDeleted(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	callsign := r.URL.Query().Get("callsign")
	if callsign == "" {
		writeError(w, http.StatusBadRequest, `"callsign" query parameter is required — the inbox is scoped to your callsign, not the shared key`)
		return
	}
	var after int64
	if a := r.URL.Query().Get("after"); a != "" {
		if _, err := fmt.Sscanf(a, "%d", &after); err != nil || after < 0 {
			writeError(w, http.StatusBadRequest, `"after" must be a non-negative message seq cursor`)
			return
		}
	}
	var limit int
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := fmt.Sscanf(l, "%d", &limit); err != nil || limit < 0 {
			writeError(w, http.StatusBadRequest, `"limit" must be a non-negative integer`)
			return
		}
	}
	msgs, cursor := s.store.Inbox(callsign, after, limit)
	if msgs == nil {
		msgs = []chat.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": msgs,
		"cursor":   cursor,
		"unread":   len(msgs),
	})
}

// handleResume returns the consolidated, read-only resume snapshot for the
// calling agent: its server-side ack cursor, unacknowledged directed inbox,
// active/needs-attention/awaiting-review/blocked-on tasks, and current task.
// It folds the §9 resume checklist steps 1–4 into one call. It does NOT
// advance the ack cursor — only POST /api/v1/inbox/ack does. The agent
// supplies its callsign (identity) and instanceId (the agentInstance from
// register/heartbeat); the composite (normalized callsign, agentInstance) is
// the cursor key, so same-key/same-callsign replacement inherits the cursor
// while a different callsign on the same key starts fresh.
func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	callsign := r.URL.Query().Get("callsign")
	if callsign == "" {
		writeError(w, http.StatusBadRequest, `"callsign" query parameter is required — resume is scoped to your callsign, not the shared key`)
		return
	}
	instanceID := r.URL.Query().Get("instanceId")
	if instanceID == "" {
		writeError(w, http.StatusBadRequest, `"instanceId" query parameter is required — the durable ack cursor is keyed by (callsign, agentInstance), so the server can attribute your resume state to the right agent instance. Get your instanceId from POST /api/v1/callsign or POST /api/v1/heartbeat.`)
		return
	}
	var limit int
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := fmt.Sscanf(l, "%d", &limit); err != nil || limit < 0 {
			writeError(w, http.StatusBadRequest, `"limit" must be a non-negative integer`)
			return
		}
	}
	snap := s.store.Resume(callsign, instanceID, limit)
	writeJSON(w, http.StatusOK, snap)
}

// handleAckInbox is the explicit mutation that advances the server-side inbox
// cursor for (callsign, instanceId) to max(existing, seq). After an agent
// processes the inbox slice returned by GET /api/v1/resume, it ACKs the
// highest seq it processed so the next resume does not re-deliver those
// events. Monotonic: a stale/lower seq never moves the cursor backwards.
func (s *Server) handleAckInbox(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		Callsign   string `json:"callsign"`
		InstanceID string `json:"instanceId"`
		Seq        int64  `json:"seq"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Callsign == "" {
		writeError(w, http.StatusBadRequest, `"callsign" is required — the API key is shared authorization, not your identity`)
		return
	}
	if in.InstanceID == "" {
		writeError(w, http.StatusBadRequest, `"instanceId" is required — the durable ack cursor is keyed by (callsign, agentInstance)`)
		return
	}
	ackSeq := s.store.AckInbox(in.Callsign, in.InstanceID, in.Seq)
	writeJSON(w, http.StatusOK, map[string]any{
		"callsign":   in.Callsign,
		"instanceId": in.InstanceID,
		"ackSeq":     ackSeq,
	})
}

func (s *Server) handleMintChildKey(w http.ResponseWriter, r *http.Request) {
	if s.keySvc == nil {
		writeError(w, http.StatusServiceUnavailable, "api keys unavailable")
		return
	}
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	childName := k.Name + "/" + in.Name
	result, err := s.keySvc.Mint(r.Context(), childName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":   toAPIKeyDTO(result.Key),
		"token": result.Token,
	})
}

func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	rooms := s.store.ListRooms()
	if rooms == nil {
		rooms = []chat.Room{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		Name   string `json:"name"`
		WaveID string `json:"waveId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	room, err := s.store.CreateRoom(in.Name, in.WaveID, k.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, room)
}

func (s *Server) handleDeleteRoom(w http.ResponseWriter, r *http.Request) {
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	id := r.PathValue("id")
	room, err := s.store.GetRoom(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if room.ModeratorKeyID == "" || room.ModeratorKeyID != k.ID {
		writeError(w, http.StatusForbidden, "only the moderator can delete this room")
		return
	}
	if err := s.store.DeleteRoom(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastRoomDeleted(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	roomID := r.PathValue("id")
	var in struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.From == "" {
		writeError(w, http.StatusBadRequest, `"from" (your callsign) is required on every message — the API key is shared authorization, not your identity`)
		return
	}
	if in.To == "" {
		writeError(w, http.StatusBadRequest, `"to" is required on every message — name a callsign or use "everyone" to address the whole room`)
		return
	}
	if in.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	msg, err := s.store.PostMessage(roomID, in.From, k.ID, in.Content, in.To)
	if err != nil {
		if errors.Is(err, chat.ErrRoomNotFound) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.hub.Broadcast(roomID, msg)
	writeJSON(w, http.StatusCreated, msg)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		From               string   `json:"from"`
		RoomID             string   `json:"roomId"`
		PlanID             string   `json:"planId"`
		Title              string   `json:"title"`
		Objective          string   `json:"objective"`
		Assignee           string   `json:"assignee"`
		AcceptanceCriteria string   `json:"acceptanceCriteria"`
		Exclusions         string   `json:"exclusions"`
		DependsOn          []string `json:"dependsOn"`
		Capabilities       []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.From == "" {
		writeError(w, http.StatusBadRequest, `"from" (your callsign) is required — the API key is shared authorization, not your identity`)
		return
	}
	// A plan-managed task inherits its room from the plan, so roomId is optional
	// when planId is supplied; otherwise roomId is required.
	if in.RoomID == "" && in.PlanID == "" {
		writeError(w, http.StatusBadRequest, `"roomId" is required (or supply "planId" to add the task to a plan)`)
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, `"title" is required`)
		return
	}
	task, card, err := s.store.CreateTask(chat.Task{
		RoomID:             in.RoomID,
		PlanID:             in.PlanID,
		Title:              in.Title,
		Objective:          in.Objective,
		Assignee:           in.Assignee,
		Creator:            in.From,
		AcceptanceCriteria: in.AcceptanceCriteria,
		Exclusions:         in.Exclusions,
		DependsOn:          in.DependsOn,
		Capabilities:       in.Capabilities,
	})
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrRoomNotFound):
			writeError(w, http.StatusNotFound, "room not found")
		case errors.Is(err, chat.ErrPlanNotFound):
			writeError(w, http.StatusNotFound, "plan not found")
		case errors.Is(err, chat.ErrPlanClosed):
			writeError(w, http.StatusConflict, "plan is closed")
		case errors.Is(err, chat.ErrTaskConflict):
			writeError(w, http.StatusConflict, "assignee already has an active task")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	s.hub.Broadcast(card.RoomID, card)
	if in.Assignee != "" {
		s.fireWebhook("task.assigned", in.Assignee, task)
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := s.store.ListTasks(chat.TaskFilter{
		RoomID:   r.URL.Query().Get("roomId"),
		Assignee: r.URL.Query().Get("assignee"),
		Status:   r.URL.Query().Get("status"),
	})
	if tasks == nil {
		tasks = []chat.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.GetTask(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleTransitionTask(w http.ResponseWriter, r *http.Request) {
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		From              string   `json:"from"`
		Action            string   `json:"action"`
		Note              string   `json:"note"`
		WaitingOn         string   `json:"waitingOn"`
		CompletedPortions string   `json:"completedPortions"`
		ChangedFiles      []string `json:"changedFiles"`
		Tests             string   `json:"tests"`
		DeploymentTarget  string   `json:"deploymentTarget"`
		ServiceHealth     string   `json:"serviceHealth"`
		EvidenceIDs       []string `json:"evidenceIds"`
		Limitations       string   `json:"limitations"`
		NewAssignee       string   `json:"newAssignee"`
		Assignee          string   `json:"assignee"`
		ExpectedVersion   int      `json:"expectedVersion"`
		Override          bool     `json:"override"`
		OverrideReason    string   `json:"overrideReason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.From == "" {
		writeError(w, http.StatusBadRequest, `"from" (your callsign) is required`)
		return
	}
	if in.Action == "" {
		writeError(w, http.StatusBadRequest, `"action" is required (one of: promote, claim, acknowledge, start, block, answer, done, accept, rework, close, cancel, handoff, ack_revision, release, defer, reinstate)`)
		return
	}
	newAssignee := in.NewAssignee
	if newAssignee == "" {
		newAssignee = in.Assignee
	}
	if in.Action == "handoff" && newAssignee == "" {
		writeError(w, http.StatusBadRequest, `"newAssignee" (the callsign taking over) is required for a handoff`)
		return
	}
	task, card, err := s.store.TransitionTask(r.PathValue("id"), in.Action, in.From, chat.TransitionInput{
		Note:              in.Note,
		WaitingOn:         in.WaitingOn,
		CompletedPortions: in.CompletedPortions,
		ChangedFiles:      in.ChangedFiles,
		Tests:             in.Tests,
		DeploymentTarget:  in.DeploymentTarget,
		ServiceHealth:     in.ServiceHealth,
		EvidenceIDs:       in.EvidenceIDs,
		Limitations:       in.Limitations,
		NewAssignee:       newAssignee,
		ExpectedVersion:   in.ExpectedVersion,
		Override:          in.Override,
		OverrideReason:    in.OverrideReason,
		KeyID:             k.ID,
	})
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrTaskNotFound):
			writeError(w, http.StatusNotFound, "task not found")
		case errors.Is(err, chat.ErrVersionConflict):
			writeError(w, http.StatusConflict, "task version conflict — your view is stale, re-fetch the task and retry")
		case errors.Is(err, chat.ErrClaimReserved):
			writeError(w, http.StatusConflict, "task is reserved for another callsign")
		case errors.Is(err, chat.ErrDependenciesPending):
			writeError(w, http.StatusConflict, "task has unmet dependencies — the tasks it depends on are not yet accepted/closed")
		case errors.Is(err, chat.ErrConcurrencyFull):
			writeError(w, http.StatusConflict, "plan concurrency limit reached — finish/close an active task first")
		case errors.Is(err, chat.ErrOverrideForbidden):
			writeError(w, http.StatusForbidden, "override is allowed only by the plan's planner and requires overrideReason")
		case errors.Is(err, chat.ErrRevisionPending):
			writeError(w, http.StatusConflict, "task specification was revised — ack_revision before marking it done")
		case errors.Is(err, chat.ErrNoRevisionPending):
			writeError(w, http.StatusConflict, "no pending specification revision to acknowledge")
		case errors.Is(err, chat.ErrWaitingOnMismatch):
			writeError(w, http.StatusForbidden, "you are not the agent this task is waiting on — only the waitingOn callsign may answer")
		case errors.Is(err, chat.ErrAnswerEmpty):
			writeError(w, http.StatusBadRequest, `answer requires a non-empty "note" with the resolution`)
		case errors.Is(err, chat.ErrInvalidTransition):
			writeError(w, http.StatusConflict, fmt.Sprintf("cannot %q a task in state %q", in.Action, task.State))
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	// An idempotent answer retry returns the task with a zero-value card (no
	// new broadcast) — the first answer's card is already in the transcript.
	if card.ID != "" {
		s.hub.Broadcast(card.RoomID, card)
	}
	// Fire a signed webhook for actionable state changes. The target is the
	// agent who should act next: the assignee for most transitions, the
	// creator for accept (the creator owes the close).
	if evtType := webhookEventType(in.Action); evtType != "" {
		target := task.Assignee
		if in.Action == "accept" {
			target = task.Creator
		}
		if in.Action == "promote" {
			// A ready task is claimable by any available agent — target the
			// plan's planner so the watcher can dispatch.
			target = task.Creator
		}
		s.fireWebhook(evtType, target, task)
	}
	writeJSON(w, http.StatusOK, task)
}

// handleReviseTask revises a task's specification in place (no lifecycle change).
// Every field is optional; an omitted field is left unchanged, a supplied one is
// applied (including clearing it with an empty value). For a plan-managed task
// only the plan's planner may revise.
func (s *Server) handleReviseTask(w http.ResponseWriter, r *http.Request) {
	k, ok := apiKeyFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		From               string   `json:"from"`
		Title              *string  `json:"title"`
		Objective          *string  `json:"objective"`
		Assignee           *string  `json:"assignee"`
		AcceptanceCriteria *string  `json:"acceptanceCriteria"`
		Exclusions         *string  `json:"exclusions"`
		DependsOn          []string `json:"dependsOn"`
		Capabilities       []string `json:"capabilities"`
		Note               string   `json:"note"`
		ExpectedVersion    int      `json:"expectedVersion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.From == "" {
		writeError(w, http.StatusBadRequest, `"from" (your callsign) is required — the API key is shared authorization, not your identity`)
		return
	}
	task, card, err := s.store.ReviseTask(r.PathValue("id"), in.From, chat.TaskRevision{
		Title:              in.Title,
		Objective:          in.Objective,
		Assignee:           in.Assignee,
		AcceptanceCriteria: in.AcceptanceCriteria,
		Exclusions:         in.Exclusions,
		DependsOn:          in.DependsOn,
		Capabilities:       in.Capabilities,
		Note:               in.Note,
		ExpectedVersion:    in.ExpectedVersion,
		KeyID:              k.ID,
	})
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrTaskNotFound):
			writeError(w, http.StatusNotFound, "task not found")
		case errors.Is(err, chat.ErrVersionConflict):
			writeError(w, http.StatusConflict, "task version conflict — your view is stale, re-fetch the task and retry")
		case errors.Is(err, chat.ErrOverrideForbidden):
			writeError(w, http.StatusForbidden, "only the plan's planner may revise a plan-managed task")
		case errors.Is(err, chat.ErrInvalidTransition):
			writeError(w, http.StatusConflict, "nothing to revise (task is terminal or no fields changed)")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	s.hub.Broadcast(card.RoomID, card)
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		From                string `json:"from"`
		RoomID              string `json:"roomId"`
		Title               string `json:"title"`
		Objective           string `json:"objective"`
		MaxConcurrentActive int    `json:"maxConcurrentActive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.From == "" {
		writeError(w, http.StatusBadRequest, `"from" (your callsign) is required — the API key is shared authorization, not your identity`)
		return
	}
	if in.RoomID == "" {
		writeError(w, http.StatusBadRequest, `"roomId" is required`)
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, `"title" is required`)
		return
	}
	plan, err := s.store.CreatePlan(chat.Plan{
		RoomID:              in.RoomID,
		Title:               in.Title,
		Objective:           in.Objective,
		Planner:             in.From,
		MaxConcurrentActive: in.MaxConcurrentActive,
	})
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrRoomNotFound):
			writeError(w, http.StatusNotFound, "room not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	plans := s.store.ListPlans(r.URL.Query().Get("roomId"))
	if plans == nil {
		plans = []chat.Plan{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
}

func (s *Server) handleGetPlan(w http.ResponseWriter, r *http.Request) {
	plan, err := s.store.GetPlan(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleRevisePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		Title               *string  `json:"title"`
		Objective           *string  `json:"objective"`
		MaxConcurrentActive *int     `json:"maxConcurrentActive"`
		TaskIDs             []string `json:"taskIds"`
		ExpectedRevision    int      `json:"expectedRevision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	plan, err := s.store.RevisePlan(r.PathValue("id"), chat.PlanRevision{
		Title:               in.Title,
		Objective:           in.Objective,
		MaxConcurrentActive: in.MaxConcurrentActive,
		TaskIDs:             in.TaskIDs,
		ExpectedRevision:    in.ExpectedRevision,
	})
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrPlanNotFound):
			writeError(w, http.StatusNotFound, "plan not found")
		case errors.Is(err, chat.ErrPlanClosed):
			writeError(w, http.StatusConflict, "plan is closed")
		case errors.Is(err, chat.ErrPlanRevisionStale):
			writeError(w, http.StatusConflict, "plan revision conflict — your view is stale, re-fetch the plan and retry")
		case errors.Is(err, chat.ErrInvalidTransition):
			writeError(w, http.StatusBadRequest, "reorder must be a permutation of the plan's tasks (no adds or drops)")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleClosePlan(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		From   string `json:"from"`
		Force  bool   `json:"force"`
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		// Body is optional for a clean close; ignore a decode error on empty body.
		_ = json.NewDecoder(r.Body).Decode(&in)
	}
	if in.From == "" {
		writeError(w, http.StatusBadRequest, `"from" (your callsign) is required — the API key is shared authorization, not your identity`)
		return
	}
	if in.Force && in.Reason == "" {
		writeError(w, http.StatusBadRequest, `"reason" is required when force-closing a plan with unfinished tasks`)
		return
	}
	plan, card, err := s.store.ClosePlan(r.PathValue("id"), in.From, in.Force, in.Reason)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrPlanNotFound):
			writeError(w, http.StatusNotFound, "plan not found")
		case errors.Is(err, chat.ErrPlanHasActiveWork):
			writeError(w, http.StatusConflict, "plan still has unfinished tasks — resolve or cancel them, or force-close with a reason")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	if card.ID != "" {
		s.hub.Broadcast(card.RoomID, card)
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleClaimNextTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiKeyFromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	var in struct {
		From         string   `json:"from"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.From == "" {
		writeError(w, http.StatusBadRequest, `"from" (your callsign) is required`)
		return
	}
	task, card, err := s.store.ClaimNextTask(r.PathValue("id"), in.From, in.Capabilities)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrPlanNotFound):
			writeError(w, http.StatusNotFound, "plan not found")
		case errors.Is(err, chat.ErrPlanClosed):
			writeError(w, http.StatusConflict, "plan is closed")
		case errors.Is(err, chat.ErrConcurrencyFull):
			writeError(w, http.StatusConflict, "plan concurrency limit reached — finish/close an active task first")
		case errors.Is(err, chat.ErrNoEligibleTask):
			writeError(w, http.StatusNotFound, "no eligible task to claim right now")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	s.hub.Broadcast(card.RoomID, card)
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handlePlanTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.PlanTasks(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}
	if tasks == nil {
		tasks = []chat.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleEligibleTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.EligibleTasks(
		r.PathValue("id"),
		r.URL.Query().Get("callsign"),
		splitCSV(r.URL.Query().Get("capabilities")),
	)
	if err != nil {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}
	if tasks == nil {
		tasks = []chat.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// handleTasksAwaitingReview answers "tasks awaiting my review" — tasks in the
// Done state that the caller (?callsign=) created and must accept or rework.
func (s *Server) handleTasksAwaitingReview(w http.ResponseWriter, r *http.Request) {
	callsign := r.URL.Query().Get("callsign")
	if callsign == "" {
		writeError(w, http.StatusBadRequest, `"callsign" query parameter is required`)
		return
	}
	tasks := s.store.TasksAwaitingReview(callsign)
	if tasks == nil {
		tasks = []chat.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// handleTasksBlockedOn answers "tasks blocked on me" — tasks whose progress is
// waiting on the caller (?callsign=), whether by an explicit block or by a
// dependency on the caller's unfinished work.
func (s *Server) handleTasksBlockedOn(w http.ResponseWriter, r *http.Request) {
	callsign := r.URL.Query().Get("callsign")
	if callsign == "" {
		writeError(w, http.StatusBadRequest, `"callsign" query parameter is required`)
		return
	}
	tasks := s.store.TasksBlockedOn(callsign)
	if tasks == nil {
		tasks = []chat.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	if _, err := s.store.GetRoom(roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if !s.authenticatedForWS(r) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.ServeWS(s.store, roomID, conn)
}

func (s *Server) authenticatedForWS(r *http.Request) bool {
	if s.keySvc != nil {
		if token := extractAPIKeyToken(r); token != "" {
			if _, err := s.keySvc.Authenticate(r.Context(), token); err == nil {
				return true
			}
		}
	}
	// Mode none means the platform gate already decided. The browser
	// websocket stays behind that gate, so it is not open on its own.
	if s.auth != nil && s.auth.Mode() == "none" {
		return true
	}
	return false
}

func toAPIKeyDTO(k apikey.Key) apiKeyDTO {
	out := apiKeyDTO{ID: k.ID, Name: k.Name, Prefix: k.Prefix, CreatedAt: k.CreatedAt.Unix()}
	if k.LastUsedAt != nil {
		out.LastUsedAt = k.LastUsedAt.Unix()
	}
	if k.RevokedAt != nil {
		out.RevokedAt = k.RevokedAt.Unix()
	}
	return out
}
