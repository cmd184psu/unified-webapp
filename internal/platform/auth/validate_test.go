package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func pinFile(t *testing.T, mode os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.pin")
	if err := os.WriteFile(path, []byte("1234"), mode); err != nil {
		t.Fatalf("writing pin file: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod pin file: %v", err)
	}
	return path
}

func TestValidatePolicy(t *testing.T) {
	knownModules := []string{"grocery", "todo"}

	tests := []struct {
		name        string
		auth        func(t *testing.T) config.AuthConfig
		adminRouted bool
		wantErr     string // substring expected in error, "" means no error
	}{
		{
			name: "zero config is valid",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{}
			},
			adminRouted: false,
			wantErr:     "",
		},
		{
			name: "unknown module rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string][]string{"nosuchmodule": {"pin"}},
					PINs:    []config.NamedHash{{Name: "a", Hash: "h"}},
				}
			},
			wantErr: `auth.modules: unknown module "nosuchmodule"`,
		},
		{
			name: "admin module key is always allowed",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules:  map[string][]string{"admin": {"pin"}},
					PINs:     []config.NamedHash{{Name: "a", Hash: "h"}},
					AdminPIN: "1234",
				}
			},
			wantErr: "",
		},
		{
			name: "unrecognized method rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string][]string{"grocery": {"carrier_pigeon"}},
				}
			},
			wantErr: `unknown auth method "carrier_pigeon"`,
		},
		{
			name: "ldap method without ldap config rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string][]string{"grocery": {"ldap"}},
				}
			},
			wantErr: "auth.ldap.url is not set",
		},
		{
			name: "pin method without pins configured rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string][]string{"grocery": {"pin"}},
				}
			},
			wantErr: "auth.pins is empty",
		},
		{
			name: "key method without api keys configured rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string][]string{"grocery": {"key"}},
				}
			},
			wantErr: "auth.api_keys is empty",
		},
		{
			name: "passkey method without rp_id configured rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string][]string{"grocery": {"passkey"}},
				}
			},
			wantErr: "auth.passkey.rp_id is not set",
		},
		{
			name: "admin_pin literal in modules list rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string][]string{"grocery": {"admin_pin"}},
				}
			},
			wantErr: `reserved token`,
		},
		{
			name: "admin routed without any pin form rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{}
			},
			adminRouted: true,
			wantErr:     "admin module requires an operator PIN",
		},
		{
			name: "both admin_pin and admin_pin_file set rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					AdminPIN:     "1234",
					AdminPINFile: pinFile(t, 0400),
				}
			},
			adminRouted: true,
			wantErr:     "exactly one of auth.admin_pin or auth.admin_pin_file",
		},
		{
			name: "admin_pin_file missing rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					AdminPINFile: filepath.Join(t.TempDir(), "does-not-exist.pin"),
				}
			},
			adminRouted: true,
			wantErr:     "auth.admin_pin_file",
		},
		{
			name: "admin_pin_file mode 0644 rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					AdminPINFile: pinFile(t, 0644),
				}
			},
			adminRouted: true,
			wantErr:     "chmod 0400",
		},
		{
			name: "admin_pin_file mode 0400 accepted",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					AdminPINFile: pinFile(t, 0400),
				}
			},
			adminRouted: true,
			wantErr:     "",
		},
		{
			name: "admin_pin_file mode 0600 accepted",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					AdminPINFile: pinFile(t, 0600),
				}
			},
			adminRouted: true,
			wantErr:     "",
		},
		{
			name: "fully populated valid config",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string][]string{
						"grocery": {"pin", "key"},
						"todo":    {"ldap", "passkey"},
						"admin":   {"pin"},
					},
					AdminPIN: "9999",
					PINs:     []config.NamedHash{{Name: "chris", Hash: "bcryptedhash"}},
					APIKeys:  []config.NamedHash{{Name: "svc", Hash: "sha256:deadbeef"}},
					LDAP: config.LDAPConfig{
						URL:            "ldaps://ldap.example.com",
						BaseDN:         "dc=example,dc=com",
						RequiredGroups: []string{"staff"},
					},
					Passkey: config.PasskeyConfig{
						RPID:      "example.com",
						RPOrigins: []string{"https://example.com"},
					},
					Session: config.SessionConfig{
						TTLHours:             168,
						RefreshAfterFraction: 0.5,
					},
				}
			},
			adminRouted: true,
			wantErr:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePolicy(tt.auth(t), knownModules, tt.adminRouted)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}
