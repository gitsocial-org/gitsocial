// url.go - Workspace repository URL resolution
package gitmsg

import (
	"sync"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var repoURLCache sync.Map // workdir → string

// ResolveRepoURL returns the normalized repo URL for a workspace,
// falling back to "local:<workdir>" for repos without a remote.
func ResolveRepoURL(workdir string) string {
	if v, ok := repoURLCache.Load(workdir); ok {
		return v.(string)
	}
	repoURL := protocol.NormalizeURL(git.GetOriginURL(workdir))
	if repoURL == "" {
		repoURL = "local:" + workdir
	}
	repoURLCache.Store(workdir, repoURL)
	return repoURL
}
