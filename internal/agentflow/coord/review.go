package coord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// ReviewRequest is one seat delivery handed to the review API (AF-14).
type ReviewRequest struct {
	Project      string
	RepoPath     string
	Branch       string
	Base         string
	TaskID       string
	Title        string
	Objective    string
	Acceptance   string
	Exclusions   string
	AuthorModel  string
	AuthorFamily string
	NotifyRoom   string
	NotifyPrefix string
}

// ReviewResult is the finished review the API returns.
type ReviewResult struct {
	ID       int64  `json:"id"`
	Verdict  string `json:"verdict"`
	Findings string `json:"findings"`
}

// ReviewClient is the coordinator's view of the review API. Tests replace it;
// production uses HTTPReviewClient.
type ReviewClient interface {
	ProjectForRepo(ctx context.Context, repoPath string) (string, error)
	CreateReview(ctx context.Context, req ReviewRequest) (ReviewResult, error)
}

// HTTPReviewClient calls the agentflow review API. A review runs verify and a
// reviewer stage, so its timeout is deliberately long.
type HTTPReviewClient struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *HTTPReviewClient) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 45 * time.Minute}
}

func (c *HTTPReviewClient) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("review api: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("review api: %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// ProjectForRepo finds the agentflow project registered on repoPath.
func (c *HTTPReviewClient) ProjectForRepo(ctx context.Context, repoPath string) (string, error) {
	var projects []struct {
		Slug     string `json:"slug"`
		RepoPath string `json:"repo_path"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/projects", nil, &projects); err != nil {
		return "", err
	}
	want := filepath.Clean(repoPath)
	for _, p := range projects {
		if filepath.Clean(p.RepoPath) == want {
			return p.Slug, nil
		}
	}
	return "", fmt.Errorf("no agentflow project uses %s", repoPath)
}

// CreateReview asks for a review and waits for its verdict.
func (c *HTTPReviewClient) CreateReview(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	body := map[string]any{
		"project": req.Project, "repo_path": req.RepoPath, "branch": req.Branch, "base": req.Base,
		"task": map[string]any{"id": req.TaskID, "title": req.Title, "objective": req.Objective,
			"acceptance": req.Acceptance, "exclusions": req.Exclusions},
		"author_model": req.AuthorModel, "author_family": req.AuthorFamily,
	}
	if req.NotifyRoom != "" {
		body["notify"] = map[string]string{"botchat_room": req.NotifyRoom, "prefix": req.NotifyPrefix}
	}
	var out ReviewResult
	err := c.do(ctx, http.MethodPost, "/api/reviews", body, &out)
	return out, err
}

// agentflowReview asks the review API for a seat delivery's review and records
// its verdict in state.json exactly as a seat's verdict is recorded. It finds
// the branch in the DONE message, the project by the repository path, and the
// author family from the seat callsign.
func (r *run) agentflowReview(ctx context.Context, t Task, d map[string]*string, all []Msg) {
	refs := BranchesIn(doneMessage(t, all), r.cfg.Base, func(sha string) []string { return r.c.Git.Contains(ctx, sha) }, "")
	if len(refs) == 0 {
		r.alert(t.ID, "%s has no branch in its DONE message for an agentflow review", t.ID)
		return
	}
	var ref, repo string
	for _, cand := range refs {
		for _, rp := range r.cfg.Repos {
			if r.c.Git.RemoteHas(ctx, rp, cand.Branch) {
				ref, repo = cand.Branch, rp.Path
				break
			}
		}
		if repo != "" {
			break
		}
	}
	if repo == "" {
		r.alert(t.ID, "no configured repository has the branch of %s", t.ID)
		return
	}
	project, err := r.c.Reviews.ProjectForRepo(ctx, repo)
	if err != nil {
		r.alert(t.ID, "agentflow review of %s: %v", t.ID, err)
		return
	}
	res, err := r.c.Reviews.CreateReview(ctx, ReviewRequest{
		Project: project, RepoPath: repo, Branch: ref, Base: r.cfg.Base, TaskID: t.ID,
		Title: t.Title, Objective: t.Objective, Acceptance: t.AcceptanceCriteria, Exclusions: t.Exclusions,
		AuthorFamily: r.cfg.family(t.Assignee),
		NotifyRoom:   r.cfg.ReviewRoom, NotifyPrefix: "REVIEW " + t.ID + ":",
	})
	if err != nil {
		r.alert(t.ID, "agentflow review of %s failed: %v", t.ID, err)
		return
	}
	id := fmt.Sprintf("AF%d", res.ID)
	set(d, "review", id)
	r.decide("review_recorded", t.ID, "agentflow", "%s for %s", res.Verdict, id)
	switch res.Verdict {
	case "PASS":
		set(d, "pass", id)
	case "CHANGES-REQUESTED":
		set(d, "cr", id)
		set(d, "status", "rework")
		who := t.ClaimedBy
		if who == "" {
			who = t.Assignee
		}
		r.decide("rework", t.ID, who, "agentflow review %s asked for changes", id)
		r.transition(ctx, t.ID, "rework", "Agentflow review "+id+" CHANGES-REQUESTED: "+strings.ReplaceAll(clipStr(res.Findings, 900), "\n", " "), who)
	default:
		r.alert(t.ID, "agentflow review %s returned no verdict", id)
	}
}
