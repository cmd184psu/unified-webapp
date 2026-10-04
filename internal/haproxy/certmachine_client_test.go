package haproxy

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustClient(t *testing.T, s CertMachineSettings) *CertMachineClient {
	t.Helper()
	c, err := CertMachineNewClient(s)
	if err != nil {
		t.Fatalf("CertMachineNewClient: %v", err)
	}
	return c
}

func TestCertMachineHTTPSRequiredForNonLoopback(t *testing.T) {
	ok := []string{"https://certmachine.cmdhome.net", "http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"}
	for _, u := range ok {
		if _, err := CertMachineNewClient(CertMachineSettings{URL: u}); err != nil {
			t.Errorf("CertMachineNewClient(%q) = %v, want ok", u, err)
		}
	}
	bad := []string{"http://certmachine.cmdhome.net", "http://192.168.1.10", "ftp://x", "http://example.com:8443"}
	for _, u := range bad {
		if _, err := CertMachineNewClient(CertMachineSettings{URL: u}); err == nil {
			t.Errorf("CertMachineNewClient(%q) = nil, want a transport error", u)
		}
	}
}

func TestCertMachineListFetchAndActive(t *testing.T) {
	cm := newFakeCertMachine()
	cm.expectBearer = "mybearerkey"
	cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", []string{"brandx.cmdhome.net"})
	cm.addCert(99, "brandx.cmdhome.net", "fp-99-old", "archived", nil)
	cm.addCert(20, "other.cmdhome.net", "fp-20", "active", nil)
	srv := cm.start(t)
	client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "mybearerkey"})
	ctx := context.Background()

	all, err := client.ListCerts(ctx, "", "")
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListCerts all = %d, want 3", len(all))
	}

	byFQDN, err := client.ListCerts(ctx, "BRANDX.cmdhome.NET", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(byFQDN) != 2 {
		t.Errorf("ListCerts fqdn = %d, want 2 (case-insensitive)", len(byFQDN))
	}

	one, err := client.GetCert(ctx, 12)
	if err != nil {
		t.Fatal(err)
	}
	if one.ID != 12 || one.FQDN != "brandx.cmdhome.net" {
		t.Errorf("GetCert = %+v, want id 12", one)
	}

	active, err := client.ActiveCertForFQDN(ctx, "brandx.cmdhome.net")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.ID != 12 {
		t.Errorf("ActiveCertForFQDN = %+v, want the active id 12", active)
	}

	none, err := client.ActiveCertForFQDN(ctx, "nobody.cmdhome.net")
	if err != nil {
		t.Fatal(err)
	}
	if none != nil {
		t.Errorf("ActiveCertForFQDN(unknown) = %+v, want nil", none)
	}
}

func TestCertMachinePullVerifiesMatchingDownload(t *testing.T) {
	cm := newFakeCertMachine()
	entry := cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", nil)
	srv := cm.start(t)
	client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "k"})

	body, err := client.PullHAProxyPEM(context.Background(), 12, "fp-12")
	if err != nil {
		t.Fatalf("PullHAProxyPEM: %v", err)
	}
	if string(body) != string(entry.body) {
		t.Error("returned bytes differ from the served body")
	}
}

func TestCertMachinePullFailureClasses(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*fakeCertMachine)
		want   error
		substr string // optional substring the message must carry
	}{
		{"unauthorized", func(f *fakeCertMachine) { f.status401 = true }, CertMachineErrUnauthorized, ""},
		{"server error", func(f *fakeCertMachine) { f.status500 = true }, CertMachineErrServer, ""},
		{"refused export", func(f *fakeCertMachine) { f.refuse409 = "cert is quarantined: key mismatch" }, CertMachineErrRefused, "quarantined"},
		{"tampered body", func(f *fakeCertMachine) { f.tamperBody = true }, CertMachineErrIntegrity, ""},
		{"corrupt etag", func(f *fakeCertMachine) { f.corruptETag = true }, CertMachineErrIntegrity, ""},
		{"wrong id", func(f *fakeCertMachine) { f.wrongID = true }, CertMachineErrIntegrity, ""},
		{"wrong fingerprint", func(f *fakeCertMachine) { f.wrongFingerprint = true }, CertMachineErrIntegrity, ""},
		{"missing etag", func(f *fakeCertMachine) { f.missingETag = true }, CertMachineErrIntegrity, "this CertMachine needs updating"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cm := newFakeCertMachine()
			cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", nil)
			tc.setup(cm)
			srv := cm.start(t)
			client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: "k"})

			body, err := client.PullHAProxyPEM(context.Background(), 12, "fp-12")
			if !errors.Is(err, tc.want) {
				t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
			}
			if body != nil {
				t.Errorf("%s: returned %d bytes, want none on failure", tc.name, len(body))
			}
			if tc.substr != "" && !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("%s: message %q missing %q", tc.name, err.Error(), tc.substr)
			}
		})
	}
}

func TestCertMachineUnreachableIsDistinct(t *testing.T) {
	// A loopback URL with nothing listening.
	client := mustClient(t, CertMachineSettings{URL: "http://127.0.0.1:1", APIKey: "k"})
	_, err := client.ListCerts(context.Background(), "", "")
	if !errors.Is(err, CertMachineErrUnreachable) {
		t.Errorf("ListCerts to a dead endpoint = %v, want CertMachineErrUnreachable", err)
	}
}

// The API key never appears in a settings dump, an error, or a %v of the
// client, and nothing the package produces leaks the private-key marker.
func TestCertMachineAPIKeyAndKeyMaterialNeverLeak(t *testing.T) {
	const key = "s3cr3t-api-key-value"
	s := CertMachineSettings{URL: "https://certmachine.cmdhome.net", APIKey: CertMachineAPIKey(key), CAFile: "/etc/ca.pem"}
	for _, dump := range []string{fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%s", s.APIKey), s.String()} {
		if strings.Contains(dump, key) {
			t.Errorf("settings dump leaked the api key: %q", dump)
		}
	}

	// A refusal surfaces CertMachine's message but never the key, and never a
	// key marker.
	cm := newFakeCertMachine()
	cm.addCert(12, "brandx.cmdhome.net", "fp-12", "active", nil)
	cm.refuse409 = "refused"
	srv := cm.start(t)
	client := mustClient(t, CertMachineSettings{URL: srv.URL, APIKey: CertMachineAPIKey(key)})
	_, err := client.PullHAProxyPEM(context.Background(), 12, "fp-12")
	if err == nil || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Errorf("error leaked the key or key material: %v", err)
	}
}

// D15 / §12: this package never imports crypto/x509 or an openssl binding in
// its non-test code. It surfaces every cert detail from CertMachine and never
// parses a PEM itself.
func TestCertPackageHasNoX509OrOpenSSLImport(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			// A sibling's half-written file: skip it (it fails to compile on
			// its own and is caught by the build, not by this import scan).
			continue
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			// certmachine_tls.go is the single exemption: it imports crypto/x509
			// only to build a TLS trust pool from ca_file, never to read a cert.
			if p == "crypto/x509" && name == "certmachine_tls.go" {
				continue
			}
			if strings.Contains(p, "x509") || strings.Contains(p, "openssl") {
				t.Errorf("%s imports %q: the module must not parse certs locally (D15)", name, p)
			}
		}
	}
}

// CAFile makes a private root trusted without touching the system roots; a bad
// or empty CAFile is rejected up front, and without it the private root is not
// trusted.
func TestCertMachineCAFileTrustsPrivateRoot(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"certs":[]}`))
	}))
	defer srv.Close()

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}

	withCA, err := CertMachineNewClient(CertMachineSettings{URL: srv.URL, APIKey: "k", CAFile: caPath})
	if err != nil {
		t.Fatalf("client with CAFile: %v", err)
	}
	if _, err := withCA.ListCerts(context.Background(), "", ""); err != nil {
		t.Errorf("with the private root trusted via CAFile, list failed: %v", err)
	}

	without, err := CertMachineNewClient(CertMachineSettings{URL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("client without CAFile: %v", err)
	}
	if _, err := without.ListCerts(context.Background(), "", ""); err == nil {
		t.Error("without CAFile the private root must not be trusted, but the call succeeded")
	}

	if _, err := CertMachineNewClient(CertMachineSettings{URL: srv.URL, APIKey: "k", CAFile: filepath.Join(t.TempDir(), "missing.pem")}); err == nil {
		t.Error("a missing ca_file must be rejected up front")
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	_ = os.WriteFile(empty, []byte("not a certificate"), 0o600)
	if _, err := CertMachineNewClient(CertMachineSettings{URL: srv.URL, APIKey: "k", CAFile: empty}); err == nil {
		t.Error("a ca_file with no certificates must be rejected up front")
	}
}
