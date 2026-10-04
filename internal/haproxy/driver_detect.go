package haproxy

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// hostGOOS and hostGOARCH are the platform DetectOS judges; variables so tests
// can inject one (see OverrideHostPlatform).
var (
	hostGOOS   = runtime.GOOS
	hostGOARCH = runtime.GOARCH
)

// OverrideHostPlatform makes DetectOS see goos/goarch and returns the restore
// func. It exists so the dispatcher's build tests can exercise the Intel Mac
// refusal on any host; production code never calls it.
func OverrideHostPlatform(goos, goarch string) (restore func()) {
	pg, pa := hostGOOS, hostGOARCH
	hostGOOS, hostGOARCH = goos, goarch
	return func() { hostGOOS, hostGOARCH = pg, pa }
}

// detectHostOS probes the running host for auto-detection (FRD §4). macOS is
// supported only on Apple silicon; Intel macOS is explicitly unsupported.
// Linux is identified from /etc/os-release: ubuntu, or the Rocky/RHEL family
// (Rocky 9 is a supported variant per FRD §4). B3 refines this; the interface
// and DetectOS contract do not change.
func detectHostOS() (OS, error) {
	switch hostGOOS {
	case "darwin":
		if hostGOARCH == "arm64" {
			return OSMacOS, nil
		}
		return "", fmt.Errorf("unsupported haproxy os %q: Intel macOS is not supported (Apple silicon only)", "darwin/"+hostGOARCH)
	case "linux":
		id := osReleaseID()
		switch id {
		case "ubuntu", "debian":
			return OSUbuntu, nil
		case "rocky", "rhel", "almalinux", "centos", "fedora":
			return OSRocky, nil
		}
		return "", fmt.Errorf("unsupported haproxy os %q: /etc/os-release ID is not ubuntu or rocky", id)
	default:
		return "", fmt.Errorf("unsupported haproxy os %q", hostGOOS)
	}
}

// osReleaseID returns the ID= value from /etc/os-release, lowercased, or ""
// when the file is absent or has no ID.
func osReleaseID() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			v := strings.TrimPrefix(line, "ID=")
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			return strings.ToLower(v)
		}
	}
	return ""
}
