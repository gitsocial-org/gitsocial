// hash.go - Hash generation for message content and refs
package protocol

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidHash = errors.New("invalid commit hash format")
	hashPattern    = regexp.MustCompile(`^[a-f0-9]+$`)
	hash12Pattern  = regexp.MustCompile(`^[a-f0-9]{12}$`)
)

// NormalizeHash normalizes a commit hash to lowercase 12 chars.
func NormalizeHash(hash string) (string, error) {
	if hash == "" || !hashPattern.MatchString(strings.ToLower(hash)) {
		return "", ErrInvalidHash
	}
	normalized := strings.ToLower(hash)
	if len(normalized) > 12 {
		normalized = normalized[:12]
	}
	return normalized, nil
}

// ValidateHash checks if a hash is a valid 12-char hex string.
func ValidateHash(hash string) bool {
	return hash != "" && hash12Pattern.MatchString(strings.ToLower(hash))
}
