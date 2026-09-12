package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestCertmachineDefaults(t *testing.T) {
	c := config.DefaultConfig()
	cm := c.Certmachine
	if cm.StaticDir != "./web/certmachine" {
		t.Errorf("static_dir = %q", cm.StaticDir)
	}
	if cm.DBPath != "./data/certmachine/certmachine.db" {
		t.Errorf("db_path = %q", cm.DBPath)
	}
	if cm.LegacyImportDir != "" {
		t.Errorf("legacy_import_dir = %q, want empty", cm.LegacyImportDir)
	}
	if cm.DefaultValidityDays != 365 {
		t.Errorf("default_validity_days = %d, want 365", cm.DefaultValidityDays)
	}
	if cm.ExpiryWarnDays != 30 {
		t.Errorf("expiry_warn_days = %d, want 30", cm.ExpiryWarnDays)
	}
}

func TestCertmachineLoadFromFile(t *testing.T) {
	path := writeCfg(t, `{"certmachine":{
		"static_dir":"/srv/web/certmachine",
		"db_path":"/var/lib/certmachine/certmachine.db",
		"legacy_import_dir":"/opt/certmachine/pki",
		"default_validity_days":730,
		"expiry_warn_days":14
	}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cm := c.Certmachine
	if cm.StaticDir != "/srv/web/certmachine" {
		t.Errorf("static_dir = %q", cm.StaticDir)
	}
	if cm.DBPath != "/var/lib/certmachine/certmachine.db" {
		t.Errorf("db_path = %q", cm.DBPath)
	}
	if cm.LegacyImportDir != "/opt/certmachine/pki" {
		t.Errorf("legacy_import_dir = %q", cm.LegacyImportDir)
	}
	if cm.DefaultValidityDays != 730 {
		t.Errorf("default_validity_days = %d, want 730", cm.DefaultValidityDays)
	}
	if cm.ExpiryWarnDays != 14 {
		t.Errorf("expiry_warn_days = %d, want 14", cm.ExpiryWarnDays)
	}
}

func TestCertmachineTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	path := writeCfg(t, `{"certmachine":{
		"static_dir":"~/web",
		"db_path":"~/certmachine.db",
		"legacy_import_dir":"~/pki"
	}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cm := c.Certmachine
	for name, pair := range map[string][2]string{
		"static_dir":        {cm.StaticDir, filepath.Join(home, "web")},
		"db_path":           {cm.DBPath, filepath.Join(home, "certmachine.db")},
		"legacy_import_dir": {cm.LegacyImportDir, filepath.Join(home, "pki")},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
}

func TestCertmachineLegacyImportDirEmptyStaysEmpty(t *testing.T) {
	path := writeCfg(t, `{"certmachine":{"legacy_import_dir":""}}`)

	c, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Certmachine.LegacyImportDir != "" {
		t.Errorf("legacy_import_dir = %q, want empty", c.Certmachine.LegacyImportDir)
	}
}

// Load is the single validation point for default_validity_days and
// expiry_warn_days (FR-1): zero takes the default, negative is rejected
// naming the field.
func TestCertmachineDaysValidation(t *testing.T) {
	cases := []struct {
		name         string
		value        string
		wantValidity int
		wantWarn     int
		wantErr      bool
		wantErrField string
	}{
		{"unset takes defaults", `{"certmachine":{}}`, 365, 30, false, ""},
		{"zero validity takes default", `{"certmachine":{"default_validity_days":0}}`, 365, 30, false, ""},
		{"zero warn takes default", `{"certmachine":{"expiry_warn_days":0}}`, 365, 30, false, ""},
		{"explicit values are kept", `{"certmachine":{"default_validity_days":90,"expiry_warn_days":7}}`, 90, 7, false, ""},
		{"negative validity is rejected", `{"certmachine":{"default_validity_days":-1}}`, 0, 0, true, "default_validity_days"},
		{"negative warn is rejected", `{"certmachine":{"expiry_warn_days":-1}}`, 0, 0, true, "expiry_warn_days"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := config.Load(writeCfg(t, tc.value))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !containsString(err.Error(), tc.wantErrField) {
					t.Errorf("error %q does not name field %q", err.Error(), tc.wantErrField)
				}
				return
			}
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if c.Certmachine.DefaultValidityDays != tc.wantValidity {
				t.Errorf("default_validity_days = %d, want %d", c.Certmachine.DefaultValidityDays, tc.wantValidity)
			}
			if c.Certmachine.ExpiryWarnDays != tc.wantWarn {
				t.Errorf("expiry_warn_days = %d, want %d", c.Certmachine.ExpiryWarnDays, tc.wantWarn)
			}
		})
	}
}

func containsString(haystack, needle string) bool {
	return needle != "" && len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// What `make init-config` writes must load back unchanged, and every FR-1 key
// must be present so an operator editing the file sees all five, not a subset
// silently defaulted.
func TestWrittenDefaultConfigLoadsUnchangedCertmachine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unified-webapp.json")
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var onDisk config.Config
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.Certmachine != onDisk.Certmachine {
		t.Errorf("init-config output did not survive Load:\n on disk %+v\n loaded  %+v", onDisk.Certmachine, loaded.Certmachine)
	}

	var raw struct {
		Certmachine map[string]json.RawMessage `json:"certmachine"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{
		"static_dir", "db_path", "legacy_import_dir", "default_validity_days", "expiry_warn_days",
	} {
		if _, ok := raw.Certmachine[key]; !ok {
			t.Errorf("init-config output is missing the %q key", key)
		}
	}
}
