package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// Extending a model's context window (H15d). An Ollama model is served with the
// num_ctx in its definition, and the OpenAI-compatible endpoint cannot change
// it per request, so the window is raised by re-creating the model under the
// same name with a larger num_ctx (the weights are shared, nothing is copied).
// How large is a sum: the backend's memory budget, less the weights and a
// margin, divided by what one token costs in the context cache.

// kvShape is what the model's architecture says about its context cache.
type kvShape struct {
	// BytesPerToken is the cache one token of context takes at f16 (key and value, every layer that keeps a cache).
	BytesPerToken int64
	// Max is the largest context the model supports.
	Max int
	// Known is false when the architecture did not say enough to compute the cost,
	// so a conservative guess was used.
	Known bool
}

// fitWindow is the largest context (a multiple of 1024) that fits a budget:
// budget less weights less a margin, over the cache cost per token, capped by
// the model's maximum and the backend's cap (0 = none). It returns 0 when not
// even `floor` tokens fit.
func fitWindow(budgetMB int, weightsBytes int64, kv kvShape, capTokens, floor int) int {
	budget := int64(budgetMB) << 20
	margin := budget / 20
	if margin < 1<<30 {
		margin = 1 << 30 // compute buffers and the runtime
	}
	free := budget - weightsBytes - margin
	if free <= 0 || kv.BytesPerToken <= 0 {
		return 0
	}
	n := int(free / kv.BytesPerToken)
	if kv.Max > 0 && n > kv.Max {
		n = kv.Max
	}
	if capTokens > 0 && n > capTokens {
		n = capTokens
	}
	n = n / 1024 * 1024
	if n < floor {
		return 0
	}
	return n
}

// numAt reads an integer or, for a per-layer list, the sum or the largest entry.
func numAt(raw json.RawMessage) (n int, perLayer []int) {
	if json.Unmarshal(raw, &n) == nil {
		return n, nil
	}
	_ = json.Unmarshal(raw, &perLayer)
	return 0, perLayer
}

// kvFromInfo computes the cache cost from /api/show's model_info. A model that
// keeps a cache in only some layers (full_attention_interval) is counted for
// those layers. When the architecture lacks the fields, every layer is assumed
// to cache 8 heads of 128: wrong in places, but on the safe side.
func kvFromInfo(info map[string]json.RawMessage) kvShape {
	var arch string
	_ = json.Unmarshal(info["general.architecture"], &arch)
	get := func(suffix string) (int, []int) {
		raw, ok := info[arch+"."+suffix]
		if !ok {
			return 0, nil
		}
		return numAt(raw)
	}
	out := kvShape{}
	for k, v := range info {
		var n int
		if strings.HasSuffix(k, ".context_length") && json.Unmarshal(v, &n) == nil && n > 0 {
			out.Max = n
		}
	}
	layers, _ := get("block_count")
	heads, _ := get("attention.head_count")
	kvHeads, kvList := get("attention.head_count_kv")
	emb, _ := get("embedding_length")
	keyLen, _ := get("attention.key_length")
	valLen, _ := get("attention.value_length")
	if keyLen == 0 && heads > 0 {
		keyLen = emb / heads
	}
	if valLen == 0 {
		valLen = keyLen
	}
	if layers <= 0 || keyLen <= 0 || (kvHeads <= 0 && len(kvList) == 0) {
		out.BytesPerToken = int64(2 * 48 * 8 * 128 * 2)
		return out
	}
	cached := layers
	if iv, _ := get("full_attention_interval"); iv > 1 {
		cached = (layers + iv - 1) / iv
	}
	var perToken int64
	if len(kvList) > 0 {
		for _, h := range kvList {
			perToken += int64(h) * int64(keyLen+valLen) * 2
		}
	} else {
		perToken = int64(cached) * int64(kvHeads) * int64(keyLen+valLen) * 2
	}
	out.BytesPerToken, out.Known = perToken, true
	return out
}

// ExtendResult is one model's outcome.
type ExtendResult struct {
	Model    string `json:"model"`
	Previous int    `json:"previous"`
	Window   int    `json:"window"`
	Applied  bool   `json:"applied"`
	Note     string `json:"note"`
}

type extendBody struct {
	// Apply false only computes and reports.
	Apply *bool `json:"apply"`
}

func (a *API) extendWindows(w http.ResponseWriter, r *http.Request) {
	var body extendBody
	if !decodeBody(w, r, &body, true) {
		return
	}
	apply := body.Apply == nil || *body.Apply
	b, err := a.deps.Store.GetBackend(r.Context(), r.PathValue("name"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if b.APIType != "ollama" {
		badRequest(w, "api_type", "only an Ollama backend's windows can be extended")
		return
	}
	if b.MemoryBudgetMB <= 0 {
		badRequest(w, "memory_budget_mb", "set the backend's memory budget first")
		return
	}
	set, err := a.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	sizes, err := ollamaSizes(ctx, http.DefaultClient, b.Host)
	if err != nil {
		writeErr(w, store.Invalid("host", err.Error()))
		return
	}
	out := []ExtendResult{}
	for _, m := range b.Models {
		res := ExtendResult{Model: m.Model, Previous: m.WindowTokens}
		a.extendOne(ctx, b, m, sizes[m.Model], set.ContextFloor, apply, &res)
		out = append(out, res)
	}
	if apply {
		a.emit("backend.update", map[string]any{"backend": b.Name})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) extendOne(ctx context.Context, b store.Backend, m store.BackendModel, weights int64, floor int, apply bool, res *ExtendResult) {
	if weights <= 0 {
		res.Note = "the host does not list this model"
		return
	}
	info, ok := showModelInfo(ctx, http.DefaultClient, strings.TrimRight(b.Host, "/"), m.Model)
	if !ok {
		res.Note = "the host did not describe this model"
		return
	}
	kv := kvFromInfo(info)
	n := fitWindow(b.MemoryBudgetMB, weights, kv, b.WindowCapTokens, floor)
	if n == 0 {
		res.Note = fmt.Sprintf("the %d MB budget leaves no room for even %d tokens of context after %d MB of weights", b.MemoryBudgetMB, floor, weights>>20)
		if apply && !m.TooBig {
			if err := a.deps.Store.SetModelTooBig(ctx, b.Name, m.Model, true); err == nil {
				res.Note += "; the model will be passed over"
			}
		}
		return
	}
	res.Window = n
	if apply && m.TooBig {
		_ = a.deps.Store.SetModelTooBig(ctx, b.Name, m.Model, false)
	}
	if !kv.Known {
		res.Note = "cache cost guessed (architecture not described): "
	}
	if n == m.WindowTokens {
		res.Note += "already at this window"
		return
	}
	if !apply {
		res.Note += fmt.Sprintf("would set %d tokens", n)
		return
	}
	if err := createWithContext(ctx, http.DefaultClient, strings.TrimRight(b.Host, "/"), m.Model, n); err != nil {
		res.Note += "not applied: " + err.Error()
		return
	}
	if err := a.deps.Store.SetModelWindow(ctx, b.Name, m.Model, n, "host"); err != nil {
		res.Note += "applied on the host but not recorded: " + err.Error()
		return
	}
	res.Applied = true
	res.Note += fmt.Sprintf("set to %d tokens", n)
}

// ollamaSizes is each model's size on disk, from /api/tags.
func ollamaSizes(ctx context.Context, c *http.Client, host string) (map[string]int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(host, "/")+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %v", host, err)
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&tags) != nil {
		return nil, fmt.Errorf("%s did not return a model list", host)
	}
	out := map[string]int64{}
	for _, m := range tags.Models {
		out[m.Name] = m.Size
	}
	return out, nil
}

func showModelInfo(ctx context.Context, c *http.Client, base, model string) (map[string]json.RawMessage, bool) {
	body, _ := json.Marshal(map[string]string{"model": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/show", bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	var out struct {
		ModelInfo map[string]json.RawMessage `json:"model_info"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out) != nil || out.ModelInfo == nil {
		return nil, false
	}
	return out.ModelInfo, true
}

// createWithContext re-creates a model under its own name with a larger num_ctx.
func createWithContext(ctx context.Context, c *http.Client, base, model string, numCtx int) error {
	body, _ := json.Marshal(map[string]any{"model": model, "from": model, "stream": false, "parameters": map[string]int{"num_ctx": numCtx}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/create", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the host answered %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var st struct{ Status, Error string }
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		_ = json.Unmarshal([]byte(line), &st)
		if st.Error != "" {
			return fmt.Errorf("%s", st.Error)
		}
	}
	return nil
}
