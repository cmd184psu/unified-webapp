package multissh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/multissh/sshproxy"
	"cmd184psu/unified-webapp/internal/platform/response"
	"github.com/gorilla/websocket"
)

type broadcastRequest struct {
	UploadID string                   `json:"uploadId"`
	FilePath string                   `json:"filePath"`
	Targets  []broadcastTargetRequest `json:"targets"`
}

// broadcastTargetRequest is one destination of a POST /api/broadcast. Key and
// Password are exactly-one-of. Password is a Secret so a %v or %+v dump of a
// decoded request cannot print it (FR-N4).
type broadcastTargetRequest struct {
	Host      string          `json:"host"`
	Port      int             `json:"port"`
	User      string          `json:"user"`
	Key       string          `json:"key"`
	Password  sshproxy.Secret `json:"password"`
	RemoteDir string          `json:"remoteDir"`
}

type broadcastProgressFrame struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Host    string `json:"host"`
	Bytes   int64  `json:"bytes"`
	Total   int64  `json:"total"`
	State   string `json:"state"`
	Message string `json:"message"`
}

type broadcastCompleteFrame struct {
	Type string `json:"type"`
}

type broadcastEvent struct {
	frame    *broadcastProgressFrame
	complete bool
}

type broadcastJob struct {
	id string

	mu      sync.Mutex
	targets []broadcastProgressFrame
	// creds holds each target's password for the life of that target's
	// transfer and no longer: update zeroes an entry as soon as its target
	// reaches a terminal state (FR-N4).
	creds []sshproxy.Secret
	done  bool
	subs  map[chan broadcastEvent]struct{}
}

type broadcastRegistry struct {
	mu sync.Mutex

	uploads     *uploadRegistry
	sshDir      string
	transferrer sshproxy.Transferrer
	jobs        map[string]*broadcastJob
	upgrader    websocket.Upgrader
}

func newBroadcastRegistry(uploads *uploadRegistry, sshDir string, transferrer sshproxy.Transferrer) *broadcastRegistry {
	return &broadcastRegistry{
		uploads:     uploads,
		sshDir:      sshDir,
		transferrer: transferrer,
		jobs:        make(map[string]*broadcastJob),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     sameOrigin,
		},
	}
}

func (b *broadcastRegistry) createJob(targets []broadcastProgressFrame, creds []sshproxy.Secret) (string, *broadcastJob, error) {
	id, err := randomHexID(16)
	if err != nil {
		return "", nil, err
	}
	job := &broadcastJob{id: id, targets: targets, creds: creds, subs: make(map[chan broadcastEvent]struct{})}
	b.mu.Lock()
	b.jobs[id] = job
	b.mu.Unlock()
	return id, job, nil
}

func (b *broadcastRegistry) getJob(id string) (*broadcastJob, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	job, ok := b.jobs[id]
	return job, ok
}

func (j *broadcastJob) snapshot() ([]broadcastProgressFrame, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]broadcastProgressFrame, len(j.targets))
	copy(out, j.targets)
	return out, j.done
}

func (j *broadcastJob) subscribe() chan broadcastEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	ch := make(chan broadcastEvent, 32)
	j.subs[ch] = struct{}{}
	return ch
}

func (j *broadcastJob) unsubscribe(ch chan broadcastEvent) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, ok := j.subs[ch]; ok {
		delete(j.subs, ch)
		close(ch)
	}
}

func (j *broadcastJob) update(frame broadcastProgressFrame) {
	j.mu.Lock()
	if frame.Index >= 0 && frame.Index < len(j.targets) {
		j.targets[frame.Index] = frame
	}
	if frame.Index >= 0 && frame.Index < len(j.creds) && isBroadcastTerminal(frame.State) {
		j.creds[frame.Index].Zero()
	}
	done := j.done
	if !done && allBroadcastTargetsTerminal(j.targets) {
		j.done = true
		done = true
	}
	subs := make([]chan broadcastEvent, 0, len(j.subs))
	for sub := range j.subs {
		subs = append(subs, sub)
	}
	j.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub <- broadcastEvent{frame: &frame}:
		default:
		}
		if done {
			select {
			case sub <- broadcastEvent{complete: true}:
			default:
			}
		}
	}
}

func allBroadcastTargetsTerminal(targets []broadcastProgressFrame) bool {
	for _, t := range targets {
		if !isBroadcastTerminal(t.State) {
			return false
		}
	}
	return true
}

func isBroadcastTerminal(state string) bool {
	return state == "done" || state == "error"
}

// credential returns the password a target authenticated with. It exists for
// the FR-N4 lifetime test; nothing in production reads it.
func (j *broadcastJob) credential(index int) sshproxy.Secret {
	j.mu.Lock()
	defer j.mu.Unlock()
	if index < 0 || index >= len(j.creds) {
		return sshproxy.Secret{}
	}
	return j.creds[index]
}

func (s *Server) resolveBroadcastSource(req broadcastRequest) (string, string, int64, error) {
	hasUpload := strings.TrimSpace(req.UploadID) != ""
	hasFilePath := strings.TrimSpace(req.FilePath) != ""
	if hasUpload == hasFilePath {
		return "", "", 0, fmt.Errorf("specify exactly one of uploadId or filePath")
	}
	if hasUpload {
		upload, ok := s.uploads.get(req.UploadID)
		if !ok {
			return "", "", 0, fmt.Errorf("invalid upload id")
		}
		return upload.Path, upload.Name, upload.Size, nil
	}
	resolvedPath, _, err := resolveWithinRoot(s.browseRoot, req.FilePath)
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid file path")
	}
	info, err := os.Stat(resolvedPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", 0, fmt.Errorf("invalid file path")
	}
	return resolvedPath, filepath.Base(resolvedPath), info.Size(), nil
}

func (s *Server) handleBroadcastPost(w http.ResponseWriter, r *http.Request) {
	if s.broadcasts == nil || s.uploads == nil {
		response.WriteError(w, http.StatusInternalServerError, "broadcast unavailable")
		return
	}
	var req broadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	localPath, baseName, totalSize, err := s.resolveBroadcastSource(req)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Targets) < 1 || len(req.Targets) > s.maxSessions {
		response.WriteError(w, http.StatusBadRequest, fmt.Sprintf("targets must contain 1 to %d entries", s.maxSessions))
		return
	}

	type resolvedTarget struct {
		index      int
		host       string
		remotePath string
		params     sshproxy.ConnectParams
	}
	resolved := make([]resolvedTarget, 0, len(req.Targets))
	initial := make([]broadcastProgressFrame, 0, len(req.Targets))
	creds := make([]sshproxy.Secret, 0, len(req.Targets))
	for i, t := range req.Targets {
		if strings.TrimSpace(t.Host) == "" || strings.TrimSpace(t.User) == "" {
			response.WriteError(w, http.StatusBadRequest, "invalid target")
			return
		}
		// A target authenticates with a key or with a password, never both and
		// never neither -- the same rule the terminal connect path applies.
		hasKey := strings.TrimSpace(t.Key) != ""
		hasPassword := !t.Password.IsZero()
		if hasKey == hasPassword {
			response.WriteError(w, http.StatusBadRequest, "each target needs exactly one of a key or a password")
			return
		}
		var keyPath string
		if hasKey {
			keyPath, err = sshproxy.ResolveKeyPath(s.opts.SSHKeyDir, t.Key)
			if err != nil {
				response.WriteError(w, http.StatusBadRequest, "invalid target key")
				return
			}
		}
		remoteDir := strings.TrimSpace(t.RemoteDir)
		if remoteDir == "" {
			remoteDir = "/tmp"
		}
		resolved = append(resolved, resolvedTarget{
			index:      i,
			host:       t.Host,
			remotePath: path.Join(remoteDir, baseName),
			params: sshproxy.ConnectParams{
				Host:     t.Host,
				Port:     t.Port,
				User:     t.User,
				KeyPath:  keyPath,
				Password: t.Password,
			},
		})
		initial = append(initial, broadcastProgressFrame{Type: "progress", Index: i, Host: t.Host, Bytes: 0, Total: totalSize, State: "pending", Message: ""})
		creds = append(creds, t.Password)
	}

	jobID, job, err := s.broadcasts.createJob(initial, creds)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to start broadcast")
		return
	}

	for _, target := range resolved {
		go s.runBroadcastTransfer(job, target.index, target.host, target.params, localPath, target.remotePath, totalSize)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"jobId": jobID})
}

func (s *Server) runBroadcastTransfer(job *broadcastJob, index int, host string, params sshproxy.ConnectParams, localPath, remotePath string, total int64) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job.update(broadcastProgressFrame{Type: "progress", Index: index, Host: host, Bytes: 0, Total: total, State: "transferring", Message: ""})
	sshproxy.Auditf("broadcast host=%q user=%q remote=%q bytes=%d outcome=start", host, params.User, remotePath, total)

	err := s.broadcasts.transferrer.Transfer(ctx, params, localPath, remotePath, func(n int64) {
		job.update(broadcastProgressFrame{Type: "progress", Index: index, Host: host, Bytes: n, Total: total, State: "transferring", Message: ""})
	})
	if err != nil {
		sshproxy.Auditf("broadcast host=%q user=%q remote=%q outcome=failed", host, params.User, remotePath)
		job.update(broadcastProgressFrame{Type: "progress", Index: index, Host: host, Bytes: 0, Total: total, State: "error", Message: broadcastTransferErrorMessage(err)})
		return
	}
	sshproxy.Auditf("broadcast host=%q user=%q remote=%q bytes=%d outcome=ok", host, params.User, remotePath, total)
	job.update(broadcastProgressFrame{Type: "progress", Index: index, Host: host, Bytes: total, Total: total, State: "done", Message: ""})
}

func (s *Server) handleBroadcastWS(w http.ResponseWriter, r *http.Request) {
	if s.broadcasts == nil {
		response.WriteError(w, http.StatusInternalServerError, "broadcast unavailable")
		return
	}
	jobID := strings.TrimSpace(r.URL.Query().Get("job"))
	if jobID == "" {
		response.WriteError(w, http.StatusBadRequest, "missing job id")
		return
	}
	job, ok := s.broadcasts.getJob(jobID)
	if !ok {
		response.WriteError(w, http.StatusNotFound, "job not found")
		return
	}

	ws, err := s.broadcasts.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	snapshot, done := job.snapshot()
	for _, frame := range snapshot {
		_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := ws.WriteJSON(frame); err != nil {
			return
		}
	}
	if done {
		_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = ws.WriteJSON(broadcastCompleteFrame{Type: "complete"})
		return
	}

	sub := job.subscribe()
	defer job.unsubscribe(sub)
	for {
		ev, ok := <-sub
		if !ok {
			return
		}
		if ev.frame != nil {
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := ws.WriteJSON(ev.frame); err != nil {
				return
			}
		}
		if ev.complete {
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = ws.WriteJSON(broadcastCompleteFrame{Type: "complete"})
			return
		}
	}
}

func broadcastTransferErrorMessage(err error) string {
	if errors.Is(err, context.Canceled) {
		return "transfer canceled"
	}
	log.Printf("server: broadcast transfer failed: %v", err)
	return "transfer failed"
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	host := r.Host
	if originHost(origin) == host {
		return true
	}
	// R1: same diagnosis as the terminal bridge -- a Host-rewriting proxy shows
	// up here as a rejected upgrade and nothing else.
	sshproxy.Auditf("broadcast ws upgrade rejected origin=%q host=%q", origin, host)
	return false
}

func originHost(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return origin
	}
	return u.Host
}
