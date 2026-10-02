package haproxy

import (
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// The in-package fake must satisfy the Driver interface so B2 and B5a can
// build and test against the interface in parallel with B3's real drivers.
// This is a compile-time assertion made runnable: newFakeDriver must return
// something usable as a Driver, exercised through a couple of its methods.
func TestFakeDriverSatisfiesDriverInterface(t *testing.T) {
	var d Driver = newFakeDriver()
	if d.ConfigPath() == "" {
		t.Error("fake driver ConfigPath() is empty")
	}
	if d.ServiceName() == "" {
		t.Error("fake driver ServiceName() is empty")
	}
	if len(d.BaselineGlobal()) == 0 {
		t.Error("fake driver BaselineGlobal() is empty")
	}
}

func TestDetectOSExplicitSupported(t *testing.T) {
	cases := map[string]OS{
		"ubuntu": OSUbuntu,
		"rocky":  OSRocky,
		"macos":  OSMacOS,
		"darwin": OSMacOS,
		"Ubuntu": OSUbuntu, // case-insensitive
	}
	for setting, want := range cases {
		got, err := DetectOS(setting)
		if err != nil {
			t.Errorf("DetectOS(%q) returned error: %v", setting, err)
			continue
		}
		if got != want {
			t.Errorf("DetectOS(%q) = %q, want %q", setting, got, want)
		}
	}
}

func TestDetectOSUnsupported(t *testing.T) {
	for _, setting := range []string{"windows", "freebsd", "intel-mac", "solaris"} {
		_, err := DetectOS(setting)
		if err == nil {
			t.Errorf("DetectOS(%q) accepted an unsupported OS; want an error", setting)
			continue
		}
		if !strings.Contains(err.Error(), setting) {
			t.Errorf("DetectOS(%q) error does not name the OS: %v", setting, err)
		}
	}
}

func withPlatform(t *testing.T, goos, goarch string) {
	t.Helper()
	prevOS, prevArch := hostGOOS, hostGOARCH
	hostGOOS, hostGOARCH = goos, goarch
	t.Cleanup(func() { hostGOOS, hostGOARCH = prevOS, prevArch })
}

// FR-H30: Intel macOS is refused whatever the setting says.
func TestDetectOSRefusesIntelMacEvenWhenExplicit(t *testing.T) {
	withPlatform(t, "darwin", "amd64")
	for _, setting := range []string{"macos", "darwin", "auto", ""} {
		_, err := DetectOS(setting)
		if err == nil || !strings.Contains(err.Error(), "Apple silicon only") {
			t.Errorf("DetectOS(%q) on darwin/amd64 = %v, want the Apple-silicon-only refusal", setting, err)
		}
	}
	if got, err := DetectOS("ubuntu"); err != nil || got != OSUbuntu {
		t.Errorf("explicit ubuntu unaffected: %v %v", got, err)
	}
}

func TestDetectOSExplicitMacOSAcceptedElsewhere(t *testing.T) {
	for _, p := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}} {
		withPlatform(t, p[0], p[1])
		if got, err := DetectOS("macos"); err != nil || got != OSMacOS {
			t.Errorf("macos on %s/%s = %v, %v", p[0], p[1], got, err)
		}
	}
}

// The CertMachine API key is a secret: it must never reach a log line, even
// when the whole config struct is formatted with %v/%s. (Mirrors how the
// admin LDAP bind_password is redacted.)
func TestCertMachineConfigStringRedactsAPIKey(t *testing.T) {
	const secret = "supersecret-haproxy-key-9f3a"
	cm := config.HaproxyCertMachineConfig{URL: "https://cm.example", APIKey: secret, CAFile: "/ca.pem"}

	if s := cm.String(); strings.Contains(s, secret) {
		t.Fatalf("HaproxyCertMachineConfig.String() leaks the api_key: %s", s)
	}
	if s := cm.String(); !strings.Contains(s, "(set)") {
		t.Errorf("String() of a set api_key should show (set): %s", s)
	}

	empty := config.HaproxyCertMachineConfig{URL: "https://cm.example"}
	if s := empty.String(); strings.Contains(s, "(set)") {
		t.Errorf("String() of an unset api_key should not show (set): %s", s)
	}
}
