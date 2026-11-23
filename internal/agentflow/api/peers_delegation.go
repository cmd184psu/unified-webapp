// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/peerclient"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// AF-25 peer delegation. The node that owns an effort stays authoritative for
// its state; a peer only executes a shadow copy of it. The effort travels as
// the same JSON bundle the export route produces plus a git bundle of its base
// commit and branches, so nothing is ever fetched from GitHub. Results come
// back as a git bundle the owner collects into its own branch names.

var delegationIDRe = regexp.MustCompile(`^del-[0-9a-f]{24}$`)

const (
	delegationFormat  = "agentflow-delegation"
	delegationVersion = 1
	// delegationBodyLimit caps one delegate delivery (export plus git bundle,
	// base64). A bundle past it is refused rather than buffered.
	delegationBodyLimit = 256 << 20
)

// delegationRequest is what the owner sends to a peer's POST /api/peer/delegate.
type delegationRequest struct {
	Format       string          `json:"format"`
	Version      int             `json:"version"`
	ID           string          `json:"id"`
	OwnerNode    string          `json:"owner_node"`
	Project      string          `json:"project"`
	Effort       int64           `json:"effort"`
	Tasks        []string        `json:"tasks,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	BaseCommit   string          `json:"base_commit"`
	BundleBase64 string          `json:"bundle_base64"`
	BundleSHA256 string          `json:"bundle_sha256"`
	Export       json.RawMessage `json:"export"`
}

// delegationResponse is what the peer answers and what the owner records.
type delegationResponse struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	RemoteEffort int64    `json:"remote_effort"`
	BaseCommit   string   `json:"base_commit"`
	Detail       string   `json:"detail,omitempty"`
	Delegable    []string `json:"delegable,omitempty"`
}

// delegationView is the live picture of one delegation, from either side.
type delegationView struct {
	ID           string            `json:"id"`
	Peer         string            `json:"peer"`
	Direction    string            `json:"direction"`
	Project      string            `json:"project"`
	Effort       int64             `json:"effort"`
	SourceEffort int64             `json:"source_effort,omitempty"`
	RemoteEffort int64             `json:"remote_effort,omitempty"`
	Tasks        []string          `json:"tasks"`
	Reason       string            `json:"reason,omitempty"`
	Status       string            `json:"status"`
	BaseCommit   string            `json:"base_commit,omitempty"`
	Detail       string            `json:"detail,omitempty"`
	EffortStatus string            `json:"effort_status,omitempty"`
	TaskStatus   map[string]string `json:"task_status,omitempty"`
	Running      int               `json:"running_stages"`
	Created      int64             `json:"created"`
	Updated      int64             `json:"updated"`
}

// resultResponse carries a completed shadow effort's branches back.
type resultResponse struct {
	ID           string            `json:"id"`
	Status       string            `json:"status"`
	EffortStatus string            `json:"effort_status"`
	TaskStatus   map[string]string `json:"task_status,omitempty"`
	Base64       string            `json:"bundle_base64,omitempty"`
	SHA256       string            `json:"bundle_sha256,omitempty"`
	Detail       string            `json:"detail,omitempty"`
}

func (a *API) registerDelegations(mux muxer) {
	mux.HandleFunc("POST /api/peers/{name}/delegate", a.peerDelegate)
	mux.HandleFunc("GET /api/delegations", a.delegationsList)
	mux.HandleFunc("GET /api/delegations/{id}", a.delegationGet)
	mux.HandleFunc("POST /api/peers/{name}/delegations/{id}/cancel", a.peerDelegateCancel)
	mux.HandleFunc("POST /api/delegations/{id}/collect", a.delegationCollect)
	// peer-to-peer (authenticated by the inbound key, not the platform gate)
	mux.HandleFunc("POST /api/peer/delegate", a.peerDelegationAccept)
	mux.HandleFunc("GET /api/peer/delegations/{id}", a.peerDelegationStatus)
	mux.HandleFunc("POST /api/peer/delegations/{id}/cancel", a.peerDelegationCancel)
	mux.HandleFunc("GET /api/peer/result/{id}", a.peerDelegationResult)
}

func delegationID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "del-" + hex.EncodeToString(buf), nil
}

func (a *API) delegationDir(id string) string {
	return filepath.Join(a.dataDir(), "delegations", id)
}

// peerStatusOf asks a paired node for its live status.
func (a *API) peerStatusOf(ctx context.Context, p store.Peer) (peerclient.PeerStatus, error) {
	key, err := a.readPeerKey(p.Name)
	if err != nil {
		return peerclient.PeerStatus{}, err
	}
	client := &peerclient.Client{Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
	var st peerclient.PeerStatus
	url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/status"
	if err := client.Do(ctx, http.MethodGet, url, key, p.HostHeader, nil, &st); err != nil {
		return st, err
	}
	return st, nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// projectSlugs lists the local projects, which is what this node can accept a
// delegation for.
func (a *API) projectSlugs(ctx context.Context) []string {
	out := []string{}
	if ps, err := a.deps.Store.ListProjects(ctx); err == nil {
		for _, p := range ps {
			out = append(out, p.Slug)
		}
	}
	return out
}

// delegationViewOf builds the live view of one row.
func (a *API) delegationViewOf(ctx context.Context, d store.Delegation) delegationView {
	v := delegationView{
		ID:           d.ID,
		Peer:         d.Peer,
		Direction:    d.Direction,
		Project:      d.Project,
		Effort:       d.Effort,
		SourceEffort: d.SourceEffort,
		RemoteEffort: d.RemoteEffort,
		Tasks:        d.TaskKeys,
		Reason:       d.Reason,
		Status:       d.Status,
		BaseCommit:   d.BaseCommit,
		Detail:       d.Detail,
		TaskStatus:   map[string]string{},
		Created:      d.Created.Unix(),
		Updated:      d.Updated.Unix(),
	}
	if v.Tasks == nil {
		v.Tasks = []string{}
	}
	if d.Effort != 0 {
		if ef, err := a.deps.Store.GetEffort(ctx, d.Effort); err == nil {
			v.EffortStatus = ef.Status
		}
		if sts, err := a.deps.Engine.TaskStatuses(ctx, d.Project, d.Effort); err == nil {
			for k, s := range sts {
				v.TaskStatus[k] = string(s)
			}
		}
	}
	if stages, err := a.deps.Store.OpenStages(ctx); err == nil {
		for _, st := range stages {
			if d.Effort != 0 && st.Effort == d.Effort {
				v.Running++
			}
		}
	}
	return v
}

// peerDelegate hands one effort to a paired node: it asks the peer what it can
// take, builds the effort export and its git bundle locally, then delivers
// both. A project the peer does not have is reported, never cloned for it.
func (a *API) peerDelegate(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "peers disabled"})
		return
	}
	name := r.PathValue("name")
	p, err := a.deps.Store.GetPeer(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown peer"})
		return
	}
	var req struct {
		Project string   `json:"project"`
		Effort  int64    `json:"effort"`
		Tasks   []string `json:"tasks"`
		Reason  string   `json:"reason"`
	}
	if !decodeBody(w, r, &req, false) {
		return
	}
	if strings.TrimSpace(req.Project) == "" || req.Effort == 0 {
		badRequest(w, "project", "project and effort are required")
		return
	}
	ef, err := a.deps.Store.GetEffort(r.Context(), req.Effort)
	if err != nil {
		writeErr(w, err)
		return
	}
	if ef.Project != req.Project {
		badRequest(w, "project", "the effort belongs to another project")
		return
	}
	if a.deps.Git == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this node has no git service"})
		return
	}
	// The peer has to have the project already; ask what it can take.
	st, err := a.peerStatusOf(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "peer offline: " + err.Error()})
		return
	}
	if !containsString(st.Delegable, req.Project) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     "project not present on peer",
			"project":   req.Project,
			"delegable": st.Delegable,
		})
		return
	}
	ex, _, err := a.buildEffortExport(r.Context(), ef)
	if err != nil {
		writeErr(w, err)
		return
	}
	if ex.Queue == nil || !ex.Approvals.FRD || !ex.Approvals.Plan || !ex.Approvals.Queue || len(ex.Queue.Tasks) == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "only an effort with approved FRD, plan and queue can be delegated"})
		return
	}
	proj, err := a.deps.Store.GetProject(r.Context(), req.Project)
	if err != nil {
		writeErr(w, err)
		return
	}
	id, err := delegationID()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	base := ef.BaseCommit
	if base == "" {
		if _, err := a.deps.Engine.SetFreshBase(r.Context(), ef.ID); err == nil {
			if cur, err := a.deps.Store.GetEffort(r.Context(), ef.ID); err == nil {
				base = cur.BaseCommit
			}
		}
	}
	bundlePath := filepath.Join(a.delegationDir(id), "effort.bundle")
	refs := []string{}
	if base != "" {
		baseRef := "refs/agentflow-delegated/" + id + "/base"
		if err := a.deps.Git.UpdateRef(r.Context(), proj.RepoPath, baseRef, base); err != nil {
			writeErr(w, err)
			return
		}
		refs = append(refs, baseRef)
	}
	refs = append(refs, "refs/heads/"+integrationBranchOf(ef))
	if err := a.deps.Git.BundleCreate(r.Context(), proj.RepoPath, bundlePath, refs); err != nil {
		writeErr(w, err)
		return
	}
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		writeErr(w, err)
		return
	}
	sum := sha256.Sum256(raw)
	exportJSON, err := json.Marshal(ex)
	if err != nil {
		writeErr(w, err)
		return
	}
	// An empty task list means the whole approved queue.
	keys := req.Tasks
	if len(keys) == 0 {
		for _, t := range ex.Queue.Tasks {
			keys = append(keys, t.Key)
		}
	}
	body := delegationRequest{
		Format:       delegationFormat,
		Version:      delegationVersion,
		ID:           id,
		OwnerNode:    a.nodeName(),
		Project:      req.Project,
		Effort:       ef.ID,
		Tasks:        keys,
		Reason:       req.Reason,
		BaseCommit:   base,
		BundleBase64: base64.StdEncoding.EncodeToString(raw),
		BundleSHA256: hex.EncodeToString(sum[:]),
		Export:       exportJSON,
	}
	d := store.Delegation{
		ID:         id,
		Peer:       p.Name,
		Project:    req.Project,
		Effort:     ef.ID,
		TaskKeys:   keys,
		Reason:     req.Reason,
		Status:     store.DelegationPending,
		Direction:  "out",
		BaseCommit: base,
	}
	if err := a.deps.Store.InsertDelegation(r.Context(), d); err != nil {
		writeErr(w, err)
		return
	}
	key, err := a.readPeerKey(p.Name)
	if err != nil {
		_ = a.deps.Store.SetDelegationDetail(r.Context(), id, "no key for peer: "+err.Error())
		writeErr(w, err)
		return
	}
	client := &peerclient.Client{Timeout: 60 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
	var resp delegationResponse
	url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/delegate"
	err = client.Do(r.Context(), http.MethodPost, url, key, p.HostHeader, body, &resp)
	if err != nil {
		_ = a.deps.Store.SetDelegationDetail(r.Context(), id, err.Error())
		_ = a.deps.Store.UpdateDelegation(r.Context(), id, store.DelegationFallback, 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "delivery failed: " + err.Error(), "id": id})
		return
	}
	status := resp.Status
	if status == "" {
		status = store.DelegationRunning
	}
	if err := a.deps.Store.UpdateDelegation(r.Context(), id, status, resp.RemoteEffort); err != nil {
		writeErr(w, err)
		return
	}
	d, err = a.deps.Store.GetDelegation(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	a.emit("delegation.update", map[string]any{"id": id, "peer": p.Name, "status": d.Status})
	writeJSON(w, http.StatusCreated, a.delegationViewOf(r.Context(), d))
}

// integrationBranchOf is the branch name the effort's work merges into.
func integrationBranchOf(ef model.Effort) string {
	if ef.IntegrationBranch != "" {
		return ef.IntegrationBranch
	}
	return fmt.Sprintf("agentflow/%d", ef.ID)
}

func (a *API) delegationsList(w http.ResponseWriter, r *http.Request) {
	peer := r.URL.Query().Get("peer")
	ds, err := a.deps.Store.ListDelegations(r.Context(), peer)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []delegationView{}
	for _, d := range ds {
		out = append(out, a.delegationViewOf(r.Context(), d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"delegations": out})
}

// delegationGet is the owner's live view: it polls the peer for a row it owns
// and falls back to the stored state when the peer does not answer.
func (a *API) delegationGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := a.deps.Store.GetDelegation(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if d.Direction == "out" && (d.Status == store.DelegationRunning || d.Status == store.DelegationPending) {
		if remote, err := a.pollDelegation(r.Context(), d); err == nil {
			d = remote
		}
	}
	writeJSON(w, http.StatusOK, a.delegationViewOf(r.Context(), d))
}

// pollDelegation asks the peer for one delegation and records what it says.
// The status poll also carries the peer's model rest times (AF-25), so a 429
// on one node holds the model back on the other.
func (a *API) pollDelegation(ctx context.Context, d store.Delegation) (store.Delegation, error) {
	p, err := a.deps.Store.GetPeer(ctx, d.Peer)
	if err != nil {
		return d, err
	}
	key, err := a.readPeerKey(p.Name)
	if err != nil {
		return d, err
	}
	client := &peerclient.Client{Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
	var v delegationView
	url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/delegations/" + d.ID
	if err := client.Do(ctx, http.MethodGet, url, key, p.HostHeader, nil, &v); err != nil {
		return d, err
	}
	_ = a.deps.Store.TouchDelegation(ctx, d.ID, time.Now())
	if v.Status != "" && v.Status != d.Status {
		_ = a.deps.Store.UpdateDelegation(ctx, d.ID, v.Status, v.RemoteEffort)
	}
	if v.Detail != "" && v.Detail != d.Detail {
		_ = a.deps.Store.SetDelegationDetail(ctx, d.ID, v.Detail)
	}
	return a.deps.Store.GetDelegation(ctx, d.ID)
}

// peerDelegateCancel cancels a delegation on the owner and tells the peer, so
// the shadow effort stops there too.
func (a *API) peerDelegateCancel(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "peers disabled"})
		return
	}
	name := r.PathValue("name")
	id := r.PathValue("id")
	d, err := a.deps.Store.GetDelegation(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if d.Peer != name {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such delegation for that peer"})
		return
	}
	if err := a.deps.Store.UpdateDelegation(r.Context(), id, store.DelegationCancelled, 0); err != nil {
		writeErr(w, err)
		return
	}
	if p, err := a.deps.Store.GetPeer(r.Context(), name); err == nil {
		key, _ := a.readPeerKey(name)
		client := &peerclient.Client{Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
		url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/delegations/" + id + "/cancel"
		_ = client.Do(r.Context(), http.MethodPost, url, key, p.HostHeader, nil, nil)
	}
	a.emit("delegation.update", map[string]any{"id": id, "peer": name, "status": store.DelegationCancelled})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "status": store.DelegationCancelled})
}

// peerDelegationAccept receives a delegation on the peer: it fetches the git
// bundle, imports the effort as a shadow marked delegated-from, and starts it
// when the load gate is open. Delivery is idempotent by delegation ID, so a
// retried request returns the existing shadow instead of creating another.
func (a *API) peerDelegationAccept(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	owner, ok := a.deps.Store.ValidateInboundKey(r.Context(), extractBearer(r))
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, delegationBodyLimit)
	var req delegationRequest
	if !decodeBody(w, r, &req, false) {
		return
	}
	if req.Format != delegationFormat || req.Version != delegationVersion {
		badRequest(w, "format", fmt.Sprintf("must be %s version %d", delegationFormat, delegationVersion))
		return
	}
	if req.ID == "" || req.Project == "" || len(req.Export) == 0 {
		badRequest(w, "id", "id, project and export are required")
		return
	}
	if !delegationIDRe.MatchString(req.ID) {
		badRequest(w, "id", "must be del-<24 hex>")
		return
	}
	if existing, err := a.deps.Store.GetDelegation(r.Context(), req.ID); err == nil {
		writeJSON(w, http.StatusOK, delegationResponse{
			ID:           existing.ID,
			Status:       existing.Status,
			RemoteEffort: existing.Effort,
			BaseCommit:   existing.BaseCommit,
			Detail:       existing.Detail,
		})
		return
	}
	proj, err := a.deps.Store.GetProject(r.Context(), req.Project)
	if err != nil {
		writeJSON(w, http.StatusConflict, delegationResponse{
			ID:        req.ID,
			Status:    store.DelegationPending,
			Detail:    "project not present",
			Delegable: a.projectSlugs(r.Context()),
		})
		return
	}
	if a.deps.Git == nil || !a.deps.Git.IsRepo(r.Context(), proj.RepoPath) {
		writeJSON(w, http.StatusConflict, delegationResponse{
			ID:     req.ID,
			Status: store.DelegationPending,
			Detail: "project has no git clone on this node",
		})
		return
	}
	var ex EffortExport
	if err := json.Unmarshal(req.Export, &ex); err != nil {
		badRequest(w, "export", err.Error())
		return
	}
	if ex.Format != bundleFormat || ex.Version != bundleVersion || ex.FRD == nil || ex.Plan == nil || ex.Queue == nil || len(ex.Queue.Tasks) == 0 {
		badRequest(w, "export", "must carry the effort's FRD, plan and queue")
		return
	}
	bundle, err := base64.StdEncoding.DecodeString(req.BundleBase64)
	if err != nil {
		badRequest(w, "bundle_base64", "is not base64")
		return
	}
	sum := sha256.Sum256(bundle)
	if req.BundleSHA256 != "" && !strings.EqualFold(hex.EncodeToString(sum[:]), req.BundleSHA256) {
		badRequest(w, "bundle_sha256", "does not match the bundle")
		return
	}
	dir := a.delegationDir(req.ID)
	bundlePath := filepath.Join(dir, "effort.bundle")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, err)
		return
	}
	if err := os.WriteFile(bundlePath, bundle, 0o600); err != nil {
		writeErr(w, err)
		return
	}
	// Fetch exactly what the bundle carries: an effort with no integration
	// branch yet has only its base commit and task branches.
	heads, err := a.deps.Git.BundleHeads(r.Context(), proj.RepoPath, bundlePath)
	if err != nil {
		writeErr(w, err)
		return
	}
	inPrefix := fmt.Sprintf("refs/agentflow-delegated/%s/", req.ID)
	integrationRef := fmt.Sprintf("refs/heads/agentflow/%d", ex.Effort.ID)
	tasksPrefix := fmt.Sprintf("refs/heads/agentflow-task/%d/", ex.Effort.ID)
	delegatedRefs := []string{}
	for _, head := range heads {
		switch {
		case strings.HasPrefix(head, inPrefix):
			delegatedRefs = append(delegatedRefs, "+"+head+":"+head)
		case head == integrationRef:
			delegatedRefs = append(delegatedRefs, "+"+head+":"+inPrefix+"integration")
		case strings.HasPrefix(head, tasksPrefix):
			delegatedRefs = append(delegatedRefs, "+"+head+":"+inPrefix+"tasks/"+strings.TrimPrefix(head, tasksPrefix))
		}
	}
	if len(delegatedRefs) == 0 {
		writeJSON(w, http.StatusConflict, delegationResponse{
			ID:     req.ID,
			Status: store.DelegationPending,
			Detail: "the bundle carries no refs this delegation can use",
		})
		return
	}
	if err := a.deps.Git.BundleFetch(r.Context(), proj.RepoPath, bundlePath, delegatedRefs...); err != nil {
		writeErr(w, err)
		return
	}
	qhash := model.QueueHash(ex.Queue.Tasks)
	if ex.Queue.Hash != "" && ex.Queue.Hash != qhash {
		badRequest(w, "export", "its queue hash does not match the task definitions")
		return
	}
	keep := ex.Approvals.FRD && ex.Approvals.Plan && ex.Approvals.Queue &&
		ex.ContentHash != "" && ex.ContentHash == bundleHash(ex.FRD.JSON, ex.Plan.JSON, qhash)
	if !keep {
		writeJSON(w, http.StatusConflict, delegationResponse{
			ID:     req.ID,
			Status: store.DelegationPending,
			Detail: "the bundle's approvals did not survive its content hash",
		})
		return
	}
	// A delegation may name a subset of the effort's tasks. Filter the
	// imported queue to those keys so the shadow runs only what was asked;
	// an unknown key is rejected rather than silently dropped.
	if len(req.Tasks) > 0 {
		wanted := map[string]bool{}
		for _, k := range req.Tasks {
			wanted[k] = true
		}
		filtered := make([]model.TaskDef, 0, len(req.Tasks))
		seen := map[string]bool{}
		for _, t := range ex.Queue.Tasks {
			if !wanted[t.Key] || seen[t.Key] {
				continue
			}
			seen[t.Key] = true
			filtered = append(filtered, t)
		}
		if len(filtered) == 0 {
			badRequest(w, "tasks", "none of the requested tasks exist in the effort")
			return
		}
		if len(filtered) != len(wanted) {
			badRequest(w, "tasks", "one or more requested tasks are not in the effort")
			return
		}
		ex.Queue.Tasks = filtered
		ex.Queue.Hash = model.QueueHash(filtered)
	}
	actor := "peer:" + owner
	ef, err := a.applyEffortExport(r.Context(), actor, req.Project, ex, keep)
	if err != nil {
		writeErr(w, err)
		return
	}
	_ = a.deps.Store.SetEffortDelegatedFrom(r.Context(), ef.ID, owner)
	base := req.BaseCommit
	if base == "" {
		base = ex.Effort.BaseCommit
	}
	if base != "" {
		if _, err := a.deps.Store.UpdateEffort(r.Context(), actor, ef.ID, store.EffortPatch{BaseCommit: &base}); err != nil {
			writeErr(w, err)
			return
		}
	}
	keys := make([]string, 0, len(ex.Queue.Tasks))
	for _, t := range ex.Queue.Tasks {
		keys = append(keys, t.Key)
	}
	d := store.Delegation{
		ID:           req.ID,
		Peer:         owner,
		Project:      req.Project,
		Effort:       ef.ID,
		TaskKeys:     keys,
		Reason:       req.Reason,
		Status:       store.DelegationPending,
		Direction:    "in",
		SourceEffort: req.Effort,
		BaseCommit:   base,
	}
	if err := a.deps.Store.InsertDelegation(r.Context(), d); err != nil {
		writeErr(w, err)
		return
	}
	status, detail := a.startInbound(r.Context(), d)
	writeJSON(w, http.StatusCreated, delegationResponse{
		ID:           req.ID,
		Status:       status,
		RemoteEffort: ef.ID,
		BaseCommit:   base,
		Detail:       detail,
	})
}

// startInbound starts a shadow effort's run when the gate is open and records
// the resulting delegation status. A closed gate leaves it pending; the peer
// loop starts it as soon as the gate clears.
func (a *API) startInbound(ctx context.Context, d store.Delegation) (string, string) {
	if !a.deps.Engine.LoadGateOK(ctx) {
		_ = a.deps.Store.SetDelegationDetail(ctx, d.ID, "waiting for the load gate")
		return store.DelegationPending, "waiting for the load gate"
	}
	actor := "peer:" + d.Peer
	if err := a.deps.Engine.Runs().Start(ctx, actor, d.Project); err != nil {
		if errors.Is(err, store.ErrConflict) {
			_ = a.deps.Store.SetDelegationDetail(ctx, d.ID, err.Error())
			return store.DelegationPending, err.Error()
		}
		_ = a.deps.Store.SetDelegationDetail(ctx, d.ID, err.Error())
		_ = a.deps.Store.UpdateDelegation(ctx, d.ID, store.DelegationFallback, 0)
		return store.DelegationFallback, err.Error()
	}
	_ = a.deps.Store.SetDelegationDetail(ctx, d.ID, "")
	_ = a.deps.Store.UpdateDelegation(ctx, d.ID, store.DelegationRunning, 0)
	return store.DelegationRunning, ""
}

func (a *API) peerDelegationStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.deps.Store.ValidateInboundKey(r.Context(), extractBearer(r)); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	d, err := a.deps.Store.GetDelegation(r.Context(), r.PathValue("id"))
	if err != nil || d.Direction != "in" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	d = a.refreshInbound(r.Context(), d)
	v := a.delegationViewOf(r.Context(), d)
	writeJSON(w, http.StatusOK, v)
}

// refreshInbound settles a shadow delegation: an effort that finished or
// stopped moves the row to done, so the owner sees it without a second call.
func (a *API) refreshInbound(ctx context.Context, d store.Delegation) store.Delegation {
	if d.Status != store.DelegationRunning && d.Status != store.DelegationPending {
		return d
	}
	ef, err := a.deps.Store.GetEffort(ctx, d.Effort)
	if err != nil {
		return d
	}
	switch ef.Status {
	case model.EffortComplete:
		_ = a.deps.Store.UpdateDelegation(ctx, d.ID, store.DelegationDone, 0)
	case model.EffortAbandoned:
		_ = a.deps.Store.UpdateDelegation(ctx, d.ID, store.DelegationCancelled, 0)
	}
	out, err := a.deps.Store.GetDelegation(ctx, d.ID)
	if err != nil {
		return d
	}
	return out
}

func (a *API) peerDelegationCancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.deps.Store.ValidateInboundKey(r.Context(), extractBearer(r)); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	d, err := a.deps.Store.GetDelegation(r.Context(), r.PathValue("id"))
	if err != nil || d.Direction != "in" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	a.abandonShadow(r.Context(), d)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": d.ID, "status": store.DelegationCancelled})
}

// abandonShadow stops a delegated run and frees the project: the shadow effort
// is abandoned so the project can take another effort, and the row records it.
func (a *API) abandonShadow(ctx context.Context, d store.Delegation) {
	actor := "peer:" + d.Peer
	_ = a.deps.Engine.Runs().Stop(ctx, actor, d.Project)
	if ef, err := a.deps.Store.GetEffort(ctx, d.Effort); err == nil && ef.Status != model.EffortComplete && ef.Status != model.EffortAbandoned {
		abandoned := model.EffortAbandoned
		_, _ = a.deps.Store.UpdateEffort(ctx, actor, d.Effort, store.EffortPatch{Status: &abandoned})
	}
	_ = a.deps.Store.UpdateDelegation(ctx, d.ID, store.DelegationCancelled, 0)
}

// abandonPeerShadow tells a peer to stop a shadow effort after the owner has
// fallen back to running it locally, so the effort never runs in two places.
// It is idempotent: a peer that already abandoned reports success.
func (a *API) abandonPeerShadow(ctx context.Context, d store.Delegation) {
	if strings.Contains(d.Detail, "abandoned on peer") {
		return
	}
	p, err := a.deps.Store.GetPeer(ctx, d.Peer)
	if err != nil {
		return
	}
	key, _ := a.readPeerKey(d.Peer)
	client := &peerclient.Client{Timeout: 10 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
	url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/delegations/" + d.ID + "/cancel"
	if err := client.Do(ctx, http.MethodPost, url, key, p.HostHeader, nil, nil); err == nil {
		_ = a.deps.Store.SetDelegationDetail(ctx, d.ID, d.Detail+" (abandoned on peer)")
	}
}

// peerDelegationResult bundles a shadow effort's branches for the owner and
// reports its task statuses. The owner collects it; nothing is pushed.
func (a *API) peerDelegationResult(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.deps.Store.ValidateInboundKey(r.Context(), extractBearer(r)); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	d, err := a.deps.Store.GetDelegation(r.Context(), r.PathValue("id"))
	if err != nil || d.Direction != "in" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	d = a.refreshInbound(r.Context(), d)
	v := a.delegationViewOf(r.Context(), d)
	resp := resultResponse{ID: d.ID, Status: d.Status, EffortStatus: v.EffortStatus, TaskStatus: v.TaskStatus}
	if a.deps.Git == nil {
		resp.Detail = "this node has no git service"
		writeJSON(w, http.StatusOK, resp)
		return
	}
	proj, err := a.deps.Store.GetProject(r.Context(), d.Project)
	if err != nil {
		writeErr(w, err)
		return
	}
	dir := a.delegationDir(d.ID)
	bundlePath := filepath.Join(dir, "result.bundle")
	refs := []string{
		fmt.Sprintf("refs/heads/agentflow/%d", d.Effort),
		fmt.Sprintf("refs/heads/agentflow-task/%d/*", d.Effort),
	}
	if err := a.deps.Git.BundleCreate(r.Context(), proj.RepoPath, bundlePath, refs); err != nil {
		resp.Detail = "no branches to collect yet: " + err.Error()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		writeErr(w, err)
		return
	}
	sum := sha256.Sum256(raw)
	resp.Base64 = base64.StdEncoding.EncodeToString(raw)
	resp.SHA256 = hex.EncodeToString(sum[:])
	writeJSON(w, http.StatusOK, resp)
}

// delegationCollect fetches a peer's result bundle into this node's branch
// names, so the usual verify and merge stages run on the owner.
func (a *API) delegationCollect(w http.ResponseWriter, r *http.Request) {
	if !a.peersEnabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "peers disabled"})
		return
	}
	d, err := a.deps.Store.GetDelegation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if d.Direction != "out" || d.RemoteEffort == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this delegation has no remote effort to collect"})
		return
	}
	if a.deps.Git == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this node has no git service"})
		return
	}
	p, err := a.deps.Store.GetPeer(r.Context(), d.Peer)
	if err != nil {
		writeErr(w, err)
		return
	}
	key, err := a.readPeerKey(p.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	client := &peerclient.Client{Timeout: 60 * time.Second, InsecureSkipVerify: p.InsecureSkipVerify}
	var res resultResponse
	url := strings.TrimRight(p.BaseURL, "/") + "/api/peer/result/" + d.ID
	if err := client.Do(r.Context(), http.MethodGet, url, key, p.HostHeader, nil, &res); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "peer offline: " + err.Error()})
		return
	}
	if res.Base64 == "" {
		detail := res.Detail
		if detail == "" {
			detail = "the peer has no result bundle yet"
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": detail})
		return
	}
	raw, err := base64.StdEncoding.DecodeString(res.Base64)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "invalid bundle from peer"})
		return
	}
	sum := sha256.Sum256(raw)
	if res.SHA256 != "" && !strings.EqualFold(hex.EncodeToString(sum[:]), res.SHA256) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "bundle checksum does not match"})
		return
	}
	proj, err := a.deps.Store.GetProject(r.Context(), d.Project)
	if err != nil {
		writeErr(w, err)
		return
	}
	dir := a.delegationDir(d.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, err)
		return
	}
	bundlePath := filepath.Join(dir, "result.bundle")
	if err := os.WriteFile(bundlePath, raw, 0o600); err != nil {
		writeErr(w, err)
		return
	}
	heads, err := a.deps.Git.BundleHeads(r.Context(), proj.RepoPath, bundlePath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "invalid bundle from peer"})
		return
	}
	integrationRef := fmt.Sprintf("refs/heads/agentflow/%d", d.RemoteEffort)
	tasksPrefix := fmt.Sprintf("refs/heads/agentflow-task/%d/", d.RemoteEffort)
	refspecs := []string{}
	for _, head := range heads {
		switch {
		case head == integrationRef:
			refspecs = append(refspecs, fmt.Sprintf("+%s:refs/heads/agentflow/%d", head, d.Effort))
		case strings.HasPrefix(head, tasksPrefix):
			refspecs = append(refspecs, fmt.Sprintf("+%s:refs/heads/agentflow-task/%d/%s", head, d.Effort, strings.TrimPrefix(head, tasksPrefix)))
		}
	}
	if len(refspecs) == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the peer's bundle has no branches for this effort"})
		return
	}
	if err := a.deps.Git.BundleFetch(r.Context(), proj.RepoPath, bundlePath, refspecs...); err != nil {
		writeErr(w, err)
		return
	}
	_ = a.deps.Store.UpdateDelegation(r.Context(), d.ID, store.DelegationDone, 0)
	_ = a.deps.Store.SetDelegationDetail(r.Context(), d.ID, "collected into this node's branches")
	a.emit("delegation.update", map[string]any{"id": d.ID, "peer": d.Peer, "status": store.DelegationDone})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": d.ID, "status": store.DelegationDone})
}

// PeerLoop keeps peer state current while the service runs: it starts waiting
// shadow efforts when the gate opens, polls outbound delegations for their
// live status, shares model rest times both ways, and re-queues a delegation
// locally when its peer has been unreachable past the timeout.
func (a *API) PeerLoop(ctx context.Context) {
	interval := 10 * time.Second
	if set, err := a.deps.Store.GetSettings(ctx); err == nil && set.PeerPollSeconds > 0 {
		interval = time.Duration(set.PeerPollSeconds) * time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_ = a.PeerPass(ctx)
		}
	}
}

// PeerPass is one PeerLoop iteration; tests call it directly.
func (a *API) PeerPass(ctx context.Context) error {
	if !a.peersEnabled() {
		return nil
	}
	var errs []error
	if err := a.startWaitingInbound(ctx); err != nil {
		errs = append(errs, err)
	}
	ds, err := a.deps.Store.ListDelegations(ctx, "")
	if err != nil {
		return err
	}
	timeout := a.delegationTimeout(ctx)
	seen := map[string]bool{}
	now := time.Now()
	for _, d := range ds {
		if d.Direction != "out" || (d.Status != store.DelegationRunning && d.Status != store.DelegationPending) {
			continue
		}
		if _, err := a.pollDelegation(ctx, d); err == nil {
			continue
		}
		last := d.LastContact
		if last.IsZero() {
			last = d.Created
		}
		if timeout > 0 && now.Sub(last) > timeout {
			if err := a.fallbackDelegation(ctx, d); err != nil {
				errs = append(errs, err)
			}
		}
	}
	// Once a delegation has fallen back, tell the peer to abandon its shadow
	// when it is reachable again, so the effort never runs in two places.
	for _, d := range ds {
		if d.Direction != "out" || d.Status != store.DelegationFallback {
			continue
		}
		if strings.Contains(d.Detail, "abandoned on peer") {
			continue
		}
		a.abandonPeerShadow(ctx, d)
	}
	// Share rest times each way once per peer.
	for _, p := range a.peerList(ctx) {
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		st, err := a.peerStatusOf(ctx, p)
		if err != nil {
			continue
		}
		a.applyPeerRests(ctx, st)
	}
	return errors.Join(errs...)
}

// startWaitingInbound starts shadow efforts left pending by a closed gate.
func (a *API) startWaitingInbound(ctx context.Context) error {
	if !a.deps.Engine.LoadGateOK(ctx) {
		return nil
	}
	ds, err := a.deps.Store.ListDelegations(ctx, "")
	if err != nil {
		return err
	}
	for _, d := range ds {
		if d.Direction != "in" || d.Status != store.DelegationPending {
			continue
		}
		a.startInbound(ctx, d)
	}
	return nil
}

// fallbackDelegation re-queues an effort locally after its peer stopped
// answering, and tells the owner's dispatcher to run it here. The peer
// abandons its shadow when the owner cancels; a repeated delivery is ignored
// by delegation ID.
func (a *API) fallbackDelegation(ctx context.Context, d store.Delegation) error {
	if err := a.deps.Store.UpdateDelegation(ctx, d.ID, store.DelegationFallback, 0); err != nil {
		return err
	}
	_ = a.deps.Store.SetDelegationDetail(ctx, d.ID, "peer unreachable; re-queued on this node")
	a.emit("delegation.update", map[string]any{"id": d.ID, "peer": d.Peer, "status": store.DelegationFallback})
	if a.deps.Engine.Runs().State(d.Project) == model.RunStopped {
		if err := a.deps.Engine.Runs().Start(ctx, "system", d.Project); err != nil {
			return fmt.Errorf("delegation %s: re-queue locally: %w", d.ID, err)
		}
	}
	a.abandonPeerShadow(ctx, d)
	return nil
}

func (a *API) delegationTimeout(ctx context.Context) time.Duration {
	set, err := a.deps.Store.GetSettings(ctx)
	if err != nil || set.DelegationTimeoutMinutes <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(set.DelegationTimeoutMinutes) * time.Minute
}

// applyPeerRests takes a peer's model rest times into this node, so one node's
// rate limit does not burn the other's tries. A model this node does not have
// is skipped; the local rest stays when it ends later.
func (a *API) applyPeerRests(ctx context.Context, st peerclient.PeerStatus) {
	if len(st.RestUntil) == 0 {
		return
	}
	now := time.Now()
	local, _ := a.deps.Store.ModelRests(ctx)
	have := map[string]int64{}
	for _, r := range local {
		have[r.Backend+"\x00"+r.Model] = r.Until
	}
	for model, until := range st.RestUntil {
		if until <= now.UnixMilli() {
			continue
		}
		backend, name, ok := strings.Cut(model, "\x00")
		if !ok {
			continue
		}
		if have[model] >= until {
			continue
		}
		_ = a.deps.Store.RestModelUntil(ctx, backend, name, "peer", time.UnixMilli(until), now)
	}
}

func (a *API) peerList(ctx context.Context) []store.Peer {
	peers, err := a.deps.Store.ListPeers(ctx)
	if err != nil {
		return nil
	}
	return peers
}
