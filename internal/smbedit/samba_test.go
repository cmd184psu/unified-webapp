package smbedit

// Ported from smbed's internal/samba/samba_test.go. Golden render assertions
// are unmodified; the only adaptations are the package move (config.Config →
// State) and the per-call log hook (D-7), which tests satisfy with noplog.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"
)

func baseState() *State {
	return &State{
		ShareOwner: "mediauser",
		Globals: []GlobalEntry{
			{Key: "workgroup", Value: "WORKGROUP"},
			{Key: "server string", Value: "Test Server"},
		},
		Shares: []Share{
			{
				Name:       "media",
				Path:       "/opt/media",
				Writable:   true,
				BrowseAble: true,
				Enabled:    true,
			},
			{
				Name:    "hidden",
				Path:    "/opt/hidden",
				Enabled: false, // should be omitted
			},
		},
	}
}

func TestRender_GlobalSection(t *testing.T) {
	defer goleak.VerifyNone(t)
	out, err := Render(baseState())
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "[global]") {
		t.Error("missing [global] section")
	}
	if !strings.Contains(s, "workgroup = WORKGROUP") {
		t.Error("missing workgroup global")
	}
}

func TestRender_ShareSection(t *testing.T) {
	defer goleak.VerifyNone(t)
	out, err := Render(baseState())
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "[media]") {
		t.Error("missing [media] share")
	}
	if !strings.Contains(s, "path = /opt/media") {
		t.Error("missing path line")
	}
	if !strings.Contains(s, "valid users = mediauser") {
		t.Error("missing valid users line")
	}
	if !strings.Contains(s, "force user = mediauser") {
		t.Error("missing force user line")
	}
	if !strings.Contains(s, "read only = no") {
		t.Error("writable share should have 'read only = no'")
	}
}

func TestRender_DisabledShareOmitted(t *testing.T) {
	defer goleak.VerifyNone(t)
	out, err := Render(baseState())
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if strings.Contains(string(out), "[hidden]") {
		t.Error("disabled share 'hidden' should not appear in rendered output")
	}
}

func TestRender_ReadOnlyShare(t *testing.T) {
	defer goleak.VerifyNone(t)
	st := baseState()
	st.Shares[0].Writable = false
	out, err := Render(st)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if !strings.Contains(string(out), "read only = yes") {
		t.Error("non-writable share should have 'read only = yes'")
	}
}

func TestRender_GuestOk(t *testing.T) {
	defer goleak.VerifyNone(t)
	st := baseState()
	st.Shares[0].Public = true
	out, err := Render(st)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if !strings.Contains(string(out), "guest ok = yes") {
		t.Error("public share should have 'guest ok = yes'")
	}
}

func TestRender_CommentLine(t *testing.T) {
	defer goleak.VerifyNone(t)
	st := baseState()
	st.Shares[0].Comment = "My media library"
	out, err := Render(st)
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if !strings.Contains(string(out), "comment = My media library") {
		t.Error("expected comment line in rendered output")
	}
}

const sampleConf = `# Sample smb.conf
[global]
   workgroup = WORKGROUP
   server string = Test Server
   security = user

[media]
   path = /opt/media
   comment = My media library
   valid users = mediauser
   force user = mediauser
   read only = no
   browseable = yes
   guest ok = no

[public]
   path = /opt/public
   writable = yes
   guest ok = yes
`

func TestParseConf_Globals(t *testing.T) {
	defer goleak.VerifyNone(t)
	globals, _, _ := ParseConf([]byte(sampleConf))
	want := map[string]string{"workgroup": "WORKGROUP", "server string": "Test Server", "security": "user"}
	if len(globals) != len(want) {
		t.Fatalf("expected %d globals, got %d: %+v", len(want), len(globals), globals)
	}
	for _, g := range globals {
		if want[g.Key] != g.Value {
			t.Errorf("global %q = %q, want %q", g.Key, g.Value, want[g.Key])
		}
	}
}

func TestParseConf_Shares(t *testing.T) {
	defer goleak.VerifyNone(t)
	_, shares, owner := ParseConf([]byte(sampleConf))
	if len(shares) != 2 {
		t.Fatalf("expected 2 shares, got %d: %+v", len(shares), shares)
	}
	if owner != "mediauser" {
		t.Errorf("expected shareOwner 'mediauser', got %q", owner)
	}

	media := shares[0]
	if media.Name != "media" || media.Path != "/opt/media" {
		t.Errorf("unexpected media share: %+v", media)
	}
	if media.Comment != "My media library" {
		t.Errorf("expected comment, got %+v", media)
	}
	if !media.Writable {
		t.Error("expected media share to be writable (read only = no)")
	}
	if media.Public {
		t.Error("expected media share not public (guest ok = no)")
	}
	if !media.BrowseAble {
		t.Error("expected media share browseable")
	}
	if !media.Enabled {
		t.Error("expected imported share to be enabled")
	}

	public := shares[1]
	if !public.Writable {
		t.Error("expected public share writable (writable = yes)")
	}
	if !public.Public {
		t.Error("expected public share public (guest ok = yes)")
	}
}

func TestParseConf_CaseAndSpacingInsensitive(t *testing.T) {
	defer goleak.VerifyNone(t)
	conf := `[global]
Workgroup = WG

[data]
   Path   =   /opt/data
   Read Only = Yes
`
	globals, shares, _ := ParseConf([]byte(conf))
	if len(globals) != 1 || globals[0].Key != "workgroup" || globals[0].Value != "WG" {
		t.Errorf("unexpected globals: %+v", globals)
	}
	if len(shares) != 1 || shares[0].Path != "/opt/data" || shares[0].Writable {
		t.Errorf("unexpected shares: %+v", shares)
	}
}

func TestRender_GeneratedHeader(t *testing.T) {
	defer goleak.VerifyNone(t)
	out, err := Render(baseState())
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	if !strings.HasPrefix(string(out), "# Generated by smbed") {
		t.Error("rendered smb.conf should start with generated header comment")
	}
}

func TestWriteConf_BacksUpExistingFile(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	confPath := filepath.Join(dir, "smb.conf")
	original := []byte("# old config\n[global]\n   workgroup = OLD\n")
	if err := os.WriteFile(confPath, original, 0o644); err != nil {
		t.Fatalf("seeding %s: %v", confPath, err)
	}

	st := baseState()
	st.SmbConfPath = confPath
	if err := WriteConf(st, noplog); err != nil {
		t.Fatalf("WriteConf() error: %v", err)
	}

	// The new smb.conf should reflect the current state, not the old file.
	written, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("reading %s: %v", confPath, err)
	}
	if strings.Contains(string(written), "OLD") {
		t.Error("expected smb.conf to be overwritten with rendered content")
	}

	// Exactly one backup of the prior content should exist alongside it.
	matches, err := filepath.Glob(confPath + ".*.bak")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 backup file, got %d: %v", len(matches), matches)
	}
	backup, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("reading backup %s: %v", matches[0], err)
	}
	if string(backup) != string(original) {
		t.Errorf("backup content = %q, want %q", backup, original)
	}
}

func TestWriteConf_NoBackupWhenFileAbsent(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	confPath := filepath.Join(dir, "smb.conf")

	st := baseState()
	st.SmbConfPath = confPath
	if err := WriteConf(st, noplog); err != nil {
		t.Fatalf("WriteConf() error: %v", err)
	}

	matches, err := filepath.Glob(confPath + ".*.bak")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected no backup file when smb.conf did not previously exist, got %v", matches)
	}
}
