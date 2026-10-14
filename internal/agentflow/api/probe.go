package api

import (
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
}

// probeBackend asks a host which models it serves, so the form can offer an
// exact list instead of typed names. It reads only; nothing is stored.
func (a *API) probeBackend(w http.ResponseWriter, r *http.Request) {
	var body probeBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	models, err := probeModels(r.Context(), http.DefaultClient, body.Host, body.APIType)
	if err != nil {
		var fe *store.FieldError
		if errors.As(err, &fe) {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusBadGateway, errorBody{Error: err.Error(), Field: "host"})
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"models": models})
}

// probeModels lists the model names a backend host serves: Ollama's
// /api/tags, or an OpenAI-compatible /v1/models.
func probeModels(ctx context.Context, c *http.Client, host, apiType string) ([]string, error) {
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
