package auth

import (
	"reflect"
	"testing"
)

func TestPasskeyOriginsDerivedFromRoutedHosts(t *testing.T) {
	hosts := []string{"todo.hero.cmdhome.net", "smb.hero.cmdhome.net", "certmachine.cmdhome.net", "other.example.org", "localhost:8787", "10.0.0.5"}
	got := PasskeyOrigins("cmdhome.net", []string{"https://extra.cmdhome.net"}, hosts)
	want := []string{"https://extra.cmdhome.net", "https://certmachine.cmdhome.net", "https://smb.hero.cmdhome.net", "https://todo.hero.cmdhome.net"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
}

func TestPasskeyOriginsNeverWidenedByNonMatchingHosts(t *testing.T) {
	if got := PasskeyOrigins("cmdhome.net", nil, []string{"evilcmdhome.net", "cmdhome.net.evil.com"}); len(got) != 0 {
		t.Fatalf("origins = %v, want none", got)
	}
	if got := PasskeyOrigins("", nil, []string{"a.cmdhome.net"}); len(got) != 0 {
		t.Fatalf("no rp_id must allow nothing, got %v", got)
	}
}

func TestPasskeyOriginsKeepsExtrasAndDedupes(t *testing.T) {
	got := PasskeyOrigins("cmdhome.net", []string{"https://a.cmdhome.net", "https://a.cmdhome.net"}, []string{"a.cmdhome.net"})
	if !reflect.DeepEqual(got, []string{"https://a.cmdhome.net"}) {
		t.Fatalf("origins = %v", got)
	}
}

func TestSuggestRPIDIsTheSharedParentDomain(t *testing.T) {
	cases := []struct {
		hosts []string
		want  string
	}{
		{[]string{"todo.hero.cmdhome.net", "certmachine.cmdhome.net"}, "cmdhome.net"},
		{[]string{"todo.hero.cmdhome.net", "smb.hero.cmdhome.net"}, "hero.cmdhome.net"},
		{[]string{"a.example.com", "b.example.org"}, ""},
		{[]string{"localhost"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := SuggestRPID(c.hosts); got != c.want {
			t.Errorf("SuggestRPID(%v) = %q, want %q", c.hosts, got, c.want)
		}
	}
}
