package multissh

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHostStore_LoadMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	store, err := newHostStore(path, 3)
	if err != nil {
		t.Fatalf("newHostStore: %v", err)
	}
	if len(store.list()) != 0 {
		t.Fatalf("expected empty hosts")
	}
}

func TestHostStore_SaveReloadRoundTripWithNormalizedDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	store, err := newHostStore(path, 3)
	if err != nil {
		t.Fatalf("newHostStore: %v", err)
	}
	if err := store.set([]hostRequest{{IP: "10.0.0.1", Port: 0, User: "root", Key: "id_rsa", RemoteDir: ""}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	reloaded, err := newHostStore(path, 3)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	hosts := reloaded.list()
	if len(hosts) != 1 {
		t.Fatalf("len(hosts) = %d", len(hosts))
	}
	if hosts[0].Port != 22 {
		t.Fatalf("port = %d, want 22", hosts[0].Port)
	}
	if hosts[0].RemoteDir != "/tmp" {
		t.Fatalf("remoteDir = %q, want /tmp", hosts[0].RemoteDir)
	}
}

func TestHostStore_RejectsMoreThanMaxSessionsHosts(t *testing.T) {
	store, err := newHostStore(filepath.Join(t.TempDir(), "hosts.json"), 3)
	if err != nil {
		t.Fatalf("newHostStore: %v", err)
	}
	err = store.set([]hostRequest{{Key: "k1"}, {Key: "k2"}, {Key: "k3"}, {Key: "k4"}})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestHostStore_RejectsInvalidKeyNamesAndAllowsEmptyKey(t *testing.T) {
	store, err := newHostStore(filepath.Join(t.TempDir(), "hosts.json"), 3)
	if err != nil {
		t.Fatalf("newHostStore: %v", err)
	}
	bad := []string{"../id", "sub/key", ".", ".."}
	for _, key := range bad {
		if err := store.set([]hostRequest{{Key: key}}); err == nil {
			t.Fatalf("expected key %q to fail", key)
		}
	}
	if err := store.set([]hostRequest{{IP: "", Port: 22, User: "", Key: "", RemoteDir: ""}}); err != nil {
		t.Fatalf("empty key should be allowed: %v", err)
	}
}

func TestHostStore_SerializesOnlyContractFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	srv := New(Options{HostsPath: path, UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	payload := []byte(`{"hosts":[{"ip":"1.2.3.4","port":22,"user":"u","key":"id1","remoteDir":"/tmp","privateKey":"SECRET-MATERIAL"}]}`)
	req, err := http.NewRequest(http.MethodPut, httpSrv.URL+"/api/hosts", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hosts file: %v", err)
	}
	if bytes.Contains(raw, []byte("SECRET-MATERIAL")) {
		t.Fatalf("hosts file contains private key material")
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal hosts file: %v", err)
	}
	hostsRaw, ok := decoded["hosts"].([]any)
	if !ok || len(hostsRaw) != 1 {
		t.Fatalf("unexpected hosts payload: %#v", decoded)
	}
	hostMap, ok := hostsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected host payload: %#v", hostsRaw[0])
	}
	if len(hostMap) != 5 {
		t.Fatalf("expected exactly 5 fields, got %#v", hostMap)
	}
}
