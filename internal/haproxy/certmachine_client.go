package haproxy

// The CertMachine REST client (FRD §6.6, D10/D15/D17, FR-H40..H47). CertMachine
// is a separate service reached by URL and is the ONLY source of certs and cert
// information: this client lists them, fetches one, finds the active cert for an
// FQDN, and pulls a verified haproxy.pem. It never parses a certificate — hence
// no crypto/x509 anywhere in this package (a test enforces it). Every download
// is verified against the ETag and identity headers before any byte is handed
// back for writing (FR-H45); a missing ETag is "this CertMachine needs updating"
// (FR-H47), never a reason to skip the check.

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Distinct, visible failure classes (FR-H44). Each is a sentinel so callers can
// errors.Is them to a user-facing message. None of their messages, or any error
// this package builds, ever carries the API key or key material.
var (
	CertMachineErrUnreachable  = errors.New("certmachine: unreachable")
	CertMachineErrUnauthorized = errors.New("certmachine: unauthorized")
	CertMachineErrTLS          = errors.New("certmachine: TLS error")
	CertMachineErrServer       = errors.New("certmachine: server error")
	CertMachineErrRefused      = errors.New("certmachine: export refused")
	CertMachineErrIntegrity    = errors.New("certmachine: integrity check failed")
)

// certMachineRedacted is what the API key renders as anywhere it might be
// printed. The raw value is read only via an explicit string() conversion for
// the Authorization header.
const certMachineRedacted = "(set)"

// maxCertMachineBody bounds a downloaded bundle. A haproxy.pem is a few KB; a
// larger body is refused rather than materialized.
const maxCertMachineBody = 1 << 20

// CertMachineAPIKey is the Bearer key. Its String/GoString/MarshalText all
// redact, so the secret can never reach a log line, an error, or a %v/%+v/%#v
// of the settings (FR-H40; mirrors the admin LDAP bind-password redaction).
type CertMachineAPIKey string

func (CertMachineAPIKey) String() string               { return certMachineRedacted }
func (CertMachineAPIKey) GoString() string             { return `"` + certMachineRedacted + `"` }
func (CertMachineAPIKey) MarshalText() ([]byte, error) { return []byte(certMachineRedacted), nil }

// CertMachineSettings is the module's connection to CertMachine. InsecureSkipVerify
// is the documented, off-by-default toggle; CAFile names a root to trust, wired
// by the caller via CertMachineWithHTTPClient so the x509 pool never has to be
// built in this package (D15).
type CertMachineSettings struct {
	URL                string
	APIKey             CertMachineAPIKey
	CAFile             string
	InsecureSkipVerify bool
}

// String redacts the API key; because it is a Stringer, %v and %+v use it too.
func (s CertMachineSettings) String() string {
	return fmt.Sprintf("CertMachineSettings{URL:%q APIKey:%s CAFile:%q InsecureSkipVerify:%t}",
		s.URL, s.APIKey, s.CAFile, s.InsecureSkipVerify)
}

// CertMachineSANs is the subject-alternative-name list exactly as CertMachine
// reports it.
type CertMachineSANs struct {
	DNS []string `json:"dns"`
	IP  []string `json:"ip"`
}

// CertMachineCert is one cert as CertMachine's list/detail API returns it. The
// editor keeps no copy of these details; it reads them live (D15). CASubject is
// the signer subject, used as the issuer only when CertMachine supplies it.
type CertMachineCert struct {
	ID          int64           `json:"id"`
	FQDN        string          `json:"fqdn"`
	SANs        CertMachineSANs `json:"sans"`
	NotBefore   *string         `json:"notBefore"`
	NotAfter    *string         `json:"notAfter"`
	Fingerprint *string         `json:"fingerprint"`
	Status      string          `json:"status"`
	CASubject   *string         `json:"caSubject,omitempty"`
	Stale       bool            `json:"stale"`
}

// CertMachinePuller is the slice of the client PullAndStage needs (so the store
// can be tested with a stub).
type CertMachinePuller interface {
	PullHAProxyPEM(ctx context.Context, id int64, wantFingerprint string) ([]byte, error)
}

// CertActiveLookup is the slice of the client the freshness check needs.
type CertActiveLookup interface {
	ActiveCertForFQDN(ctx context.Context, fqdn string) (*CertMachineCert, error)
}

// CertMachineClient talks to one CertMachine instance.
type CertMachineClient struct {
	settings CertMachineSettings
	base     *url.URL
	http     *http.Client
}

// CertMachineOption customises a client at construction.
type CertMachineOption func(*CertMachineClient)

// CertMachineWithHTTPClient replaces the HTTP client (tests, or a caller with
// its own transport). By default the client honors CAFile/InsecureSkipVerify
// via CertMachineHTTPClient.
func CertMachineWithHTTPClient(hc *http.Client) CertMachineOption {
	return func(c *CertMachineClient) {
		if hc != nil {
			c.http = hc
		}
	}
}

// CertMachineNewClient validates the URL and the transport policy (HTTPS is
// required for a non-loopback address, since a bundle carries a private key)
// and returns a ready client.
func CertMachineNewClient(s CertMachineSettings, opts ...CertMachineOption) (*CertMachineClient, error) {
	base, err := url.Parse(strings.TrimSpace(s.URL))
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("haproxy: invalid certmachine url %q", s.URL)
	}
	if err := certMachineCheckTransport(base); err != nil {
		return nil, err
	}
	hc, err := CertMachineHTTPClient(s)
	if err != nil {
		return nil, err
	}
	c := &CertMachineClient{settings: s, base: base, http: hc}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// certMachineCheckTransport enforces §6.6: https always ok; plain http only for
// a loopback host; anything else refused with a clear message.
func certMachineCheckTransport(u *url.URL) error {
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if certMachineIsLoopback(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("haproxy: certmachine url %q uses plain http; https is required for a non-loopback address", u.Redacted())
	default:
		return fmt.Errorf("haproxy: certmachine url scheme %q is not supported (use https)", u.Scheme)
	}
}

func certMachineIsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ListCerts returns CertMachine's certs, optionally filtered by fqdn (exact,
// case-insensitive) and status (active|archived|quarantined).
func (c *CertMachineClient) ListCerts(ctx context.Context, fqdn, status string) ([]CertMachineCert, error) {
	q := url.Values{}
	if fqdn != "" {
		q.Set("fqdn", fqdn)
	}
	if status != "" {
		q.Set("status", status)
	}
	resp, err := c.get(ctx, "/api/certs", q)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.statusError(resp); err != nil {
		return nil, err
	}
	var body struct {
		Certs []CertMachineCert `json:"certs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: decode cert list: %v", CertMachineErrServer, err)
	}
	return body.Certs, nil
}

// GetCert fetches one cert's details by id.
func (c *CertMachineClient) GetCert(ctx context.Context, id int64) (CertMachineCert, error) {
	resp, err := c.get(ctx, "/api/certs/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return CertMachineCert{}, err
	}
	defer resp.Body.Close()
	if err := c.statusError(resp); err != nil {
		return CertMachineCert{}, err
	}
	var cert CertMachineCert
	if err := json.NewDecoder(resp.Body).Decode(&cert); err != nil {
		return CertMachineCert{}, fmt.Errorf("%w: decode cert: %v", CertMachineErrServer, err)
	}
	return cert, nil
}

// ActiveCertForFQDN returns the active cert for an FQDN, or nil when there is
// none (CertMachine allows at most one active cert per FQDN).
func (c *CertMachineClient) ActiveCertForFQDN(ctx context.Context, fqdn string) (*CertMachineCert, error) {
	certs, err := c.ListCerts(ctx, fqdn, "active")
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, nil
	}
	cert := certs[0]
	return &cert, nil
}

// PullHAProxyPEM downloads a cert's haproxy.pem and verifies it (FR-H45): the
// SHA-256 of the received bytes must equal the ETag, X-Cert-Id must be the id
// asked for, and X-Cert-Fingerprint must equal wantFingerprint. On any mismatch,
// a missing ETag (FR-H47), or a transport/refusal failure, it returns a typed
// error and NO bytes.
func (c *CertMachineClient) PullHAProxyPEM(ctx context.Context, id int64, wantFingerprint string) ([]byte, error) {
	resp, err := c.get(ctx, "/api/certs/"+strconv.FormatInt(id, 10)+"/files/haproxy.pem", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.statusError(resp); err != nil {
		return nil, err
	}

	etag := resp.Header.Get("ETag")
	if etag == "" {
		return nil, fmt.Errorf("%w: the download carried no ETag; this CertMachine needs updating", CertMachineErrIntegrity)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCertMachineBody+1))
	if err != nil {
		return nil, c.classify(err)
	}
	if len(body) > maxCertMachineBody {
		return nil, fmt.Errorf("%w: download exceeds the size limit", CertMachineErrIntegrity)
	}

	sum := sha256.Sum256(body)
	want := `"sha256-` + hex.EncodeToString(sum[:]) + `"`
	if etag != want {
		return nil, fmt.Errorf("%w: the body hash does not match the ETag", CertMachineErrIntegrity)
	}
	if got := resp.Header.Get("X-Cert-Id"); got != strconv.FormatInt(id, 10) {
		return nil, fmt.Errorf("%w: X-Cert-Id %q is not the requested id %d", CertMachineErrIntegrity, got, id)
	}
	if resp.Header.Get("X-Cert-Fingerprint") != wantFingerprint {
		return nil, fmt.Errorf("%w: X-Cert-Fingerprint does not match the one CertMachine listed", CertMachineErrIntegrity)
	}
	return body, nil
}

// get issues an authenticated GET. The API key travels only in the
// Authorization header, never in the URL or a log.
func (c *CertMachineClient) get(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	if q != nil {
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", CertMachineErrUnreachable, err)
	}
	if key := string(c.settings.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.classify(err)
	}
	return resp, nil
}

// statusError maps a non-200 response to a typed error. It consumes the body
// only for a 409, whose CertMachine-authored message is surfaced.
func (c *CertMachineClient) statusError(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return CertMachineErrUnauthorized
	case resp.StatusCode == http.StatusConflict:
		return fmt.Errorf("%w: %s", CertMachineErrRefused, certMachineErrMsg(resp))
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: status %d", CertMachineErrServer, resp.StatusCode)
	default:
		return fmt.Errorf("%w: unexpected status %d", CertMachineErrServer, resp.StatusCode)
	}
}

// classify turns a transport error into a typed TLS or unreachable error. The
// underlying error never contains the API key (it is a header, not the URL).
func (c *CertMachineClient) classify(err error) error {
	var verify *tls.CertificateVerificationError
	var rec tls.RecordHeaderError
	if errors.As(err, &verify) || errors.As(err, &rec) {
		return fmt.Errorf("%w: %v", CertMachineErrTLS, err)
	}
	msg := err.Error()
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate") {
		return fmt.Errorf("%w: %v", CertMachineErrTLS, err)
	}
	return fmt.Errorf("%w: %v", CertMachineErrUnreachable, err)
}

// certMachineErrMsg reads a {"error": "..."} body (bounded), for surfacing a
// 409 refusal's CertMachine-authored message.
func certMachineErrMsg(resp *http.Response) string {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var b struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &b) == nil && b.Error != "" {
		return b.Error
	}
	return strings.TrimSpace(string(data))
}
