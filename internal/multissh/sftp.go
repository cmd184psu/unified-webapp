package multissh

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/multissh/sshproxy"
)

func (s *Server) handleSFTPListDir(w http.ResponseWriter, r *http.Request) {
	if s.opts.RemoteLister == nil {
		writeError(w, http.StatusInternalServerError, "remote lister unavailable")
		return
	}
	var req struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		User string `json:"user"`
		Key  string `json:"key"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.User) == "" {
		writeError(w, http.StatusBadRequest, "invalid target")
		return
	}
	keyPath, err := sshproxy.ResolveKeyPath(s.opts.SSHKeyDir, req.Key)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid target key")
		return
	}
	pathListed, entries, err := s.opts.RemoteLister.ListDir(r.Context(), sshproxy.ConnectParams{
		Host:    req.Host,
		Port:    req.Port,
		User:    req.User,
		KeyPath: keyPath,
	}, req.Path)
	if err != nil {
		log.Printf("server: sftp listdir failed: %v", err)
		writeError(w, http.StatusBadGateway, "unable to list remote directory")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"path": pathListed, "entries": entries})
}
