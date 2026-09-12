package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// fakeLDAPConn is a scripted ldapConn used to test LDAPClient without a
// network. bindFn/searchFn are called for every Bind/Search; nil defaults
// to "succeed with no results".
type fakeLDAPConn struct {
	bindFn   func(username, password string) error
	searchFn func(req *ldap.SearchRequest) (*ldap.SearchResult, error)
	binds    []struct{ username, password string }
	closed   bool
}

func (f *fakeLDAPConn) Bind(username, password string) error {
	f.binds = append(f.binds, struct{ username, password string }{username, password})
	if f.bindFn != nil {
		return f.bindFn(username, password)
	}
	return nil
}

func (f *fakeLDAPConn) Search(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
	if f.searchFn != nil {
		return f.searchFn(req)
	}
	return &ldap.SearchResult{}, nil
}

func (f *fakeLDAPConn) Close() error {
	f.closed = true
	return nil
}

// scriptedUserConn builds a fakeLDAPConn that resolves any user search to
// userDN with the given groups, and lets the caller control whether the
// user's own Bind (identified by binding as userDN) succeeds.
func scriptedUserConn(userDN string, groups []string, userBindErr error) *fakeLDAPConn {
	return &fakeLDAPConn{
		searchFn: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			if strings.Contains(req.Filter, "member") {
				entries := make([]*ldap.Entry, 0, len(groups))
				for _, g := range groups {
					entries = append(entries, &ldap.Entry{
						DN:         "cn=" + g,
						Attributes: []*ldap.EntryAttribute{{Name: "cn", Values: []string{g}}},
					})
				}
				return &ldap.SearchResult{Entries: entries}, nil
			}
			return &ldap.SearchResult{Entries: []*ldap.Entry{{DN: userDN}}}, nil
		},
		bindFn: func(username, password string) error {
			if username == userDN {
				return userBindErr
			}
			return nil
		},
	}
}

func clientWithConn(cfg config.LDAPConfig, conn *fakeLDAPConn) *LDAPClient {
	c := NewLDAPClient(cfg)
	c.dialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) {
		return conn, nil
	}
	return c
}

func TestAuthenticateBindSuccess(t *testing.T) {
	conn := scriptedUserConn("uid=alice,ou=people,dc=example,dc=com", []string{"users"}, nil)
	c := clientWithConn(config.LDAPConfig{BaseDN: "dc=example,dc=com"}, conn)

	got, err := c.Authenticate(context.Background(), "alice", "correct-horse")
	if err != nil {
		t.Fatalf("Authenticate: unexpected error: %v", err)
	}
	if got != "alice" {
		t.Fatalf("Authenticate: got identity %q, want %q", got, "alice")
	}
	if !conn.closed {
		t.Fatal("Authenticate: connection was not closed")
	}
}

func TestAuthenticateBindFailure(t *testing.T) {
	const password = "s3cr3t-password"
	conn := scriptedUserConn("uid=alice,ou=people,dc=example,dc=com", []string{"users"}, errors.New("LDAP Result Code 49 \"Invalid Credentials\""))
	c := clientWithConn(config.LDAPConfig{BaseDN: "dc=example,dc=com"}, conn)

	_, err := c.Authenticate(context.Background(), "alice", password)
	if err == nil {
		t.Fatal("Authenticate: expected error on bad bind, got nil")
	}
	if !errors.Is(err, ErrLDAPAuth) {
		t.Fatalf("Authenticate: got error %v, want ErrLDAPAuth", err)
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("Authenticate: error %q leaks the password", err.Error())
	}
}

func TestAuthenticateEmptyPasswordRejected(t *testing.T) {
	conn := scriptedUserConn("uid=alice,ou=people,dc=example,dc=com", []string{"users"}, nil)
	c := clientWithConn(config.LDAPConfig{BaseDN: "dc=example,dc=com"}, conn)

	_, err := c.Authenticate(context.Background(), "alice", "")
	if !errors.Is(err, ErrLDAPAuth) {
		t.Fatalf("Authenticate: got error %v, want ErrLDAPAuth", err)
	}
}

func TestAuthenticateRequiredGroupsDenied(t *testing.T) {
	conn := scriptedUserConn("uid=alice,ou=people,dc=example,dc=com", []string{"users"}, nil)
	c := clientWithConn(config.LDAPConfig{
		BaseDN:         "dc=example,dc=com",
		RequiredGroups: []string{"admins"},
	}, conn)

	_, err := c.Authenticate(context.Background(), "alice", "correct-horse")
	if !errors.Is(err, ErrLDAPForbidden) {
		t.Fatalf("Authenticate: got error %v, want ErrLDAPForbidden", err)
	}
}

func TestAuthenticateRequiredGroupsEmptyAllowsAnyUser(t *testing.T) {
	conn := scriptedUserConn("uid=alice,ou=people,dc=example,dc=com", []string{"some-other-group"}, nil)
	c := clientWithConn(config.LDAPConfig{
		BaseDN:         "dc=example,dc=com",
		RequiredGroups: nil,
	}, conn)

	got, err := c.Authenticate(context.Background(), "alice", "correct-horse")
	if err != nil {
		t.Fatalf("Authenticate: unexpected error: %v", err)
	}
	if got != "alice" {
		t.Fatalf("Authenticate: got identity %q, want %q", got, "alice")
	}
}

func TestAuthorizeRequiredGroupsIntersect(t *testing.T) {
	conn := scriptedUserConn("uid=alice,ou=people,dc=example,dc=com", []string{"users", "admins"}, nil)
	c := clientWithConn(config.LDAPConfig{
		BaseDN:         "dc=example,dc=com",
		RequiredGroups: []string{"admins"},
	}, conn)

	got, err := c.Authorize(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Authorize: unexpected error: %v", err)
	}
	if got != "alice" {
		t.Fatalf("Authorize: got identity %q, want %q", got, "alice")
	}
}

func TestConnectPlumbsInsecureTLSAndTimeout(t *testing.T) {
	var got ldapDialOptions
	c := NewLDAPClient(config.LDAPConfig{
		URL:            "ldap://dc.example.com:389",
		InsecureTLS:    true,
		TimeoutSeconds: 7,
		BaseDN:         "dc=example,dc=com",
	})
	c.dialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) {
		got = opts
		return &fakeLDAPConn{}, nil
	}

	conn, err := c.connect(context.Background())
	if err != nil {
		t.Fatalf("connect: unexpected error: %v", err)
	}
	defer conn.Close()

	if got.url != "ldap://dc.example.com:389" {
		t.Errorf("connect: url = %q, want %q", got.url, "ldap://dc.example.com:389")
	}
	if !got.insecureTLS {
		t.Error("connect: insecureTLS was not plumbed through from config")
	}
	if got.timeout != 7*time.Second {
		t.Errorf("connect: timeout = %v, want %v", got.timeout, 7*time.Second)
	}
}

func TestConnectTimeoutDefaultsWhenUnset(t *testing.T) {
	var got ldapDialOptions
	c := NewLDAPClient(config.LDAPConfig{URL: "ldap://dc.example.com:389"})
	c.dialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) {
		got = opts
		return &fakeLDAPConn{}, nil
	}

	if _, err := c.connect(context.Background()); err != nil {
		t.Fatalf("connect: unexpected error: %v", err)
	}
	if got.timeout != ldap.DefaultTimeout {
		t.Errorf("connect: timeout = %v, want go-ldap default %v", got.timeout, ldap.DefaultTimeout)
	}
}

func TestConnectTimeoutCappedByContextDeadline(t *testing.T) {
	var got ldapDialOptions
	c := NewLDAPClient(config.LDAPConfig{URL: "ldap://dc.example.com:389", TimeoutSeconds: 60})
	c.dialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) {
		got = opts
		return &fakeLDAPConn{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.connect(ctx); err != nil {
		t.Fatalf("connect: unexpected error: %v", err)
	}
	if got.timeout <= 0 || got.timeout > 2*time.Second {
		t.Errorf("connect: timeout = %v, want capped to <= 2s by context deadline", got.timeout)
	}
}

func TestConnectAdminBindUsesConfiguredCredentials(t *testing.T) {
	conn := &fakeLDAPConn{}
	c := NewLDAPClient(config.LDAPConfig{
		URL:          "ldap://dc.example.com:389",
		BindDN:       "cn=service,dc=example,dc=com",
		BindPassword: "service-secret",
	})
	c.dialer = func(ctx context.Context, opts ldapDialOptions) (ldapConn, error) {
		return conn, nil
	}

	if _, err := c.connect(context.Background()); err != nil {
		t.Fatalf("connect: unexpected error: %v", err)
	}
	if len(conn.binds) != 1 {
		t.Fatalf("connect: got %d binds, want 1", len(conn.binds))
	}
	if conn.binds[0].username != "cn=service,dc=example,dc=com" || conn.binds[0].password != "service-secret" {
		t.Errorf("connect: admin bind used %+v, want service DN/password from config", conn.binds[0])
	}
}

func TestLdapNeedsStartTLS(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		startTLS bool
		want     bool
	}{
		{"ldaps url never needs StartTLS even if configured", "ldaps://dc.example.com:636", true, false},
		{"ldaps url without start_tls", "ldaps://dc.example.com:636", false, false},
		{"plain url with start_tls requests the upgrade", "ldap://dc.example.com:389", true, true},
		{"plain url without start_tls stays plaintext", "ldap://dc.example.com:389", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ldapNeedsStartTLS(tc.url, tc.startTLS); got != tc.want {
				t.Errorf("ldapNeedsStartTLS(%q, %v) = %v, want %v", tc.url, tc.startTLS, got, tc.want)
			}
		})
	}
}

func TestLdapAllowed(t *testing.T) {
	if !ldapAllowed([]string{"users"}, nil) {
		t.Error("ldapAllowed: empty required list should allow any group set")
	}
	if !ldapAllowed([]string{"users", "admins"}, []string{"admins"}) {
		t.Error("ldapAllowed: intersecting groups should be allowed")
	}
	if ldapAllowed([]string{"users"}, []string{"admins"}) {
		t.Error("ldapAllowed: disjoint groups should be denied")
	}
}
