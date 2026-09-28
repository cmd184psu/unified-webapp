// main_test.go -- installs a small pool of pre-generated RSA-2048 leaf keys
// as the package's genLeafKey seam (pki.go) for the whole certmachine test
// suite, cutting the suite's wall-clock time: RSA-2048 keygen is the single
// largest cost driver here, one full keygen per leaf certificate generated
// across hundreds of GenerateLeaf calls in this package's tests. Reusing a
// small pool round-robin does not change what is exercised -- every leaf
// still gets a real RSA-2048 key, possibly shared with another cert, which
// is fine: nothing in this package's tests asserts that two certs' keys
// differ (only that two certs' *serials* differ -- see
// TestGenerateLeafSerialsAreRandomNotTimestamps in pki_test.go, which does
// not touch keys at all).
//
// genLeafKey is installed exactly once, here, before m.Run() starts any
// test, and is never mutated again afterward -- required for it to be
// race-safe as a package var read concurrently by every t.Parallel() test
// that calls GenerateLeaf.
package certmachine

import (
	"crypto/rsa"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
)

// leafKeyPoolSize is arbitrary but small: large enough that a handful of
// concurrently running parallel tests aren't all sharing one identical key
// (which would be surprising to read in a failure dump), small enough that
// pre-generating the pool itself stays fast.
const leafKeyPoolSize = 8

func TestMain(m *testing.M) {
	pool := make([]*rsa.PrivateKey, leafKeyPoolSize)
	for i := range pool {
		key, err := defaultGenLeafKey()
		if err != nil {
			fmt.Fprintf(os.Stderr, "certmachine: TestMain: pre-generate leaf key pool: %v\n", err)
			os.Exit(1)
		}
		pool[i] = key
	}

	var next atomic.Uint64
	genLeafKey = func() (*rsa.PrivateKey, error) {
		i := next.Add(1) - 1
		return pool[i%uint64(leafKeyPoolSize)], nil
	}

	os.Exit(m.Run())
}

// TestDefaultGenLeafKeyIsRealRSA2048 exercises defaultGenLeafKey directly --
// bypassing the pooled genLeafKey TestMain installs above -- so the suite
// still has direct coverage of the real, unpooled generator GenerateLeaf
// uses in production. Every pool entry above is itself built by calling this
// same function, so every GenerateLeaf call in this package's tests already
// runs against a real RSA-2048 key, just possibly reused across certs.
func TestDefaultGenLeafKeyIsRealRSA2048(t *testing.T) {
	t.Parallel()
	key, err := defaultGenLeafKey()
	if err != nil {
		t.Fatalf("defaultGenLeafKey: %v", err)
	}
	if key == nil {
		t.Fatal("defaultGenLeafKey returned a nil key")
	}
	if got := key.N.BitLen(); got != 2048 {
		t.Fatalf("defaultGenLeafKey key bit length = %d, want 2048", got)
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("defaultGenLeafKey key failed Validate: %v", err)
	}
}
