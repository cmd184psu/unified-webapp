// pinfiles.go implements the two pin-file management endpoints: a
// server-side directory listing of the config directory's pin-file
// candidates (GET /api/config/pin-files) and an atomic, permission-locked
// write of an operator-typed PIN into one of them (POST
// /api/config/pin-files). Neither endpoint touches auth.* config -- the UI
// separately points a module's pin_file at the written path via
// PUT /api/config/modules.
package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// --- GET /api/config/pin-files ---

// pinFileEntry is one entry in getPinFilesResponse's Files.
type pinFileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// getPinFilesResponse is the GET /api/config/pin-files response shape.
type getPinFilesResponse struct {
	ConfigDir string         `json:"config_dir"`
	Files     []pinFileEntry `json:"files"`
}

// handleGetPinFiles lists the regular, non-hidden files in the config
// file's directory, excluding the config file itself, for the operator
// console's server-side file picker -- an admin pointing a module's
// pin_file at a file picks from what actually exists next to config.json
// rather than typing a path blind.
func (h *Handler) handleGetPinFiles(w http.ResponseWriter, r *http.Request) {
	configDir, err := filepath.Abs(filepath.Dir(h.deps.ConfigPath))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "resolving config directory")
		return
	}
	configBase := filepath.Base(h.deps.ConfigPath)

	entries, err := os.ReadDir(configDir)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "listing config directory")
		return
	}

	files := make([]pinFileEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == configBase {
			continue
		}
		files = append(files, pinFileEntry{Name: name, Path: filepath.Join(configDir, name)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	response.WriteJSON(w, http.StatusOK, getPinFilesResponse{ConfigDir: configDir, Files: files})
}

// --- POST /api/config/pin-files ---

// postPinFileRequest is the POST /api/config/pin-files body: exactly one of
// Name (a bare filename resolved against the config directory) or Path (an
// absolute path, permitted only inside the config directory or matching an
// already-configured pin source) must be set, alongside the PIN to write.
type postPinFileRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
	PIN  string `json:"pin"`
}

// postPinFileResponse is the POST /api/config/pin-files success response.
type postPinFileResponse struct {
	Path string `json:"path"`
}

// handlePostPinFile writes an operator-typed PIN to a pin file, atomically
// and 0400-locked, without touching auth.* config -- the UI separately
// points a module's pin_file at the written path via a follow-up
// PUT /api/config/modules. The PIN is validated (validatePIN) but never
// echoed back, logged, or included in any error message.
func (h *Handler) handlePostPinFile(w http.ResponseWriter, r *http.Request) {
	var req postPinFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}

	if (req.Name == "") == (req.Path == "") {
		response.WriteError(w, http.StatusBadRequest, "exactly one of name or path is required")
		return
	}
	if err := validatePIN(req.PIN); err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	configDir, err := filepath.Abs(filepath.Dir(h.deps.ConfigPath))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "resolving config directory")
		return
	}

	target, err := h.resolvePinFileTarget(req, configDir)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := writePinFileAtomically(target, req.PIN); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "writing pin file")
		return
	}

	log.Printf("event=admin_pinfile_write path=%q", target)
	response.WriteJSON(w, http.StatusOK, postPinFileResponse{Path: target})
}

// validatePIN reports whether pin is acceptable to write to a pin file:
// after strings.TrimSpace it must be non-empty, identical to the raw value
// (i.e. no leading/trailing whitespace), 4-128 bytes, and contain no
// whitespace character anywhere in it. The returned error names what's
// wrong but never echoes pin itself.
func validatePIN(pin string) error {
	trimmed := strings.TrimSpace(pin)
	if trimmed == "" {
		return errors.New("pin is required")
	}
	if trimmed != pin {
		return errors.New("pin must not have leading or trailing whitespace")
	}
	if len(pin) < 4 || len(pin) > 128 {
		return errors.New("pin must be 4-128 bytes")
	}
	for _, r := range pin {
		if unicode.IsSpace(r) {
			return errors.New("pin must not contain whitespace")
		}
	}
	return nil
}

// resolvePinFileTarget resolves req's name-or-path field to an absolute
// target file path, per POST /api/config/pin-files' contract:
//
//   - name must be a bare filename (filepath.Base(name) == name) and not
//     ".", "..", or dotfile-shaped -- rejected otherwise, so a name can
//     never escape configDir; the target is always configDir/name.
//   - path is made absolute and cleaned, then permitted only when it lands
//     inside configDir (filepath.Rel not starting with ".."), or when it
//     exactly equals a pin source already configured in h.authConfig (any
//     module's PinFile, or AdminPINFile) -- letting an admin overwrite a pin
//     file that already lives outside the config directory without opening
//     a path-traversal write anywhere else on disk.
func (h *Handler) resolvePinFileTarget(req postPinFileRequest, configDir string) (string, error) {
	if req.Name != "" {
		name := req.Name
		if name != filepath.Base(name) || name == "." || name == ".." || strings.HasPrefix(name, ".") {
			return "", errors.New("name must be a bare filename, not a path or dotfile")
		}
		return filepath.Join(configDir, name), nil
	}

	target, err := filepath.Abs(req.Path)
	if err != nil {
		return "", errors.New("path could not be resolved")
	}
	target = filepath.Clean(target)

	if rel, err := filepath.Rel(configDir, target); err == nil && !strings.HasPrefix(rel, "..") {
		return target, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.authConfig.AdminPINFile != "" && target == h.authConfig.AdminPINFile {
		return target, nil
	}
	for _, m := range h.authConfig.Modules {
		if m.PinFile != "" && target == m.PinFile {
			return target, nil
		}
	}
	return "", errors.New("path is outside the config directory and not a configured pin file")
}

// writePinFileAtomically writes pin+"\n" to target with mode 0400,
// replacing any existing file at target (even one already 0400) via a
// temp-file-plus-rename within target's own directory -- os.Rename replaces
// the destination regardless of the destination's existing permissions, and
// the temp file is removed on any error path before the rename succeeds.
// Chmod is explicit rather than relying on the process umask, since the
// server's umask is not guaranteed here.
func writePinFileAtomically(target, pin string) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".pinfile-*.tmp")
	if err != nil {
		return fmt.Errorf("admin: creating temp pin file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.WriteString(pin + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("admin: writing temp pin file: %w", err)
	}
	if err := tmp.Chmod(0o400); err != nil {
		tmp.Close()
		return fmt.Errorf("admin: setting temp pin file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("admin: closing temp pin file: %w", err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("admin: replacing pin file: %w", err)
	}
	return nil
}
