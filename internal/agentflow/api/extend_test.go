package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func infoOf(t *testing.T, s string) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// H15d: what one token of context costs comes from the architecture.
func TestH15d_CacheCostFromTheArchitecture(t *testing.T) {
	dense := kvFromInfo(infoOf(t, `{"general.architecture":"q","q.block_count":48,"q.attention.head_count":32,"q.attention.head_count_kv":8,"q.embedding_length":4096,"q.attention.key_length":128,"q.context_length":262144}`))
	if !dense.Known || dense.BytesPerToken != 48*8*256*2 || dense.Max != 262144 {
		t.Fatalf("dense = %+v", dense)
	}
	// head size from embedding / heads when key_length is absent
	derived := kvFromInfo(infoOf(t, `{"general.architecture":"q","q.block_count":10,"q.attention.head_count":8,"q.attention.head_count_kv":2,"q.embedding_length":1024}`))
	if derived.BytesPerToken != 10*2*256*2 {
		t.Fatalf("derived = %+v", derived)
	}
	// a model that keeps a cache in one layer of four
	hybrid := kvFromInfo(infoOf(t, `{"general.architecture":"q","q.block_count":48,"q.attention.head_count":16,"q.attention.head_count_kv":2,"q.attention.key_length":256,"q.full_attention_interval":4}`))
	if hybrid.BytesPerToken != 12*2*512*2 {
		t.Fatalf("hybrid = %+v", hybrid)
	}
	// a per-layer list of kv heads
	list := kvFromInfo(infoOf(t, `{"general.architecture":"q","q.block_count":2,"q.attention.head_count":8,"q.attention.head_count_kv":[2,4],"q.attention.key_length":64}`))
	if list.BytesPerToken != (2+4)*128*2 {
		t.Fatalf("list = %+v", list)
	}
	// nothing described: a conservative guess, flagged
	if g := kvFromInfo(infoOf(t, `{"general.architecture":"q"}`)); g.Known || g.BytesPerToken <= 0 {
		t.Fatalf("guess = %+v", g)
	}
}

func TestH15d_FitWindow(t *testing.T) {
	kv := kvShape{BytesPerToken: 196608, Max: 262144, Known: true}
	gb := int64(1) << 30
	cases := []struct {
		name     string
		budgetMB int
		weights  int64
		kv       kvShape
		capTok   int
		want     int
	}{
		{"memory limits it", 30720, 18 * gb, kv, 0, 57344},
		{"the cap limits it", 30720, 18 * gb, kv, 32768, 32768},
		{"the model maximum limits it", 300 * 1024, 18 * gb, kv, 0, 262144},
		{"the weights alone fill it", 18432, 18 * gb, kv, 0, 0},
		{"too little room for the floor", 22016, 18 * gb, kv, 0, 0},
	}
	for _, c := range cases {
		if got := fitWindow(c.budgetMB, c.weights, c.kv, c.capTok, 16384); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// fakeSizedOllama is a host with one model of 18 GiB and the architecture above;
// it records the create calls.
func fakeSizedOllama(t *testing.T) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var created []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{"name": "m1", "size": int64(18) << 30}}})
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": []string{"tools"}, "model_info": map[string]any{
				"general.architecture": "q", "q.block_count": 48, "q.attention.head_count": 32, "q.attention.head_count_kv": 8,
				"q.embedding_length": 4096, "q.attention.key_length": 128, "q.context_length": 262144}})
		case "/api/create":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			mu.Lock()
			created = append(created, b)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"status":"success"}` + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), created...)
	}
}

func TestH15d_ExtendWindowsRecreatesTheModelAndRecordsIt(t *testing.T) {
	srv, created := fakeSizedOllama(t)
	a := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	b["models"] = []map[string]any{{"model": "m1", "context_tokens": 262144}}
	a.ok("POST", "/api/backends", b, http.StatusCreated)

	// no budget yet: refused
	a.ok("POST", "/api/backends/gpu/extend-windows", map[string]any{}, http.StatusBadRequest)

	a.ok("PATCH", "/api/backends/gpu", map[string]any{"memory_budget_mb": 30720}, http.StatusOK)
	// a dry run reports and changes nothing
	res := decode[[]ExtendResult](t, a.ok("POST", "/api/backends/gpu/extend-windows", map[string]any{"apply": false}, http.StatusOK))
	if len(res) != 1 || res[0].Window != 57344 || res[0].Applied || len(created()) != 0 {
		t.Fatalf("dry run = %+v, created %v", res, created())
	}
	res = decode[[]ExtendResult](t, a.ok("POST", "/api/backends/gpu/extend-windows", nil, http.StatusOK))
	if len(res) != 1 || !res[0].Applied || res[0].Window != 57344 {
		t.Fatalf("apply = %+v", res)
	}
	c := created()
	if len(c) != 1 || c[0]["model"] != "m1" || c[0]["from"] != "m1" || c[0]["parameters"].(map[string]any)["num_ctx"].(float64) != 57344 {
		t.Fatalf("create call = %v", c)
	}
	if m := modelView(t, a, "gpu", "m1"); m.WindowTokens != 57344 || m.WindowSource != "host" {
		t.Fatalf("recorded window = %+v", m)
	}
	// a second run has nothing to do
	res = decode[[]ExtendResult](t, a.ok("POST", "/api/backends/gpu/extend-windows", nil, http.StatusOK))
	if res[0].Applied || len(created()) != 1 {
		t.Fatalf("second run = %+v", res)
	}
	// the cap is respected, and both settings survive a save that omits them
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"window_cap_tokens": 32768}, http.StatusOK)
	res = decode[[]ExtendResult](t, a.ok("POST", "/api/backends/gpu/extend-windows", nil, http.StatusOK))
	if res[0].Window != 32768 || !res[0].Applied {
		t.Fatalf("capped = %+v", res)
	}
	bs := decode[[]BackendView](t, a.ok("GET", "/api/backends", nil, http.StatusOK))
	if bs[0].MemoryBudgetMB != 30720 || bs[0].WindowCapTokens != 32768 {
		t.Fatalf("settings lost: %+v", bs[0])
	}
	// a budget with no room for the weights marks the model, and a save keeps the mark
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"memory_budget_mb": 100}, http.StatusOK)
	res = decode[[]ExtendResult](t, a.ok("POST", "/api/backends/gpu/extend-windows", nil, http.StatusOK))
	if res[0].Window != 0 || res[0].Applied || !modelView(t, a, "gpu", "m1").TooBig {
		t.Fatalf("too big: %+v, view %+v", res, modelView(t, a, "gpu", "m1"))
	}
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"models": []map[string]any{{"model": "m1", "context_tokens": 262144}}}, http.StatusOK)
	if !modelView(t, a, "gpu", "m1").TooBig {
		t.Fatal("a save cleared the too-big mark")
	}
	// a larger budget clears it
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"memory_budget_mb": 30720}, http.StatusOK)
	a.ok("POST", "/api/backends/gpu/extend-windows", nil, http.StatusOK)
	if modelView(t, a, "gpu", "m1").TooBig {
		t.Fatal("a budget that fits left the mark")
	}
}
