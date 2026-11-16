package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// probeBody is what POST /api/backends/probe takes: the host and API type
// typed into the Backends form, before the backend is saved.
type probeBody struct {
	Host    string `json:"host"`
	APIType string `json:"api_type"`
	// APIKey is a key typed into the form; Backend names a saved backend whose
	// stored key is used when none was typed. Hosts that need a bearer key
	// answer 401 without one.
	APIKey  string `json:"api_key"`
	Backend string `json:"backend"`
}

// probeBackend asks a host which models it serves, so the form can offer an
// exact list instead of typed names. It reads only; nothing is stored.
func (a *API) probeBackend(w http.ResponseWriter, r *http.Request) {
	var body probeBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	if body.APIType == "opencode" || body.APIType == "grok" {
		a.probeBuiltin(w, r, body.APIType)
		return
	}
	key := body.APIKey
	if key == "" && body.Backend != "" {
		key, _ = a.deps.Store.BackendKey(r.Context(), body.Backend)
	}
	models, err := probeModels(r.Context(), http.DefaultClient, body.Host, body.APIType, key)
	if err != nil {
		var fe *store.FieldError
		if errors.As(err, &fe) {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, errorBody{Error: err.Error(), Field: "host"})
		return
	}
	info := probeInfo(r.Context(), http.DefaultClient, body.Host, body.APIType, models)
	contexts, tools := map[string]int{}, map[string]string{}
	for name, i := range info {
		if i.Context > 0 {
			contexts[name] = i.Context
		}
		if i.Tools != "" {
			tools[name] = i.Tools
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models, "contexts": contexts, "tools": tools})
}

// modelInfo is what an Ollama host reports about a model: its context window
// and whether it supports tool calls, which every agentflow role needs. Tools
// is "yes" or "no", or empty when the host did not say (an old Ollama has no
// capabilities list, and an unreachable host says nothing).
type modelInfo struct {
	Context int
	Tools   string
}

// probeInfo asks an Ollama host about each model (/api/show). It is best
// effort: a model whose answer is missing or unreadable is left out, and hosts
// of other API types have no such call.
func probeInfo(ctx context.Context, c *http.Client, host, apiType string, models []string) map[string]modelInfo {
	out := map[string]modelInfo{}
	if apiType != "ollama" {
		return out
	}
	base := strings.TrimRight(strings.TrimSpace(host), "/")
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for _, name := range models {
		if info, ok := showInfo(ctx, c, base, name); ok {
			out[name] = info
		}
	}
	return out
}

// probeContexts is the context windows from probeInfo, for the models that report one.
func probeContexts(ctx context.Context, c *http.Client, host, apiType string, models []string) map[string]int {
	out := map[string]int{}
	for name, info := range probeInfo(ctx, c, host, apiType, models) {
		if info.Context > 0 {
			out[name] = info.Context
		}
	}
	return out
}

func showInfo(ctx context.Context, c *http.Client, base, model string) (modelInfo, bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"model": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/show", bytes.NewReader(body))
	if err != nil {
		return modelInfo{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return modelInfo{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return modelInfo{}, false
	}
	var out struct {
		ModelInfo    map[string]json.RawMessage `json:"model_info"`
		Capabilities []string                   `json:"capabilities"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil || json.Unmarshal(raw, &out) != nil {
		return modelInfo{}, false
	}
	var info modelInfo
	for k, v := range out.ModelInfo {
		var n int
		if strings.HasSuffix(k, ".context_length") && json.Unmarshal(v, &n) == nil && n > 0 {
			info.Context = n
			break
		}
	}
	if out.Capabilities != nil { // an old host sends none: then nothing is known
		info.Tools = "no"
		for _, c := range out.Capabilities {
			if c == "tools" {
				info.Tools = "yes"
			}
		}
	}
	return info, true
}

// probeModels lists the model names a backend host serves: Ollama's
// /api/tags, or an OpenAI-compatible /v1/models.
func probeModels(ctx context.Context, c *http.Client, host, apiType, key string) ([]string, error) {
	u, err := url.Parse(strings.TrimSpace(host))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, store.Invalid("host", "an http or https URL is required")
	}
	base := strings.TrimRight(u.String(), "/")
	var path string
	switch apiType {
	case "ollama":
		path = "/api/tags"
	case "openai_compatible":
		path = "/v1/models"
	default:
		return nil, store.Invalid("api_type", "model lists can only be fetched for ollama and openai_compatible hosts; type the model names")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %v", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s%s answered %s", base, path, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s%s did not return a model list", base, path)
	}
	names := []string{}
	for _, m := range out.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	for _, m := range out.Data {
		if m.ID != "" {
			names = append(names, m.ID)
		}
	}
	sort.Strings(names)
	return names, nil
}

// probeBuiltin lists the own models of opencode or the Grok CLI; there is no host to ask.
func (a *API) probeBuiltin(w http.ResponseWriter, r *http.Request, apiType string) {
	list := a.deps.BuiltinModels
	if a.deps.BuiltinModelsFor != nil {
		list = func(ctx context.Context) ([]string, error) { return a.deps.BuiltinModelsFor(ctx, apiType) }
	}
	if list == nil {
		writeJSON(w, http.StatusBadGateway, errorBody{Error: apiType + " is not available", Field: "api_type"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	models, err := list(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorBody{Error: err.Error(), Field: "api_type"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models, "contexts": map[string]int{}})
}
