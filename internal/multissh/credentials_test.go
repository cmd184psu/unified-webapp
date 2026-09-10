package multissh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/multissh/sshproxy"
)

const testHostPassword = "hunter2-never-persisted"

// --- 2.4c type-level proof -------------------------------------------------

// hostConfig is the only host type that is written to disk. This fails the
// moment someone adds a credential field back to it, with no live password
// needed to trigger it. hostRequest is deliberately not asserted here: carrying
// a password is its job.
func TestPersistedHostTypeHasNoCredentialField(t *testing.T) {
	cfgType := reflect.TypeOf(hostConfig{})
	secretType := reflect.TypeOf(sshproxy.Secret{})
	for i := 0; i < cfgType.NumField(); i++ {
		f := cfgType.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if strings.EqualFold(f.Name, "password") || strings.EqualFold(tag, "password") {
			t.Errorf("hostConfig has a password field %q (json %q); it must not reach disk", f.Name, tag)
		}
		if f.Type == secretType {
			t.Errorf("hostConfig field %q is a Secret; Secret must never sit on a persisted type", f.Name)
		}
	}
}

// The wire type is the other half of the split: it must carry the password, or
// the exactly-one-of credential path has nowhere to receive one from.
func TestHostRequestCarriesTheCredential(t *testing.T) {
	f, ok := reflect.TypeOf(hostRequest{}).FieldByName("Password")
	if !ok {
		t.Fatal("hostRequest has no Password field")
	}
	if f.Type != reflect.TypeOf(sshproxy.Secret{}) {
		t.Errorf("hostRequest.Password is %s, want sshproxy.Secret", f.Type)
	}
}

// --- 2.4c file-level proof (AC-10) ----------------------------------------

// Stronger than grepping for the plaintext: the persisted record must have no
// password key at all, which also catches a redacted "***" being written.
func TestPutHostsWritesNoPasswordKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	srv := New(Options{HostsPath: path, UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	body := fmt.Sprintf(`{"hosts":[{"ip":"10.0.0.9","port":22,"user":"u","key":"","password":%q,"remoteDir":"/tmp"}]}`, testHostPassword)
	res := putHosts(t, httpSrv.URL, []byte(body))
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hosts file: %v", err)
	}
	if bytes.Contains(raw, []byte(testHostPassword)) {
		t.Fatalf("hosts file contains the password: %s", raw)
	}
	if bytes.Contains(raw, []byte("password")) {
		t.Fatalf("hosts file contains a password key: %s", raw)
	}
	for _, record := range decodeHostRecords(t, raw) {
		want := map[string]bool{"ip": true, "port": true, "user": true, "key": true, "remoteDir": true}
		if len(record) != len(want) {
			t.Fatalf("record has %d fields, want %d: %#v", len(record), len(want), record)
		}
		for k := range record {
			if !want[k] {
				t.Errorf("unexpected persisted field %q", k)
			}
		}
	}

	// The response body is the other place a password could surface.
	var echoed map[string]any
	if err := json.NewDecoder(res.Body).Decode(&echoed); err == nil {
		if encoded, err := json.Marshal(echoed); err == nil && bytes.Contains(encoded, []byte(testHostPassword)) {
			t.Errorf("PUT response echoed the password: %s", encoded)
		}
	}
}

// --- 3.4 over-capacity hosts file ------------------------------------------

// The operator lowered max_sessions after saving more hosts. Nothing may
// destroy those entries: the file loads whole, GET returns all of them, and an
// over-capacity PUT is refused without touching the file.
func TestOverCapacityHostsFileIsPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	seed := `{"hosts":[{"ip":"a","port":22,"user":"u","key":"","remoteDir":"/tmp"},{"ip":"b","port":22,"user":"u","key":"","remoteDir":"/tmp"},{"ip":"c","port":22,"user":"u","key":"","remoteDir":"/tmp"},{"ip":"d","port":22,"user":"u","key":"","remoteDir":"/tmp"},{"ip":"e","port":22,"user":"u","key":"","remoteDir":"/tmp"}]}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed hosts file: %v", err)
	}

	srv := New(Options{HostsPath: path, UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 2})
	if srv.hosts == nil {
		t.Fatal("over-capacity hosts file failed to load")
	}
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	res, err := http.Get(httpSrv.URL + "/api/hosts")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	var got struct {
		Hosts []hostConfig `json:"hosts"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Hosts) != 5 {
		t.Fatalf("GET returned %d hosts, want all 5 -- the server must not truncate on read", len(got.Hosts))
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	putRes := putHosts(t, httpSrv.URL, []byte(seed))
	defer putRes.Body.Close()
	if putRes.StatusCode != http.StatusBadRequest {
		t.Fatalf("over-capacity PUT status = %d, want 400", putRes.StatusCode)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("rejected PUT rewrote the hosts file:\nbefore: %s\nafter:  %s", before, after)
	}
}

// --- 3.7 max_sessions bounds the broadcast fan-out -------------------------

func TestBroadcastTargetCountBoundedByMaxSessions(t *testing.T) {
	sshDir := t.TempDir()
	for i := 0; i < 6; i++ {
		if err := os.WriteFile(filepath.Join(sshDir, fmt.Sprintf("id%d", i)), []byte("KEY"), 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}
	}
	srv := New(Options{SSHKeyDir: sshDir, UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 5, Transferrer: &fakeTransferrer{}})
	uploadID := stageUploadForBroadcastTest(t, srv, "bundle.tar", []byte("abcdef"))
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	targetsFor := func(n int) []map[string]any {
		out := make([]map[string]any, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{"host": fmt.Sprintf("h%d", i), "port": 22, "user": "u", "key": fmt.Sprintf("id%d", i), "remoteDir": "/tmp"})
		}
		return out
	}

	jobID := startBroadcastJob(t, httpSrv.URL, map[string]any{"uploadId": uploadID, "targets": targetsFor(5)})
	if jobID == "" {
		t.Fatal("five targets should be accepted at max_sessions 5")
	}
	waitBroadcastComplete(t, httpSrv.URL, jobID)

	res := postBroadcast(t, httpSrv.URL, map[string]any{"uploadId": uploadID, "targets": targetsFor(6)})
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("six targets status = %d, want 400", res.StatusCode)
	}
	var errBody map[string]string
	if err := json.NewDecoder(res.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if !strings.Contains(errBody["error"], "5") {
		t.Errorf("error %q should be phrased against the configured max", errBody["error"])
	}
}

// --- 3.5 exactly-one-of on broadcast targets -------------------------------

func TestBroadcastTargetRequiresExactlyOneCredential(t *testing.T) {
	sshDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sshDir, "id1"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	srv := New(Options{SSHKeyDir: sshDir, UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3, Transferrer: &fakeTransferrer{}})
	uploadID := stageUploadForBroadcastTest(t, srv, "bundle.tar", []byte("abcdef"))
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	cases := []struct {
		name       string
		target     map[string]any
		wantStatus int
	}{
		{"key only", map[string]any{"host": "h", "port": 22, "user": "u", "key": "id1", "remoteDir": "/tmp"}, http.StatusOK},
		{"password only", map[string]any{"host": "h", "port": 22, "user": "u", "password": testHostPassword, "remoteDir": "/tmp"}, http.StatusOK},
		{"neither", map[string]any{"host": "h", "port": 22, "user": "u", "remoteDir": "/tmp"}, http.StatusBadRequest},
		{"both", map[string]any{"host": "h", "port": 22, "user": "u", "key": "id1", "password": testHostPassword, "remoteDir": "/tmp"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := postBroadcast(t, httpSrv.URL, map[string]any{"uploadId": uploadID, "targets": []map[string]any{tc.target}})
			defer res.Body.Close()
			if res.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.wantStatus)
			}
		})
	}
}

// --- 2.4b FR-N4 lifetime bound ---------------------------------------------

// A password lives for the job that uses it and no longer: once a target
// reaches a terminal state, the credential the job holds for it is empty even
// though the job record itself is still reachable.
func TestBroadcastJobCredentialZeroedAtTerminalState(t *testing.T) {
	srv := New(Options{SSHKeyDir: t.TempDir(), UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3, Transferrer: &fakeTransferrer{}})
	uploadID := stageUploadForBroadcastTest(t, srv, "bundle.tar", []byte("abcdef"))
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	jobID := startBroadcastJob(t, httpSrv.URL, map[string]any{
		"uploadId": uploadID,
		"targets":  []map[string]any{{"host": "h1", "port": 22, "user": "u", "password": testHostPassword, "remoteDir": "/tmp"}},
	})
	waitBroadcastComplete(t, httpSrv.URL, jobID)

	job, ok := srv.broadcasts.getJob(jobID)
	if !ok {
		t.Fatal("job record disappeared")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if job.credential(0).Reveal() == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("completed job still holds a credential: %q", job.credential(0).Reveal())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func putHosts(t *testing.T, baseURL string, payload []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, baseURL+"/api/hosts", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

func decodeHostRecords(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var decoded struct {
		Hosts []map[string]any `json:"hosts"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal hosts file: %v", err)
	}
	if len(decoded.Hosts) == 0 {
		t.Fatal("hosts file has no records")
	}
	return decoded.Hosts
}
