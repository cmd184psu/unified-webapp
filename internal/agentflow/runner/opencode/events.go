package opencode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// rawEvent is the part of an `opencode run --format json` line the adapter
// reads (spike S5). Every other field is ignored.
type rawEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      struct {
		Text   string `json:"text"`
		Reason string `json:"reason"`
		Tokens struct {
			Total  int64 `json:"total"`
			Input  int64 `json:"input"`
			Output int64 `json:"output"`
		} `json:"tokens"`
	} `json:"part"`
	Error struct {
		Name string `json:"name"`
		Data struct {
			Message    string `json:"message"`
			StatusCode int    `json:"statusCode"`
		} `json:"data"`
	} `json:"error"`
}

// outcome is what a stream of events says, before the exit status is known.
type outcome struct {
	events     []runner.Event
	final      string
	usage      runner.Usage
	sawUsage   bool
	sessionID  string
	lastReason string
	lastTotal  int64 // tokens in the context at the last step
	hasError   bool
	errMessage string
	errStatus  int
}

// parseStream reads JSONL events from r until EOF. raw, when non-nil, gets
// every line verbatim; onEvent, when non-nil, gets each parsed event as it
// arrives. A line that is not JSON is kept as an event of type "raw" so the log
// stays complete.
func parseStream(r io.Reader, raw io.Writer, onEvent func(runner.Event)) (*outcome, error) {
	o := &outcome{}
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if raw != nil {
				if _, werr := raw.Write(append(append([]byte(nil), trimmed...), '\n')); werr != nil {
					return o, werr
				}
			}
			ev := o.add(trimmed)
			if onEvent != nil {
				onEvent(ev)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return o, nil
			}
			return o, err
		}
	}
}

func (o *outcome) add(line []byte) runner.Event {
	ev := runner.Event{Type: "raw", Raw: line}
	var re rawEvent
	if json.Unmarshal(line, &re) != nil || re.Type == "" {
		o.events = append(o.events, ev)
		return ev
	}
	ev.Type = re.Type
	if re.SessionID != "" {
		o.sessionID = re.SessionID
	}
	switch re.Type {
	case "text":
		ev.Text = re.Part.Text
		o.final = re.Part.Text
	case "step_finish":
		o.lastReason = re.Part.Reason
		o.lastTotal = re.Part.Tokens.Total
		o.sawUsage = true
		o.usage.InputTokens += re.Part.Tokens.Input
		o.usage.OutputTokens += re.Part.Tokens.Output
		o.usage.TotalTokens += re.Part.Tokens.Total
	case "error":
		ev.Text = re.Error.Data.Message
		if ev.Text == "" {
			ev.Text = re.Error.Name
		}
		o.hasError = true
		o.errMessage = ev.Text
		o.errStatus = re.Error.Data.StatusCode
	}
	o.events = append(o.events, ev)
	return ev
}

// errorClass maps an error event to a runner class (S5): 429 is rate_limit, a
// "Cannot connect" without a status is unreachable, every other failure
// (including HTTP 500) is a crash.
func (o *outcome) errorClass() runner.ErrClass {
	switch {
	case o.errStatus == 429:
		return runner.ErrRateLimit
	case o.errStatus == 0 && strings.Contains(o.errMessage, "Cannot connect"):
		return runner.ErrUnreachable
	}
	return runner.ErrCrash
}

func (o *outcome) usagePtr() *runner.Usage {
	if !o.sawUsage {
		return nil
	}
	u := o.usage
	return &u
}
