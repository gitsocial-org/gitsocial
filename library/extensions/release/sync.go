// sync.go - Release extension sync and commit processing
package release

import (
	"sync"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var lastSyncedTip sync.Map

// SyncWorkspaceToCache synchronizes release commits from the workspace to the cache.
func SyncWorkspaceToCache(workdir string) error {
	branch := gitmsg.GetExtBranch(workdir, "release")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	tip, err := git.ReadRef(workdir, branch)
	if err != nil {
		return nil
	}
	key := workdir + "\x00" + branch
	if prev, ok := lastSyncedTip.Load(key); ok && prev.(string) == tip {
		return nil
	}

	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{
		Branch: branch,
	})
	if err != nil {
		return err
	}

	cacheCommits := make([]cache.Commit, 0, len(commits))
	for _, c := range commits {
		cacheCommits = append(cacheCommits, cache.Commit{
			Hash:        c.Hash,
			RepoURL:     repoURL,
			Branch:      branch,
			AuthorName:  c.Author,
			AuthorEmail: c.Email,
			Message:     c.Message,
			Timestamp:   c.Timestamp,
		})
	}
	if err := cache.InsertCommits(cacheCommits); err != nil {
		return err
	}
	liveHashes := make(map[string]bool, len(commits))
	for _, c := range commits {
		liveHashes[c.Hash] = true
	}
	_, _ = cache.MarkCommitsStale(repoURL, branch, liveHashes)

	for _, gc := range commits {
		msg := protocol.ParseMessage(gc.Message)
		processReleaseCommit(gc, msg, repoURL, branch)
	}
	lastSyncedTip.Store(key, tip)
	return nil
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
}
