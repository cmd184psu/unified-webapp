package gitsvc

import (
	"context"
	"strings"
)

// PushInstructions returns the commands a human runs to bring the effort's
// integration branch into baseBranch and publish it (GIT4). They are text for
// the UI: nothing in agentflow executes them, and gitsvc has no push path.
func PushInstructions(repoPath, integrationBranch, baseBranch string) []string {
	return []string{
		"cd " + shQuote(repoPath),
		"git log --oneline " + shQuote(baseBranch) + ".." + shQuote(integrationBranch),
		"git switch " + shQuote(baseBranch),
		"git merge --no-ff " + shQuote(integrationBranch),
		"git push origin " + shQuote(baseBranch),
	}
}

// shQuote single-quotes s for a POSIX shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Commit is one commit of a completed effort.
type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// Completion is what a completed effort added on its integration branch.
type Completion struct {
	Base     string   `json:"base"` // short sha
	Commits  []Commit `json:"commits"`
	Diffstat string   `json:"diffstat"`
	Files    int      `json:"files"`
}

// Completion summarizes base..branch for the "ready to merge" view: commits
// oldest first, the `git diff --stat` text and the changed-file count. Tool
// state directories (.omc, .claude) are left out of the diff. It only reads.
func (s *Service) Completion(ctx context.Context, repo, base, branch string) (Completion, error) {
	b, err := s.resolveCommit(ctx, repo, base)
	if err != nil {
		return Completion{}, err
	}
	tip, err := s.resolveCommit(ctx, repo, branch)
	if err != nil {
		return Completion{}, err
	}
	out := Completion{Base: b, Commits: []Commit{}}
	if len(b) > 7 {
		out.Base = b[:7]
	}
	log, err := s.run(ctx, repo, "log", "--reverse", "--format=%h%x00%s", b+".."+tip, "--")
	if err != nil {
		return Completion{}, err
	}
	for _, line := range strings.Split(log, "\n") {
		if sha, subject, ok := strings.Cut(line, "\x00"); ok {
			out.Commits = append(out.Commits, Commit{SHA: sha, Subject: subject})
		}
	}
	diff := func(mode string) (string, error) {
		args := []string{"diff", mode, "--no-color", "--no-ext-diff", "--no-textconv", b, tip, "--", "."}
		for _, d := range toolStateDirs {
			args = append(args, ":(exclude)"+d)
		}
		return s.run(ctx, repo, args...)
	}
	if out.Diffstat, err = diff("--stat"); err != nil {
		return Completion{}, err
	}
	names, err := diff("--numstat")
	if err != nil {
		return Completion{}, err
	}
	if names != "" {
		out.Files = strings.Count(names, "\n") + 1
	}
	return out, nil
}
