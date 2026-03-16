// post.go - Post creation, editing, retraction, and comments
package social

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var lastSyncedTip sync.Map

// SyncWorkspaceToCache synchronizes workspace commits and lists to the cache.
func SyncWorkspaceToCache(workdir string) error {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	socialBranch := gitmsg.GetExtBranch(workdir, "social")
	defaultBranch, _ := git.GetDefaultBranch(workdir)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	// Quick tip check to skip redundant syncs within the same session
	socialTip, _ := git.ReadRef(workdir, socialBranch)
	defaultTip, _ := git.ReadRef(workdir, defaultBranch)
	combinedTip := socialTip + "\x00" + defaultTip
	key := workdir + "\x00" + repoURL
	if prev, ok := lastSyncedTip.Load(key); ok && prev.(string) == combinedTip {
		return nil
	}
	if err := cache.InsertRepository(cache.Repository{
		URL:         repoURL,
		Branch:      "*",
		StoragePath: workdir,
	}); err != nil {
		return fmt.Errorf("insert repository: %w", err)
	}
	// Get all commits across all branches (default limit 10k)
	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{All: true})
	if err != nil {
		return fmt.Errorf("get commits: %w", err)
	}
	cacheCommits := make([]cache.Commit, 0, len(commits))
	for _, gc := range commits {
		branch := fetch.CleanRefname(gc.Refname)
		if branch == "" {
			branch = defaultBranch
		}
		cacheCommits = append(cacheCommits, cache.Commit{
			Hash:        gc.Hash,
			RepoURL:     repoURL,
			Branch:      branch,
			AuthorName:  gc.Author,
			AuthorEmail: gc.Email,
			Message:     gc.Message,
			Timestamp:   gc.Timestamp,
		})
	}
	if err := cache.InsertCommits(cacheCommits); err != nil {
		return fmt.Errorf("insert commits: %w", err)
	}
	if _, err := cache.ReconcileVersions(); err != nil {
		return fmt.Errorf("reconcile versions: %w", err)
	}
	// Batch collect social items, then insert in one transaction
	var socialItems []SocialItem
	var virtualItems []SocialItem
	for _, gc := range commits {
		branch := fetch.CleanRefname(gc.Refname)
		if branch == "" {
			branch = defaultBranch
		}
		msg := protocol.ParseMessage(gc.Message)
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
			socialItems = append(socialItems, SocialItem{
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
					virtualItems = append(virtualItems, *vi)
				}
			}
		} else {
			upgradeVirtualItem(gc, repoURL)
		}
	}
	if err := InsertSocialItems(socialItems); err != nil {
		log.Warn("batch insert social items failed", "error", err)
	}
	for _, vi := range virtualItems {
		if err := InsertSocialItem(vi); err != nil {
			log.Debug("insert virtual social item failed", "hash", vi.Hash, "error", err)
		}
	}
	// Mark stale across all branches
	liveHashes := make(map[string]bool, len(commits))
	for _, c := range commits {
		liveHashes[c.Hash] = true
	}
	_, _ = cache.MarkCommitsStaleByRepo(repoURL, liveHashes)
	syncListsToCache(workdir)
	lastSyncedTip.Store(key, combinedTip)
	return nil
}

// syncListsToCache persists all workspace lists to the cache database.
func syncListsToCache(workdir string) {
	result := GetLists(workdir)
	if !result.Success {
		return
	}
	for _, list := range result.Data {
		syncListToCache(list, workdir)
	}
}

// processWorkspaceCommits parses and inserts workspace commits as social items.
func processWorkspaceCommits(commits []git.Commit, repoURL, branch string) {
	for _, gc := range commits {
		msg := protocol.ParseMessage(gc.Message)

		if msg != nil && msg.Header.Ext == "social" {
			itemType := string(extractPostType(msg))
			// Parse original and reply-to refs to extract repo_url, hash, and branch
			originalRepoURL, originalHash, originalBranch := "", "", ""
			if orig := msg.Header.Fields["original"]; orig != "" {
				normalizedRef := protocol.NormalizeRefWithContext(orig, repoURL, branch)
				parsed := protocol.ParseRef(normalizedRef)
				if parsed.Value != "" {
					originalRepoURL = parsed.Repository
					originalHash = parsed.Value
					originalBranch = parsed.Branch
					if originalBranch == "" {
						originalBranch = branch // default to current branch
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
						replyToBranch = branch // default to current branch
					}
				}
			}

			if err := InsertSocialItem(SocialItem{
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
			}); err != nil {
				log.Warn("insert social item failed", "hash", gc.Hash, "error", err)
			}

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
}

// upgradeVirtualItem converts a virtual item to a real one when fetched.
func upgradeVirtualItem(gc git.Commit, repoURL string) {
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`
			UPDATE core_commits
			SET is_virtual = 0,
				author_name = ?,
				author_email = ?,
				message = ?,
				timestamp = ?
			WHERE repo_url = ? AND hash = ? AND is_virtual = 1`,
			gc.Author, gc.Email, gc.Message, gc.Timestamp.Format(time.RFC3339),
			repoURL, gc.Hash)
		return err
	}); err != nil {
		log.Debug("upgrade virtual item failed", "hash", gc.Hash, "error", err)
	}
}

// GetPosts retrieves posts based on scope (timeline, repository, list, etc.).
func GetPosts(workdir string, scope string, opts *GetPostsOptions) Result[[]Post] {
	if opts == nil {
		opts = &GetPostsOptions{}
	}

	workspaceURL := gitmsg.ResolveRepoURL(workdir)

	switch {
	case scope == "timeline":
		return getTimeline(workdir, workspaceURL, opts)
	case scope == "repository:my":
		return getMyPosts(workdir, workspaceURL, opts)
	case scope == "repository:workspace":
		return getWorkspaceRepository(workdir, workspaceURL, opts)
	case strings.HasPrefix(scope, "repository:"):
		rest := strings.TrimPrefix(scope, "repository:")
		repoURL := rest
		branch := ""
		if idx := strings.Index(rest, "@"); idx != -1 {
			repoURL = rest[:idx]
			branch = rest[idx+1:]
		}
		return getRepositoryPosts(repoURL, branch, workspaceURL, opts)
	case strings.HasPrefix(scope, "list:"):
		listID := strings.TrimPrefix(scope, "list:")
		return getListPosts(listID, workspaceURL, opts)
	case strings.HasPrefix(scope, "post:"):
		postID := strings.TrimPrefix(scope, "post:")
		return getSinglePost(postID, workspaceURL)
	case strings.HasPrefix(scope, "thread:"):
		postID := strings.TrimPrefix(scope, "thread:")
		return getThreadPosts(workdir, postID, workspaceURL)
	default:
		return Failure[[]Post]("INVALID_SCOPE", "Unknown scope: "+scope)
	}
}

type GetPostsOptions struct {
	Types           []PostType
	Since           *time.Time
	Until           *time.Time
	Limit           int
	Cursor          string // RFC3339 timestamp for keyset pagination (items older than this)
	IncludeImplicit bool
	SkipCache       bool
	SortBy          string
}

// CreatePostOptions configures post creation.
type CreatePostOptions struct {
	Origin *protocol.Origin
}

// CreatePost creates a new post as a git commit in the workspace.
func CreatePost(workdir, content string, opts *CreatePostOptions) Result[Post] {
	if strings.TrimSpace(content) == "" {
		return Failure[Post]("EMPTY_CONTENT", "Post content cannot be empty")
	}

	branch := gitmsg.GetExtBranch(workdir, "social")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	message := content
	if opts != nil && opts.Origin != nil {
		fields := map[string]string{"type": "post"}
		protocol.ApplyOrigin(fields, opts.Origin)
		header := protocol.Header{Ext: "social", V: "0.1.0", Fields: fields, FieldOrder: socialFieldOrder}
		message = protocol.FormatMessage(content, header, nil)
	}

	hash, err := git.CreateCommitOnBranch(workdir, branch, message)
	if err != nil {
		return FailureWithDetails[Post]("COMMIT_ERROR", "Failed to create commit", err)
	}

	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return FailureWithDetails[Post]("COMMIT_ERROR", "Failed to get commit", err)
	}
	authorName := ""
	authorEmail := ""
	if commit != nil {
		authorName = commit.Author
		authorEmail = commit.Email
	}

	unpushed, _ := git.GetUnpushedCommits(workdir, branch)
	_, isUnpushed := unpushed[hash[:12]]

	now := time.Now()
	post := Post{
		ID:         protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
		Repository: repoURL,
		Branch:     branch,
		Author: Author{
			Name:  authorName,
			Email: authorEmail,
		},
		Timestamp:       now,
		Content:         content,
		Type:            PostTypePost,
		Source:          PostSourceExplicit,
		CleanContent:    content,
		IsWorkspacePost: true,
		Display: Display{
			CommitHash:      hash,
			IsWorkspacePost: true,
			IsUnpushed:      isUnpushed,
		},
	}

	// Insert into commits and social_items
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      branch,
		AuthorName:  authorName,
		AuthorEmail: authorEmail,
		Message:     message,
		Timestamp:   now,
	}}); err != nil {
		log.Warn("insert commits after post creation failed", "hash", hash, "error", err)
	}
	if err := InsertSocialItem(SocialItem{
		RepoURL: repoURL,
		Hash:    hash,
		Type:    "post",
	}); err != nil {
		log.Warn("insert social item after post creation failed", "hash", hash, "error", err)
	}

	return Success(post)
}

// getTimeline retrieves posts from all subscribed lists and workspace.
func getTimeline(workdir string, workspaceURL string, opts *GetPostsOptions) Result[[]Post] {
	gitRoot, err := git.GetRootDir(workdir)
	if err != nil || gitRoot == "" {
		gitRoot = workdir
	}

	// Get unpushed commits for workspace posts
	branch := gitmsg.GetExtBranch(workdir, "social")
	unpushed, _ := git.GetUnpushedCommits(workdir, branch)

	listIDs, _ := cache.GetListIDs(gitRoot)
	items, err := GetTimeline(listIDs, workspaceURL, opts.Limit, opts.Cursor)
	if err != nil {
		return FailureWithDetails[[]Post]("CACHE_ERROR", "Failed to get timeline", err)
	}

	posts := make([]Post, 0, len(items))
	for _, item := range items {
		post := SocialItemToPost(item)
		if item.RepoURL == workspaceURL {
			post.Display.IsWorkspacePost = true
			_, post.Display.IsUnpushed = unpushed[item.Hash]
		}
		posts = append(posts, post)
	}

	return Success(posts)
}

// CountTimeline returns the total number of timeline posts for the workspace.
func CountTimeline(workdir string) int {
	gitRoot, err := git.GetRootDir(workdir)
	if err != nil || gitRoot == "" {
		gitRoot = workdir
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	listIDs, _ := cache.GetListIDs(gitRoot)
	count, _ := GetTimelineCount(listIDs, workspaceURL)
	return count
}

// CountRepository returns the total number of posts for a repository scope.
func CountRepository(workdir, repoURL, branch string, isWorkspace bool) int {
	if isWorkspace {
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		count, _ := GetAllItemsCount(SocialQuery{RepoURL: workspaceURL})
		return count
	}
	count, _ := GetAllItemsCount(SocialQuery{RepoURL: repoURL, Branch: branch})
	return count
}

// CountListPosts returns the total number of posts in a list.
func CountListPosts(listID string) int {
	count, _ := GetTimelineCount([]string{listID}, "")
	return count
}

// getMyPosts retrieves posts from the current workspace repository.
func getMyPosts(workdir string, workspaceURL string, opts *GetPostsOptions) Result[[]Post] {
	if err := SyncWorkspaceToCache(workdir); err != nil {
		log.Warn("sync workspace to cache failed", "error", err)
	}
	branch := gitmsg.GetExtBranch(workdir, "social")
	unpushed, _ := git.GetUnpushedCommits(workdir, branch)

	items, err := GetAllItems(SocialQuery{
		RepoURL:          workspaceURL,
		Limit:            opts.Limit,
		Cursor:           opts.Cursor,
		Since:            opts.Since,
		Until:            opts.Until,
		ForFollowerCheck: workspaceURL,
	})
	if err != nil {
		return FailureWithDetails[[]Post]("CACHE_ERROR", "Failed to get posts", err)
	}

	posts := make([]Post, 0, len(items))
	for _, item := range items {
		post := SocialItemToPost(item)
		_, post.Display.IsUnpushed = unpushed[item.Hash]
		post.Display.IsWorkspacePost = true
		posts = append(posts, post)
	}

	return Success(posts)
}

// getRepositoryPosts retrieves posts from a specific external repository.
func getRepositoryPosts(repoURL, branch, workspaceURL string, opts *GetPostsOptions) Result[[]Post] {
	items, err := GetAllItems(SocialQuery{
		RepoURL:          repoURL,
		Branch:           branch,
		Limit:            opts.Limit,
		Cursor:           opts.Cursor,
		Since:            opts.Since,
		Until:            opts.Until,
		ForFollowerCheck: workspaceURL,
	})
	if err != nil {
		return FailureWithDetails[[]Post]("CACHE_ERROR", "Failed to get posts", err)
	}

	posts := make([]Post, 0, len(items))
	for _, item := range items {
		post := SocialItemToPost(item)
		if item.RepoURL == workspaceURL {
			post.Display.IsWorkspacePost = true
		}
		posts = append(posts, post)
	}

	return Success(posts)
}

// getWorkspaceRepository retrieves all posts from the workspace repository.
func getWorkspaceRepository(workdir string, workspaceURL string, opts *GetPostsOptions) Result[[]Post] {
	if err := SyncWorkspaceToCache(workdir); err != nil {
		log.Warn("sync workspace to cache failed", "error", err)
	}
	branch := gitmsg.GetExtBranch(workdir, "social")
	unpushed, _ := git.GetUnpushedCommits(workdir, branch)

	items, err := GetAllItems(SocialQuery{
		RepoURL:          workspaceURL,
		Limit:            opts.Limit,
		Cursor:           opts.Cursor,
		ForFollowerCheck: workspaceURL,
	})
	if err != nil {
		return FailureWithDetails[[]Post]("CACHE_ERROR", "Failed to get posts", err)
	}

	posts := make([]Post, 0, len(items))
	for _, item := range items {
		post := SocialItemToPost(item)
		_, post.Display.IsUnpushed = unpushed[item.Hash]
		post.Display.IsWorkspacePost = true
		posts = append(posts, post)
	}

	return Success(posts)
}

// getListPosts retrieves posts from repositories in a specific list.
func getListPosts(listID string, _ string, opts *GetPostsOptions) Result[[]Post] {
	limit := 0
	cursor := ""
	if opts != nil {
		limit = opts.Limit
		cursor = opts.Cursor
	}
	// Don't include workspace posts - only show posts from repos in the list
	items, err := GetTimeline([]string{listID}, "", limit, cursor)
	if err != nil {
		return FailureWithDetails[[]Post]("CACHE_ERROR", "Failed to get posts", err)
	}

	posts := make([]Post, 0, len(items))
	for _, item := range items {
		posts = append(posts, SocialItemToPost(item))
	}

	return Success(posts)
}

// getSinglePost retrieves a single post by its ID.
func getSinglePost(postID string, workspaceURL string) Result[[]Post] {
	postID = cache.ResolveRefToCanonical(postID)
	item, err := GetSocialItemByRef(postID, workspaceURL)
	if err != nil {
		return FailureWithDetails[[]Post]("CACHE_ERROR", "Failed to get post", err)
	}

	if item == nil {
		return Success([]Post{})
	}
	post := SocialItemToPost(*item)
	if item.RepoURL == workspaceURL {
		post.Display.IsWorkspacePost = true
	}
	return Success([]Post{post})
}

// getThreadPosts retrieves a post and all its replies as a thread.
func getThreadPosts(workdir, postID string, workspaceURL string) Result[[]Post] {
	canonicalPostID := cache.ResolveRefToCanonical(postID)
	parsed := protocol.ParseRef(canonicalPostID)
	if parsed.Value == "" {
		return Failure[[]Post]("INVALID_REF", "Invalid post ID: "+postID)
	}

	branch := parsed.Branch
	if branch == "" {
		branch = "main"
	}

	// Get unpushed commits for workspace posts
	workspaceBranch := gitmsg.GetExtBranch(workdir, "social")
	unpushed, _ := git.GetUnpushedCommits(workdir, workspaceBranch)

	items, err := GetThread(parsed.Repository, parsed.Value, branch, workspaceURL)
	if err != nil {
		return FailureWithDetails[[]Post]("CACHE_ERROR", "Failed to get thread", err)
	}

	parentItems := getParentChain(parsed.Repository, parsed.Value, branch, workspaceURL)

	posts := make([]Post, 0, len(items))
	var rootPost Post
	for _, item := range items {
		p := SocialItemToPost(item)
		if item.RepoURL == workspaceURL {
			p.Display.IsWorkspacePost = true
			_, p.Display.IsUnpushed = unpushed[item.Hash]
		}
		if p.ID == canonicalPostID {
			p.Depth = 0
			rootPost = p
		}
		posts = append(posts, p)
	}

	// If root post not found in thread results, fetch it directly
	// GetSocialItem uses social_items_resolved view (LEFT JOINs social_items)
	// so it works even for posts without social_items records
	if rootPost.ID == "" {
		item, err := GetSocialItem(parsed.Repository, parsed.Value, branch, workspaceURL)
		if err == nil && item != nil {
			rootPost = SocialItemToPost(*item)
			rootPost.Depth = 0
			if item.RepoURL == workspaceURL {
				rootPost.Display.IsWorkspacePost = true
				_, rootPost.Display.IsUnpushed = unpushed[item.Hash]
			}
		}
	}

	sorted := SortThreadTree(canonicalPostID, posts)

	result := make([]Post, 0, len(parentItems)+len(sorted)+1)
	for _, item := range parentItems {
		p := SocialItemToPost(item)
		if item.RepoURL == workspaceURL {
			p.Display.IsWorkspacePost = true
			_, p.Display.IsUnpushed = unpushed[item.Hash]
		}
		result = append(result, p)
	}
	if rootPost.ID != "" {
		result = append(result, rootPost)
	}
	result = append(result, sorted...)

	return Success(result)
}

// getParentChain retrieves ancestor posts for building thread context.
func getParentChain(repoURL, hash, branch, workspaceURL string) []SocialItem {
	parents, err := GetParentChain(repoURL, hash, branch, workspaceURL)
	if err != nil {
		return nil
	}
	return parents
}
