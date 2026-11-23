package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"cmd184psu/unified-webapp/internal/botchat/apikey"
	"cmd184psu/unified-webapp/internal/botchat/auth"
	"cmd184psu/unified-webapp/internal/botchat/chat"
	"cmd184psu/unified-webapp/internal/botchat/watcher"
)

type Options struct {
	Auth   auth.Provider
	KeySvc *apikey.Service
	Store  *chat.Store
	Hub    *chat.Hub

	WebhookURL    string
	WebhookSecret string
}

type Server struct {
	auth          auth.Provider
	keySvc        *apikey.Service
	store         *chat.Store
	hub           *chat.Hub
	mux           *http.ServeMux
	upgrader      websocket.Upgrader
	webhookURL    string
	webhookSecret string
	webhookClient *http.Client
}

type apiKeyContextKey struct{}

func New(opts Options) *Server {
	s := &Server{
		auth:          opts.Auth,
		keySvc:        opts.KeySvc,
		store:         opts.Store,
		hub:           opts.Hub,
		mux:           http.NewServeMux(),
		webhookURL:    opts.WebhookURL,
		webhookSecret: opts.WebhookSecret,
		webhookClient: &http.Client{Timeout: 10 * time.Second},
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/auth/mode", s.handleAuthMode)

	s.mux.HandleFunc("GET /api/admin/keys", s.handleListKeys)
	s.mux.HandleFunc("POST /api/admin/keys", s.handleMintKey)
	s.mux.HandleFunc("DELETE /api/admin/keys/{id}", s.handleRevokeKey)
	s.mux.HandleFunc("POST /api/admin/callsigns/release", s.handleReleaseCallsign)
	s.mux.HandleFunc("GET /api/admin/rooms/{id}/messages", s.handleAdminRoomMessages)
	s.mux.HandleFunc("DELETE /api/admin/rooms/{id}", s.handleAdminDeleteRoom)

	s.mux.Handle("POST /api/v1/callsign", s.requireAPIKey(http.HandlerFunc(s.handleRegisterCallsign)))
	s.mux.Handle("DELETE /api/v1/callsign", s.requireAPIKey(http.HandlerFunc(s.handleUnregisterCallsign)))
	s.mux.HandleFunc("GET /api/v1/callsigns", s.handleListCallsigns)
	s.mux.Handle("POST /api/v1/heartbeat", s.requireAPIKey(http.HandlerFunc(s.handleHeartbeat)))
	s.mux.Handle("POST /api/v1/keys/child", s.requireAPIKey(http.HandlerFunc(s.handleMintChildKey)))
	s.mux.HandleFunc("GET /api/v1/rooms", s.handleListRooms)
	s.mux.Handle("POST /api/v1/rooms", s.requireAPIKey(http.HandlerFunc(s.handleCreateRoom)))
	s.mux.Handle("DELETE /api/v1/rooms/{id}", s.requireAPIKey(http.HandlerFunc(s.handleDeleteRoom)))
	s.mux.Handle("GET /api/v1/rooms/{id}/messages", s.requireAPIKey(http.HandlerFunc(s.handleGetMessages)))
	s.mux.Handle("POST /api/v1/rooms/{id}/messages", s.requireAPIKey(http.HandlerFunc(s.handlePostMessage)))

	s.mux.Handle("GET /api/v1/inbox", s.requireAPIKey(http.HandlerFunc(s.handleInbox)))
	s.mux.Handle("GET /api/v1/resume", s.requireAPIKey(http.HandlerFunc(s.handleResume)))
	s.mux.Handle("POST /api/v1/inbox/ack", s.requireAPIKey(http.HandlerFunc(s.handleAckInbox)))

	s.mux.HandleFunc("GET /api/v1/tasks", s.handleListTasks)
	s.mux.HandleFunc("GET /api/v1/tasks/awaiting-review", s.handleTasksAwaitingReview)
	s.mux.HandleFunc("GET /api/v1/tasks/blocked-on", s.handleTasksBlockedOn)
	s.mux.Handle("POST /api/v1/tasks", s.requireAPIKey(http.HandlerFunc(s.handleCreateTask)))
	s.mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleGetTask)
	s.mux.Handle("POST /api/v1/tasks/{id}/transition", s.requireAPIKey(http.HandlerFunc(s.handleTransitionTask)))
	s.mux.Handle("POST /api/v1/tasks/{id}/revise", s.requireAPIKey(http.HandlerFunc(s.handleReviseTask)))

	s.mux.HandleFunc("GET /api/v1/plans", s.handleListPlans)
	s.mux.Handle("POST /api/v1/plans", s.requireAPIKey(http.HandlerFunc(s.handleCreatePlan)))
	s.mux.HandleFunc("GET /api/v1/plans/{id}", s.handleGetPlan)
	s.mux.HandleFunc("GET /api/v1/plans/{id}/tasks", s.handlePlanTasks)
	s.mux.HandleFunc("GET /api/v1/plans/{id}/eligible", s.handleEligibleTasks)
	s.mux.Handle("POST /api/v1/plans/{id}/revise", s.requireAPIKey(http.HandlerFunc(s.handleRevisePlan)))
	// PATCH is an alias for revise: agents reaching for REST-style PATCH get the
	// same planner revision instead of falling through to the UI shell with a
	// misleading 200 (which silently discards the update).
	s.mux.Handle("PATCH /api/v1/plans/{id}", s.requireAPIKey(http.HandlerFunc(s.handleRevisePlan)))
	s.mux.Handle("POST /api/v1/plans/{id}/close", s.requireAPIKey(http.HandlerFunc(s.handleClosePlan)))
	s.mux.Handle("POST /api/v1/plans/{id}/claim", s.requireAPIKey(http.HandlerFunc(s.handleClaimNextTask)))

	s.mux.HandleFunc("GET /api/v1/ws/{roomId}", s.handleWS)

	s.mux.HandleFunc("GET /docs", s.handleDocs)
	s.mux.HandleFunc("GET /docs/", s.handleDocs)

	if dist, ok := DistFS(); ok {
		s.mux.Handle("/", spaHandler(dist))
	}
}

func (s *Server) Handler() http.Handler {
	if s.auth != nil {
		return s.auth.Middleware(s.mux)
	}
	return s.mux
}

func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.keySvc == nil {
			writeError(w, http.StatusServiceUnavailable, "api keys unavailable")
			return
		}
		token := extractAPIKeyToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing api key")
			return
		}
		k, err := s.keySvc.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid api key")
			return
		}
		ctx := context.WithValue(r.Context(), apiKeyContextKey{}, k)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractAPIKeyToken(r *http.Request) string {
	if v := r.Header.Get("Authorization"); v != "" {
		if strings.HasPrefix(v, "Bearer ") {
			return strings.TrimPrefix(v, "Bearer ")
		}
	}
	if v := r.Header.Get("X-API-Key"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("token"); v != "" {
		return v
	}
	return ""
}

func apiKeyFromContext(ctx context.Context) (apikey.Key, bool) {
	k, ok := ctx.Value(apiKeyContextKey{}).(apikey.Key)
	return k, ok
}

func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		// The API namespace never serves the UI shell: an unknown /api/* path
		// or method is a caller bug and must fail loudly as JSON, not return
		// the index page with a 200 that looks like success.
		if p == "api" || strings.HasPrefix(p, "api/") {
			writeError(w, http.StatusNotFound, "unknown api route")
			return
		}
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(fsys, p); err != nil {
			r2 := new(http.Request)
			*r2 = *r
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// splitCSV parses a comma-separated query value into a trimmed, non-empty slice.
// An empty or whitespace-only input yields nil so callers can treat "no value"
// and "empty list" identically.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// webhookEventType maps a task action to the webhook event type. Returns
// empty string for actions that don't warrant a webhook.
func webhookEventType(action string) string {
	switch action {
	case "promote":
		return "task.ready"
	case "claim", "handoff":
		return "task.assigned"
	case "answer":
		return "task.answered"
	case "done":
		return "task.done"
	case "accept":
		return "task.accepted"
	default:
		return ""
	}
}

// fireWebhook emits a signed webhook event for a task state change. It is
// fire-and-forget: the POST runs in a goroutine and errors are logged but
// never returned to the caller. The webhook is skipped if no URL is
// configured. The target callsign is the agent who should act next —
// the assignee for assigned/answered/done, the creator for accepted.
func (s *Server) fireWebhook(eventType, targetCallsign string, task chat.Task) {
	if s.webhookURL == "" || s.webhookSecret == "" || targetCallsign == "" {
		return
	}
	snap := s.store.Resume(targetCallsign, "", 50)
	eventID := fmt.Sprintf("%s:%s:%d", eventType, task.ID, time.Now().UnixNano())
	evt := watcher.WebhookEvent{
		EventID:    eventID,
		EventType:  eventType,
		Callsign:   targetCallsign,
		TaskID:     task.ID,
		Timestamp:  time.Now().Unix(),
		Actionable: snap.Actionable,
		RecommendedNextAction: watcher.RecommendedNextAction{
			Action: snap.RecommendedNextAction.Action,
			TaskID: snap.RecommendedNextAction.TaskID,
			PlanID: snap.RecommendedNextAction.PlanID,
			Reason: snap.RecommendedNextAction.Reason,
		},
	}
	body, err := json.Marshal(evt)
	if err != nil {
		log.Printf("webhook: marshal: %v", err)
		return
	}
	sig := watcher.SignWebhook(body, s.webhookSecret)
	go func() {
		req, err := http.NewRequest("POST", s.webhookURL, bytes.NewReader(body))
		if err != nil {
			log.Printf("webhook: build request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BotChat-Signature", sig)
		req.Header.Set("X-BotChat-Event", eventType)
		resp, err := s.webhookClient.Do(req)
		if err != nil {
			log.Printf("webhook: POST %s: %v", s.webhookURL, err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Printf("webhook: POST %s: HTTP %d", s.webhookURL, resp.StatusCode)
		}
	}()
}
