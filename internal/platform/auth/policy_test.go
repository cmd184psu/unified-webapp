package auth

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// TestServicePolicyRace hammers Service.policy() with concurrent readers
// while another goroutine repeatedly SwapPolicy's between two distinct
// snapshots. It must be run with -race: a reader must always observe a
// single, internally-consistent Policy -- never a torn mix of one
// snapshot's PIN table and the other's cookie domain.
func TestServicePolicyRace(t *testing.T) {
	polA := &Policy{
		Modules:      map[string][]string{"grocery": {"pin"}},
		PINs:         []config.NamedHash{{Name: "alice", Hash: hashFor(t, "1111")}},
		CookieDomain: "A",
	}
	polB := &Policy{
		Modules:      map[string][]string{"grocery": {"pin"}},
		PINs:         []config.NamedHash{{Name: "bob", Hash: hashFor(t, "2222")}},
		CookieDomain: "B",
	}

	s := &Service{now: time.Now, throttle: newThrottle(time.Now)}
	s.SwapPolicy(polA)

	// Each iteration costs up to three bcrypt compares per reader; 250
	// keeps the -race interleaving coverage while holding the test to a
	// few seconds instead of nearly a minute.
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
	// CookieDomain sentinel and the PIN table it was paired with in
	// BuildPolicy/the literal above must always match.
	for r := 0; r < 4; r++ {
		readersWG.Add(1)
		go func() {
			defer readersWG.Done()
			for i := 0; i < iterations; i++ {
				p := s.policy()
				switch p.CookieDomain {
				case "A":
					if name, ok := checkPIN(p.PINs, "1111"); !ok || name != "alice" {
						t.Errorf("torn snapshot: CookieDomain=A but PINs did not match alice's pin (name=%q ok=%v)", name, ok)
					}
					if _, ok := checkPIN(p.PINs, "2222"); ok {
						t.Error("torn snapshot: CookieDomain=A but PINs matched bob's pin")
					}
				case "B":
					if name, ok := checkPIN(p.PINs, "2222"); !ok || name != "bob" {
						t.Errorf("torn snapshot: CookieDomain=B but PINs did not match bob's pin (name=%q ok=%v)", name, ok)
					}
					if _, ok := checkPIN(p.PINs, "1111"); ok {
						t.Error("torn snapshot: CookieDomain=B but PINs matched alice's pin")
					}
				default:
					t.Errorf("torn snapshot: unexpected CookieDomain %q", p.CookieDomain)
				}
				// Also drive it through the Service method, as the gate
				// and authenticators will.
				s.checkPIN("1111")
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
	if len(p.PINs) != 0 || len(p.APIKeys) != 0 {
		t.Errorf("policy has non-empty PIN/APIKey tables: %+v", p)
	}

	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("auth.data_dir %s was created (or stat failed unexpectedly: %v); FromConfig must not touch the filesystem for a fully-unprotected config", dataDir, err)
	}
}

// TestFromConfigPinProtectedModule verifies a pin-protected config produces
// a working Service: checkPIN succeeds via the Service method, and the
// session key file was created with a restrictive mode.
func TestFromConfigPinProtectedModule(t *testing.T) {
	dataDir := t.TempDir()

	auth := config.AuthConfig{
		Modules: map[string][]string{"grocery": {"pin"}},
		PINs:    []config.NamedHash{{Name: "alice", Hash: hashFor(t, "1111")}},
		DataDir: dataDir,
	}

	svc, err := FromConfig(auth, []string{"grocery", "todo"}, false)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}

	if name, ok := svc.checkPIN("1111"); !ok || name != "alice" {
		t.Fatalf("svc.checkPIN(1111) = (%q, %v), want (alice, true)", name, ok)
	}
	if _, ok := svc.checkPIN("wrong"); ok {
		t.Fatal("svc.checkPIN(wrong) = ok, want not ok")
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
// constructs a passkeyService when passkeys are actually configured and
// used, and that a construction failure (here: an unwritable/blocked data
// directory) is propagated to the caller rather than silently ignored.
func TestBuildPolicyPropagatesPasskeyError(t *testing.T) {
	base := t.TempDir()
	blockedDataDir := filepath.Join(base, "blocked")
	// Create a plain file where the passkey data directory needs to be, so
	// os.MkdirAll inside newPasskeyService fails.
	if err := os.WriteFile(blockedDataDir, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("writing blocking file: %v", err)
	}

	auth := config.AuthConfig{
		Modules: map[string][]string{"grocery": {"passkey"}},
		Passkey: config.PasskeyConfig{RPID: "example.com", RPOrigins: []string{"https://example.com"}},
		DataDir: blockedDataDir,
	}

	if _, err := BuildPolicy(auth); err == nil {
		t.Fatal("BuildPolicy with a blocked passkey data dir returned no error, want one")
	}

	// A config that does not use "passkey" in any module must not build a
	// passkeyService (and therefore must not touch the filesystem at all
	// under an equally-blocked path).
	auth.Modules = map[string][]string{"grocery": {"pin"}}
	p, err := BuildPolicy(auth)
	if err != nil {
		t.Fatalf("BuildPolicy with passkey unused: %v", err)
	}
	if p.passkeys != nil {
		t.Error("BuildPolicy constructed a passkeyService when no module uses \"passkey\"")
	}
}

// TestSwapPolicySeesNewTables verifies that after SwapPolicy, Service
// methods observe the new snapshot's tables -- the old PIN no longer
// matches, and the new one does.
func TestSwapPolicySeesNewTables(t *testing.T) {
	dataDir := t.TempDir()

	oldAuth := config.AuthConfig{
		Modules: map[string][]string{"grocery": {"pin"}},
		PINs:    []config.NamedHash{{Name: "alice", Hash: hashFor(t, "1111")}},
		DataDir: dataDir,
	}
	svc, err := FromConfig(oldAuth, []string{"grocery"}, false)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	if name, ok := svc.checkPIN("1111"); !ok || name != "alice" {
		t.Fatalf("before swap: svc.checkPIN(1111) = (%q, %v), want (alice, true)", name, ok)
	}

	newAuth := config.AuthConfig{
		Modules: map[string][]string{"grocery": {"pin"}},
		PINs:    []config.NamedHash{{Name: "carol", Hash: hashFor(t, "9999")}},
		DataDir: dataDir,
	}
	newPolicy, err := BuildPolicy(newAuth)
	if err != nil {
		t.Fatalf("BuildPolicy: %v", err)
	}
	svc.SwapPolicy(newPolicy)

	if _, ok := svc.checkPIN("1111"); ok {
		t.Error("after swap: old pin 1111 still matches, want it rejected")
	}
	if name, ok := svc.checkPIN("9999"); !ok || name != "carol" {
		t.Fatalf("after swap: svc.checkPIN(9999) = (%q, %v), want (carol, true)", name, ok)
	}
}
