// Command agentflow-effort moves a prepared effort between agentflow
// installations as one JSON bundle (AF-15).
//
//	agentflow-effort export -id 12 effort.json [-url http://127.0.0.1:8788]
//	agentflow-effort import effort.json [-project my-project] [-url ...]
//
// The bundle carries the FRD, plan, compiled queue, budget, priority and start
// condition. It never carries keys, candidates or run history. An import keeps
// the approvals only when the content hash still matches.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "export":
		export(os.Args[2:])
	case "import":
		importBundle(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: agentflow-effort export <file> -id N [-url URL]")
	fmt.Fprintln(os.Stderr, "       agentflow-effort import <file> [-project SLUG] [-url URL]")
	os.Exit(2)
}

func export(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	id := fs.Int64("id", 0, "effort id")
	base := fs.String("url", envOr("AGENTFLOW_URL", "http://127.0.0.1:8788"), "agentflow base URL")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || *id <= 0 {
		usage()
	}
	endpoint := strings.TrimRight(*base, "/") + "/api/efforts/" + strconv.FormatInt(*id, 10) + "/export?format=json"
	body, err := request(http.MethodGet, endpoint, nil)
	if err != nil {
		fatal(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		fatal(fmt.Errorf("export is not JSON: %w", err))
	}
	pretty.WriteByte('\n')
	if err := os.WriteFile(fs.Arg(0), pretty.Bytes(), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("exported effort %d to %s\n", *id, fs.Arg(0))
}

func importBundle(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	project := fs.String("project", "", "project slug to import into (default: the bundle's project)")
	base := fs.String("url", envOr("AGENTFLOW_URL", "http://127.0.0.1:8788"), "agentflow base URL")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fatal(err)
	}
	var bundle any
	if err := json.Unmarshal(raw, &bundle); err != nil {
		fatal(fmt.Errorf("%s is not JSON: %w", fs.Arg(0), err))
	}
	endpoint := strings.TrimRight(*base, "/") + "/api/import"
	if *project != "" {
		endpoint += "?project=" + url.QueryEscape(*project)
	}
	body, err := request(http.MethodPost, endpoint, raw)
	if err != nil {
		fatal(err)
	}
	var out struct {
		ID      int64  `json:"id"`
		Project string `json:"project"`
		Status  string `json:"status"`
	}
	_ = json.Unmarshal(body, &out)
	fmt.Printf("imported effort %d in %s (%s)\n", out.ID, out.Project, out.Status)
}

func request(method, endpoint string, body []byte) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, endpoint, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s %s: %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func fatal(err error) {
	if errors.Is(err, io.EOF) {
		err = errors.New("unexpected end of response")
	}
	fmt.Fprintln(os.Stderr, "agentflow-effort:", err)
	os.Exit(1)
}
