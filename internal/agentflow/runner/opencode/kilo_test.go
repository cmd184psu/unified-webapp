package opencode

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestKiloDirsKeepsAuthOutOfTheRealDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config := filepath.Join(home, ".config", "kilo")
	data := filepath.Join(home, ".local", "share", "kilo")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	auth := filepath.Join(data, "auth.json")
	if err := os.WriteFile(auth, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	binds, env, err := kiloDirs(stage)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(HomeDir(stage), ".local", "share", "kilo", "auth.json")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("auth link: %v", err)
	}
	if runtime.GOOS == "darwin" {
		if target != auth || env["XDG_CONFIG_HOME"] != filepath.Join(home, ".config") {
			t.Fatalf("darwin target=%q env=%v", target, env)
		}
	} else {
		if target != "/sandbox/kilo-auth" || env["XDG_CONFIG_HOME"] != "/sandbox/kilo-config" {
			t.Fatalf("target=%q env=%v", target, env)
		}
	}
	var got []string
	for _, b := range binds {
		got = append(got, b.Src+"->"+b.Dst)
	}
	want := config + "->" + config
	if runtime.GOOS != "darwin" {
		want = config + "->/sandbox/kilo-config/kilo"
	}
	if !strings.Contains(strings.Join(got, ","), want) {
		t.Fatalf("binds = %v, want %s", got, want)
	}
}
