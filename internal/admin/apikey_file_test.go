package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

const apiKeyPrefix = "{\n\t\"port\":    8081 ,\n   \"zeta\": {\"a\":1},\n"
const apiKeySuffix = ",\n\t\t\"zzz\":   [1,\n   2]\n}\n"

func writeCfg(t *testing.T, auth string) string {
	t.Helper()
	body := apiKeyPrefix
	if auth != "" {
		body += "  \"auth\": " + auth
	} else {
		body += "  \"alpha\": true"
	}
	body += apiKeySuffix
	p := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(p, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	return p
}

func loadKeys(t *testing.T, p string) []config.NamedHash {
	t.Helper()
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return c.Auth.APIKeys
}

func TestAddAPIKeyToConfigFile_EmptyList(t *testing.T) {
	p := writeCfg(t, `{"api_keys": []}`)
	replaced, err := AddAPIKeyToConfigFile(p, "haproxy-editor", "sha256:aa")
	if err != nil || replaced {
		t.Fatalf("replaced=%v err=%v", replaced, err)
	}
	k := loadKeys(t, p)
	if len(k) != 1 || k[0].Name != "haproxy-editor" || k[0].Hash != "sha256:aa" {
		t.Fatalf("keys=%+v", k)
	}
}

func TestAddAPIKeyToConfigFile_NoAPIKeysMember(t *testing.T) {
	p := writeCfg(t, `{"cookie_secure": true}`)
	if _, err := AddAPIKeyToConfigFile(p, "n", "sha256:bb"); err != nil {
		t.Fatal(err)
	}
	if k := loadKeys(t, p); len(k) != 1 || k[0].Hash != "sha256:bb" {
		t.Fatalf("keys=%+v", k)
	}
}

func TestAddAPIKeyToConfigFile_NoAuthMember(t *testing.T) {
	p := writeCfg(t, "")
	if _, err := AddAPIKeyToConfigFile(p, "n", "sha256:bb"); err != nil {
		t.Fatal(err)
	}
	if k := loadKeys(t, p); len(k) != 1 {
		t.Fatalf("keys=%+v", k)
	}
}

func TestAddAPIKeyToConfigFile_AppendsAndLeavesOthers(t *testing.T) {
	p := writeCfg(t, `{"api_keys": [{"name":"old","hash":"sha256:01"}]}`)
	replaced, err := AddAPIKeyToConfigFile(p, "new", "sha256:02")
	if err != nil || replaced {
		t.Fatalf("replaced=%v err=%v", replaced, err)
	}
	k := loadKeys(t, p)
	if len(k) != 2 || k[0] != (config.NamedHash{Name: "old", Hash: "sha256:01"}) || k[1].Name != "new" {
		t.Fatalf("keys=%+v", k)
	}
}

func TestAddAPIKeyToConfigFile_ReplaceSameName(t *testing.T) {
	p := writeCfg(t, `{"api_keys": [{"name":"a","hash":"sha256:01"},{"name":"b","hash":"sha256:02"}]}`)
	replaced, err := AddAPIKeyToConfigFile(p, "b", "sha256:99")
	if err != nil || !replaced {
		t.Fatalf("replaced=%v err=%v", replaced, err)
	}
	k := loadKeys(t, p)
	if len(k) != 2 || k[0].Hash != "sha256:01" || k[1] != (config.NamedHash{Name: "b", Hash: "sha256:99"}) {
		t.Fatalf("keys=%+v", k)
	}
}

func TestAddAPIKeyToConfigFile_BytesOutsideAuthUntouched(t *testing.T) {
	p := writeCfg(t, `{"api_keys": []}`)
	if _, err := AddAPIKeyToConfigFile(p, "n", "sha256:cc"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.HasPrefix(got, []byte(apiKeyPrefix+"  \"auth\": ")) {
		t.Fatalf("prefix changed:\n%s", got)
	}
	if !bytes.HasSuffix(got, []byte(apiKeySuffix)) {
		t.Fatalf("suffix changed:\n%s", got)
	}
}

func TestAddAPIKeyToConfigFile_StoredHashMatchesKey(t *testing.T) {
	key, hash, err := generateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	p := writeCfg(t, `{}`)
	if _, err := AddAPIKeyToConfigFile(p, "n", hash); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(key))
	if k := loadKeys(t, p); k[0].Hash != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("hash mismatch: %+v", k)
	}
}

func TestAddAPIKeyToConfigFile_PreservesMode(t *testing.T) {
	p := writeCfg(t, `{}`)
	if _, err := AddAPIKeyToConfigFile(p, "n", "sha256:dd"); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%v", st.Mode().Perm())
	}
}

func TestAddAPIKeyToConfigFile_MissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.json")
	if _, err := AddAPIKeyToConfigFile(p, "n", "sha256:ee"); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file created: %v", err)
	}
}

func TestAddAPIKeyToConfigFile_InvalidJSONUnchanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	orig := []byte("{ not json")
	os.WriteFile(p, orig, 0o600)
	if _, err := AddAPIKeyToConfigFile(p, "n", "sha256:ee"); err == nil {
		t.Fatal("want error")
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, orig) {
		t.Fatalf("changed: %s", got)
	}
}

func TestAddAPIKeyToConfigFile_BlankName(t *testing.T) {
	p := writeCfg(t, `{}`)
	before, _ := os.ReadFile(p)
	for _, n := range []string{"", "   "} {
		if _, err := AddAPIKeyToConfigFile(p, n, "sha256:ee"); err == nil {
			t.Fatalf("name %q: want error", n)
		}
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Fatal("file changed")
	}
}

// The post-write validator is the package var validateConfigFile; the test
// swaps it for one that always fails, simulating a result config.Load rejects.
func TestAddAPIKeyToConfigFile_RestoresOriginalOnValidationFailure(t *testing.T) {
	p := writeCfg(t, `{"api_keys": []}`)
	before, _ := os.ReadFile(p)
	old := validateConfigFile
	validateConfigFile = func(string) error { return errors.New("boom") }
	defer func() { validateConfigFile = old }()

	_, err := AddAPIKeyToConfigFile(p, "n", "sha256:ff")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatalf("not restored:\n%s", after)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%v", st.Mode().Perm())
	}
}
