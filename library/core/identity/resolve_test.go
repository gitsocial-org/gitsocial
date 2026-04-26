// resolve_test.go - Tests for the mail-subdomain heuristic and DNS resolver
package identity

import "testing"

func TestMailParentDomain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		// Recognized prefixes with a multi-label parent → walk up
		{"mail.example.com", "example.com"},
		{"email.example.com", "example.com"},
		{"smtp.example.com", "example.com"},
		{"imap.example.com", "example.com"},
		{"pop.example.com", "example.com"},
		{"mx.example.com", "example.com"},
		{"mail.deeper.example.com", "deeper.example.com"},
		// Recognized prefix but parent has no dot → no fallback
		{"mail.com", ""},
		{"mx.io", ""},
		// No recognized prefix
		{"example.com", ""},
		{"plain.example.com", ""},
		{"webmail.example.com", ""}, // "webmail" is not in the prefix list
		// Empty
		{"", ""},
	}
	for _, tt := range tests {
		if got := mailParentDomain(tt.in); got != tt.want {
			t.Errorf("mailParentDomain(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}
