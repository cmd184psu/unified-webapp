package haproxy

// The haproxy module's HTTP API (FRD §9). All routes are JSON unless noted and
// sit behind the platform auth gate (nothing here re-checks auth). A method not
// listed for a path answers 405. No response carries key material, the
// CertMachine API key or any OS-specific value; error bodies are
// {"error": "<message>"}.
//
//	GET    /api/status                    {module, moduleVersion, service:{active,detail}, version, lastApply:{time,outcome,message}|null, pending, certmachineConfigured,
//	                                      diagnostics:[string]}; diagnostics are plain-text, read-only OS conditions (empty when healthy) and never fail the route
//	GET    /api/model                     the Model plus imported:bool (DefaultModel() when no state.json yet)
//	GET    /api/model/check               {issues:[{severity:"error"|"warning", where, message}]} from the STORED model + cert tracking rows (FR-H6)
//	PUT    /api/model                     replace the stored Model; 400 + message when Model.Validate fails; body limited to 1 MiB
//	POST   /api/import {commit}           read the live config, Import it -> {model, report}; stored only when commit is true; 404 when no live config
//	GET    /api/changes                   Changes{hasChanges, configDiff, crtListDiff, summary}
//	GET    /api/stats                     {available, message, info:{version,uptimeSec,currConns,pid}, rows:[StatRow]}; read-only (only `show stat`/`show info`
//	                                      on the stats socket); socket missing/unreachable is 200 with available:false and a plain message
//	GET    /api/raw                       {config, crtList}: exactly what Apply would write
//	POST   /api/check                     CheckResult{ok, message} (haproxy -c on the staged candidate)
//	POST   /api/apply                     ApplyResult{applied, rolledBack, outcome, message, started?}; started:true when HAProxy was
//	                                      not running and Apply started it instead of reloading; 409 {error, issues} when any
//	                                      error-severity /api/model/check issue exists (warnings never block)
//	POST   /api/reload | /api/restart | /api/start   {ok:true}
//	GET    /api/backups                   []Backup
//	GET    /api/backups/{name}            {name, content}
//	POST   /api/backups/{name}/restore    ApplyResult
//	GET    /api/ops                       []OpEntry snapshot
//	GET    /api/ops/stream                SSE of OpEntry (503 past sse_max_subscribers)
//	GET    /api/certs                     {certs:[tracking row + missing + details|detailsError + removable + inUseReason], unmanaged};
//	                                      removable is false (inUseReason says why) while the live crt-list names the cert, an enabled
//	                                      cert's FQDN is named by a service, or the live crt-list cannot be read; read once per request
//	GET    /api/certmachine/certs         ?forFqdn=<name>[&all=1] CertMachine's active certs with covers:bool;
//	                                      only covering certs unless all=1 (then any status, flagged)
//	POST   /api/certs/pull {certmachineId, note}     verified download + stage -> {name, superseded}
//	GET    /api/certs/importable          untrusted bundles already in the certs dir [{name, fqdns, notAfter, expired}]
//	POST   /api/certs/import {names}      track them as-is (never renamed or deleted) -> {imported}
//	PUT    /api/certs/{name}/enabled {enabled}       toggle the tracking flag only
//	DELETE /api/certs/{name}              remove file + row; 409 while a service uses it
//	GET    /api/coverage                  []CoverageResult per service (warnings only; unknown when CertMachine is down)
//	GET    /api/settings                  {effective:{os,certmachineUrl,certmachineCaFile,configPath,certsDir,crtListPath,statsSocketPath,backupDir,serviceName,
//	                                      backupKeep,expiryWarnDays}, defaults:{<each path/service setting>: the driver's per-OS default}, apiKeySet,
//	                                      certmachineConfigured, unavailable}; NEVER the api key; unavailable is the unsupported-OS reason or ""
//	PUT    /api/settings                  the full effective set + apiKey ("" keeps the stored key) + clearApiKey; plain-text 400 on a bad value and
//	                                      nothing changes; on success saved to <data_dir>/settings.json (0600) and applied live ->
//	                                      {ok, needsRestart, message, unavailable}
//	POST   /api/settings/test-connection  optional body {certmachineUrl, certmachineCaFile, apiKey} tests unsaved values (no body: the current effective
//	                                      settings) -> {ok, class:""|unreachable|unauthorized|tls|server|notConfigured, message}; 200 unless the values are invalid
//	GET    /api/certs/freshness           [{name, fqdn, status}] up to date | update available | no active cert | unknown
//
// Apply and restore answer HTTP 200 for the outcomes applied, no_changes,
// validation_failed and rolled_back (the body carries outcome and message), and
// HTTP 500 {"error"} only for a non-nil error (rollback_failed or an
// infrastructure failure). The CertMachine routes answer 409 "CertMachine is
// not configured" when no certmachine.url is set. Pull failures: unreachable,
// TLS, unauthorized, integrity and server errors are 502; a CertMachine
// refusal is 409 with CertMachine's own message.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// maxBodyBytes bounds every JSON request body (a model is a few KB).
const maxBodyBytes = 1 << 20

// actionTimeout bounds a request's calls into the driver and CertMachine.
const actionTimeout = 2 * time.Minute

type lastApply struct {
	Time    time.Time `json:"time"`
	Outcome Outcome   `json:"outcome"`
	Message string    `json:"message"`
}

// live is everything a settings change rebuilds: handlers take one snapshot
// per request (server.rt) so an in-flight request finishes on the old set.
type live struct {
	driver   Driver
	certs    *CertStore
	client   *CertMachineClient // nil when certmachine.url is unset
	applier  *Applier
	warnDays int
	settings Settings
	apiKey   string
	// unavailable is the reason the module cannot drive HAProxy (unsupported
	// OS); driver, certs and applier are nil and every API route but settings 503s.
	unavailable string
}

type server struct {
	cfg     config.HaproxyConfig
	models  *ModelStore
	log     *OpLog
	dataDir string
	maxSubs int
	subs    atomic.Int64
	mk      driverFactory
	putMu   sync.Mutex // serializes settings saves

	mu   sync.Mutex
	last *lastApply

	rtMu sync.RWMutex
	cur  *live
}

// rt is the one accessor for the settings-dependent collaborators.
func (s *server) rt() *live {
	s.rtMu.RLock()
	defer s.rtMu.RUnlock()
	return s.cur
}

func (s *server) swap(next *live) {
	s.rtMu.Lock()
	s.cur = next
	s.rtMu.Unlock()
}

type handlerFuncs map[string]http.HandlerFunc

// methods dispatches on the HTTP method; any other method is a 405 with Allow.
func methods(m handlerFuncs) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h, ok := m[r.Method]; ok {
			h(w, r)
			return
		}
		allow := make([]string, 0, len(m))
		for k := range m {
			allow = append(allow, k)
		}
		w.Header().Set("Allow", strings.Join(allow, ", "))
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) routes(staticDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", methods(handlerFuncs{"GET": s.handleStatus}))
	mux.HandleFunc("/api/model", methods(handlerFuncs{"GET": s.handleModelGet, "PUT": s.handleModelPut}))
	mux.HandleFunc("/api/model/check", methods(handlerFuncs{"GET": s.handleModelCheck}))
	mux.HandleFunc("/api/import", methods(handlerFuncs{"POST": s.handleImport}))
	mux.HandleFunc("/api/changes", methods(handlerFuncs{"GET": s.handleChanges}))
	mux.HandleFunc("/api/raw", methods(handlerFuncs{"GET": s.handleRaw}))
	mux.HandleFunc("/api/stats", methods(handlerFuncs{"GET": s.handleStats}))
	mux.HandleFunc("/api/check", methods(handlerFuncs{"POST": s.handleCheck}))
	mux.HandleFunc("/api/apply", methods(handlerFuncs{"POST": s.handleApply}))
	mux.HandleFunc("/api/reload", methods(handlerFuncs{"POST": s.serviceAction("reload", (*Applier).Reload)}))
	mux.HandleFunc("/api/restart", methods(handlerFuncs{"POST": s.serviceAction("restart", (*Applier).Restart)}))
	mux.HandleFunc("/api/start", methods(handlerFuncs{"POST": s.serviceAction("start", (*Applier).Start)}))
	mux.HandleFunc("/api/settings", methods(handlerFuncs{"GET": s.handleSettingsGet, "PUT": s.handleSettingsPut}))
	mux.HandleFunc("/api/settings/test-connection", methods(handlerFuncs{"POST": s.handleSettingsTestConnection}))
	mux.HandleFunc("/api/backups", methods(handlerFuncs{"GET": s.handleBackups}))
	mux.HandleFunc("/api/backups/{name}", methods(handlerFuncs{"GET": s.handleBackupView}))
	mux.HandleFunc("/api/backups/{name}/restore", methods(handlerFuncs{"POST": s.handleBackupRestore}))
	mux.HandleFunc("/api/ops", methods(handlerFuncs{"GET": s.handleOps}))
	mux.HandleFunc("/api/ops/stream", methods(handlerFuncs{"GET": s.handleOpsStream}))
	mux.HandleFunc("/api/certs", methods(handlerFuncs{"GET": s.handleCerts}))
	mux.HandleFunc("/api/certs/importable", methods(handlerFuncs{"GET": s.handleCertImportable}))
	mux.HandleFunc("/api/certs/import", methods(handlerFuncs{"POST": s.handleCertImport}))
	mux.HandleFunc("/api/certs/pull", methods(handlerFuncs{"POST": s.handleCertPull}))
	mux.HandleFunc("/api/certs/freshness", methods(handlerFuncs{"GET": s.handleFreshness}))
	mux.HandleFunc("/api/certs/{name}", methods(handlerFuncs{"DELETE": s.handleCertDelete}))
	mux.HandleFunc("/api/certs/{name}/enabled", methods(handlerFuncs{"PUT": s.handleCertEnabled}))
	mux.HandleFunc("/api/certmachine/certs", methods(handlerFuncs{"GET": s.handleCertMachineCerts}))
	mux.HandleFunc("/api/coverage", methods(handlerFuncs{"GET": s.handleCoverage}))
	mux.Handle("/", static.NewHandler(staticDir))
	// Unsupported OS: the module still serves its UI and settings routes but
	// every other API route answers a scoped 503 with the reason.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reason := s.rt().unavailable; reason != "" && strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/settings") {
			scoped503Handler(reason).ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func fail(w http.ResponseWriter, status int, msg string) { response.WriteError(w, status, msg) }

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		response.WriteDecodeError(w, err)
		return false
	}
	return true
}

func reqCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), actionTimeout)
}

// ---- status / model ------------------------------------------------------

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	svc := ServiceStatus{Detail: "status unavailable"}
	version := ""
	if st, err := rt.applier.Status(ctx); err == nil {
		svc, version = st.ServiceStatus, st.Version
	}
	pending := false
	if ch, err := rt.applier.Pending(ctx); err == nil {
		pending = ch.HasChanges
	}
	s.mu.Lock()
	last := s.last
	s.mu.Unlock()
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"diagnostics":           s.diagnostics(rt, ctx),
		"module":                "haproxy",
		"moduleVersion":         Version,
		"service":               svc,
		"version":               version,
		"lastApply":             last,
		"pending":               pending,
		"certmachineConfigured": rt.client != nil,
	})
}

// diagnostics asks the driver for its OS conditions; a panic is logged to the
// op log and answered as none, so /api/status never fails over it.
func (s *server) diagnostics(rt *live, ctx context.Context) (lines []string) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Addf("status: reading diagnostics failed: %v", r)
			lines = nil
		}
		if lines == nil {
			lines = []string{}
		}
	}()
	return rt.driver.Diagnostics(ctx)
}

func (s *server) handleModelGet(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(filepath.Join(s.dataDir, modelStateFile))
	response.WriteJSON(w, http.StatusOK, struct {
		*Model
		Imported bool `json:"imported"`
	}{s.models.Snapshot(), err == nil})
}

func (s *server) handleModelPut(w http.ResponseWriter, r *http.Request) {
	m := DefaultModel()
	if !decodeBody(w, r, m) {
		return
	}
	if err := m.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.models.Update(func(cur *Model) { *cur = *m }); err != nil {
		fail(w, http.StatusInternalServerError, "saving the model failed")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleModelCheck(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	issues, err := s.checkStored(rt, ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "listing certificates failed")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

func (s *server) handleImport(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	var req struct {
		Commit bool `json:"commit"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	data, err := rt.driver.PrivilegedRead(ctx, rt.driver.ConfigPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(w, http.StatusNotFound, "no live HAProxy config was found to import")
			return
		}
		s.log.Addf("import: reading the live config failed: %v", err)
		fail(w, http.StatusInternalServerError, "reading the live config failed")
		return
	}
	m, rep, err := Import(data)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if req.Commit {
		if _, err := s.models.Update(func(cur *Model) { *cur = *m }); err != nil {
			fail(w, http.StatusInternalServerError, "saving the imported model failed")
			return
		}
		s.log.Add("import: live config imported into the model")
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"model": m, "report": rep})
}

// ---- changes / check / apply --------------------------------------------

func (s *server) handleChanges(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	ch, err := rt.applier.Pending(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, ch)
}

func (s *server) handleRaw(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	_, cfgText, crtText, err := rt.applier.renderCandidate()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"config": cfgText, "crtList": crtText})
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	info, rows, err := FetchStats(ctx, rt.driver.StatsSocketPath(), statsTimeout)
	if err != nil {
		var ue *ErrStatsUnavailable
		if !errors.As(err, &ue) {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.WriteJSON(w, http.StatusOK, map[string]any{"available": false, "message": ue.Error(), "info": StatsInfo{}, "rows": []StatRow{}})
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"available": true, "message": "", "info": info, "rows": rows})
}

func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	res, err := rt.applier.Check(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, res)
}

// writeApplyResult maps an ApplyResult/error to the HTTP contract and records
// the last-apply summary.
func (s *server) writeApplyResult(w http.ResponseWriter, res ApplyResult, err error) {
	if res.Outcome != "" {
		s.mu.Lock()
		s.last = &lastApply{Time: time.Now(), Outcome: res.Outcome, Message: res.Message}
		s.mu.Unlock()
	}
	if err != nil {
		msg := res.Message
		if msg == "" {
			msg = err.Error()
		}
		fail(w, http.StatusInternalServerError, msg)
		return
	}
	response.WriteJSON(w, http.StatusOK, res)
}

func (s *server) handleApply(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	issues, err := s.checkStored(rt, ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "listing certificates failed")
		return
	}
	if hasErrorIssue(issues) {
		response.WriteJSON(w, http.StatusConflict, map[string]any{
			"error":  "the model has errors; fix them before applying",
			"issues": issues,
		})
		return
	}
	res, err := rt.applier.Apply(ctx)
	s.writeApplyResult(w, res, err)
}

func (s *server) serviceAction(name string, fn func(*Applier, context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := reqCtx(r)
		defer cancel()
		if err := fn(s.rt().applier, ctx); err != nil {
			fail(w, http.StatusInternalServerError, name+" failed: "+err.Error())
			return
		}
		response.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ---- backups / ops -------------------------------------------------------

func (s *server) handleBackups(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	list, err := rt.applier.ListBackups(ctx)
	if err != nil {
		s.log.Addf("backups: could not list them: %v", err) // quiet; the page just shows none
		list = []Backup{}
	}
	response.WriteJSON(w, http.StatusOK, list)
}

func (s *server) handleBackupView(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	name := r.PathValue("name")
	data, err := rt.applier.ViewBackup(ctx, name)
	switch {
	case err == nil:
		response.WriteJSON(w, http.StatusOK, map[string]string{"name": name, "content": string(data)})
	case errors.Is(err, os.ErrNotExist):
		fail(w, http.StatusNotFound, "no such backup")
	case validateBackupName(name) != nil, !isBackupName(name):
		fail(w, http.StatusBadRequest, "not a backup file name")
	default:
		fail(w, http.StatusInternalServerError, "reading the backup failed")
	}
}

func isBackupName(name string) bool {
	_, _, _, ok := classifyBackup(name)
	return ok
}

func (s *server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	name := r.PathValue("name")
	if validateBackupName(name) != nil || !isBackupName(name) {
		fail(w, http.StatusBadRequest, "not a backup file name")
		return
	}
	res, err := rt.applier.Restore(ctx, name)
	if err != nil && res.Outcome == "" && errors.Is(err, os.ErrNotExist) {
		fail(w, http.StatusNotFound, "no such backup")
		return
	}
	s.writeApplyResult(w, res, err)
}

func (s *server) handleOps(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, s.log.Snapshot())
}

func (s *server) handleOpsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	if s.subs.Add(1) > int64(s.maxSubs) {
		s.subs.Add(-1)
		fail(w, http.StatusServiceUnavailable, "too many log streams")
		return
	}
	defer s.subs.Add(-1)

	ch, cancel := s.log.Subscribe()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ---- certs ---------------------------------------------------------------

var errNoCertMachine = errors.New("CertMachine is not configured")

// requireClient writes the 409 and returns false when CertMachine is not set up.
func (s *server) requireClient(rt *live, w http.ResponseWriter) bool {
	if rt.client == nil {
		fail(w, http.StatusConflict, errNoCertMachine.Error())
		return false
	}
	return true
}

// cmError maps a CertMachine client error to an HTTP status and a safe,
// operator-readable message (never the API key or key material).
func cmError(err error) (int, string) {
	switch {
	case errors.Is(err, CertMachineErrUnauthorized):
		return http.StatusBadGateway, "CertMachine rejected the API key; check the configured CertMachine API key"
	case errors.Is(err, CertMachineErrIntegrity):
		return http.StatusBadGateway, err.Error()
	case errors.Is(err, CertMachineErrRefused):
		return http.StatusConflict, strings.TrimPrefix(err.Error(), CertMachineErrRefused.Error()+": ")
	case errors.Is(err, CertMachineErrTLS):
		return http.StatusBadGateway, err.Error()
	case errors.Is(err, CertMachineErrUnreachable):
		return http.StatusBadGateway, "CertMachine is unreachable"
	case errors.Is(err, CertMachineErrServer):
		return http.StatusBadGateway, err.Error()
	}
	return http.StatusBadGateway, "CertMachine request failed"
}

type certView struct {
	CertManaged
	Details      *CertDetails `json:"details,omitempty"`
	DetailsError string       `json:"detailsError,omitempty"`
	Removable    bool         `json:"removable"`
	InUseReason  string       `json:"inUseReason"`
}

func (s *server) handleCerts(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	list, err := rt.certs.List(ctx)
	if err != nil {
		s.log.Addf("certs: listing failed: %v", err)
		fail(w, http.StatusInternalServerError, "listing certificates failed")
		return
	}
	now := time.Now()
	live := s.readLiveCrtList(rt, ctx)
	rows := make([]certView, 0, len(list.Certs))
	for _, c := range list.Certs {
		v := certView{CertManaged: c}
		kind, reason := s.certUsage(rt, ctx, c, live)
		v.Removable, v.InUseReason = kind == certFree, reason
		if rt.client != nil && c.CertMachine.ID != 0 {
			if cm, err := rt.client.GetCert(ctx, c.CertMachine.ID); err != nil {
				_, v.DetailsError = cmError(err)
			} else {
				d := CertDetailsFrom(cm, now, rt.warnDays)
				v.Details = &d
			}
		}
		rows = append(rows, v)
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"certs": rows, "unmanaged": list.Unmanaged})
}

type cmCertView struct {
	CertMachineCert
	Covers bool `json:"covers"`
}

func certNames(c CertMachineCert) []string {
	return append([]string{c.FQDN}, c.SANs.DNS...)
}

func (s *server) handleCertMachineCerts(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	if !s.requireClient(rt, w) {
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	q := r.URL.Query()
	forFqdn := strings.TrimSpace(q.Get("forFqdn"))
	all := q.Get("all") == "1"
	status := "active"
	if all {
		status = ""
	}
	certs, err := rt.client.ListCerts(ctx, "", status)
	if err != nil {
		code, msg := cmError(err)
		fail(w, code, msg)
		return
	}
	out := make([]cmCertView, 0, len(certs))
	for _, c := range certs {
		covers := forFqdn != "" && coverageCoveredBy(forFqdn, certNames(c))
		if forFqdn != "" && !all && !covers {
			continue
		}
		out = append(out, cmCertView{CertMachineCert: c, Covers: covers})
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"certs": out})
}

func (s *server) handleCertPull(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	if !s.requireClient(rt, w) {
		return
	}
	var req struct {
		CertMachineID int64  `json:"certmachineId"`
		Note          string `json:"note"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.CertMachineID <= 0 {
		fail(w, http.StatusBadRequest, "certmachineId is required")
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	cert, err := rt.client.GetCert(ctx, req.CertMachineID)
	if err != nil {
		code, msg := cmError(err)
		fail(w, code, msg)
		return
	}
	if cert.Status != "active" {
		fail(w, http.StatusConflict, "only an active CertMachine certificate can be installed")
		return
	}
	res, err := rt.certs.PullAndStage(ctx, rt.driver, rt.client, cert, req.Note)
	if err != nil {
		if isCMError(err) {
			code, msg := cmError(err)
			s.log.Addf("cert pull %d refused: %s", req.CertMachineID, msg)
			fail(w, code, msg)
			return
		}
		s.log.Addf("cert pull %d failed: %v", req.CertMachineID, err)
		fail(w, http.StatusInternalServerError, "installing the certificate failed; see the operations log")
		return
	}
	s.log.Addf("cert pulled: %s (CertMachine id %d)", res.Name, req.CertMachineID)
	sup := res.Superseded
	if sup == nil {
		sup = []string{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"name": res.Name, "superseded": sup})
}

func isCMError(err error) bool {
	for _, e := range []error{CertMachineErrUnreachable, CertMachineErrUnauthorized, CertMachineErrTLS,
		CertMachineErrServer, CertMachineErrRefused, CertMachineErrIntegrity} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// findCert returns the tracking row called name.
func (s *server) findCert(rt *live, ctx context.Context, name string) (CertManaged, bool, error) {
	list, err := rt.certs.List(ctx)
	if err != nil {
		return CertManaged{}, false, err
	}
	for _, c := range list.Certs {
		if c.Name == name {
			return c, true, nil
		}
	}
	return CertManaged{}, false, nil
}

func (s *server) handleCertImportable(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	found, err := rt.certs.Importable(ctx)
	if err != nil {
		s.log.Addf("certs: scanning for importable certificates failed: %v", err)
		response.WriteJSON(w, http.StatusOK, []ImportableCert{})
		return
	}
	response.WriteJSON(w, http.StatusOK, found)
}

func (s *server) handleCertImport(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	var req struct {
		Names []string `json:"names"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	adopted, err := rt.certs.Import(ctx, req.Names)
	if err != nil {
		s.log.Addf("certs: import failed: %v", err)
		fail(w, http.StatusInternalServerError, "importing the certificates failed")
		return
	}
	s.log.Addf("certs: imported %d existing certificate(s)", len(adopted))
	response.WriteJSON(w, http.StatusOK, map[string]any{"imported": adopted})
}

func (s *server) handleCertEnabled(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	name := r.PathValue("name")
	_, ok, err := s.findCert(rt, ctx, name)
	if err != nil {
		fail(w, http.StatusInternalServerError, "listing certificates failed")
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "no such certificate")
		return
	}
	if err := rt.certs.SetEnabled(name, req.Enabled); err != nil {
		fail(w, http.StatusInternalServerError, "saving the certificate state failed")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleCertDelete(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	name := r.PathValue("name")
	if err := NamingValidateName(name); err != nil {
		fail(w, http.StatusBadRequest, "not a managed certificate name")
		return
	}
	// Check and delete under the Applier's lock so a Remove cannot run
	// mid-Apply or between a crt-list install and its reload.
	status, msg := 0, ""
	_ = rt.applier.WithLock(func() error {
		row, ok, err := s.findCert(rt, ctx, name)
		if err != nil {
			status, msg = http.StatusInternalServerError, "listing certificates failed"
			return nil
		}
		if !ok {
			status, msg = http.StatusNotFound, "no such certificate"
			return nil
		}
		kind, _ := s.certUsage(rt, ctx, row, s.readLiveCrtList(rt, ctx))
		switch kind {
		case certUnreadable:
			status, msg = http.StatusInternalServerError, "reading the live crt-list failed"
			return nil
		case certInLive:
			status, msg = http.StatusConflict, "this certificate is still referenced by the live crt-list; Apply the disable first"
			return nil
		case certInService:
			status, msg = http.StatusConflict, "this certificate is in use by a service; remove it from the service first"
			return nil
		}
		if err := rt.certs.Remove(ctx, rt.driver, name, nil); err != nil {
			s.log.Addf("cert remove %s failed: %v", name, err)
			status, msg = http.StatusInternalServerError, "removing the certificate failed; see the operations log"
		}
		return nil
	})
	if status != 0 {
		fail(w, status, msg)
		return
	}
	s.log.Addf("cert removed: %s", name)
	response.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// liveCrtList is the live crt-list as read once for a request; a missing file
// is an empty list, any other read failure is err.
type liveCrtList struct {
	data string
	err  error
}

// readLiveCrtList reads the LIVE crt-list. HAProxy loads it on its next
// restart, start or host reboot, so a cert it still names must keep its file.
func (s *server) readLiveCrtList(rt *live, ctx context.Context) liveCrtList {
	b, err := rt.driver.PrivilegedRead(ctx, rt.driver.CrtListPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return liveCrtList{err: err}
	}
	return liveCrtList{data: string(b)}
}

type certUse int

const (
	certFree certUse = iota
	certInLive
	certInService
	certUnreadable
)

const (
	reasonInLive     = "Listed in the live configuration. Disable it, then Apply, before removing it."
	reasonUnreadable = "Could not read the live configuration."
	reasonServiceFmt = "Enabled and used by service %s."
)

// certUsage is the single "is this cert in use" decision shared by the list
// (removable / inUseReason) and Remove (the 409 backstop). Unreadable live
// state is treated as in use.
func (s *server) certUsage(rt *live, _ context.Context, row CertManaged, live liveCrtList) (certUse, string) {
	if live.err != nil {
		return certUnreadable, reasonUnreadable
	}
	if crtListReferences(live.data, rt.driver.CertsDir(), row.Name) {
		return certInLive, reasonInLive
	}
	if row.Enabled {
		for _, svc := range s.models.Snapshot().Services {
			if strings.EqualFold(svc.Cert.FQDN, row.CertMachine.FQDN) {
				return certInService, fmt.Sprintf(reasonServiceFmt, svc.Name)
			}
		}
	}
	return certFree, ""
}

// crtListReferences reports whether a crt-list names the cert by its full path
// or its exact file name (first field of a non-comment line).
func crtListReferences(crtList, certsDir, name string) bool {
	full := filepath.Join(certsDir, name)
	for _, line := range strings.Split(crtList, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if f[0] == full || filepath.Base(f[0]) == name {
			return true
		}
	}
	return false
}

// removeSuperseded is the Applier's AfterSuccess hook. The superseded set is
// read from certs.json each time (rows explicitly marked Superseded by a pull
// and still disabled), so it survives a restart. A row the owner disabled on
// purpose is never in it.
func (rt *live) removeSuperseded(ctx context.Context) error {
	list, err := rt.certs.List(ctx)
	if err != nil {
		return err
	}
	var drop []string
	for _, c := range list.Certs {
		if c.Superseded && !c.Enabled {
			drop = append(drop, c.Name)
		}
	}
	if len(drop) == 0 {
		return nil
	}
	return rt.certs.RemoveSuperseded(ctx, rt.driver, drop)
}

// ---- coverage / freshness -----------------------------------------------

func (s *server) handleCoverage(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	ctx, cancel := reqCtx(r)
	defer cancel()
	var svcs []CoverageService
	for _, svc := range s.models.Snapshot().Services {
		if strings.TrimSpace(svc.Cert.FQDN) == "" {
			continue
		}
		svcs = append(svcs, CoverageService{Name: svc.Name, FQDNs: svc.FQDNs, CertFQDN: svc.Cert.FQDN})
	}
	cache := map[string][]string{}
	lookup := func(certFQDN string) ([]string, error) {
		if rt.client == nil {
			return nil, errNoCertMachine
		}
		if names, ok := cache[certFQDN]; ok {
			return names, nil
		}
		c, err := rt.client.ActiveCertForFQDN(ctx, certFQDN)
		if err != nil {
			return nil, err
		}
		names := []string{}
		if c != nil {
			names = certNames(*c)
		}
		cache[certFQDN] = names
		return names, nil
	}
	res := CoverageCheck(svcs, lookup)
	if res == nil {
		res = []CoverageResult{}
	}
	response.WriteJSON(w, http.StatusOK, res)
}

func (s *server) handleFreshness(w http.ResponseWriter, r *http.Request) {
	rt := s.rt()
	if !s.requireClient(rt, w) {
		return
	}
	ctx, cancel := reqCtx(r)
	defer cancel()
	list, err := rt.certs.List(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, "listing certificates failed")
		return
	}
	type row struct {
		Name   string `json:"name"`
		FQDN   string `json:"fqdn"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	out := []row{}
	for _, c := range list.Certs {
		if !c.Enabled || c.CertMachine.ID == 0 {
			continue
		}
		v := row{Name: c.Name, FQDN: c.CertMachine.FQDN}
		f, err := CertFreshnessFor(ctx, rt.client, c.CertMachine.ID, c.CertMachine.FQDN)
		if err != nil {
			v.Status = "unknown"
			_, v.Error = cmError(err)
		} else {
			v.Status = string(f)
		}
		out = append(out, v)
	}
	response.WriteJSON(w, http.StatusOK, out)
}
