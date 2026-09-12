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
					Modules: map[string]config.ModuleAuthConfig{"nosuchmodule": {}},
				}
			},
			wantErr: `auth.modules: unknown module "nosuchmodule"`,
		},
		{
			name: "admin module key is always allowed",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules:  map[string]config.ModuleAuthConfig{"admin": {}},
					AdminPIN: "1234",
				}
			},
			adminRouted: true,
			wantErr:     "",
		},
		{
			name: "protected non-admin module without ldap configured rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{"grocery": {}},
				}
			},
			wantErr: `module "grocery" is protected but auth.ldap.url is not set`,
		},
		{
			name: "protected non-admin module with ldap configured accepted",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{"grocery": {}},
					LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
				}
			},
			wantErr: "",
		},
		{
			name: "pin_file protected module still requires ldap",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{"todo": {PinFile: pinFile(t, 0400)}},
				}
			},
			wantErr: `module "todo" is protected but auth.ldap.url is not set`,
		},
		{
			name: "missing module pin_file rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{
						"todo": {PinFile: filepath.Join(t.TempDir(), "does-not-exist.pin")},
					},
					LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com"},
				}
			},
			wantErr: "auth.modules.todo.pin_file",
		},
		{
			name: "world-readable module pin_file rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{
						"todo": {PinFile: pinFile(t, 0644)},
					},
					LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com"},
				}
			},
			wantErr: "chmod 0400",
		},
		{
			name: "0400 module pin_file accepted",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{
						"todo": {PinFile: pinFile(t, 0400)},
					},
					LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com"},
				}
			},
			wantErr: "",
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
			name: "admin_pin and admin_pin_file both set rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					AdminPIN:     "1234",
					AdminPINFile: pinFile(t, 0400),
				}
			},
			adminRouted: true,
			wantErr:     "exactly one of auth.admin_pin, auth.admin_pin_file, or auth.modules.admin.pin_file",
		},
		{
			name: "admin_pin and modules.admin.pin_file both set rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					AdminPIN: "1234",
					Modules:  map[string]config.ModuleAuthConfig{"admin": {PinFile: pinFile(t, 0400)}},
				}
			},
			adminRouted: true,
			wantErr:     "exactly one of auth.admin_pin, auth.admin_pin_file, or auth.modules.admin.pin_file",
		},
		{
			name: "admin_pin_file and modules.admin.pin_file both set rejected",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					AdminPINFile: pinFile(t, 0400),
					Modules:      map[string]config.ModuleAuthConfig{"admin": {PinFile: pinFile(t, 0400)}},
				}
			},
			adminRouted: true,
			wantErr:     "exactly one of auth.admin_pin, auth.admin_pin_file, or auth.modules.admin.pin_file",
		},
		{
			name: "admin routed via modules.admin.pin_file alone accepted",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{"admin": {PinFile: pinFile(t, 0400)}},
				}
			},
			adminRouted: true,
			wantErr:     "",
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
			name: "empty api_keys is fine",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{"grocery": {}},
					LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
					APIKeys: nil,
				}
			},
			wantErr: "",
		},
		{
			name: "passkey config is optional",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{"grocery": {}},
					LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
				}
			},
			wantErr: "",
		},
		{
			name: "fully populated valid config",
			auth: func(t *testing.T) config.AuthConfig {
				return config.AuthConfig{
					Modules: map[string]config.ModuleAuthConfig{
						"grocery": {},
						"todo":    {PinFile: pinFile(t, 0400)},
						"admin":   {},
					},
					AdminPIN: "9999",
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
