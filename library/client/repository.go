// repository.go - The repository-removal sequence every thin client runs.
package client

import (
	"os"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// RemoveRepository deletes a repository's cached rows and its storage, then
// recounts the interactions its items carried on posts that stay.
func RemoveRepository(repoURL, storagePath string) error {
	var firstErr error
	if repoURL != "" {
		if err := cache.DeleteRepository(repoURL); err != nil {
			firstErr = err
		}
	}
	if storagePath != "" {
		if err := os.RemoveAll(storagePath); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := social.RecountAllInteractions(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
