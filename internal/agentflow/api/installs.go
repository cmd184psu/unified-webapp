package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerInstalls(mux muxer) {
	mux.HandleFunc("POST /api/backends/{name}/install", a.startInstall)
	mux.HandleFunc("GET /api/backends/{name}/installs", a.listInstalls)
	mux.HandleFunc("POST /api/installs/{id}/cancel", a.cancelInstall)
}

type installBody struct {
	Model string `json:"model"`
	Tier  int    `json:"tier"`
}

// startInstall answers POST /api/backends/{name}/install (H6): it opens an
// install and pulls the model onto the backend's Ollama host in the
// background. The reply is the install row; its progress arrives as
// install.update events and from GET .../installs.
func (a *API) startInstall(w http.ResponseWriter, r *http.Request) {
	var body installBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	name := r.PathValue("name")
	b, err := a.deps.Store.GetBackend(r.Context(), name)
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := a.deps.Store.CreateInstall(r.Context(), actorOf(r), name, body.Model, body.Tier)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if a.pulls == nil {
		a.pulls = map[int64]context.CancelFunc{}
	}
	a.pulls[in.ID] = cancel
	a.mu.Unlock()
	go a.runInstall(ctx, in, b.Host)
	a.emitInstall(in)
	writeJSON(w, http.StatusAccepted, in)
}

func (a *API) listInstalls(w http.ResponseWriter, r *http.Request) {
	list, err := a.deps.Store.ListInstalls(r.Context(), r.PathValue("name"), 20)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// cancelInstall stops a running pull. Ollama keeps the layers it has, so
// installing the model again carries on from there.
func (a *API) cancelInstall(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		badRequest(w, "id", "must be a number")
		return
	}
	in, err := a.deps.Store.GetInstall(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	a.mu.Lock()
	cancel := a.pulls[id]
	a.mu.Unlock()
	if in.State != store.InstallRunning || cancel == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the install is not running"})
		return
	}
	cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) emitInstall(in store.Install) {
	a.emit("install.update", map[string]any{"id": in.ID, "backend": in.Backend, "model": in.Model, "state": in.State,
		"status": in.Status, "completed": in.Completed, "total": in.Total})
}

// pullLine is one line of Ollama's streamed answer to POST /api/pull.
type pullLine struct {
	Status    string `json:"status"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error"`
}

// runInstall pulls in.Model from host and records the progress. When the host
// says "success" the model is added to the backend with the tier that was
// chosen; anything else ends the install failed, with the host's own words.
func (a *API) runInstall(ctx context.Context, in store.Install, host string) {
	st := a.deps.Store
	defer func() {
		a.mu.Lock()
		delete(a.pulls, in.ID)
		a.mu.Unlock()
	}()
	finish := func(state, msg string) {
		fctx := context.WithoutCancel(ctx)
		_ = st.FinishInstall(fctx, in.ID, state, msg)
		if cur, err := st.GetInstall(fctx, in.ID); err == nil {
			a.emitInstall(cur)
		}
	}
	err := a.pull(ctx, in, host)
	switch {
	case ctx.Err() != nil:
		finish(store.InstallCancelled, "")
	case err != nil:
		finish(store.InstallFailed, err.Error())
	default:
		if err := a.addInstalled(ctx, in, host); err != nil {
			finish(store.InstallFailed, "downloaded, but could not be added to the backend: "+err.Error())
			return
		}
		finish(store.InstallDone, "")
	}
}

// pull streams the pull and writes progress at most about once a second, and
// whenever the step changes.
func (a *API) pull(ctx context.Context, in store.Install, host string) error {
	body, _ := json.Marshal(map[string]any{"model": in.Model, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(host, "/")+"/api/pull", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var l pullLine
		if json.Unmarshal(raw, &l) == nil && l.Error != "" {
			return fmt.Errorf("%s", l.Error)
		}
		return fmt.Errorf("the host answered %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var last time.Time
	lastStatus := ""
	success := false
	for sc.Scan() {
		var l pullLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if l.Error != "" {
			return fmt.Errorf("%s", l.Error)
		}
		if l.Status == "success" {
			success = true
		}
		if l.Status != lastStatus || time.Since(last) >= time.Second {
			lastStatus, last = l.Status, time.Now()
			if err := a.deps.Store.SetInstallProgress(ctx, in.ID, l.Status, l.Completed, l.Total); err != nil {
				return err
			}
			cur := in
			cur.Status, cur.Completed, cur.Total = l.Status, l.Completed, l.Total
			a.emitInstall(cur)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if !success {
		return fmt.Errorf("the host closed the stream before the pull finished")
	}
	return nil
}

// RefreshCapabilities asks each Ollama backend's host whether its models whose
// tool support is not known support tool calls, and records the answers (H10).
// It is best effort and meant to run once in the background at start-up, so
// models added before the check existed get a badge (and a refusal when they
// are made candidates) without anyone re-saving the backend.
func (a *API) RefreshCapabilities(ctx context.Context) {
	bs, err := a.deps.Store.ListBackends(ctx)
	if err != nil {
		return
	}
	for _, b := range bs {
		if b.APIType != "ollama" || b.Retired {
			continue
		}
		var ask []string
		for _, m := range b.Models {
			if m.Tools == "" {
				ask = append(ask, m.Model)
			}
		}
		if len(ask) == 0 {
			continue
		}
		info := probeInfo(ctx, http.DefaultClient, b.Host, b.APIType, ask)
		changed := false
		for i := range b.Models {
			if got := info[b.Models[i].Model].Tools; got != "" && b.Models[i].Tools == "" {
				b.Models[i].Tools, changed = got, true
			}
		}
		if changed {
			if err := a.deps.Engine.UpdateBackend(ctx, "capabilities", b); err == nil {
				a.emit("backend.update", map[string]any{"backend": b.Name})
			}
		}
	}
}

// addInstalled lists the new model on its backend with the chosen tier and
// its context window, unless it is already listed.
func (a *API) addInstalled(ctx context.Context, in store.Install, host string) error {
	ctx = context.WithoutCancel(ctx)
	b, err := a.deps.Store.GetBackend(ctx, in.Backend)
	if err != nil {
		return err
	}
	for i, m := range b.Models {
		if m.Model == in.Model {
			if in.Tier > 0 {
				b.Models[i].Tier = in.Tier
				if err := a.deps.Engine.UpdateBackend(ctx, "install", b); err != nil {
					return err
				}
				a.emit("backend.update", map[string]any{"backend": in.Backend})
			}
			return nil
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info := probeInfo(cctx, http.DefaultClient, host, b.APIType, []string{in.Model})[in.Model]
	if info.Tools == "no" {
		return fmt.Errorf("it does not support tool calls, which every agentflow role needs, so it was not added (it is still on the host: remove it there with ollama rm %s)", in.Model)
	}
	b.Models = append(b.Models, store.BackendModel{Model: in.Model, Tier: in.Tier, ContextTokens: info.Context, Tools: info.Tools})
	if err := a.deps.Engine.UpdateBackend(ctx, "install", b); err != nil {
		return err
	}
	a.emit("backend.update", map[string]any{"backend": in.Backend})
	return nil
}
