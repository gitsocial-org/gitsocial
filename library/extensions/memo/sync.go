// sync.go - Memo extension cache sync and fetch processing
package memo

import (
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

var lastSyncedTip sync.Map

func init() {
	fetch.RegisterProcessor("memo", func(commits []git.Commit, _, repoURL, extBranch, _ string) {
		ProcessWorkspaceBatch(commits, repoURL, extBranch)
	})
}

// Processors returns the commit processors for the memo extension. Wired into
// the followed-repo fetch path so inherited/external memos populate memo_items
// (the workspace WorkspaceSyncFunc only covers the workspace itself).
func Processors() []fetch.CommitProcessor {
	return []fetch.CommitProcessor{processMemoCommit}
}

// SyncWorkspaceToCache replays memo commits from the workspace's gitmsg/memo
// branch into the cache. Skips work when the tip hasn't moved since last sync.
func SyncWorkspaceToCache(workdir string) error {
	branch := MemoBranch
	repoURL := gitmsg.ResolveRepoURL(workdir)

	tip, err := git.ReadRef(workdir, branch)
	if err != nil {
		return nil
	}
	key := workdir + "\x00" + branch
	if prev, ok := lastSyncedTip.Load(key); ok && prev.(string) == tip {
		return nil
	}
	if persisted, err := cache.GetSyncTip(key); err == nil && persisted == tip {
		lastSyncedTip.Store(key, tip)
		return nil
	}

	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{Branch: branch})
	if err != nil {
		return err
	}
	if err := indexCommits(repoURL, branch, commits); err != nil {
		return err
	}
	lastSyncedTip.Store(key, tip)
	_ = cache.SetSyncTip(key, tip)
	return nil
}

// SyncTierRepoToCache reads gitmsg/memo from a bare tier repo (personal or
// session) and indexes its commits under repo_url = `local:<path>`.
func SyncTierRepoToCache(repoPath string) error {
	if repoPath == "" || !git.BareRepoExists(repoPath) {
		return nil
	}
	repoURL := LocalRepoURL(repoPath)
	commits, err := git.GetCommits(repoPath, &git.GetCommitsOptions{Branch: MemoBranch})
	if err != nil {
		// Branch may not exist yet on a freshly-init'd repo.
		return nil
	}
	return indexCommits(repoURL, MemoBranch, commits)
}

// SyncAllTierReposToCache syncs the workspace plus the personal repo and
// every session repo recorded under this workspace (legacy untagged sessions
// also sync, since their visibility is workspace-wide). Missing repos are
// silently skipped.
func SyncAllTierReposToCache(workdir string) error {
	if err := SyncWorkspaceToCache(workdir); err != nil {
		return err
	}
	if path, err := settings.PersonalRepoPath(); err == nil && git.BareRepoExists(path) {
		if err := SyncTierRepoToCache(path); err != nil {
			log.Debug("memo personal sync failed", "path", path, "error", err)
		}
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	for _, p := range listSessionReposForWorkspace(workspaceURL) {
		if err := SyncTierRepoToCache(p); err != nil {
			log.Debug("memo session sync failed", "path", p, "error", err)
		}
	}
	return nil
}

// ProcessWorkspaceBatch processes pre-fetched commits for memo extension items.
func ProcessWorkspaceBatch(commits []git.Commit, repoURL, branch string) {
	for _, gc := range commits {
		if fetch.CleanRefname(gc.Refname) != branch {
			continue
		}
		msg := protocol.ParseMessage(gc.Message)
		processMemoCommit(gc, msg, repoURL, branch)
	}
}

// indexCommits inserts cache rows and runs version + extension processing for
// every commit in the given list.
func indexCommits(repoURL, branch string, commits []git.Commit) error {
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
	live := make(map[string]bool, len(commits))
	for _, c := range commits {
		live[c.Hash] = true
	}
	_, _ = cache.MarkCommitsStale(repoURL, branch, live)

	for _, gc := range commits {
		msg := protocol.ParseMessage(gc.Message)
		processMemoCommit(gc, msg, repoURL, branch)
	}
	return nil
}

// processMemoCommit handles a single commit for memo extension processing.
func processMemoCommit(gc git.Commit, msg *protocol.Message, repoURL, branch string) {
	if msg == nil || msg.Header.Ext != "memo" {
		return
	}
	cache.ProcessVersionFromHeader(msg, gc.Hash, repoURL, branch)
	if err := InsertMemoItem(MemoItem{RepoURL: repoURL, Hash: gc.Hash, Branch: branch, Type: "memo"}); err != nil {
		log.Debug("insert memo item failed", "hash", gc.Hash, "error", err)
	}
	cache.SyncEditExtensionFields([]cache.EditKey{{RepoURL: repoURL, Hash: gc.Hash, Branch: branch}})
}
