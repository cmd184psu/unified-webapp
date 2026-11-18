package main

// OpenAI-compatible stub. Script file (JSON array) is consumed one entry per
// /chat/completions request; when exhausted, the last entry repeats (or default text).
// entry: {"kind":"text|tool|status|hang|close","text":"..","tool":"bash","args":{..},"status":429,"body":".."}
import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type entry struct {
	Kind    string                 `json:"kind"`
	Text    string                 `json:"text"`
	Tool    string                 `json:"tool"`
	Args    map[string]interface{} `json:"args"`
	Status  int                    `json:"status"`
	Body    string                 `json:"body"`
	Headers map[string]string      `json:"headers"`
}

var (
	mu     sync.Mutex
	script []entry
	idx    int
	logf   *os.File
)

func logj(v map[string]interface{}) {
	mu.Lock()
	defer mu.Unlock()
	v["t"] = time.Now().Format(time.RFC3339Nano)
	b, _ := json.Marshal(v)
	logf.Write(append(b, '\n'))
}

func next() entry {
	mu.Lock()
	defer mu.Unlock()
	if len(script) == 0 {
		return entry{Kind: "text", Text: "OK-STUB"}
	}
	e := script[idx%len(script)]
	if idx < len(script)-1 {
		idx++
	}
	return e
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "")
	sf := flag.String("script", "", "")
	lf := flag.String("log", "stub.log.jsonl", "")
	flag.Parse()
	var err error
	logf, err = os.OpenFile(*lf, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	if *sf != "" {
		b, err := os.ReadFile(*sf)
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(b, &script); err != nil {
			panic(err)
		}
	}
	http.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"object":"list","data":[{"id":"stub-model","object":"model"}]}`)
	})
	http.HandleFunc("/v1/chat/completions", handle)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		logj(map[string]interface{}{"path": r.URL.Path, "method": r.Method, "note": "unhandled"})
		http.NotFound(w, r)
	})
	fmt.Println("listening", *addr)
	http.ListenAndServe(*addr, nil)
}

func handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Model    string                   `json:"model"`
		Stream   bool                     `json:"stream"`
		Messages []map[string]interface{} `json:"messages"`
		Tools    []map[string]interface{} `json:"tools"`
	}
	json.Unmarshal(body, &req)
	var lens []int
	var roles []string
	maxContent := 0
	userTotal := 0
	var tails []string
	for _, m := range req.Messages {
		s := ""
		switch c := m["content"].(type) {
		case string:
			s = c
		default:
			b, _ := json.Marshal(c)
			s = string(b)
		}
		lens = append(lens, len(s))
		role, _ := m["role"].(string)
		roles = append(roles, role)
		if len(s) > maxContent {
			maxContent = len(s)
		}
		if role == "user" {
			userTotal += len(s)
		}
		t := s
		if len(t) > 60 {
			t = t[len(t)-60:]
		}
		tails = append(tails, t)
	}
	var toolNames []string
	for _, t := range req.Tools {
		if f, ok := t["function"].(map[string]interface{}); ok {
			toolNames = append(toolNames, fmt.Sprint(f["name"]))
		}
	}
	e := entry{Kind: "text", Text: "Title Stub"}
	if len(req.Tools) > 0 {
		e = next()
	}
	if d := os.Getenv("STUB_DUMP"); d != "" && len(req.Tools) > 0 {
		os.WriteFile(fmt.Sprintf("%s/req-%d.json", d, time.Now().UnixNano()), body, 0644)
	}
	logj(map[string]interface{}{"path": r.URL.Path, "bodyBytes": len(body), "model": req.Model, "stream": req.Stream,
		"nMessages": len(req.Messages), "roles": roles, "contentLens": lens, "maxContent": maxContent, "userTotal": userTotal,
		"tails": tails, "tools": toolNames, "kind": e.Kind,
		"containsMarkerEnd": strings.Contains(string(body), "END-OF-PROMPT-MARKER")})
	switch e.Kind {
	case "status":
		for k, v := range e.Headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(e.Status)
		if e.Body == "" {
			e.Body = fmt.Sprintf(`{"error":{"message":"stub status %d","type":"stub_error","code":"%d"}}`, e.Status, e.Status)
		}
		io.WriteString(w, e.Body)
		return
	case "hang":
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Minute):
		}
		return
	case "close":
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		c.Close()
		return
	}
	if !req.Stream {
		w.Header().Set("content-type", "application/json")
		msg := map[string]interface{}{"role": "assistant", "content": e.Text}
		fr := "stop"
		if e.Kind == "tool" {
			a, _ := json.Marshal(e.Args)
			msg = map[string]interface{}{"role": "assistant", "content": nil, "tool_calls": []map[string]interface{}{{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": e.Tool, "arguments": string(a)}}}}
			fr = "tool_calls"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "c1", "object": "chat.completion", "created": 1, "model": "stub-model",
			"choices": []map[string]interface{}{{"index": 0, "message": msg, "finish_reason": fr}},
			"usage":   map[string]int{"prompt_tokens": len(body) / 4, "completion_tokens": 7, "total_tokens": len(body)/4 + 7}})
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	fl := w.(http.Flusher)
	send := func(v interface{}) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); fl.Flush() }
	chunk := func(delta map[string]interface{}, fr interface{}) map[string]interface{} {
		return map[string]interface{}{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "stub-model",
			"choices": []map[string]interface{}{{"index": 0, "delta": delta, "finish_reason": fr}}}
	}
	send(chunk(map[string]interface{}{"role": "assistant"}, nil))
	fr := "stop"
	if e.Kind == "tool" {
		a, _ := json.Marshal(e.Args)
		send(chunk(map[string]interface{}{"tool_calls": []map[string]interface{}{{"index": 0, "id": "call_" + fmt.Sprint(time.Now().UnixNano()), "type": "function", "function": map[string]interface{}{"name": e.Tool, "arguments": string(a)}}}}, nil))
		fr = "tool_calls"
	} else {
		send(chunk(map[string]interface{}{"content": e.Text}, nil))
	}
	send(chunk(map[string]interface{}{}, fr))
	send(map[string]interface{}{"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "stub-model", "choices": []interface{}{},
		"usage": map[string]int{"prompt_tokens": len(body) / 4, "completion_tokens": 7, "total_tokens": len(body)/4 + 7}})
	fmt.Fprint(w, "data: [DONE]\n\n")
	fl.Flush()
}
