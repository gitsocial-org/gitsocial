// sync.go - Release extension sync and commit processing
package release

import (
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// SyncWorkspaceBatch ingests pre-fetched workspace commits from the release branch.
func SyncWorkspaceBatch(commits []git.Commit, workdir, repoURL, _ string) {
	ProcessWorkspaceBatch(commits, repoURL, gitmsg.GetExtBranch(workdir, "release"))
}

// isReleaseEdit reports whether a parsed message is a release edit commit.
func isReleaseEdit(msg *protocol.Message) bool {
	return msg != nil && msg.Header.Ext == "release" && msg.Header.Fields["edits"] != ""
}

// ProcessWorkspaceBatch processes pre-fetched commits for release extension items.
// Used by the unified workspace sync to avoid redundant git log calls.
func ProcessWorkspaceBatch(commits []git.Commit, repoURL, branch string) {
	var editKeys []cache.EditKey
	for _, gc := range commits {
		if fetch.CleanRefname(gc.Refname) != branch {
			continue
		}
		msg := protocol.ParseMessage(gc.Message)
		processReleaseCommit(gc, msg, repoURL, branch)
		if isReleaseEdit(msg) {
			editKeys = append(editKeys, cache.EditKey{RepoURL: repoURL, Hash: gc.Hash, Branch: branch})
		}
	}
	// Re-propagate after the batch so processing order cannot leave a
	// canonical with its raw, pre-edit fields.
	cache.SyncEditExtensionFields(editKeys)
}

// processReleaseCommit handles a single commit for release extension processing.
func processReleaseCommit(gc git.Commit, msg *protocol.Message, repoURL, branch string) {
	if msg == nil || msg.Header.Ext != "release" {
		return
	}

	cache.ProcessVersionFromHeader(msg, gc.Hash, repoURL, branch)

	prerelease := msg.Header.Fields["prerelease"] == "true"

	item := ReleaseItem{
		RepoURL:     repoURL,
		Hash:        gc.Hash,
		Branch:      branch,
		Tag:         cache.ToNullString(msg.Header.Fields["tag"]),
		Version:     cache.ToNullString(msg.Header.Fields["version"]),
		Prerelease:  prerelease,
		Artifacts:   cache.ToNullString(msg.Header.Fields["artifacts"]),
		ArtifactURL: cache.ToNullString(msg.Header.Fields["artifact-url"]),
		Checksums:   cache.ToNullString(msg.Header.Fields["checksums"]),
		SignedBy:    cache.ToNullString(msg.Header.Fields["signed-by"]),
		SBOM:        cache.ToNullString(msg.Header.Fields["sbom"]),
	}

	if err := InsertReleaseItem(item); err != nil {
		log.Debug("insert release item failed", "hash", gc.Hash, "error", err)
	}

	// Propagate mutable fields from edit to canonical
	cache.SyncEditExtensionFields([]cache.EditKey{{RepoURL: repoURL, Hash: gc.Hash, Branch: branch}})
}
