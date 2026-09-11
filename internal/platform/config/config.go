package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const DefaultConfigPath = "~/.unified-webapp.json"

// Config is the top-level unified server configuration.
type Config struct {
	Port        int               `json:"port"`
	TLSCert     string            `json:"tls_cert"`
	TLSKey      string            `json:"tls_key"`
	Routing     map[string]string `json:"host_routing"` // hostname → module name
	Server      ServerConfig      `json:"server"`
	Grocery     GroceryConfig     `json:"grocery"`
	Todo        TodoConfig        `json:"todo"`
	Slideshow   SlideshowConfig   `json:"slideshow"`
	Menuserver  MenuserverConfig  `json:"menuserver"`
	Obsidianoid ObsidianoidConfig `json:"obsidianoid"`
	Multissh    MultisshConfig    `json:"multissh"`
	Auth        AuthConfig        `json:"auth"`
	Admin       AdminConfig       `json:"admin"`

	// configPath is the absolute path Load read this Config from (empty when
	// built via DefaultConfig()/WriteDefault without going through Load, or
	// when the config file did not exist). It is unexported (json:"-" is
	// redundant for an unexported field, but not serialized regardless) so
	// admin's live-apply (T5.3) can locate the file to splice without every
	// caller threading a path around separately.
	configPath string
}

// ConfigPath returns the absolute path this Config was loaded from (see
// Load), or "" if it was never loaded from a file.
func (c *Config) ConfigPath() string {
	return c.configPath
}

// AuthConfig holds the shared authentication/authorization configuration
// surface used across modules. Modules maps a module name (or the reserved
// "admin" pseudo-module) to the ordered list of methods accepted for it.
type AuthConfig struct {
	Modules      map[string][]string `json:"modules"`
	AdminPIN     string              `json:"admin_pin"`
	AdminPINFile string              `json:"admin_pin_file"`
	DataDir      string              `json:"data_dir"`
	CookieSecure bool                `json:"cookie_secure"`
	CookieDomain string              `json:"cookie_domain"`
	Session      SessionConfig       `json:"session"`
	LDAP         LDAPConfig          `json:"ldap"`
	PINs         []NamedHash         `json:"pins"`
	APIKeys      []NamedHash         `json:"api_keys"`
	Passkey      PasskeyConfig       `json:"passkey"`
}

// SessionConfig controls session lifetime and sliding-refresh behavior.
type SessionConfig struct {
	TTLHours             int     `json:"ttl_hours"`              // default 720 (30d) applied downstream, 0 = default
	RefreshAfterFraction float64 `json:"refresh_after_fraction"` // default 0.5, 0 = default
}

// NamedHash pairs an operator-facing name with a stored credential hash.
// Hash is bcrypt for pins, "sha256:<hex>" for api keys.
type NamedHash struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// LDAPConfig configures LDAP authentication. Full behavior is ported in
// T3.7; fields are defined now for validation.
type LDAPConfig struct {
	URL            string   `json:"url"`
	StartTLS       bool     `json:"start_tls"`
	InsecureTLS    bool     `json:"insecure_tls"`
	BindDN         string   `json:"bind_dn"`
	BindPassword   string   `json:"bind_password"`
	BaseDN         string   `json:"base_dn"`
	UserFilter     string   `json:"user_filter"`
	RequiredGroups []string `json:"required_groups"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// PasskeyConfig configures WebAuthn/passkey authentication.
type PasskeyConfig struct {
	RPID      string   `json:"rp_id"`
	RPOrigins []string `json:"rp_origins"`
}

// ServerConfig holds configuration for the shared HTTP server infrastructure,
// as opposed to any single module.
type ServerConfig struct {
	// OriginCheck controls request-origin validation. Semantics are wired up
	// separately; this field only carries the configured value for now.
	OriginCheck string `json:"origin_check"`
	// SSEMaxSubscribers caps concurrent SSE subscribers per broker across all
	// modules. 0 (unset) takes DefaultSSEMaxSubscribers.
	SSEMaxSubscribers int `json:"sse_max_subscribers"`
}

// DefaultSSEMaxSubscribers is the SSE subscriber cap applied when
// server.sse_max_subscribers is unset (0) in the config file.
const DefaultSSEMaxSubscribers = 64

// ObsidianoidVault holds the per-vault configuration for the obsidianoid module.
type ObsidianoidVault struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Theme string `json:"theme"`
}

// ObsidianoidConfig holds configuration specific to the obsidianoid module.
type ObsidianoidConfig struct {
	StaticDir        string             `json:"static_dir"`
	DataDir          string             `json:"data_dir"`
	Vaults           []ObsidianoidVault `json:"vaults"`
	ThreadsFolder    string             `json:"threads_folder"`
	ThreadCount      int                `json:"thread_count"`
	AutoSaveDisabled bool               `json:"autosave_disabled"`
	// SSEMaxSubscribers is the effective SSE subscriber cap, copied from
	// Config.Server.SSEMaxSubscribers by Load. Not read from the config file.
	SSEMaxSubscribers int `json:"-"`
}

// TodoConfig holds configuration specific to the todo module.
type TodoConfig struct {
	StaticDir           string `json:"static_dir"`
	DataDir             string `json:"data_dir"`
	Ext                 string `json:"ext"`
	DefaultSubject      string `json:"default_subject"`
	SyncIntervalSeconds int    `json:"sync_interval_seconds"`
	// SSEMaxSubscribers is the effective SSE subscriber cap, copied from
	// Config.Server.SSEMaxSubscribers by Load. Not read from the config file.
	SSEMaxSubscribers int `json:"-"`
}

// AdminConfig holds configuration specific to the admin module (FR-M). Admin
// is always protected (T3.1's boot validation refuses it routed without
// exactly one operator-PIN form), so it carries no other module's data
// fields yet -- T5.1 is a static-shell scaffold only.
type AdminConfig struct {
	StaticDir    string `json:"static_dir"`
	MaxBodyBytes int64  `json:"max_body_bytes"`
}

// MenuserverConfig holds configuration specific to the menuserver module.
type MenuserverConfig struct {
	StaticDir    string `json:"static_dir"`
	DataDir      string `json:"data_dir"`
	ShowAllPages bool   `json:"show_all_pages"`
}

// MusicConfig holds music configuration. Collections are discovered automatically
// by scanning subdirectories of AudioDir; no explicit list is needed.
type MusicConfig struct {
	AudioDir string `json:"audio_dir"`
}

// SlideshowConfig holds configuration specific to the slideshow module.
type SlideshowConfig struct {
	StaticDir       string      `json:"static_dir"`
	ImageDir        string      `json:"image_dir"`
	Prefix          string      `json:"prefix"`
	DefaultSubject  string      `json:"default_subject"`
	IntervalSeconds int         `json:"interval_seconds"`
	AgeCutoffDays   int         `json:"age_cutoff_days"`
	DefaultMode     string      `json:"default_mode"`
	DefaultShuffle  bool        `json:"default_shuffle"`
	DefaultTheme    string      `json:"default_theme"`
	Music           MusicConfig `json:"music"`
	// SSEMaxSubscribers is the effective SSE subscriber cap, copied from
	// Config.Server.SSEMaxSubscribers by Load. Not read from the config file.
	SSEMaxSubscribers int `json:"-"`
}

// GroceryConfig holds configuration specific to the grocery module.
type GroceryConfig struct {
	StaticDir           string   `json:"static_dir"`
	DataFile            string   `json:"data_file"`
	Groups              []string `json:"groups"`
	Progress            bool     `json:"progress"`
	SyncIntervalSeconds int      `json:"sync_interval_seconds"`
	Title               string   `json:"title"`
	// SSEMaxSubscribers is the effective SSE subscriber cap, copied from
	// Config.Server.SSEMaxSubscribers by Load. Not read from the config file.
	SSEMaxSubscribers int `json:"-"`
}

// MultisshConfig holds configuration specific to the multissh module.
//
// StrictHostKey controls SSH host-key verification. When false (the default,
// suited to trusted lab networks with frequently rebuilt VMs) SSH connections
// skip host-key verification. When true, host keys are verified against
// KnownHostsPath for both the terminal bridge and SFTP broadcasts, failing
// closed if that file is missing or a key is unknown/mismatched.
//
// SSHDir, UploadDir, BrowseRoot and KnownHostsPath are resolved at Build time
// when left empty, so the written default config keeps them as present-but-empty
// strings rather than baking machine-specific paths into the file.
type MultisshConfig struct {
	StaticDir      string `json:"static_dir"`
	SSHDir         string `json:"ssh_dir"`
	UploadDir      string `json:"upload_dir"`
	HostsPath      string `json:"hosts_path"`
	BrowseRoot     string `json:"browse_root"`
	MaxSessions    int    `json:"max_sessions"`
	MaxUploadBytes int64  `json:"max_upload_bytes"`
	StrictHostKey  bool   `json:"strict_host_key"`
	KnownHostsPath string `json:"known_hosts_path"`
}

// Multissh session-count bounds. MaxSessions is validated in exactly one place
// (Load); Build trusts the resolved value and performs no re-validation.
const (
	DefaultMaxSessions = 3
	MaxMaxSessions     = 16
)

// DefaultConfig returns a Config populated with safe defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:    8080,
		Routing: map[string]string{},
		Grocery: GroceryConfig{
			StaticDir:           "./web/grocery",
			DataFile:            "./data/grocery.json",
			SyncIntervalSeconds: 1,
			Title:               "Grocery List",
			Groups: []string{
				"Produce",
				"Meats",
				"mid store",
				"back wall",
				"frozen",
				"deli area near front",
			},
		},
		Todo: TodoConfig{
			StaticDir:           "./web/todo",
			DataDir:             "./data/todo",
			Ext:                 "json",
			DefaultSubject:      "home",
			SyncIntervalSeconds: 1,
		},
		Slideshow: SlideshowConfig{
			StaticDir:       "./web/slideshow",
			ImageDir:        "./data/slideshow",
			Prefix:          "slides",
			IntervalSeconds: 8,
			DefaultMode:     "kenburns",
			DefaultTheme:    "dark",
		},
		Menuserver: MenuserverConfig{
			StaticDir: "./web/menuserver",
			DataDir:   "./data/menuserver",
		},
		Obsidianoid: ObsidianoidConfig{
			StaticDir:     "./web/obsidianoid",
			DataDir:       "./data/obsidianoid",
			ThreadsFolder: "Threads",
			ThreadCount:   4,
		},
		Multissh: MultisshConfig{
			StaticDir:      "./web/multissh",
			HostsPath:      "./data/multissh/multissh-hosts.json",
			MaxSessions:    DefaultMaxSessions,
			MaxUploadBytes: 8 << 30, // 8 GiB
			StrictHostKey:  false,
		},
		Admin: AdminConfig{
			StaticDir:    "./web/admin",
			MaxBodyBytes: defaultModuleBodyBytes,
		},
		Auth: AuthConfig{
			// Present-but-empty (FR-A14): a config with no operator edits to
			// this section loads identically to a config with no "auth" key
			// at all -- ValidatePolicy's fast path and FromConfig's lazy key
			// creation both key off len()/=="" checks, which empty
			// maps/slices satisfy exactly like nil. Written out explicitly
			// (rather than left as Go's zero value, which would marshal
			// Modules/PINs/APIKeys as JSON null) so an operator opening the
			// generated file sees the auth surface's shape instead of an
			// unexplained null.
			Modules: map[string][]string{},
			PINs:    []NamedHash{},
			APIKeys: []NamedHash{},
		},
		Server: ServerConfig{
			OriginCheck:       "enforce",
			SSEMaxSubscribers: DefaultSSEMaxSubscribers,
		},
	}
}

// defaultModuleBodyBytes is the request-body ceiling applied to a module
// config's MaxBodyBytes when left unset (0), mirroring
// cmd/server/main.go's defaultBodyLimit convention.
const defaultModuleBodyBytes int64 = 1 << 20 // 1 MiB

// ExpandPath expands a leading ~ to the user home directory.
func ExpandPath(path string) (string, error) {
	if len(path) == 0 || path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}

// Load reads the config file at path (~ supported). Missing file returns defaults.
func Load(path string) (*Config, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	cfg.configPath = expanded

	data, err := os.ReadFile(expanded)
	if os.IsNotExist(err) {
		applyServerDefaults(cfg)
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", expanded, err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", expanded, err)
	}

	if err := expandGroceryPaths(&cfg.Grocery); err != nil {
		return nil, err
	}
	if err := expandTodoPaths(&cfg.Todo); err != nil {
		return nil, err
	}
	if err := expandSlideshowPaths(&cfg.Slideshow); err != nil {
		return nil, err
	}
	if err := expandMenuserverPaths(&cfg.Menuserver); err != nil {
		return nil, err
	}
	if err := expandObsidianoidPaths(&cfg.Obsidianoid); err != nil {
		return nil, err
	}
	if err := expandMultisshPaths(&cfg.Multissh); err != nil {
		return nil, err
	}
	if err := normalizeMultissh(&cfg.Multissh); err != nil {
		return nil, err
	}
	if err := expandAuthPaths(cfg, filepath.Dir(expanded)); err != nil {
		return nil, err
	}
	if err := expandAdminPaths(&cfg.Admin); err != nil {
		return nil, err
	}
	if cfg.TLSCert != "" {
		if cfg.TLSCert, err = ExpandPath(cfg.TLSCert); err != nil {
			return nil, err
		}
	}
	if cfg.TLSKey != "" {
		if cfg.TLSKey, err = ExpandPath(cfg.TLSKey); err != nil {
			return nil, err
		}
	}
	applyServerDefaults(cfg)
	return cfg, nil
}

// applyServerDefaults normalizes cfg.Server and copies the effective SSE
// subscriber cap into each SSE-serving module's config, mirroring the
// path-expansion pattern used elsewhere in Load.
func applyServerDefaults(cfg *Config) {
	max := cfg.Server.SSEMaxSubscribers
	if max <= 0 {
		max = DefaultSSEMaxSubscribers
	}
	cfg.Grocery.SSEMaxSubscribers = max
	cfg.Todo.SSEMaxSubscribers = max
	cfg.Slideshow.SSEMaxSubscribers = max
	cfg.Obsidianoid.SSEMaxSubscribers = max
}

func expandMenuserverPaths(m *MenuserverConfig) error {
	var err error
	if m.StaticDir, err = ExpandPath(m.StaticDir); err != nil {
		return err
	}
	if m.DataDir, err = ExpandPath(m.DataDir); err != nil {
		return err
	}
	return nil
}

func expandAdminPaths(a *AdminConfig) error {
	var err error
	if a.StaticDir, err = ExpandPath(a.StaticDir); err != nil {
		return err
	}
	return nil
}

func expandSlideshowPaths(s *SlideshowConfig) error {
	var err error
	if s.StaticDir, err = ExpandPath(s.StaticDir); err != nil {
		return err
	}
	if s.ImageDir, err = ExpandPath(s.ImageDir); err != nil {
		return err
	}
	if s.Music.AudioDir != "" {
		if s.Music.AudioDir, err = ExpandPath(s.Music.AudioDir); err != nil {
			return err
		}
	}
	return nil
}

func expandTodoPaths(t *TodoConfig) error {
	var err error
	if t.StaticDir, err = ExpandPath(t.StaticDir); err != nil {
		return err
	}
	if t.DataDir, err = ExpandPath(t.DataDir); err != nil {
		return err
	}
	return nil
}

func expandGroceryPaths(g *GroceryConfig) error {
	var err error
	if g.StaticDir, err = ExpandPath(g.StaticDir); err != nil {
		return err
	}
	if g.DataFile, err = ExpandPath(g.DataFile); err != nil {
		return err
	}
	return nil
}

func expandObsidianoidPaths(o *ObsidianoidConfig) error {
	var err error
	if o.StaticDir, err = ExpandPath(o.StaticDir); err != nil {
		return err
	}
	if o.DataDir, err = ExpandPath(o.DataDir); err != nil {
		return err
	}
	for i := range o.Vaults {
		if o.Vaults[i].Path, err = ExpandPath(o.Vaults[i].Path); err != nil {
			return err
		}
	}
	return nil
}

// expandAuthPaths makes Auth.DataDir and Auth.AdminPINFile absolute relative
// to the config file's directory (baseDir), mirroring the other expand*Paths
// functions but resolving against the config's location rather than the
// working directory.
func expandAuthPaths(cfg *Config, baseDir string) error {
	var err error
	if cfg.Auth.DataDir != "" {
		if cfg.Auth.DataDir, err = expandRelativeTo(cfg.Auth.DataDir, baseDir); err != nil {
			return err
		}
	}
	if cfg.Auth.AdminPINFile != "" {
		if cfg.Auth.AdminPINFile, err = expandRelativeTo(cfg.Auth.AdminPINFile, baseDir); err != nil {
			return err
		}
	}
	return nil
}

// expandRelativeTo expands a leading ~ via ExpandPath, then makes the result
// absolute relative to baseDir if it is not already absolute.
func expandRelativeTo(path, baseDir string) (string, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(expanded) {
		return expanded, nil
	}
	return filepath.Join(baseDir, expanded), nil
}

func expandMultisshPaths(m *MultisshConfig) error {
	var err error
	if m.StaticDir, err = ExpandPath(m.StaticDir); err != nil {
		return err
	}
	if m.SSHDir, err = ExpandPath(m.SSHDir); err != nil {
		return err
	}
	if m.UploadDir, err = ExpandPath(m.UploadDir); err != nil {
		return err
	}
	if m.HostsPath, err = ExpandPath(m.HostsPath); err != nil {
		return err
	}
	if m.BrowseRoot, err = ExpandPath(m.BrowseRoot); err != nil {
		return err
	}
	if m.KnownHostsPath, err = ExpandPath(m.KnownHostsPath); err != nil {
		return err
	}
	return nil
}

// normalizeMultissh is the single validation point for max_sessions (FR-N1).
// Zero means "unset" and takes the default; negative is an operator error and
// is rejected; an absurdly large value is a typo and is clamped with a warning
// rather than refused. multissh.Build trusts the result and does not re-check.
func normalizeMultissh(m *MultisshConfig) error {
	switch {
	case m.MaxSessions < 0:
		return fmt.Errorf("multissh: max_sessions must be at least 1, got %d", m.MaxSessions)
	case m.MaxSessions == 0:
		m.MaxSessions = DefaultMaxSessions
	case m.MaxSessions > MaxMaxSessions:
		log.Printf("multissh: max_sessions %d exceeds the maximum of %d; clamping to %d", m.MaxSessions, MaxMaxSessions, MaxMaxSessions)
		m.MaxSessions = MaxMaxSessions
	}
	return nil
}

// WriteDefault writes a default config to path if the file does not already exist.
func WriteDefault(path string) error {
	expanded, err := ExpandPath(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(expanded); err == nil {
		return nil
	}
	data, err := json.MarshalIndent(DefaultConfig(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(expanded, data, 0600)
}
