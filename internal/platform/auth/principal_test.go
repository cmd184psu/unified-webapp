package auth

import (
	"context"
	"testing"
)

func TestWithPrincipalRoundTrip(t *testing.T) {
	want := Principal{Method: "ldap", Subject: "alice"}
	ctx := WithPrincipal(context.Background(), want)

	got, ok := PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("PrincipalFromContext: ok = false, want true")
	}
	if got != want {
		t.Fatalf("PrincipalFromContext = %+v, want %+v", got, want)
	}
}

func TestPrincipalFromContextAbsent(t *testing.T) {
	got, ok := PrincipalFromContext(context.Background())
	if ok {
		t.Fatalf("PrincipalFromContext: ok = true, want false (no principal attached), got %+v", got)
	}
	if got != (Principal{}) {
		t.Fatalf("PrincipalFromContext = %+v, want zero value when absent", got)
	}
}

func TestIdentityGrantOf(t *testing.T) {
	cases := []struct {
		name   string
		grants []string
		want   string
	}{
		{"ldap grant", []string{"ldap"}, "ldap"},
		{"passkey grant", []string{"passkey"}, "passkey"},
		{"admin_pin grant", []string{"admin_pin"}, "admin_pin"},
		{"door-code only", []string{pinGrant("grocery")}, ""},
		{"nil grants", nil, ""},
		{"identity among door codes", []string{pinGrant("grocery"), "ldap"}, "ldap"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := identityGrantOf(c.grants); got != c.want {
				t.Fatalf("identityGrantOf(%v) = %q, want %q", c.grants, got, c.want)
			}
		})
	}
}
