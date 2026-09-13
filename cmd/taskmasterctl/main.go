// Command taskmasterctl is the CLI for the taskmaster module. It talks to
// the unified-webapp taskmaster API over HTTP, authenticating every request
// (including health) with a platform API key sent as an Authorization:
// Bearer header. Ported from reference/continuous-task-runner-queue's
// cmd/ctrqctl.go, with the JWT/passcode auth flow replaced by API-key
// resolution (flags > env > config file) per Phase 7 of taskmaster-plan.md.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"cmd184psu/unified-webapp/internal/taskmaster/models"
)

var (
	baseURL string
	apiKey  string
	// httpClient has no Timeout set (Go's zero value), so long-lived
	// requests such as the SSE output stream are never cut off by the
	// client. All requests, including the SSE follow, use this client.
	httpClient = &http.Client{}
)

// ctlConfig is the shape of ~/.taskmasterctl.json.
type ctlConfig struct {
	URL string `json:"url"`
	Key string `json:"key"`
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home + "/.taskmasterctl.json"
}

// resolveConfig resolves the server URL and API key with precedence:
// flags > env (TASKMASTER_URL / TASKMASTER_KEY) > ~/.taskmasterctl.json.
// If the config file exists with a mode looser than 0600, a warning is
// printed to stderr.
func resolveConfig(urlFlag, keyFlag string) (resolvedURL, resolvedKey string) {
	var fileCfg ctlConfig
	if path := defaultConfigPath(); path != "" {
		if info, err := os.Stat(path); err == nil {
			if info.Mode().Perm()&0o077 != 0 {
				fmt.Fprintf(os.Stderr, "warning: config file %s has mode %04o; recommend chmod 0600 %s\n", path, info.Mode().Perm(), path)
			}
			if data, err := os.ReadFile(path); err == nil {
				_ = json.Unmarshal(data, &fileCfg)
			}
		}
	}

	resolvedURL = urlFlag
	if resolvedURL == "" {
		resolvedURL = os.Getenv("TASKMASTER_URL")
	}
	if resolvedURL == "" {
		resolvedURL = fileCfg.URL
	}

	resolvedKey = keyFlag
	if resolvedKey == "" {
		resolvedKey = os.Getenv("TASKMASTER_KEY")
	}
	if resolvedKey == "" {
		resolvedKey = fileCfg.Key
	}

	return resolvedURL, resolvedKey
}

func main() {
	urlFlag := flag.String("url", "", "server URL (overrides TASKMASTER_URL env / ~/.taskmasterctl.json)")
	keyFlag := flag.String("key", "", "API key sent as \"Authorization: Bearer <key>\" (overrides TASKMASTER_KEY env / ~/.taskmasterctl.json)")
	flag.Parse()

	resolvedURL, resolvedKey := resolveConfig(*urlFlag, *keyFlag)

	args := flag.Args()
	if len(args) < 1 {
		usage()
		os.Exit(1)
	}

	if resolvedURL == "" {
		fmt.Fprintln(os.Stderr, "error: no server URL configured; set it via -url, the TASKMASTER_URL environment variable, or \"url\" in ~/.taskmasterctl.json")
		usage()
		os.Exit(1)
	}
	baseURL = resolvedURL
	apiKey = resolvedKey

	noun := args[0]
	rest := args[1:]

	switch noun {
	case "group":
		runGroup(rest)
	case "task":
		runTask(rest)
	case "executions":
		runExecutions(rest)
	case "output":
		runOutput(rest)
	case "metrics":
		runMetrics(rest)
	case "health":
		runHealth()
	default:
		fmt.Fprintf(os.Stderr, "unknown noun: %s\n\n", noun)
		usage()
		os.Exit(1)
	}
}

// ─── Group commands ───────────────────────────────────────────────────────────

func runGroup(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: taskmasterctl group <list|pause|resume|create|update|delete> [name] [flags]")
		os.Exit(1)
	}
	verb := args[0]
	switch verb {
	case "list":
		var groups []models.GroupStatus
		apiGet("/api/groups", &groups)
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tLIMIT\tRUNNING\tPAUSED")
		for _, g := range groups {
			paused := "no"
			if g.Paused {
				paused = "YES"
			}
			fmt.Fprintf(tw, "%s\t%d\t%d\t%s\n", g.Name, g.PoolLimit, g.RunningCount, paused)
		}
		tw.Flush()

	case "pause":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl group pause <name>")
		}
		apiPost("/api/groups/"+args[1]+"/pause", nil, nil)
		fmt.Printf("group %q paused\n", args[1])

	case "resume":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl group resume <name>")
		}
		apiPost("/api/groups/"+args[1]+"/resume", nil, nil)
		fmt.Printf("group %q resumed\n", args[1])

	case "create":
		fs := flag.NewFlagSet("group create", flag.ExitOnError)
		name := fs.String("name", "", "group name (required)")
		limit := fs.Int("limit", 1, "pool limit")
		fs.Parse(args[1:])
		if *name == "" {
			fatalf("--name is required")
		}
		var result models.Group
		apiPost("/api/groups", map[string]any{"name": *name, "pool_limit": *limit, "allowed_types": []string{}}, &result)
		fmt.Printf("created group %q (pool_limit=%d)\n", result.Name, result.PoolLimit)

	case "update":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl group update <name> [--limit N]")
		}
		fs := flag.NewFlagSet("group update", flag.ExitOnError)
		limit := fs.Int("limit", 0, "new pool limit")
		fs.Parse(args[2:])
		updates := map[string]any{}
		if *limit > 0 {
			updates["pool_limit"] = *limit
		}
		apiPut("/api/groups/"+args[1], updates, nil)
		fmt.Printf("group %q updated\n", args[1])

	case "delete":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl group delete <name>")
		}
		apiDelete("/api/groups/" + args[1])
		fmt.Printf("group %q deleted\n", args[1])

	default:
		fatalf("unknown group verb: %s", verb)
	}
}

// ─── Task commands ────────────────────────────────────────────────────────────

func runTask(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: taskmasterctl task <list|add|update|pause|resume|delete|enqueue> [flags]")
		os.Exit(1)
	}
	verb := args[0]
	switch verb {
	case "list":
		fs := flag.NewFlagSet("task list", flag.ExitOnError)
		group := fs.String("group", "", "filter by group")
		fs.Parse(args[1:])
		path := "/api/tasks"
		if *group != "" {
			path += "?group=" + *group
		}
		var tasks []models.Task
		apiGet(path, &tasks)
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tGROUP\tTYPE\tENABLED\tPAUSED\tREPEAT\tCOOLDOWN")
		for _, t := range tasks {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\t%v\t%v\t%ds\n",
				t.Name, t.GroupName, t.TaskType, t.Enabled, t.Paused, t.Repeat, t.CooldownSeconds)
		}
		tw.Flush()

	case "add":
		fs := flag.NewFlagSet("task add", flag.ExitOnError)
		name := fs.String("name", "", "task name (required)")
		group := fs.String("group", "", "group name (required)")
		taskType := fs.String("type", "shell", "task type: exec|shell|script|migration")
		taskArgs := fs.String("args", "{}", "task args JSON")
		repeat := fs.Bool("repeat", false, "re-enqueue after cooldown")
		cooldown := fs.Int("cooldown", 0, "cooldown seconds between runs")
		priority := fs.Int("priority", 50, "priority (lower runs first)")
		sudo := fs.Bool("sudo", false, "run with sudo")
		enabled := fs.Bool("enabled", true, "enable task immediately")
		outputFile := fs.String("output-file", "", "append output to this file path ({task} and {exec_id} supported)")
		fs.Parse(args[1:])
		if *name == "" || *group == "" {
			fatalf("--name and --group are required")
		}
		body := map[string]any{
			"name": *name, "group_name": *group, "task_type": *taskType,
			"args": *taskArgs, "repeat": *repeat, "cooldown_seconds": *cooldown,
			"priority": *priority, "sudo": *sudo, "enabled": *enabled,
			"output_file": *outputFile,
		}
		var task models.Task
		apiPostStatus("/api/tasks", body, &task, http.StatusCreated)
		fmt.Printf("added task %q in group %q\n", task.Name, task.GroupName)

	case "update":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl task update <name> [--priority N] [--cooldown N] [--enabled] [--repeat] [--args JSON]")
		}
		name := args[1]
		fs := flag.NewFlagSet("task update", flag.ExitOnError)
		priority := fs.Int("priority", -1, "new priority")
		cooldown := fs.Int("cooldown", -1, "new cooldown seconds")
		repeat := fs.Bool("repeat", false, "set repeat")
		noRepeat := fs.Bool("no-repeat", false, "unset repeat")
		enabled := fs.Bool("enabled", false, "enable task")
		disabled := fs.Bool("disabled", false, "disable task")
		taskArgs := fs.String("args", "", "new args JSON")
		outputFile := fs.String("output-file", "\x00", "output file path (set to empty string to clear)")
		fs.Parse(args[2:])
		updates := map[string]any{}
		if *priority >= 0 {
			updates["priority"] = *priority
		}
		if *cooldown >= 0 {
			updates["cooldown_seconds"] = *cooldown
		}
		if *repeat {
			updates["repeat"] = 1
		}
		if *noRepeat {
			updates["repeat"] = 0
		}
		if *enabled {
			updates["enabled"] = 1
		}
		if *disabled {
			updates["enabled"] = 0
		}
		if *taskArgs != "" {
			updates["args"] = *taskArgs
		}
		if *outputFile != "\x00" {
			updates["output_file"] = *outputFile
		}
		apiPut("/api/tasks/"+name, updates, nil)
		fmt.Printf("task %q updated\n", name)

	case "pause":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl task pause <name>")
		}
		apiPost("/api/tasks/"+args[1]+"/pause", nil, nil)
		fmt.Printf("task %q paused\n", args[1])

	case "resume":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl task resume <name>")
		}
		apiPost("/api/tasks/"+args[1]+"/resume", nil, nil)
		fmt.Printf("task %q resumed\n", args[1])

	case "delete":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl task delete <name>")
		}
		apiDelete("/api/tasks/" + args[1])
		fmt.Printf("task %q deleted\n", args[1])

	case "enqueue":
		if len(args) < 2 {
			fatalf("usage: taskmasterctl task enqueue <name>")
		}
		var result map[string]int64
		apiPostStatus("/api/tasks/"+args[1]+"/enqueue", nil, &result, http.StatusCreated)
		fmt.Printf("enqueued task %q → execution id %d\n", args[1], result["execution_id"])

	default:
		fatalf("unknown task verb: %s", verb)
	}
}

// ─── Other commands ───────────────────────────────────────────────────────────

func runExecutions(args []string) {
	fs := flag.NewFlagSet("executions", flag.ExitOnError)
	task := fs.String("task", "", "filter by task name")
	limit := fs.Int("limit", 20, "number of results")
	fs.Parse(args)
	path := fmt.Sprintf("/api/executions?limit=%d", *limit)
	if *task != "" {
		path += "&task=" + url.QueryEscape(*task)
	}
	var execs []models.TaskExecution
	apiGet(path, &execs)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTASK\tSTATUS\tDURATION\tSTARTED")
	for _, e := range execs {
		dur := "-"
		if e.DurationMs != nil {
			dur = fmt.Sprintf("%dms", *e.DurationMs)
		}
		started := "-"
		if e.StartedAt != nil {
			started = e.StartedAt.Format("2006-01-02 15:04:05")
		}
		name := strconv.FormatInt(e.TaskID, 10)
		if e.TaskName != "" {
			name = e.TaskName
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", e.ID, name, e.Status, dur, started)
	}
	tw.Flush()
}

// runOutput follows the SSE output stream for an execution (or the most
// recent execution of a named task). It issues a plain authenticated GET
// and reads line-by-line off the response body; httpClient has no request
// timeout, so a long-running task's stream is never cut off.
func runOutput(args []string) {
	if len(args) < 1 {
		fatalf("usage: taskmasterctl output <execution-id|task-name>")
	}
	execID := args[0]
	// If the argument is not a number, treat it as a task name and resolve
	// the most recent execution ID for that task.
	if _, err := strconv.ParseInt(execID, 10, 64); err != nil {
		var execs []models.TaskExecution
		apiGet("/api/executions?task="+url.QueryEscape(execID)+"&limit=1", &execs)
		if len(execs) == 0 {
			fatalf("no executions found for task %q", execID)
		}
		execID = strconv.FormatInt(execs[0].ID, 10)
	}
	req, err := http.NewRequest("GET", baseURL+"/api/executions/"+execID+"/output", nil)
	if err != nil {
		fatalf("build request: %v", err)
	}
	setAuthHeader(req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		fatalf("connect: %v", err)
	}
	defer resp.Body.Close()
	checkAuth(resp)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fatalf("server error %d: %s", resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var event struct {
				Stream string `json:"stream"`
				Line   string `json:"line"`
			}
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				fmt.Printf("[%s] %s\n", event.Stream, event.Line)
			}
		}
		if line == "event: done" {
			return
		}
	}
}

func runMetrics(args []string) {
	fs := flag.NewFlagSet("metrics", flag.ExitOnError)
	group := fs.String("group", "", "filter by group")
	task := fs.String("task", "", "filter by task")
	hours := fs.Int("hours", 24, "time window in hours")
	fs.Parse(args)
	path := fmt.Sprintf("/api/metrics?hours=%d", *hours)
	if *group != "" {
		path += "&group=" + url.QueryEscape(*group)
	}
	if *task != "" {
		path += "&task=" + url.QueryEscape(*task)
	}
	var summaries []models.MetricSummary
	apiGet(path, &summaries)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TASK\tGROUP\tOK\tFAIL\tAVG(ms)\tMIN\tMAX\tLAST")
	for _, s := range summaries {
		avg, minD, maxD := "-", "-", "-"
		if s.AvgDurationMs != nil {
			avg = fmt.Sprintf("%.0f", *s.AvgDurationMs)
		}
		if s.MinDurationMs != nil {
			minD = strconv.FormatInt(*s.MinDurationMs, 10)
		}
		if s.MaxDurationMs != nil {
			maxD = strconv.FormatInt(*s.MaxDurationMs, 10)
		}
		last := "-"
		if s.LastExecution != nil {
			last = s.LastExecution.Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\t%s\t%s\t%s\n",
			s.TaskName, s.GroupName, s.SuccessCount, s.FailedCount, avg, minD, maxD, last)
	}
	tw.Flush()
}

// runHealth checks server health. Like every other request, it sends the
// bearer key when one is configured.
func runHealth() {
	req, err := http.NewRequest("GET", baseURL+"/api/health", nil)
	if err != nil {
		fatalf("build request: %v", err)
	}
	setAuthHeader(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		fatalf("connect: %v", err)
	}
	defer resp.Body.Close()
	checkAuth(resp)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		fmt.Println("ok")
	} else {
		fmt.Printf("unhealthy (%d): %s\n", resp.StatusCode, body)
		os.Exit(1)
	}
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

// setAuthHeader sets the Authorization header when an API key is
// configured. A request with no key configured is sent without one, so
// the server's own auth gate produces the 401/403 guidance.
func setAuthHeader(req *http.Request) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// checkAuth prints guidance and exits when the response is 401/403. Every
// call site invokes this before its own status handling.
func checkAuth(resp *http.Response) {
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		fmt.Fprintln(os.Stderr, "authentication failed: check the API key (minted in the admin panel) and that taskmaster has an auth.modules entry")
		os.Exit(1)
	}
}

func apiGet(path string, out any) {
	req, _ := http.NewRequest("GET", baseURL+path, nil)
	setAuthHeader(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	checkAuth(resp)
	checkStatus(resp, path)
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
}

func apiPost(path string, body, out any) {
	apiPostStatus(path, body, out, http.StatusOK)
}

func apiPostStatus(path string, body, out any, expectedStatus int) {
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest("POST", baseURL+path, r)
	setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	checkAuth(resp)
	if resp.StatusCode != expectedStatus {
		b, _ := io.ReadAll(resp.Body)
		fatalf("POST %s returned %d: %s", path, resp.StatusCode, b)
	}
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
}

func apiPut(path string, body, out any) {
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest("PUT", baseURL+path, r)
	setAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		fatalf("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	checkAuth(resp)
	checkStatus(resp, path)
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
}

func apiDelete(path string) {
	req, _ := http.NewRequest("DELETE", baseURL+path, nil)
	setAuthHeader(req)
	resp, err := httpClient.Do(req)
	if err != nil {
		fatalf("DELETE %s: %v", path, err)
	}
	defer resp.Body.Close()
	checkAuth(resp)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		fatalf("DELETE %s returned %d: %s", path, resp.StatusCode, b)
	}
}

func checkStatus(resp *http.Response, path string) {
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		fatalf("%s returned %d: %s", path, resp.StatusCode, b)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `taskmasterctl — taskmaster module CLI

Usage: taskmasterctl [-url URL] [-key KEY] <noun> <verb> [flags]

Nouns:
  group       list|create|update|delete|pause|resume
  task        list|add|update|pause|resume|delete|enqueue
  executions  [--task NAME] [--limit N]
  output      <execution-id|task-name>
  metrics     [--group G] [--task T] [--hours N]
  health

Every request (including health) is sent with an Authorization: Bearer
<key> header when a key is configured. The server URL and API key are
resolved in this order (highest precedence first):
  1. -url / -key command-line flags
  2. TASKMASTER_URL / TASKMASTER_KEY environment variables
  3. ~/.taskmasterctl.json config file: {"url": "...", "key": "..."}

Global flags:
  -url URL   server base URL
  -key KEY   API key sent as "Authorization: Bearer <key>"`)
}
