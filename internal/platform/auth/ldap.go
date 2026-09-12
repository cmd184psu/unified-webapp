package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// ErrLDAPAuth indicates a failed LDAP authentication attempt: an unknown
// user, a bad password, or a directory error encountered while resolving
// the user. It never wraps the underlying directory error, so it is always
// safe to surface to a client without risk of leaking a password or
// directory-internal detail.
var ErrLDAPAuth = errors.New("auth: invalid ldap credentials")

// ErrLDAPForbidden indicates the user authenticated successfully but is not
// a member of any of the configured required groups.
var ErrLDAPForbidden = errors.New("auth: ldap user is not in an allowed group")

// ldapConn is the seam between LDAPClient and the directory connection. It
// captures only the methods the ported logic below actually calls, so tests
// can substitute a fake without a real LDAP server. *ldap.Conn satisfies
// this interface.
type ldapConn interface {
	Bind(username, password string) error
	Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
	Close() error
}

// ldapDialOptions is the resolved, observable set of parameters a dialer
// seam receives -- resolved from config.LDAPConfig plus the per-call
// context deadline, so a test's fake dialer can assert on exactly what was
// requested without reaching into an LDAPClient's private config.
type ldapDialOptions struct {
	url         string
	startTLS    bool
	insecureTLS bool
	timeout     time.Duration
}

// ldapDialer opens a connection to the directory described by opts. The
// real implementation is realLDAPDial; tests inject a fake.
type ldapDialer func(ctx context.Context, opts ldapDialOptions) (ldapConn, error)

// LDAPClient authenticates and authorizes users against an LDAP directory,
// per config.LDAPConfig.
type LDAPClient struct {
	cfg    config.LDAPConfig
	dialer ldapDialer
}

// NewLDAPClient builds an LDAPClient from cfg, using the real go-ldap
// dialer.
func NewLDAPClient(cfg config.LDAPConfig) *LDAPClient {
	if cfg.UserFilter == "" {
		cfg.UserFilter = "(uid=%s)"
	}
	return &LDAPClient{cfg: cfg, dialer: realLDAPDial}
}

// Authenticate binds as the user to verify the password and returns the
// resolved username, enforcing required-group membership. The returned
// error is always ErrLDAPAuth or ErrLDAPForbidden (or a connection-level
// error from dialing the directory); it never includes password.
func (c *LDAPClient) Authenticate(ctx context.Context, username, password string) (string, error) {
	if password == "" {
		return "", ErrLDAPAuth
	}
	conn, err := c.connect(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	userDN, groups, err := c.findUser(conn, username)
	if err != nil {
		return "", ErrLDAPAuth
	}
	if err := conn.Bind(userDN, password); err != nil {
		return "", ErrLDAPAuth
	}
	if !ldapAllowed(groups, c.cfg.RequiredGroups) {
		return "", ErrLDAPForbidden
	}
	return username, nil
}

// Authorize resolves a user's groups without a password bind, used to
// confirm a passkey holder is still permitted before issuing a session.
func (c *LDAPClient) Authorize(ctx context.Context, username string) (string, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_, groups, err := c.findUser(conn, username)
	if err != nil {
		return "", ErrLDAPAuth
	}
	if !ldapAllowed(groups, c.cfg.RequiredGroups) {
		return "", ErrLDAPForbidden
	}
	return username, nil
}

// connect opens a directory connection via c.dialer and, if a service bind
// DN is configured, binds as it before returning. LDAP reachability is
// deliberately not checked anywhere outside a live auth attempt (no boot
// ping), since the directory may become reachable after the webapp starts.
func (c *LDAPClient) connect(ctx context.Context) (ldapConn, error) {
	timeout := ldapTimeout(c.cfg.TimeoutSeconds)
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	opts := ldapDialOptions{
		url:         c.cfg.URL,
		startTLS:    c.cfg.StartTLS,
		insecureTLS: c.cfg.InsecureTLS,
		timeout:     timeout,
	}
	conn, err := c.dialer(ctx, opts)
	if err != nil {
		return nil, err
	}
	if c.cfg.BindDN != "" {
		if err := conn.Bind(c.cfg.BindDN, c.cfg.BindPassword); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

// ldapTimeout resolves the configured auth.ldap.timeout_seconds into a
// duration, falling back to the go-ldap default when unset.
func ldapTimeout(seconds int) time.Duration {
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return ldap.DefaultTimeout
}

// realLDAPDial is the production ldapDialer: it dials opts.url (ldaps:// or
// plain), optionally upgrading a plain connection with StartTLS, per
// opts.startTLS.
func realLDAPDial(ctx context.Context, opts ldapDialOptions) (ldapConn, error) {
	conn, err := ldap.DialURL(opts.url,
		ldap.DialWithDialer(&net.Dialer{Timeout: opts.timeout}),
		ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: opts.insecureTLS}),
	)
	if err != nil {
		return nil, err
	}
	if ldapNeedsStartTLS(opts.url, opts.startTLS) {
		if err := conn.StartTLS(&tls.Config{InsecureSkipVerify: opts.insecureTLS}); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

// ldapNeedsStartTLS reports whether the StartTLS extended operation should
// be issued after dialing rawURL: only when start_tls is configured and
// rawURL is not already an ldaps:// URL (which is TLS-wrapped from the
// first byte and never upgraded in-band).
func ldapNeedsStartTLS(rawURL string, startTLS bool) bool {
	return startTLS && !strings.HasPrefix(rawURL, "ldaps://")
}

// findUser resolves username to its DN and group memberships via
// c.cfg.BaseDN. It returns ErrLDAPAuth for an empty/filter-unsafe username,
// a search error, or a non-unique match, so callers cannot distinguish
// "no such user" from other search failures.
func (c *LDAPClient) findUser(conn ldapConn, username string) (string, []string, error) {
	if username == "" || strings.ContainsAny(username, "*,()\\\x00") {
		return "", nil, ErrLDAPAuth
	}
	filter := fmt.Sprintf(c.cfg.UserFilter, ldap.EscapeFilter(username))
	req := ldap.NewSearchRequest(c.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 0, false, filter, []string{"dn", "uid", "cn"}, nil)
	res, err := conn.Search(req)
	if err != nil || len(res.Entries) != 1 {
		return "", nil, ErrLDAPAuth
	}
	userDN := res.Entries[0].DN
	groups := c.findGroups(conn, userDN, username)
	return userDN, groups, nil
}

// findGroups resolves the group cn's userDN/username belong to, via
// c.cfg.BaseDN. A search error is treated as "no groups" rather than
// propagated, matching the ported reference behavior.
func (c *LDAPClient) findGroups(conn ldapConn, userDN, username string) []string {
	filter := fmt.Sprintf("(|(member=%s)(memberUid=%s)(uniqueMember=%s))", ldap.EscapeFilter(userDN), ldap.EscapeFilter(username), ldap.EscapeFilter(userDN))
	req := ldap.NewSearchRequest(c.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, filter, []string{"cn"}, nil)
	res, err := conn.Search(req)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		if cn := e.GetAttributeValue("cn"); cn != "" {
			out = append(out, cn)
		}
	}
	return out
}

// ldapAllowed reports whether groups intersects required. An empty required
// list allows any authenticated user.
func ldapAllowed(groups, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]bool, len(groups))
	for _, g := range groups {
		set[g] = true
	}
	for _, g := range required {
		if set[g] {
			return true
		}
	}
	return false
}
