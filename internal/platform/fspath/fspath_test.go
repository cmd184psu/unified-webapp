package fspath_test

import (
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/fspath"
)

func TestValidName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty string rejected", "", false},
		{"dot-prefixed rejected", ".hidden", false},
		{"exact dotdot rejected", "..", false},
		{"dotdot-prefixed rejected", "..name", false},
		{"single dot rejected", ".", false},
		{"forward slash rejected", "a/b", false},
		{"backslash rejected", "a\\b", false},
		{"embedded slash rejected", "sub/dir", false},
		{"plain name accepted", "beach", true},
		{"name with extension accepted", "photo.jpg", true},
		{"dot not at prefix accepted", "a.b", true},
		{"name with dashes accepted", "my-list", true},
		{"name with spaces accepted", "New Note.md", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fspath.ValidName(c.in)
			if got != c.want {
				t.Errorf("ValidName(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestConfineTo(t *testing.T) {
	root := filepath.FromSlash("/srv/data")

	cases := []struct {
		name    string
		rel     string
		wantErr bool
		want    string // expected joined path, only checked when !wantErr
	}{
		{"empty rel stays at root", "", false, root},
		{"dot rel stays at root", ".", false, root},
		{"plain file accepted", "photo.jpg", false, filepath.Join(root, "photo.jpg")},
		{"nested path accepted", "sub/dir/file.txt", false, filepath.Join(root, "sub", "dir", "file.txt")},
		{"exact dotdot rejected", "..", true, ""},
		{"leading dotdot rejected", "../x", true, ""},
		{"nested traversal rejected", "a/../../x", true, ""},
		{"deep traversal rejected", "a/../../../etc/passwd", true, ""},
		{"traversal that cancels out accepted", "a/../b", false, filepath.Join(root, "b")},
		{"absolute-looking rel stays confined", "/etc/passwd", false, filepath.Join(root, "etc", "passwd")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := fspath.ConfineTo(root, c.rel)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ConfineTo(%q, %q) = %q, nil; want error", root, c.rel, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConfineTo(%q, %q) unexpected error: %v", root, c.rel, err)
			}
			if got != c.want {
				t.Errorf("ConfineTo(%q, %q) = %q, want %q", root, c.rel, got, c.want)
			}
		})
	}
}
