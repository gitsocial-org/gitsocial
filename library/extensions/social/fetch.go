// fetch.go - Social extension fetch wrappers over core/fetch
package social

import (
	"database/sql"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/storage"
)

// FetchStats contains social fetch operation statistics.
type FetchStats struct {
	Repositories int
	Posts        int
	Errors       []FetchError
}

// FetchError records a per-repository social fetch failure.
type FetchError struct {
	Repository string
	Error      string
}

// FetchOptions controls social fetch behavior.
type FetchOptions struct {
	ListID           string
	Since            string
	Before           string
	Parallel         int
	FetchAllBranches bool
	ExtraProcessors  []fetch.CommitProcessor
	ExtraHooks       []fetch.PostFetchHook
	OnProgress       func(repoURL string, processed, total int)
}

// Fetch retrieves updates from all subscribed repositories and syncs to cache.
func Fetch(workdir, cacheDir string, opts *FetchOptions) Result[FetchStats] {
	if opts == nil {
		opts = &FetchOptions{}
	}

	gitRoot, err := git.GetRootDir(workdir)
	if err != nil || gitRoot == "" {
		gitRoot = workdir
	}

	result := GetLists(workdir)
	if !result.Success {
		return Failure[FetchStats](result.Error.Code, result.Error.Message)
	}

	var repos []fetch.RepoInfo
	for _, list := range result.Data {
		if opts.ListID != "" && list.ID != opts.ListID {
			continue
		}

		syncListToCache(list, gitRoot)

		for _, repoRef := range list.Repositories {
			id := protocol.ParseRepositoryID(repoRef)
			if id.Repository == "" {
				continue
			}
			repos = append(repos, fetch.RepoInfo{
				URL:    id.Repository,
				Branch: id.Branch,
				ListID: list.ID,
			})
		}
	}

	coreOpts := &fetch.Options{
		WorkspaceBranch:  gitmsg.GetExtBranch(workdir, "social"),
		Since:            opts.Since,
		Before:           opts.Before,
		Parallel:         opts.Parallel,
		FetchAllBranches: opts.FetchAllBranches,
		OnProgress:       opts.OnProgress,
	}

	processors := socialProcessors()
	if len(opts.ExtraProcessors) > 0 {
		processors = append(processors, opts.ExtraProcessors...)
	}
	hooks := socialHooks()
	if len(opts.ExtraHooks) > 0 {
		hooks = append(hooks, opts.ExtraHooks...)
	}
	coreResult := fetch.FetchAll(workdir, cacheDir, coreOpts, repos, processors, hooks)
	return convertResult(coreResult)
}

// FetchRepositoryRange fetches a repository with explicit date range (for "load more" pagination).
func FetchRepositoryRange(cacheDir, repoURL, branch, since, before, workspaceURL string) Result[FetchStats] {
	coreResult := fetch.FetchRepositoryRange(cacheDir, repoURL, branch, since, before, workspaceURL, socialProcessors(), socialHooks())
	return convertResult(coreResult)
}

// FetchRepository fetches complete history for a repository.
func FetchRepository(cacheDir, repoURL, branch, workspaceURL string, extraProcessors ...fetch.CommitProcessor) Result[FetchStats] {
	processors := socialProcessors()
	if len(extraProcessors) > 0 {
		processors = append(processors, extraProcessors...)
	}
	coreResult := fetch.FetchRepository(cacheDir, repoURL, branch, workspaceURL, processors, socialHooks())
	return convertResult(coreResult)
}

// CacheExternalRepoLists fetches and caches lists defined by an external repository.
func CacheExternalRepoLists(cacheDir, repoURL, branch string) {
	storageDir, err := storage.EnsureRepository(cacheDir, repoURL, branch, nil)
	if err != nil {
		return
	}
	cacheExternalRepoLists(storageDir, repoURL, "", "")
}

// socialProcessors returns the commit processors for the social extension.
func socialProcessors() []fetch.CommitProcessor {
	return []fetch.CommitProcessor{processSocialCommit}
}

// socialHooks returns the post-fetch hooks for the social extension.
func socialHooks() []fetch.PostFetchHook {
	return []fetch.PostFetchHook{fetchSocialListRefs, checkIfRepoFollowsWorkspace, cacheExternalRepoLists}
}

// convertResult maps core fetch.Stats to social FetchStats.
func convertResult(r fetch.Result) Result[FetchStats] {
	if !r.Success {
		return Failure[FetchStats](r.Error.Code, r.Error.Message)
	}
	errors := make([]FetchError, 0, len(r.Data.Errors))
	for _, e := range r.Data.Errors {
		errors = append(errors, FetchError{Repository: e.Repository, Error: e.Error})
	}
	return Success(FetchStats{
		Repositories: r.Data.Repositories,
		Posts:        r.Data.Items,
		Errors:       errors,
	})
}

// processSocialCommit handles social-specific commit processing.
func processSocialCommit(gc git.Commit, msg *protocol.Message, repoURL, branch string) {
	if msg != nil && msg.Header.Ext == "social" {
		itemType := string(extractPostType(msg))
		originalRepoURL, originalHash, originalBranch := "", "", ""
		if orig := msg.Header.Fields["original"]; orig != "" {
			normalizedRef := protocol.NormalizeRefWithContext(orig, repoURL, branch)
			parsed := protocol.ParseRef(normalizedRef)
			if parsed.Value != "" {
				originalRepoURL = parsed.Repository
				originalHash = parsed.Value
				originalBranch = parsed.Branch
				if originalBranch == "" {
					originalBranch = branch
				}
			}
		}
		replyToRepoURL, replyToHash, replyToBranch := "", "", ""
		if replyTo := msg.Header.Fields["reply-to"]; replyTo != "" {
			normalizedRef := protocol.NormalizeRefWithContext(replyTo, repoURL, branch)
			parsed := protocol.ParseRef(normalizedRef)
			if parsed.Value != "" {
				replyToRepoURL = parsed.Repository
				replyToHash = parsed.Value
				replyToBranch = parsed.Branch
				if replyToBranch == "" {
					replyToBranch = branch
				}
			}
		}

		_ = InsertSocialItem(SocialItem{
			RepoURL:         repoURL,
			Hash:            gc.Hash,
			Branch:          branch,
			Type:            itemType,
			OriginalRepoURL: sql.NullString{String: originalRepoURL, Valid: originalRepoURL != ""},
			OriginalHash:    sql.NullString{String: originalHash, Valid: originalHash != ""},
			OriginalBranch:  sql.NullString{String: originalBranch, Valid: originalBranch != ""},
			ReplyToRepoURL:  sql.NullString{String: replyToRepoURL, Valid: replyToRepoURL != ""},
			ReplyToHash:     sql.NullString{String: replyToHash, Valid: replyToHash != ""},
			ReplyToBranch:   sql.NullString{String: replyToBranch, Valid: replyToBranch != ""},
		})

		for _, ref := range msg.References {
			if vi := CreateVirtualSocialItem(ref, repoURL, branch); vi != nil {
				if err := InsertSocialItem(*vi); err != nil {
					log.Debug("insert virtual item failed", "ref", ref, "error", err)
				}
			}
		}
	} else {
		upgradeVirtualItem(gc, repoURL)
	}
}

// extractPostType determines the post type from a protocol message header.
func extractPostType(msg *protocol.Message) PostType {
	if t, ok := msg.Header.Fields["type"]; ok {
		switch t {
		case "comment":
			return PostTypeComment
		case "repost":
			return PostTypeRepost
		case "quote":
			return PostTypeQuote
		}
	}
	return PostTypePost
}

// syncListToCache persists a list and its repositories to the cache database.
func syncListToCache(list List, workdir string) {
	repos := make([]cache.ListRepository, 0, len(list.Repositories))
	for _, repoRef := range list.Repositories {
		id := protocol.ParseRepositoryID(repoRef)
		repos = append(repos, cache.ListRepository{
			ListID:  list.ID,
			RepoURL: id.Repository,
			Branch:  id.Branch,
			AddedAt: time.Now(),
		})
	}

	if err := cache.InsertList(cache.CachedList{
		ID:           list.ID,
		Name:         list.Name,
		Version:      list.Version,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		Workdir:      workdir,
		Repositories: repos,
	}); err != nil {
		log.Warn("insert list to cache failed", "list", list.ID, "error", err)
	}
}

// fetchSocialListRefs is a no-op: storage.FetchRepository now fetches all refs/gitmsg/* refs.
func fetchSocialListRefs(_, _, _, _ string) {}

// checkIfRepoFollowsWorkspace detects if a remote repo has the workspace in its lists.
func checkIfRepoFollowsWorkspace(storageDir, repoURL, _, workspaceURL string) {
	if workspaceURL == "" {
		return
	}

	lists, err := gitmsg.EnumerateLists(storageDir, socialExtension)
	if err != nil || len(lists) == 0 {
		return
	}

	for _, listName := range lists {
		data, err := gitmsg.ReadList(storageDir, socialExtension, listName)
		if err != nil || data == nil {
			continue
		}

		for _, repoRef := range data.Repositories {
			id := protocol.ParseRepositoryID(repoRef)
			if id.Repository == workspaceURL {
				followedAt, commitHash, found := gitmsg.FindListAdditionTime(storageDir, socialExtension, listName, workspaceURL)
				if !found {
					followedAt = time.Now()
				}
				if err := InsertFollower(repoURL, workspaceURL, listName, commitHash, followedAt); err != nil {
					log.Debug("insert follower failed", "repo", repoURL, "workspace", workspaceURL, "error", err)
				}
				return
			}
		}
	}
}

// cacheExternalRepoLists stores lists defined by an external repository in the cache.
func cacheExternalRepoLists(storageDir, repoURL, _, _ string) {
	lists, err := gitmsg.EnumerateLists(storageDir, socialExtension)
	if err != nil || len(lists) == 0 {
		return
	}

	for _, listName := range lists {
		data, err := gitmsg.ReadList(storageDir, socialExtension, listName)
		if err != nil || data == nil {
			continue
		}

		ref := "refs/gitmsg/" + socialExtension + "/lists/" + listName
		commitHash, _ := git.ReadRef(storageDir, ref)

		repos := make([]cache.ListRepository, 0, len(data.Repositories))
		for _, repoRef := range data.Repositories {
			id := protocol.ParseRepositoryID(repoRef)
			repos = append(repos, cache.ListRepository{
				ListID:  data.ID,
				RepoURL: id.Repository,
				Branch:  id.Branch,
			})
		}

		if err := cache.InsertExternalRepoList(cache.ExternalRepoList{
			RepoURL:      repoURL,
			ListID:       data.ID,
			Name:         data.Name,
			Version:      data.Version,
			CommitHash:   commitHash,
			CachedAt:     time.Now(),
			Repositories: repos,
		}); err != nil {
			log.Debug("cache external repo list failed", "repo", repoURL, "list", data.ID, "error", err)
		}
	}
}
