// identity_test.go - Tests for Identity type and email normalization
package identity

import "testing"

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"alice@example.com", "alice@example.com"},
		{"ALICE@EXAMPLE.COM", "alice@example.com"},
		{"  alice@example.com  ", "alice@example.com"},
		{"Alice@Example.COM", "alice@example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeEmail(tt.in); got != tt.want {
			t.Errorf("NormalizeEmail(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestIdentityKeyType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want string
	}{
		{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5", "ssh-ed25519"},
		{"ssh-rsa AAAAB3NzaC1yc2E", "ssh-rsa"},
		{"ecdsa-sha2-nistp256 ABC", "ecdsa-sha2-nistp256"},
		{"gpg:ABCDEF1234567890", "gpg"},
		{"gpg:1234", "gpg"},
		{"", ""},
		{"justonefield", "justonefield"},
	}
	for _, tt := range tests {
		id := &Identity{Key: tt.key}
		if got := id.KeyType(); got != tt.want {
			t.Errorf("Identity{Key:%q}.KeyType() = %q; want %q", tt.key, got, tt.want)
		}
	}
}
