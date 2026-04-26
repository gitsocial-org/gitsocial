// identity.go - Core identity types
package identity

import "strings"

// Identity describes a key/email binding declared by an external authority
// (DNS well-known, forge GPG endpoint, forge commits API).
type Identity struct {
	Key   string `json:"key"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// KeyType returns the algorithm portion of the key (e.g. "ssh-ed25519", "gpg").
func (id *Identity) KeyType() string {
	if strings.HasPrefix(id.Key, "gpg:") {
		return "gpg"
	}
	parts := strings.SplitN(id.Key, " ", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// NormalizeEmail lowercases and trims an email for consistent ref lookup.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
