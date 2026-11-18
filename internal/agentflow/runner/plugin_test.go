package runner

import "testing"

func TestPluginMustBePinnedToAnExactVersion(t *testing.T) {
	for _, v := range []string{"1.2.3", "0.0.1", "10.20.30", "1.0.0-beta.1", "1.0.0-rc.1+build.5", "2.0.0+meta"} {
		if err := (Plugin{Name: "opencode-notify", Version: v}).Validate(); err != nil {
			t.Errorf("version %q rejected: %v", v, err)
		}
	}
	for _, v := range []string{"", "latest", "next", "*", "x", "^1.2.3", "~1.2.3", "1.x", "1.2.x", ">=1.0.0", "1.2.3 - 2.0.0", "1.2.3 || 2.0.0",
		"1.2", "1", "v1.2.3", "01.2.3", "1.2.3-", "1.2.3 ", "1.2.3\n", "file:../x", "git+https://x/y"} {
		if err := (Plugin{Name: "opencode-notify", Version: v}).Validate(); err == nil {
			t.Errorf("version %q accepted", v)
		}
	}
	for _, n := range []string{"opencode-notify", "@scope/pkg", "a", "my.plugin_x"} {
		if err := (Plugin{Name: n, Version: "1.0.0"}).Validate(); err != nil {
			t.Errorf("name %q rejected: %v", n, err)
		}
	}
	for _, n := range []string{"", "pkg@latest", "pkg@1.0.0", "Pkg", "../x", "/abs", "@scope", "@/pkg", "a b", "-x", "http://x/y.tgz"} {
		if err := (Plugin{Name: n, Version: "1.0.0"}).Validate(); err == nil {
			t.Errorf("name %q accepted", n)
		}
	}
	if got := (Plugin{Name: "@s/p", Version: "1.2.3"}).Spec(); got != "@s/p@1.2.3" {
		t.Errorf("spec = %q", got)
	}
}
