package haproxy

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestNamingFileNameDeterministicAndContentAddressed(t *testing.T) {
	body := []byte("the-exact-bundle-bytes")
	want := func(fqdn string) string {
		sum := sha256.Sum256(body)
		return NamingSafeFQDN(fqdn) + "-" + hex.EncodeToString(sum[:])[:12] + ".pem"
	}

	n1 := NamingFileName("brandx.cmdhome.net", body)
	n2 := NamingFileName("brandx.cmdhome.net", body)
	if n1 != n2 {
		t.Fatalf("not deterministic: %q vs %q", n1, n2)
	}
	if n1 != want("brandx.cmdhome.net") {
		t.Fatalf("name = %q, want %q", n1, want("brandx.cmdhome.net"))
	}
	if !strings.HasSuffix(n1, ".pem") {
		t.Errorf("name %q must end in .pem", n1)
	}

	// New/changed bytes => new file name (content addressed, 6.2).
	changed := NamingFileName("brandx.cmdhome.net", []byte("different-bytes"))
	if changed == n1 {
		t.Errorf("changed bundle produced same name %q", changed)
	}
}

func TestNamingSafeFQDNMatchesCertMachineRule(t *testing.T) {
	cases := map[string]string{
		"brandx.cmdhome.net": "brandx.cmdhome.net",
		"*.cmdhome.net":      "_wildcard.cmdhome.net",
		"a/b\\c:d":           "a_b_c_d",
		"..":                 "cert", // all-filler collapses to the fallback
	}
	for in, want := range cases {
		if got := NamingSafeFQDN(in); got != want {
			t.Errorf("NamingSafeFQDN(%q) = %q, want %q", in, got, want)
		}
	}
	// No separator or traversal survives into a safe fqdn.
	for _, in := range []string{"../../etc/passwd", "a/../b", "x\x00y"} {
		got := NamingSafeFQDN(in)
		if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
			t.Errorf("NamingSafeFQDN(%q) = %q leaks a separator/traversal", in, got)
		}
	}
}

func TestNamingValidateNameRejectsSeparatorsAndTraversal(t *testing.T) {
	good := NamingFileName("brandx.cmdhome.net", []byte("x"))
	if err := NamingValidateName(good); err != nil {
		t.Errorf("a generated name must validate: %v", err)
	}
	for _, bad := range []string{"", "..", "a/b.pem", "a\\b.pem", "../x.pem", "x/..", "a..b.pem"} {
		if err := NamingValidateName(bad); err == nil {
			t.Errorf("NamingValidateName(%q) = nil, want an error", bad)
		}
	}
}

func TestNamingIsManaged(t *testing.T) {
	managed := NamingFileName("brandx.cmdhome.net", []byte("x"))
	if !NamingIsManaged(managed) {
		t.Errorf("%q should be recognized as managed", managed)
	}
	for _, unmanaged := range []string{"hand-placed.pem", "server.crt", "notes.txt", "brandx.pem", "x-ZZZ.pem"} {
		if NamingIsManaged(unmanaged) {
			t.Errorf("%q should NOT be recognized as managed", unmanaged)
		}
	}
}
