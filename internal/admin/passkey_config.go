package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

const (
	maxPasskeyOrigins   = 64
	maxPasskeyBodyBytes = 64 << 10
)

// putPasskeyRequest is the PUT /api/config/passkey body.
type putPasskeyRequest struct {
	RPID      string   `json:"rp_id"`
	RPOrigins []string `json:"rp_origins"`
}

// validatePasskeyConfig normalizes (lower-cased RP ID, trimmed and
// de-duplicated origins) and validates a passkey configuration. The error
// text is shown verbatim by the admin UI. Both fields empty is valid and
// means passkeys are disabled.
func validatePasskeyConfig(req putPasskeyRequest) (config.PasskeyConfig, error) {
	id := strings.ToLower(strings.TrimSpace(req.RPID))
	var origins []string
	seen := map[string]bool{}
	for _, o := range req.RPOrigins {
		o = strings.TrimSpace(o)
		if o == "" || seen[o] {
			continue
		}
		seen[o] = true
		origins = append(origins, o)
	}
	if id == "" && len(origins) == 0 {
		return config.PasskeyConfig{RPOrigins: []string{}}, nil
	}
	if id == "" {
		return config.PasskeyConfig{}, fmt.Errorf("Relying Party ID is required when allowed origins are set")
	}
	if len(origins) == 0 {
		return config.PasskeyConfig{}, fmt.Errorf("at least one allowed origin is required when a Relying Party ID is set")
	}
	if len(origins) > maxPasskeyOrigins {
		return config.PasskeyConfig{}, fmt.Errorf("too many allowed origins (%d): at most %d", len(origins), maxPasskeyOrigins)
	}
	if strings.ContainsAny(id, " \t:/?#@\\") {
		return config.PasskeyConfig{}, fmt.Errorf("Relying Party ID %q must be a bare host name: no scheme, port, path or spaces", id)
	}
	if !strings.Contains(id, ".") && id != "localhost" {
		return config.PasskeyConfig{}, fmt.Errorf("Relying Party ID %q must contain at least one dot (for example example.com); a single label is not allowed", id)
	}
	for _, o := range origins {
		u, err := url.Parse(o)
		if err != nil || u.Scheme == "" {
			return config.PasskeyConfig{}, fmt.Errorf("%q is not a valid origin: use a URL like https://host.example.com", o)
		}
		host := strings.ToLower(u.Hostname())
		if host == "" {
			return config.PasskeyConfig{}, fmt.Errorf("%q has no host: an origin looks like https://host.example.com", o)
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && host == "localhost") {
			return config.PasskeyConfig{}, fmt.Errorf("%s must use https (http is only allowed for http://localhost)", o)
		}
		if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return config.PasskeyConfig{}, fmt.Errorf("%s must be an origin only: no path, query or fragment", o)
		}
		if host != id && !strings.HasSuffix(host, "."+id) {
			return config.PasskeyConfig{}, fmt.Errorf("%s is not under %s: an origin's host must be the RP ID or a subdomain of it", host, id)
		}
	}
	if id == "localhost" {
		for _, o := range origins {
			if !strings.HasPrefix(o, "http://localhost") {
				return config.PasskeyConfig{}, fmt.Errorf("Relying Party ID localhost is for development only and needs http://localhost origins; %s is not one", o)
			}
		}
	}
	return config.PasskeyConfig{RPID: id, RPOrigins: origins}, nil
}

// handlePutConfigPasskey replaces auth.passkey wholesale via mutateAuth, so
// a valid save is persisted and hot-applied (passkeys switch on or off with
// no restart); an invalid one changes nothing.
func (h *Handler) handlePutConfigPasskey(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPasskeyBodyBytes)
	var req putPasskeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	pk, err := validatePasskeyConfig(req)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.mutateAuth(func(cur config.AuthConfig) (config.AuthConfig, bool) {
		cur.Passkey = pk
		return cur, true
	}); err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, pk)
}
