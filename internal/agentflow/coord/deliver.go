package coord

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// afDeliver is coordinator.py's af_deliver port: when an agentflow effort on a
// configured repository completes, push its integration branch to origin as
// feature/af-<slug>-<id>, merge it into the repository's base through the same
// gates as every other merge, and remember the result in state.json's
// af_delivered map.
func (r *run) afDeliver(ctx context.Context) {
	if r.c.Store == nil {
		return
	}
	projects, err := r.c.Store.ListProjects(ctx)
	if err != nil {
		r.alert("", "af_deliver: list projects: %v", err)
		return
	}
	for _, p := range projects {
		var repo *Repo
		for i := range r.cfg.Repos {
			if filepath.Clean(r.cfg.Repos[i].Path) == filepath.Clean(p.RepoPath) {
				repo = &r.cfg.Repos[i]
				break
			}
		}
		if repo == nil {
			continue
		}
		efforts, err := r.c.Store.ListEfforts(ctx, p.Slug)
		if err != nil {
			r.alert("", "af_deliver: %s efforts: %v", p.Slug, err)
			continue
		}
		for _, ef := range efforts {
			if ef.Status != model.EffortComplete {
				continue
			}
			key := strconv.FormatInt(ef.ID, 10)
			if r.st.AFDelivered[key] == "merged" {
				continue
			}
			tid := fmt.Sprintf("AF-%s-%s", p.Slug, key)
			target := fmt.Sprintf("feature/af-%s-%s", p.Slug, key)
			base := repo.base(r.cfg)
			if r.c.Git.Merged(ctx, *repo, target, base) {
				r.st.AFDelivered[key] = "merged"
				continue
			}
			r.decide("deliver", tid, "", "%s into %s in %s", target, base, repo.Name)
			if err := r.c.Git.Deliver(ctx, *repo, "agentflow/effort-"+key, target); err != nil {
				r.alert(tid, "%s delivery: %v", tid, err)
				r.st.AFDelivered[key] = "blocked"
				continue
			}
			if err := r.c.Git.Merge(ctx, *repo, base, target, tid, false); err != nil {
				r.alert(tid, "%s delivery blocked: %v", tid, err)
				r.st.AFDelivered[key] = "blocked"
				continue
			}
			r.st.AFDelivered[key] = "merged"
			_ = r.bus.Post(ctx, r.cfg.EngineRoom, r.cfg.Me, "all",
				fmt.Sprintf("agentflow effort %s (%s, project %s) delivered: %s merged into %s after the gates.", key, ef.Title, p.Slug, target, base))
		}
	}
}
