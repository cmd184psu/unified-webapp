package runner

import (
	"fmt"
	"regexp"
)

// Plugin is a runner plugin a project has opted into (OA3). It is always
// pinned: Version is an exact semantic version, never a tag such as "latest"
// and never a range, so a stage runs the same plugin code today and next month.
type Plugin struct {
	Name    string
	Version string
}

var (
	// pluginName is an npm package name, optionally scoped.
	pluginName = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
	// semver is semantic versioning 2.0.0: three numeric parts without leading
	// zeros, optional pre-release and build metadata.
	semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(-(0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?` +
		`(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
)

// Validate rejects a plugin that is not pinned to an exact semantic version:
// an empty or "latest" version, a range (^1.2.3, ~1.2, 1.x, >=1) or a partial
// version (1.2) are all refused.
func (p Plugin) Validate() error {
	if len(p.Name) > 214 || !pluginName.MatchString(p.Name) {
		return fmt.Errorf("plugin name %q must be an npm package name", p.Name)
	}
	if p.Version == "" {
		return fmt.Errorf("plugin %s needs a version", p.Name)
	}
	if !semver.MatchString(p.Version) {
		return fmt.Errorf("plugin %s version %q must be an exact version such as 1.2.3 (no latest, no ranges)", p.Name, p.Version)
	}
	return nil
}

// Spec is the pinned name@version form a runner config lists.
func (p Plugin) Spec() string { return p.Name + "@" + p.Version }
