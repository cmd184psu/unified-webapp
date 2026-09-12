package auth

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// TestOfferedMethods exercises Policy.OfferedMethods across every module
// state the two-state auth model defines: open, protected (plain), protected
// with a pin_file, protected with a passkey service configured, and the
// "admin" pseudo-module (always exactly admin_pin, regardless of its own
// Modules entry).
func TestOfferedMethods(t *testing.T) {
	tests := []struct {
		name   string
		policy *Policy
		module string
		want   []string
	}{
		{
			name:   "open module returns nil",
			policy: &Policy{Modules: map[string]ModulePolicy{}},
			module: "grocery",
			want:   nil,
		},
		{
			name:   "protected module with no pin_file and no passkey service offers only ldap",
			policy: &Policy{Modules: map[string]ModulePolicy{"menuserver": {}}},
			module: "menuserver",
			want:   []string{"ldap"},
		},
		{
			name:   "protected module with a pin_file also offers pin",
			policy: &Policy{Modules: map[string]ModulePolicy{"todo": {PinFile: "/data/todo.pin"}}},
			module: "todo",
			want:   []string{"ldap", "pin"},
		},
		{
			name: "protected module with a passkey service configured also offers passkey",
			policy: &Policy{
				Modules:  map[string]ModulePolicy{"obsidianoid": {}},
				passkeys: &passkeyService{},
			},
			module: "obsidianoid",
			want:   []string{"ldap", "passkey"},
		},
		{
			name: "protected module with both a pin_file and a passkey service offers all three",
			policy: &Policy{
				Modules:  map[string]ModulePolicy{"todo": {PinFile: "/data/todo.pin"}},
				passkeys: &passkeyService{},
			},
			module: "todo",
			want:   []string{"ldap", "passkey", "pin"},
		},
		{
			name:   "admin is always exactly admin_pin",
			policy: &Policy{Modules: map[string]ModulePolicy{}},
			module: "admin",
			want:   []string{"admin_pin"},
		},
		{
			name: "admin with its own pin_file entry is still exactly admin_pin",
			policy: &Policy{
				Modules:  map[string]ModulePolicy{"admin": {PinFile: "/data/admin.pin"}},
				passkeys: &passkeyService{},
			},
			module: "admin",
			want:   []string{"admin_pin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.policy.OfferedMethods(tt.module)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("OfferedMethods(%q) = %v, want %v", tt.module, got, tt.want)
			}
		})
	}
}

// TestServicePolicyRace hammers Service.policy() with concurrent readers
// while another goroutine repeatedly SwapPolicy's between two distinct
// snapshots. It must be run with -race: a reader must always observe a
// single, internally-consistent Policy -- never a torn mix of one
// snapshot's admin PIN and the other's cookie domain.
func TestServicePolicyRace(t *testing.T) {
	polA := &Policy{
		Modules:      map[string]ModulePolicy{"grocery": {}},
		AdminPIN:     hashFor(t, "1111"),
		CookieDomain: "A",
	}
	polB := &Policy{
		Modules:      map[string]ModulePolicy{"grocery": {}},
		AdminPIN:     hashFor(t, "2222"),
		CookieDomain: "B",
	}

	s := &Service{now: time.Now, throttle: newThrottle(time.Now)}
	s.SwapPolicy(polA)

	// Each iteration costs up to two bcrypt compares per reader; 250 keeps
	// the -race interleaving coverage while holding the test to a few
	// seconds instead of nearly a minute.
	const iterations = 250
	var stop atomic.Bool
	var writerWG sync.WaitGroup
	var readersWG sync.WaitGroup

	// Writer: swaps between polA and polB continuously until the readers
	// (below) have all finished their fixed number of iterations.
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		for i := 0; !stop.Load(); i++ {
			if i%2 == 0 {
				s.SwapPolicy(polA)
			} else {
				s.SwapPolicy(polB)
			}
		}
	}()

	// Readers: each read must see a self-consistent snapshot -- the
	// CookieDomain sentinel and the admin PIN it was paired with in the
	// literal above must always match.
	for r := 0; r < 4; r++ {
		readersWG.Add(1)
		go func() {
			defer readersWG.Done()
			for i := 0; i < iterations; i++ {
				p := s.policy()
				switch p.CookieDomain {
				case "A":
					if ok, err := checkAdminPIN(p.AdminPIN, p.AdminPINFile, "1111"); err != nil || !ok {
						t.Errorf("torn snapshot: CookieDomain=A but AdminPIN did not match 1111 (ok=%v err=%v)", ok, err)
					}
					if ok, _ := checkAdminPIN(p.AdminPIN, p.AdminPINFile, "2222"); ok {
						t.Error("torn snapshot: CookieDomain=A but AdminPIN matched 2222")
					}
				case "B":
					if ok, err := checkAdminPIN(p.AdminPIN, p.AdminPINFile, "2222"); err != nil || !ok {
						t.Errorf("torn snapshot: CookieDomain=B but AdminPIN did not match 2222 (ok=%v err=%v)", ok, err)
					}
					if ok, _ := checkAdminPIN(p.AdminPIN, p.AdminPINFile, "1111"); ok {
						t.Error("torn snapshot: CookieDomain=B but AdminPIN matched 1111")
					}
				default:
					t.Errorf("torn snapshot: unexpected CookieDomain %q", p.CookieDomain)
				}
				// Also drive it through the Service method, as the gate
				// and login handler will.
				s.checkAdminPIN("1111")
			}
		}()
	}

	readersWG.Wait()
	stop.Store(true)
	writerWG.Wait()
}

// TestFromConfigZeroAuthIsLazy verifies that a config with no modules
// protected and no admin operator PIN produces a working, empty-policy
// Service without touching the filesystem under auth.data_dir -- no
// session key directory or file is created.
func TestFromConfigZeroAuthIsLazy(t *testing.T) {
	base := t.TempDir()
	dataDir := filepath.Join(base, "authdata")

	svc, err := FromConfig(config.AuthConfig{DataDir: dataDir}, []string{"grocery", "todo"}, false)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if svc == nil {
		t.Fatal("FromConfig returned nil Service")
	}

	p := svc.policy()
	if len(p.Modules) != 0 {
		t.Errorf("policy.Modules = %v, want empty", p.Modules)
	}
	if len(p.APIKeys) != 0 {
		t.Errorf("policy has a non-empty API-key table: %+v", p)
	}

	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("auth.data_dir %s was created (or stat failed unexpectedly: %v); FromConfig must not touch the filesystem for a fully-unprotected config", dataDir, err)
	}
}

// TestFromConfigPinProtectedModule verifies a pin_file-protected module
// produces a working Service: the module's PinFile lands unchanged in the
// built policy, checkPINFile validates against it directly, and the session
// key file was created with a restrictive mode.
func TestFromConfigPinProtectedModule(t *testing.T) {
	dataDir := t.TempDir()
	pinPath := filepath.Join(t.TempDir(), "grocery.pin")
	if err := os.WriteFile(pinPath, []byte("1111"), 0400); err != nil {
		t.Fatalf("writing pin file: %v", err)
	}

	auth := config.AuthConfig{
		Modules: map[string]config.ModuleAuthConfig{"grocery": {PinFile: pinPath}},
		LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
		DataDir: dataDir,
	}

	svc, err := FromConfig(auth, []string{"grocery", "todo"}, false)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}

	p := svc.policy()
	if got := p.Modules["grocery"].PinFile; got != pinPath {
		t.Fatalf("policy.Modules[grocery].PinFile = %q, want %q", got, pinPath)
	}

	if ok, err := checkPINFile(pinPath, "1111"); err != nil || !ok {
		t.Fatalf("checkPINFile(correct) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := checkPINFile(pinPath, "wrong"); err != nil || ok {
		t.Fatalf("checkPINFile(wrong) = (%v, %v), want (false, nil)", ok, err)
	}

	keyPath := filepath.Join(dataDir, sessionKeyFileName)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat session key file: %v", err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Errorf("session key file mode = %#o, want no group/other bits", info.Mode().Perm())
	}
}

// TestBuildPolicyPropagatesPasskeyError verifies BuildPolicy only
// constructs a passkeyService when passkeys are actually usable (RPID set
// AND at least one protected non-admin module configured), and that a
// construction failure (here: an unwritable/blocked data directory) is
// propagated to the caller rather than silently ignored.
func TestBuildPolicyPropagatesPasskeyError(t *testing.T) {
	base := t.TempDir()
	blockedDataDir := filepath.Join(base, "blocked")
	// Create a plain file where the passkey data directory needs to be, so
	// os.MkdirAll inside newPasskeyService fails.
	if err := os.WriteFile(blockedDataDir, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("writing blocking file: %v", err)
	}

	auth := config.AuthConfig{
		Modules: map[string]config.ModuleAuthConfig{"grocery": {}},
		LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
		Passkey: config.PasskeyConfig{RPID: "example.com", RPOrigins: []string{"https://example.com"}},
		DataDir: blockedDataDir,
	}

	if _, err := BuildPolicy(auth); err == nil {
		t.Fatal("BuildPolicy with a blocked passkey data dir returned no error, want one")
	}

	// A config with no protected non-admin module (only "admin") must not
	// construct a passkeyService, even under an equally-blocked data dir.
	auth.Modules = map[string]config.ModuleAuthConfig{"admin": {}}
	p, err := BuildPolicy(auth)
	if err != nil {
		t.Fatalf("BuildPolicy with only admin protected: %v", err)
	}
	if p.passkeys != nil {
		t.Error("BuildPolicy constructed a passkeyService when no protected non-admin module is configured")
	}
}

// TestSwapPolicySeesNewTables verifies that after SwapPolicy, Service
// methods observe the new snapshot's fields -- the old admin PIN no longer
// matches, and the new one does.
func TestSwapPolicySeesNewTables(t *testing.T) {
	dataDir := t.TempDir()

	oldAuth := config.AuthConfig{
		AdminPIN: hashFor(t, "1111"),
		DataDir:  dataDir,
	}
	svc, err := FromConfig(oldAuth, nil, true)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if ok, err := svc.checkAdminPIN("1111"); err != nil || !ok {
		t.Fatalf("before swap: svc.checkAdminPIN(1111) = (%v, %v), want (true, nil)", ok, err)
	}

	newAuth := config.AuthConfig{
		AdminPIN: hashFor(t, "9999"),
		DataDir:  dataDir,
	}
	newPolicy, err := BuildPolicy(newAuth)
	if err != nil {
		t.Fatalf("BuildPolicy: %v", err)
	}
	svc.SwapPolicy(newPolicy)

	if ok, _ := svc.checkAdminPIN("1111"); ok {
		t.Error("after swap: old admin pin 1111 still matches, want it rejected")
	}
	if ok, err := svc.checkAdminPIN("9999"); err != nil || !ok {
		t.Fatalf("after swap: svc.checkAdminPIN(9999) = (%v, %v), want (true, nil)", ok, err)
	}
}
