package gitsvc

import "strings"

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
