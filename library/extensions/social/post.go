// post.go - Post creation, editing, retraction, and comments
package social

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/identity"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// SyncWorkspaceBatch ingests pre-fetched workspace commits and the workspace lists.
func SyncWorkspaceBatch(commits []git.Commit, workdir, repoURL, defaultBranch string) {
	processWorkspaceBatch(commits, repoURL, defaultBranch)
	syncListsToCache(workdir)
}

// processWorkspaceBatch ingests pre-fetched workspace commits as social items.
func processWorkspaceBatch(commits []git.Commit, repoURL, defaultBranch string) {
	var socialItems []SocialItem
	var virtualItems []SocialItem
	for _, gc := range commits {
		branch := fetch.CleanRefname(gc.Refname)
		if branch == "" {
			branch = defaultBranch
		}
		msg := protocol.ParseMessage(gc.Message)
		if msg != nil && msg.Header.Ext == "social" {
			socialItems = append(socialItems, buildSocialItem(gc, msg, repoURL, branch))
			for _, ref := range msg.References {
				if vi := createVirtualSocialItem(ref, repoURL, branch); vi != nil {
					virtualItems = append(virtualItems, *vi)
				}
			}
		} else {
			upgradeVirtualItem(gc, repoURL)
		}
	}
	if err := insertSocialItems(socialItems); err != nil {
		log.Warn("batch insert social items failed", "error", err)
	}
	for _, vi := range virtualItems {
		if err := InsertSocialItem(vi); err != nil {
			log.Debug("insert virtual social item failed", "hash", vi.Hash, "error", err)
		}
	}
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

// buildSocialItem constructs a SocialItem from a parsed commit and message.
func buildSocialItem(gc git.Commit, msg *protocol.Message, repoURL, branch string) SocialItem {
	originalRepoURL, originalHash, originalBranch := parseSocialRefField(msg.Header.Fields["original"], repoURL, branch)
	replyToRepoURL, replyToHash, replyToBranch := parseSocialRefField(msg.Header.Fields["reply-to"], repoURL, branch)
	return SocialItem{
		RepoURL:         repoURL,
		Hash:            gc.Hash,
		Branch:          branch,
		Type:            string(getPostType(msg)),
		OriginalRepoURL: cache.ToNullString(originalRepoURL),
		OriginalHash:    cache.ToNullString(originalHash),
		OriginalBranch:  cache.ToNullString(originalBranch),
		ReplyToRepoURL:  cache.ToNullString(replyToRepoURL),
		ReplyToHash:     cache.ToNullString(replyToHash),
		ReplyToBranch:   cache.ToNullString(replyToBranch),
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

	var result Result[[]Post]
	switch {
	case scope == "timeline":
		result = getTimelinePosts(workdir, workspaceURL, opts)
	case scope == "repository:my", scope == "repository:workspace":
		result = getWorkspacePosts(workdir, workspaceURL, opts)
	case strings.HasPrefix(scope, "repository:"):
		rest := strings.TrimPrefix(scope, "repository:")
		repoURL := rest
		branch := ""
		if idx := strings.Index(rest, "@"); idx != -1 {
			repoURL = rest[:idx]
			branch = rest[idx+1:]
		}
		result = getRepositoryPosts(repoURL, branch, workspaceURL, opts)
	case strings.HasPrefix(scope, "list:"):
		listID := strings.TrimPrefix(scope, "list:")
		result = getListPosts(listID, workspaceURL, opts)
	case strings.HasPrefix(scope, "post:"):
		postID := strings.TrimPrefix(scope, "post:")
		result = getSinglePost(postID, workspaceURL)
	case strings.HasPrefix(scope, "thread:"):
		postID := strings.TrimPrefix(scope, "thread:")
		result = getThreadPosts(workdir, postID, workspaceURL)
	default:
		return failure[[]Post]("INVALID_SCOPE", "Unknown scope: "+scope)
	}
	if result.Success {
		annotateVerified(result.Data)
	}
	return result
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
	GitRoot         string // pre-computed git root to avoid subprocess on hot path
	SkipUnpushed    bool   // skip unpushed decoration for fast initial load
}

// CreatePostOptions configures post creation.
type CreatePostOptions struct {
	Origin *protocol.Origin
	Labels []string
}

// CreatePost creates a new post as a git commit in the workspace.
func CreatePost(workdir, content string, opts *CreatePostOptions) Result[Post] {
	if strings.TrimSpace(content) == "" {
		return failure[Post]("EMPTY_CONTENT", "Post content cannot be empty")
	}

	branch := gitmsg.GetExtBranch(workdir, "social")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	var labelStr string
	if opts != nil {
		labelStr = joinSocialLabels(opts.Labels)
	}

	message := content
	if (opts != nil && opts.Origin != nil) || labelStr != "" {
		fields := map[string]string{"type": "post"}
		if opts != nil {
			protocol.ApplyOrigin(fields, opts.Origin)
		}
		if labelStr != "" {
			fields["labels"] = labelStr
		}
		header := protocol.Header{Ext: "social", V: "0.1.0", Fields: fields, FieldOrder: socialFieldOrder}
		message = protocol.FormatMessage(content, header, nil)
	}

	hash, author, isUnpushed, err := commitSocialMessage(workdir, branch, message)
	if err != nil {
		return failureWithDetails[Post]("COMMIT_ERROR", "Failed to create commit", err)
	}

	now := time.Now()
	recordSocialCommit(SocialItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  branch,
		Type:    "post",
	}, message, author, now)

	return success(Post{
		ID:              protocol.CreateRef(protocol.RefTypeCommit, hash, repoURL, branch),
		Repository:      repoURL,
		Branch:          branch,
		Author:          author,
		Timestamp:       now,
		Content:         content,
		Type:            PostTypePost,
		CleanContent:    content,
		IsWorkspacePost: true,
		Display: Display{
			CommitHash:      hash,
			IsWorkspacePost: true,
			IsUnpushed:      isUnpushed,
		},
	})
}

// commitSocialMessage commits a message on the social branch and reads back its author and push state.
func commitSocialMessage(workdir, branch, message string) (string, Author, bool, error) {
	hash, err := git.CreateCommitOnBranch(workdir, branch, message)
	if err != nil {
		return "", Author{}, false, fmt.Errorf("create commit on %s: %w", branch, err)
	}
	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return "", Author{}, false, fmt.Errorf("read commit %s: %w", hash, err)
	}
	var author Author
	if commit != nil {
		author = Author{Name: commit.Author, Email: commit.Email}
	}
	unpushed, _ := git.GetUnpushedCommits(workdir, branch)
	_, isUnpushed := unpushed[hash[:12]]
	return hash, author, isUnpushed, nil
}

// recordSocialCommit caches a written commit and its social item.
func recordSocialCommit(item SocialItem, message string, author Author, timestamp time.Time) {
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        item.Hash,
		RepoURL:     item.RepoURL,
		Branch:      item.Branch,
		AuthorName:  author.Name,
		AuthorEmail: author.Email,
		Message:     message,
		Timestamp:   timestamp,
	}}); err != nil {
		log.Warn("insert commit failed", "hash", item.Hash, "error", err)
	}
	if err := InsertSocialItem(item); err != nil {
		log.Warn("insert social item failed", "hash", item.Hash, "error", err)
	}
}

// getTimelinePosts retrieves posts from all subscribed lists and workspace.
func getTimelinePosts(workdir string, workspaceURL string, opts *GetPostsOptions) Result[[]Post] {
	gitRoot := opts.GitRoot
	if gitRoot == "" {
		var err error
		gitRoot, err = git.GetRootDir(workdir)
		if err != nil || gitRoot == "" {
			gitRoot = workdir
		}
	}

	// Every unpushed commit, so a cross-extension or feature-branch item gets the badge.
	var unpushed map[string]struct{}
	if !opts.SkipUnpushed {
		unpushed, _ = git.GetAllUnpushedCommits(workdir)
	}

	listIDs, _ := cache.GetListIDs(gitRoot)
	forkURLs := gitmsg.GetForks(workdir)
	items, err := getTimeline(listIDs, workspaceURL, workspaceURL, forkURLs, opts.Limit, opts.Cursor)
	if err != nil {
		return failureWithDetails[[]Post]("CACHE_ERROR", "Failed to get timeline", err)
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

	return success(posts)
}

// CountTimeline returns the total number of timeline posts for the workspace.
func CountTimeline(workdir, gitRoot string) int {
	if gitRoot == "" {
		var err error
		gitRoot, err = git.GetRootDir(workdir)
		if err != nil || gitRoot == "" {
			gitRoot = workdir
		}
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	listIDs, _ := cache.GetListIDs(gitRoot)
	forkURLs := gitmsg.GetForks(workdir)
	count, _ := getTimelineCount(listIDs, workspaceURL, forkURLs)
	return count
}

// CountRepository returns the total number of posts for a repository scope.
func CountRepository(workdir, repoURL, branch string, isWorkspace bool) int {
	if isWorkspace {
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		count, _ := getAllItemsCount(socialQuery{RepoURL: workspaceURL})
		return count
	}
	count, _ := getAllItemsCount(socialQuery{RepoURL: repoURL, Branch: branch})
	return count
}

// CountListPosts returns the total number of posts in a list.
func CountListPosts(listID string) int {
	count, _ := getTimelineCount([]string{listID}, "", nil)
	return count
}

// getWorkspacePosts retrieves posts from the workspace repository.
func getWorkspacePosts(workdir string, workspaceURL string, opts *GetPostsOptions) Result[[]Post] {
	unpushed, _ := git.GetAllUnpushedCommits(workdir)

	items, err := getSocialItems(socialQuery{
		RepoURL:          workspaceURL,
		Limit:            opts.Limit,
		Cursor:           opts.Cursor,
		Since:            opts.Since,
		Until:            opts.Until,
		ForFollowerCheck: workspaceURL,
	})
	if err != nil {
		return failureWithDetails[[]Post]("CACHE_ERROR", "Failed to get posts", err)
	}

	posts := make([]Post, 0, len(items))
	for _, item := range items {
		post := SocialItemToPost(item)
		_, post.Display.IsUnpushed = unpushed[item.Hash]
		post.Display.IsWorkspacePost = true
		posts = append(posts, post)
	}

	return success(posts)
}

// getRepositoryPosts retrieves posts from a specific external repository.
func getRepositoryPosts(repoURL, branch, workspaceURL string, opts *GetPostsOptions) Result[[]Post] {
	items, err := getSocialItems(socialQuery{
		RepoURL:          repoURL,
		Branch:           branch,
		Limit:            opts.Limit,
		Cursor:           opts.Cursor,
		Since:            opts.Since,
		Until:            opts.Until,
		ForFollowerCheck: workspaceURL,
	})
	if err != nil {
		return failureWithDetails[[]Post]("CACHE_ERROR", "Failed to get posts", err)
	}

	posts := make([]Post, 0, len(items))
	for _, item := range items {
		post := SocialItemToPost(item)
		if item.RepoURL == workspaceURL {
			post.Display.IsWorkspacePost = true
		}
		posts = append(posts, post)
	}

	return success(posts)
}

// getListPosts retrieves posts from repositories in a specific list.
func getListPosts(listID string, workspaceURL string, opts *GetPostsOptions) Result[[]Post] {
	limit := 0
	cursor := ""
	if opts != nil {
		limit = opts.Limit
		cursor = opts.Cursor
	}
	// The empty workspace leaves workspace posts out of a list scope; the follower mark still reads against it.
	items, err := getTimeline([]string{listID}, "", workspaceURL, nil, limit, cursor)
	if err != nil {
		return failureWithDetails[[]Post]("CACHE_ERROR", "Failed to get posts", err)
	}

	posts := make([]Post, 0, len(items))
	for _, item := range items {
		posts = append(posts, SocialItemToPost(item))
	}

	return success(posts)
}

// getSinglePost retrieves a single post by its ID.
func getSinglePost(postID string, workspaceURL string) Result[[]Post] {
	postID = cache.ResolveRefToCanonical(postID)
	item, err := GetSocialItemByRef(postID, workspaceURL)
	if err != nil {
		return failureWithDetails[[]Post]("CACHE_ERROR", "Failed to get post", err)
	}

	if item == nil {
		return success([]Post{})
	}
	post := SocialItemToPost(*item)
	if item.RepoURL == workspaceURL {
		post.Display.IsWorkspacePost = true
	}
	return success([]Post{post})
}

// isInFamily reports whether url is the workspace or one of its registered forks.
func isInFamily(url, workspaceURL string, forks []string) bool {
	if url == workspaceURL {
		return true
	}
	for _, f := range forks {
		if url == f {
			return true
		}
	}
	return false
}

// getThreadPosts retrieves a post, its ancestors and all its replies as a thread.
func getThreadPosts(workdir, postID string, workspaceURL string) Result[[]Post] {
	canonicalPostID := cache.ResolveRefToCanonical(postID)
	parsed := protocol.ParseRef(canonicalPostID)
	if parsed.Value == "" {
		return failure[[]Post]("INVALID_REF", "Invalid post ID: "+postID)
	}

	branch := parsed.Branch
	if branch == "" {
		branch = "main"
	}

	// Every unpushed commit, so a cross-extension item in the thread gets the badge.
	unpushed, _ := git.GetAllUnpushedCommits(workdir)

	// A thread on the workspace or one of its forks is one conversation; a followed repo's is not.
	forks := gitmsg.GetForks(workdir)
	var forkURLs []string
	if isInFamily(parsed.Repository, workspaceURL, forks) {
		forkURLs = append([]string{workspaceURL}, forks...)
	}
	items, err := getThread(parsed.Repository, parsed.Value, branch, workspaceURL, forkURLs)
	if err != nil {
		return failureWithDetails[[]Post]("CACHE_ERROR", "Failed to get thread", err)
	}

	// A failed ancestor read drops the thread context, not the thread.
	parentItems, _ := getParentChain(parsed.Repository, parsed.Value, branch, workspaceURL)

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

	// The view LEFT JOINs social_items, so a root with no social item still reads.
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

	sorted := sortThreadTree(canonicalPostID, posts)

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

	return success(result)
}

// annotateVerified stamps Display.IsVerified and IsEditorVerified in two batched passes.
func annotateVerified(posts []Post) {
	canonVerified := batchVerify(posts, func(p *Post) (string, string, string) {
		if p.Repository == "" || p.Author.Email == "" || p.Display.CommitHash == "" {
			return "", "", ""
		}
		return p.Repository, p.Display.CommitHash, p.Author.Email
	})
	editVerified := batchVerify(posts, func(p *Post) (string, string, string) {
		if p.EditHash == "" || p.EditRepoURL == "" {
			return "", "", ""
		}
		email := p.EditorEmail
		if email == "" {
			email = p.Author.Email
		}
		return p.EditRepoURL, p.EditHash, email
	})
	for i := range posts {
		if posts[i].EditHash == "" {
			posts[i].Display.IsVerified = canonVerified[i]
			continue
		}
		if posts[i].EditorEmail == "" {
			posts[i].Display.IsVerified = canonVerified[i] && editVerified[i]
			continue
		}
		posts[i].Display.IsVerified = canonVerified[i]
		posts[i].Display.IsEditorVerified = editVerified[i]
	}
}

// batchVerify runs one IsVerifiedCommitBatch per repository and email group that key names.
func batchVerify(posts []Post, key func(*Post) (repo, hash, email string)) []bool {
	groups := make(map[string]map[string][]int)
	hashes := make([]string, len(posts))
	for i := range posts {
		repo, hash, email := key(&posts[i])
		if repo == "" || hash == "" || email == "" {
			continue
		}
		email = identity.NormalizeEmail(email)
		if email == "" {
			continue
		}
		byEmail := groups[repo]
		if byEmail == nil {
			byEmail = make(map[string][]int)
			groups[repo] = byEmail
		}
		byEmail[email] = append(byEmail[email], i)
		hashes[i] = hash
	}
	out := make([]bool, len(posts))
	for repo, byEmail := range groups {
		for email, indices := range byEmail {
			groupHashes := make([]string, 0, len(indices))
			for _, i := range indices {
				groupHashes = append(groupHashes, hashes[i])
			}
			res := identity.IsVerifiedCommitBatch(repo, groupHashes, email)
			for _, i := range indices {
				if res[hashes[i]] {
					out[i] = true
				}
			}
		}
	}
	return out
}
