package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/botchat"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerReviews(mux muxer) {
	mux.HandleFunc("GET /api/reviews", a.listReviews)
	mux.HandleFunc("GET /api/reviews/{id}", a.getReview)
	mux.HandleFunc("POST /api/reviews", a.createReview)
}

type reviewTaskBody struct {
	ID         string          `json:"id"`
	Title      string          `json:"title"`
	Objective  string          `json:"objective"`
	Acceptance json.RawMessage `json:"acceptance"`
	Exclusions json.RawMessage `json:"exclusions"`
}

type reviewNotifyBody struct {
	BotchatRoom string `json:"botchat_room"`
	Prefix      string `json:"prefix"`
}

type reviewBody struct {
	Project      string            `json:"project"`
	RepoPath     string            `json:"repo_path"`
	Branch       string            `json:"branch"`
	SHA          string            `json:"sha"`
	Base         string            `json:"base"`
	Task         reviewTaskBody    `json:"task"`
	AuthorModel  string            `json:"author_model"`
	AuthorFamily string            `json:"author_family"`
	Notify       *reviewNotifyBody `json:"notify"`
}

// reviewText accepts either a string or a list of strings in the task text
// fields, because a coordinator passes acceptance criteria as a list and a
// person may pass one paragraph. The result is the text the reviewer reads.
func reviewText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s), nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, "\n- "), nil
	}
	return "", errors.New("must be a string or a list of strings")
}

// createReview is POST /api/reviews: it stores the request, runs verify and
// one independent reviewer synchronously, posts the verdict when notify names
// a room, and returns the finished record.
func (a *API) createReview(w http.ResponseWriter, r *http.Request) {
	runner := a.deps.ReviewRunner
	if runner == nil {
		runner = a.deps.Engine
	}
	if runner == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Error: "the engine is not configured"})
		return
	}
	var body reviewBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	body.Project = strings.TrimSpace(body.Project)
	body.Branch = strings.TrimSpace(body.Branch)
	body.SHA = strings.TrimSpace(body.SHA)
	body.Task.ID = strings.TrimSpace(body.Task.ID)
	if body.Task.ID == "" {
		badRequest(w, "task.id", "required")
		return
	}
	rev := strings.TrimSpace(body.Branch)
	if rev == "" {
		rev = body.SHA
	}
	if rev == "" {
		badRequest(w, "branch", "required: give branch or sha")
		return
	}
	p, err := a.deps.Store.GetProject(r.Context(), body.Project)
	if err != nil {
		writeErr(w, err)
		return
	}
	acceptance, err := reviewText(body.Task.Acceptance)
	if err != nil {
		badRequest(w, "task.acceptance", err.Error())
		return
	}
	exclusions, err := reviewText(body.Task.Exclusions)
	if err != nil {
		badRequest(w, "task.exclusions", err.Error())
		return
	}
	repo := strings.TrimSpace(body.RepoPath)
	if repo == "" {
		repo = p.RepoPath
	}
	notifyRoom, notifyPrefix := "", ""
	if body.Notify != nil {
		notifyRoom = strings.TrimSpace(body.Notify.BotchatRoom)
		notifyPrefix = strings.TrimSpace(body.Notify.Prefix)
	}
	in := model.Review{
		Project: body.Project, RepoPath: repo, Branch: rev, Base: strings.TrimSpace(body.Base),
		TaskID: body.Task.ID, Title: body.Task.Title, Objective: body.Task.Objective,
		Acceptance: acceptance, Exclusions: exclusions,
		AuthorModel: strings.TrimSpace(body.AuthorModel), AuthorFamily: strings.TrimSpace(body.AuthorFamily),
		NotifyRoom: notifyRoom, NotifyPrefix: notifyPrefix, State: "queued",
	}
	created, err := a.deps.Store.CreateReview(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	done, runErr := runner.RunReview(r.Context(), created.ID)
	if runErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"review": done, "error": runErr.Error()})
		return
	}
	if done.NotifyRoom != "" {
		if err := a.postReviewVerdict(r.Context(), done); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"review": done, "error": err.Error()})
			return
		}
	}
	a.emit("review.update", map[string]any{"review": done.ID, "project": done.Project})
	writeJSON(w, http.StatusCreated, done)
}

// postReviewVerdict posts the verdict in the configured room from the review
// callsign, starting exactly "REVIEW <task id>: PASS" or
// "REVIEW <task id>: CHANGES-REQUESTED" and followed by numbered findings.
func (a *API) postReviewVerdict(ctx context.Context, rev model.Review) error {
	set, err := a.deps.Store.GetSettings(ctx)
	if err != nil {
		return err
	}
	if set.BotchatURL == "" || set.BotchatKeyFile == "" {
		return errors.New("botchat_url and botchat_key_file must be set to notify a review verdict")
	}
	head := strings.TrimSpace(rev.NotifyPrefix)
	if head == "" {
		head = "REVIEW " + rev.TaskID
	}
	if !strings.HasSuffix(head, ":") {
		head += ":"
	}
	content := fmt.Sprintf("%s %s\n\n%s", head, rev.Verdict, rev.Findings)
	client := &botchat.Client{BaseURL: set.BotchatURL, KeyFile: set.BotchatKeyFile}
	return client.Post(ctx, rev.NotifyRoom, set.ReviewCallsign, "all", content)
}

func (a *API) listReviews(w http.ResponseWriter, r *http.Request) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	list, err := a.deps.Store.ListReviews(r.Context(), project)
	if err != nil {
		writeErr(w, err)
		return
	}
	if list == nil {
		list = []model.Review{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) getReview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(w, "id", "must be a positive integer")
		return
	}
	rev, err := a.deps.Store.GetReview(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: "no such review"})
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rev)
}
