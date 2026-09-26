package certmachine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/sshclient"
	"cmd184psu/unified-webapp/internal/platform/sshclient/sshtest"

	"golang.org/x/crypto/ssh"
)

const testPEM = "-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----\n"
const testTmp = "/tmp/certmachine-rootCA.Ab12Cd"

// fakeMachine scripts a remote host: the OS answers, the uid, and a record of
// the file uploaded with `cat >`. Any other command succeeds silently unless
// failOn matches it.
type fakeMachine struct {
	uname     string // "" = no uname (Windows)
	osRelease string
	uid       string
	mktemp    string
	failOn    string // substring of a command that fails
	failOut   string
	uploaded  string
}

func (m *fakeMachine) handle(cmd string, stdin []byte) (string, int) {
	if m.failOn != "" && strings.Contains(cmd, m.failOn) {
		return m.failOut, 1
	}
	switch {
	case cmd == "uname -s":
		if m.uname == "" {
			return "'uname' is not recognized as an internal or external command\r\n", 1
		}
		return m.uname + "\n", 0
	case cmd == "cmd /c ver":
		if m.uname == "" {
			return "\r\nMicrosoft Windows [Version 10.0.22631.4317]\r\n", 0
		}
		return "", 127
	case cmd == "cat /etc/os-release":
		return m.osRelease, 0
	case cmd == "id -u":
		return m.uid + "\n", 0
	case strings.HasPrefix(cmd, "mktemp "):
		return m.mktemp + "\n", 0
	case strings.HasPrefix(cmd, "cat > "):
		m.uploaded = string(stdin)
		return "", 0
	case strings.HasPrefix(cmd, "powershell "):
		m.uploaded = string(stdin)
		return "CertUtil: -addstore command completed successfully.\r\n", 0
	}
	return "", 0
}

func runRemote(t *testing.T, m *fakeMachine) (TrustPlatform, []string, error) {
	t.Helper()
	srv := sshtest.Start(t, "ops", "pw", m.handle)
	creds := sshclient.Credentials{Host: srv.Host, Port: srv.Port, User: "ops", Password: sshclient.NewSecret("pw")}
	platform, _, err := InstallTrustRemote(context.Background(), creds, ssh.InsecureIgnoreHostKey(), []byte(testPEM), trustAnchorName("Home Lab CA 2026"))
	return platform, srv.Commands(), err
}

func has(cmds []string, want string) bool {
	for _, c := range cmds {
		if c == want {
			return true
		}
	}
	return false
}

func TestRemoteTrustRockyWithSudo(t *testing.T) {
	m := &fakeMachine{uname: "Linux", osRelease: `ID="rocky"` + "\nVERSION_ID=\"10.0\"\n", uid: "1000", mktemp: testTmp}
	platform, cmds, err := runRemote(t, m)
	if err != nil || platform != TrustPlatformRHELFamily {
		t.Fatalf("platform %q, err %v", platform, err)
	}
	if m.uploaded != testPEM {
		t.Errorf("uploaded %q, want the PEM", m.uploaded)
	}
	for _, want := range []string{
		"sudo -n cp " + testTmp + " /etc/pki/ca-trust/source/anchors/certmachine-Home-Lab-CA-2026.pem",
		"sudo -n update-ca-trust extract",
		"rm -f " + testTmp,
	} {
		if !has(cmds, want) {
			t.Errorf("missing command %q in %q", want, cmds)
		}
	}
}

func TestRemoteTrustUbuntuAsRootSkipsSudo(t *testing.T) {
	m := &fakeMachine{uname: "Linux", osRelease: "ID=ubuntu\nID_LIKE=debian\n", uid: "0", mktemp: testTmp}
	platform, cmds, err := runRemote(t, m)
	if err != nil || platform != TrustPlatformDebian {
		t.Fatalf("platform %q, err %v", platform, err)
	}
	if !has(cmds, "cp "+testTmp+" /usr/local/share/ca-certificates/certmachine-Home-Lab-CA-2026.crt") || !has(cmds, "update-ca-certificates") {
		t.Errorf("root should run the Debian steps without sudo: %q", cmds)
	}
	for _, c := range cmds {
		if strings.HasPrefix(c, "sudo") {
			t.Errorf("root must not use sudo: %q", c)
		}
	}
}

func TestRemoteTrustMacOS(t *testing.T) {
	m := &fakeMachine{uname: "Darwin", uid: "501", mktemp: testTmp}
	platform, cmds, err := runRemote(t, m)
	if err != nil || platform != TrustPlatformDarwin {
		t.Fatalf("platform %q, err %v", platform, err)
	}
	want := "sudo -n security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain " + testTmp
	if !has(cmds, want) {
		t.Errorf("missing %q in %q", want, cmds)
	}
}

func TestRemoteTrustWindows(t *testing.T) {
	m := &fakeMachine{uname: ""}
	platform, cmds, err := runRemote(t, m)
	if err != nil || platform != TrustPlatformWindows {
		t.Fatalf("platform %q, err %v", platform, err)
	}
	if m.uploaded != testPEM {
		t.Errorf("the PEM should reach PowerShell on stdin, got %q", m.uploaded)
	}
	if !has(cmds, windowsTrustCommand) {
		t.Errorf("missing the Windows install command in %q", cmds)
	}
}

func TestRemoteTrustUnsupportedDoesNothing(t *testing.T) {
	m := &fakeMachine{uname: "FreeBSD", uid: "0", mktemp: testTmp}
	_, cmds, err := runRemote(t, m)
	if !errors.Is(err, ErrTrustPlatformUnsupported) {
		t.Fatalf("err %v, want ErrTrustPlatformUnsupported", err)
	}
	for _, c := range cmds {
		if c != "uname -s" {
			t.Errorf("an unsupported OS must get no further commands, ran %q", c)
		}
	}
	if m.uploaded != "" {
		t.Error("nothing should be uploaded to an unsupported OS")
	}
}

func TestRemoteTrustUnknownLinuxDoesNothing(t *testing.T) {
	m := &fakeMachine{uname: "Linux", osRelease: "ID=arch\n", uid: "0", mktemp: testTmp}
	_, cmds, err := runRemote(t, m)
	if !errors.Is(err, ErrTrustPlatformUnsupported) {
		t.Fatalf("err %v, want ErrTrustPlatformUnsupported", err)
	}
	if has(cmds, "id -u") || m.uploaded != "" {
		t.Errorf("an unrecognized distro must not be touched: %q", cmds)
	}
}

func TestRemoteTrustSudoNeedsPassword(t *testing.T) {
	m := &fakeMachine{uname: "Linux", osRelease: "ID=ubuntu\n", uid: "1000", mktemp: testTmp,
		failOn: "sudo -n cp", failOut: "sudo: a password is required\n"}
	_, cmds, err := runRemote(t, m)
	if !errors.Is(err, ErrRemoteSudo) {
		t.Fatalf("err %v, want ErrRemoteSudo", err)
	}
	if !has(cmds, "rm -f "+testTmp) {
		t.Errorf("the temp file should still be cleaned up: %q", cmds)
	}
}

func TestRemoteTrustRejectsOddTempPath(t *testing.T) {
	m := &fakeMachine{uname: "Linux", osRelease: "ID=ubuntu\n", uid: "0", mktemp: "/tmp/x; rm -rf /"}
	_, cmds, err := runRemote(t, m)
	if err == nil || !strings.Contains(err.Error(), "unexpected temp path") {
		t.Fatalf("err %v, want an unexpected temp path error", err)
	}
	for _, c := range cmds {
		if strings.HasPrefix(c, "cat > ") || strings.Contains(c, "rm -rf") {
			t.Errorf("an unvalidated path must never be used: %q", c)
		}
	}
}

// newRemoteTrustHTTP builds a certmachine server with remote trust enabled
// (lax host keys, an empty key folder) and an initialized CA.
func newRemoteTrustHTTP(t *testing.T) string {
	t.Helper()
	srv := newHandlerTestServer(t)
	srv.opts.SSH = &sshclient.Resolved{SSHDir: t.TempDir(), HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	_, httpSrv := serveHandlerTestServer(t, srv)
	if res := doJSON(t, "POST", httpSrv.URL+"/api/ca/init", ""); res.StatusCode != 201 {
		t.Fatalf("init CA: %d", res.StatusCode)
	}
	return httpSrv.URL
}

func postRemote(t *testing.T, base, body string) (int, trustResponse) {
	t.Helper()
	res := doJSON(t, "POST", base+"/api/ca/trust/remote", body)
	defer res.Body.Close()
	var tr trustResponse
	_ = json.NewDecoder(res.Body).Decode(&tr)
	return res.StatusCode, tr
}

func TestHandlerRemoteTrust(t *testing.T) {
	base := newRemoteTrustHTTP(t)

	ubuntu := &fakeMachine{uname: "Linux", osRelease: "ID=ubuntu\n", uid: "0", mktemp: testTmp}
	srv := sshtest.Start(t, "ops", "pw", ubuntu.handle)
	body := fmt.Sprintf(`{"host":%q,"port":%d,"user":"ops","password":"pw"}`, srv.Host, srv.Port)
	code, tr := postRemote(t, base, body)
	if code != 200 || tr.Platform != "debian" || !strings.Contains(tr.Output, "update-ca-certificates") {
		t.Fatalf("ubuntu: %d %+v", code, tr)
	}
	if !strings.HasPrefix(ubuntu.uploaded, "-----BEGIN CERTIFICATE-----") {
		t.Errorf("the real root CA should be uploaded, got %q", ubuntu.uploaded)
	}

	freebsd := &fakeMachine{uname: "FreeBSD"}
	srv2 := sshtest.Start(t, "ops", "pw", freebsd.handle)
	body = fmt.Sprintf(`{"host":%q,"port":%d,"user":"ops","password":"pw"}`, srv2.Host, srv2.Port)
	if code, tr := postRemote(t, base, body); code != 422 || !strings.Contains(tr.Error, "nothing was installed") {
		t.Errorf("unsupported OS: %d %+v", code, tr)
	}

	for _, bad := range []string{
		`{"user":"ops","password":"pw"}`,                       // no host
		`{"host":"h","user":"ops"}`,                            // no credential
		`{"host":"h","user":"ops","key":"id","password":"pw"}`, // unknown key
		`{"host":"h","user":"ops","key":"../../etc/passwd"}`,   // traversal
	} {
		if code, _ := postRemote(t, base, bad); code != 400 {
			t.Errorf("%s: status %d, want 400", bad, code)
		}
	}

	// Wrong password: the connection fails, reported as 502.
	body = fmt.Sprintf(`{"host":%q,"port":%d,"user":"ops","password":"nope"}`, srv.Host, srv.Port)
	if code, _ := postRemote(t, base, body); code != 502 {
		t.Errorf("bad password: status %d, want 502", code)
	}
}

func TestHandlerRemoteTrustUnavailable(t *testing.T) {
	srv := newHandlerTestServer(t)
	srv.opts.SSHReason = "known_hosts missing"
	_, httpSrv := serveHandlerTestServer(t, srv)
	code, tr := postRemote(t, httpSrv.URL, `{"host":"h","user":"u","password":"p"}`)
	if code != 409 || !strings.Contains(tr.Error, "known_hosts missing") {
		t.Errorf("unavailable: %d %+v", code, tr)
	}
	res := doJSON(t, "GET", httpSrv.URL+"/api/config", "")
	var cfg configResponse
	_ = json.NewDecoder(res.Body).Decode(&cfg)
	res.Body.Close()
	if cfg.TrustRemoteAvailable || cfg.TrustRemoteReason != "known_hosts missing" {
		t.Errorf("config should report remote trust unavailable: %+v", cfg)
	}
}
