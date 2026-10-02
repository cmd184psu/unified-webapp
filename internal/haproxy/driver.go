// Package haproxy is the OS-agnostic HAProxy editor module. Every platform
// difference lives behind the Driver interface defined here; everything above
// it (model, render, validate, apply, UI, HTTP API) is OS-agnostic and tested
// with the in-package fake (FRD §4, D1, D13). No OS-specific value ever
// appears in an HTTP API shape.
package haproxy

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// OS is a supported platform the module knows how to drive. Anything else
// (including Intel macOS) is unsupported and makes the module serve a scoped
// 503 (FR-H30).
type OS string

const (
	OSUbuntu OS = "ubuntu"
	OSRocky  OS = "rocky" // also Rocky 9 / RHEL-family, per FRD §4
	OSMacOS  OS = "macos" // Apple silicon only
)

// DetectOS resolves the haproxy.os setting to a supported OS. An explicit,
// case-insensitive name ("ubuntu", "rocky", "macos"/"darwin") selects that
// platform directly. "auto" (or "") probes the running host: /etc/os-release
// ID ubuntu/rocky, or uname Darwin on arm64 for macOS. Any other value — an
// unsupported or unrecognised OS, including Intel macOS — returns an error
// whose message names the offending OS, which the caller surfaces as the
// scoped-503 reason.
//
// Intel macOS is refused for every setting (FR-H30), including an explicit
// "macos"; an explicit "macos" on a non-darwin host stays accepted so
// development and tests can run anywhere.
func DetectOS(setting string) (OS, error) {
	s := strings.ToLower(strings.TrimSpace(setting))
	if hostGOOS == "darwin" && hostGOARCH == "amd64" && (s == "macos" || s == "darwin" || s == "" || s == "auto") {
		return "", fmt.Errorf("unsupported haproxy os %q: Intel macOS is not supported (Apple silicon only)", "darwin/amd64")
	}
	switch s {
	case "ubuntu":
		return OSUbuntu, nil
	case "rocky":
		return OSRocky, nil
	case "macos", "darwin":
		return OSMacOS, nil
	case "", "auto":
		return detectHostOS()
	default:
		return "", fmt.Errorf("unsupported haproxy os %q: want one of auto, ubuntu, rocky, macos", setting)
	}
}

// Directive is one ordered key/value line of an HAProxy section (a global or
// defaults entry, or a baseline directive). It is the single kv shape shared
// by the model and the drivers.
type Directive struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// FileOwnership is the ownership and mode policy a driver applies to the files
// it installs. User/Group are empty when the platform installs as the running
// user and performs no chown (macOS). ConfigMode and CertMode are the file
// modes for the config/crt-list and the cert bundles respectively (certs are
// stricter because they embed a private key).
type FileOwnership struct {
	User       string
	Group      string
	ConfigMode os.FileMode
	CertMode   os.FileMode
}

// FileInfo is the file facts PrivilegedList reports for one file: just enough
// for the cert list (name, size, modified time); never any contents.
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// ServiceStatus is the driver's read of the HAProxy service. Active reports
// whether it is running; Detail is a short, human-readable line for the UI
// status panel. Version and uptime are reported separately (Version, and the
// module's own op log).
type ServiceStatus struct {
	Active bool   `json:"active"`
	Detail string `json:"detail"`
}

// Exec is the injectable command layer every privileged or external operation
// goes through, so tests never run real sudo/haproxy and no secret ever
// reaches a command line. Run executes name with args, feeds stdin bytes
// (used for atomic writes, so file contents and keys travel over stdin, never
// argv), and returns captured stdout and stderr. A non-zero exit is returned
// as a non-nil error.
type Exec interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) (stdout, stderr []byte, err error)
}

// Driver is the complete OS abstraction for the HAProxy editor. It is defined
// here, in full, at scaffolding time so the model/render (B2), the real
// per-OS implementations (B3) and the CertMachine install funnel (B5a) all
// build and test against the same, stable surface in parallel. Implementations
// add no methods to it. No method returns or accepts an OS-specific value that
// reaches an HTTP API shape (D13, FR-H31).
type Driver interface {
	// OSKind reports which platform this driver is for.
	OSKind() OS

	// --- paths (FRD §4) ---------------------------------------------------

	// ConfigPath is the absolute path of the live haproxy.cfg the editor owns
	// and overwrites.
	ConfigPath() string
	// CertsDir is the directory the editor owns and names cert files under.
	CertsDir() string
	// CrtListPath is the managed crt-list file the TLS binds reference.
	CrtListPath() string
	// StatsSocketOwner is the OS user the generated `stats socket` line hands
	// the socket to so the app can read it without sudo: the current app user on
	// Linux, "" on macOS (same user there; the `user` option needs root).
	StatsSocketOwner() string
	// StatsSocketPath is the HAProxy stats socket path; the generated global
	// creates it at level user, and Apply/stats read it.
	StatsSocketPath() string
	// BackupDir is the directory timestamped backups of the config and
	// crt-list are kept in (keep-N).
	BackupDir() string

	// --- service (FRD §4) -------------------------------------------------

	// ServiceName is the platform service identifier (the systemd unit, or
	// the driver-owned launchd job on macOS).
	ServiceName() string
	// Reload performs a graceful reload (systemctl reload / SIGUSR2 to the
	// master). It must not drop live connections. Returns an error if the
	// reload command fails.
	Reload(ctx context.Context) error
	// Restart fully restarts the service, dropping connections. Returns an
	// error on failure.
	Restart(ctx context.Context) error
	// Start starts the service (bootstrapping the launchd job on first run on
	// macOS). Returns an error on failure.
	Start(ctx context.Context) error
	// Status reports whether the service is active plus a short detail line.
	// A failure to query the service is returned as an error.
	Status(ctx context.Context) (ServiceStatus, error)

	// --- installed-file ownership (FRD §4) --------------------------------

	// Ownership is the user/group and modes this platform installs config and
	// cert files with.
	Ownership() FileOwnership

	// --- privileged I/O via Exec (D9) -------------------------------------

	// PrivilegedRead reads path with the platform's privilege (sudo), so the
	// editor can read root-owned config and cert files. Returns the file
	// bytes, or an error if the read fails.
	PrivilegedRead(ctx context.Context, path string) ([]byte, error)
	// PrivilegedWrite writes data to path atomically (write beside, then
	// rename) with the platform's privilege, applying mode and the driver's
	// ownership policy. The contents travel over the Exec stdin, never a
	// command line, so a cert's private key is never exposed in argv. Returns
	// an error if the write fails.
	PrivilegedWrite(ctx context.Context, path string, data []byte, mode os.FileMode) error

	// PrivilegedList lists the regular files directly in dir with the
	// platform's privilege (names and file facts only; never contents), so the
	// editor can show the managed certs directory (FR-H10) even when it is
	// root-owned. Returns an empty slice for an empty directory and an error if
	// the directory cannot be listed.
	PrivilegedList(ctx context.Context, dir string) ([]FileInfo, error)
	// PrivilegedRemove deletes the single file at path with the platform's
	// privilege (cert Update and Remove delete a superseded file only after a
	// successful Apply). Removing a path that is already gone is not an error.
	PrivilegedRemove(ctx context.Context, path string) error

	// --- baseline + binary (FRD §4) ---------------------------------------

	// BaselineGlobal returns the global directives this platform requires and
	// the UI marks as OS-managed (user/group/chroot/log/socket on Linux; the
	// macOS log/pidfile shape). The operator may change values the platform
	// allows; the driver rejects ones that cannot work on it.
	BaselineGlobal() []Directive
	// Validate runs `haproxy -c -f <configPath>` through the Exec layer and
	// returns a non-nil error (carrying haproxy's own message) when the config
	// is rejected. This is the arbiter of validity (D6).
	Validate(ctx context.Context, configPath string) error
	// Diagnostics returns human-readable, non-fatal, read-only OS conditions
	// the operator should know about (plain sentences, no OS-specific
	// controls), shown in the status panel and GET /api/status. It never
	// changes the system and returns nil/empty on a healthy system. A failure
	// to inspect is reported as no lines, never as an error.
	Diagnostics(ctx context.Context) []string

	// Version returns the output of `haproxy -v` through the Exec layer, for
	// the status panel. Returns an error if the binary cannot be run.
	Version(ctx context.Context) (string, error)
}
