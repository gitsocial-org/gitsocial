// social_import_test.go - Discussion imports driven through the gh seam and the real write path
package github

import (
	"os"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
	importpkg "github.com/gitsocial-org/gitsocial/library/import"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// repliesDiscussionJSON is one discussion with a top-level comment carrying two replies.
const repliesDiscussionJSON = `{"data":{"repository":{"discussions":{
	"nodes":[
		{"number":11,"title":"Nested thread","body":"Start here.",
		 "author":{"login":"alice","name":"Alice GraphQL"},
		 "category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-15T12:00:00Z",
		 "comments":{"nodes":[
			{"id":"DC_top","databaseId":501,"body":"Top comment","author":{"login":"bob"},
			 "createdAt":"2024-06-15T13:00:00Z",
			 "replies":{"nodes":[
				{"id":"DC_r1","databaseId":502,"body":"First reply","author":{"login":"alice"},
				 "createdAt":"2024-06-15T14:00:00Z"},
				{"id":"DC_r2","databaseId":503,"body":"Reply to the reply","author":{"login":"bob"},
				 "createdAt":"2024-06-15T15:00:00Z"}],
			  "pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		  "pageInfo":{"hasNextPage":false,"endCursor":null}}}],
	"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`

// newImportWorkspace opens a temp cache and returns a fresh workspace repo for one import run.
func newImportWorkspace(t *testing.T) string {
	t.Helper()
	testutil.OpenTempCache(t, "")
	dir, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("NewRepoTemplate() error = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// runSocialImport imports the discussions the routed gh output carries into workdir.
func runSocialImport(t *testing.T, workdir, cacheDir string, update bool) importpkg.Stats {
	t.Helper()
	counts := importpkg.ItemCounts{Issues: -1, PRs: -1, Releases: -1, Discussions: 1}
	stats, err := importpkg.Run(New("acme", "widgets"), importpkg.Options{
		WorkDir:    workdir,
		RepoURL:    "https://github.com/acme/widgets",
		CacheDir:   cacheDir,
		Extensions: []string{"social"},
		Counts:     &counts,
		Update:     update,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return stats
}

// commentsByContent reads the imported thread and keys its comments by content.
func commentsByContent(t *testing.T, workdir, postHash string) map[string]social.Post {
	t.Helper()
	repoURL := gitmsg.ResolveRepoURL(workdir)
	branch := gitmsg.GetExtBranch(workdir, "social")
	postRef := protocol.CreateRef(protocol.RefTypeCommit, postHash, repoURL, branch)
	comments, err := social.GetComments(repoURL, postHash, branch, postRef)
	if err != nil {
		t.Fatalf("GetComments() error = %v", err)
	}
	byContent := map[string]social.Post{}
	for _, c := range comments {
		byContent[c.Content] = c
	}
	return byContent
}

func TestImportDiscussions_RepliesNestUnderTheirComment(t *testing.T) {
	workdir := newImportWorkspace(t)
	cacheDir := t.TempDir()
	socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: repliesDiscussionJSON}
	})

	stats := runSocialImport(t, workdir, cacheDir, false)
	if stats.Posts != 1 {
		t.Fatalf("Posts = %d, want 1", stats.Posts)
	}
	if stats.Comments != 3 {
		t.Fatalf("Comments = %d, want 3 (the comment and its two replies)", stats.Comments)
	}
	if len(stats.Errors) != 0 {
		t.Fatalf("Errors = %+v, want none", stats.Errors)
	}

	mapping, err := importpkg.ReadMapping(cacheDir, "https://github.com/acme/widgets", "")
	if err != nil {
		t.Fatalf("ReadMapping() error = %v", err)
	}
	postHash := mapping.GetHash(importpkg.MappingKey("github", "post", "11"))
	if postHash == "" {
		t.Fatalf("mapping is missing the discussion: %+v", mapping.Items)
	}

	byContent := commentsByContent(t, workdir, postHash)
	if len(byContent) != 3 {
		t.Fatalf("thread comments = %+v, want 3", byContent)
	}
	top, ok := byContent["Top comment"]
	if !ok {
		t.Fatalf("thread comments = %+v, want the top-level comment", byContent)
	}
	if top.ParentCommentID != "" {
		t.Errorf("top comment ParentCommentID = %q, want none", top.ParentCommentID)
	}
	if protocol.ParseRef(top.OriginalPostID).Value != postHash {
		t.Errorf("top comment OriginalPostID = %q, want the discussion %s", top.OriginalPostID, postHash)
	}
	topHash := protocol.ParseRef(top.ID).Value
	// GitHub threads replies one level deep, so a reply to a reply also answers the top-level comment.
	for _, content := range []string{"First reply", "Reply to the reply"} {
		reply, ok := byContent[content]
		if !ok {
			t.Fatalf("thread comments = %+v, want %q", byContent, content)
		}
		if protocol.ParseRef(reply.ParentCommentID).Value != topHash {
			t.Errorf("%q ParentCommentID = %q, want the top comment %s", content, reply.ParentCommentID, topHash)
		}
		if protocol.ParseRef(reply.OriginalPostID).Value != postHash {
			t.Errorf("%q OriginalPostID = %q, want the discussion %s", content, reply.OriginalPostID, postHash)
		}
	}

	// The reply commit carries both fields GITSOCIAL.md 1.3 requires of a nested comment.
	replyHash := protocol.ParseRef(byContent["First reply"].ID).Value
	commit, err := git.GetCommit(workdir, replyHash)
	if err != nil {
		t.Fatalf("GetCommit() error = %v", err)
	}
	msg := protocol.ParseMessage(commit.Message)
	if msg == nil {
		t.Fatalf("reply commit has no GitMsg header: %q", commit.Message)
	}
	if protocol.ParseRef(msg.Header.Fields["reply-to"]).Value != topHash {
		t.Errorf("reply-to = %q, want the top comment %s", msg.Header.Fields["reply-to"], topHash)
	}
	if protocol.ParseRef(msg.Header.Fields["original"]).Value != postHash {
		t.Errorf("original = %q, want the discussion %s", msg.Header.Fields["original"], postHash)
	}
	if len(msg.References) != 2 {
		t.Errorf("reply refs = %d, want the parent comment and the post", len(msg.References))
	}
}

func TestImportDiscussions_ReimportWritesNothing(t *testing.T) {
	workdir := newImportWorkspace(t)
	cacheDir := t.TempDir()
	socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: repliesDiscussionJSON}
	})

	runSocialImport(t, workdir, cacheDir, false)
	before := socialCommitCount(t, workdir)

	stats := runSocialImport(t, workdir, cacheDir, false)
	if stats.Total() != 0 {
		t.Errorf("second run imported %d items, want 0 (%+v)", stats.Total(), stats)
	}
	if got := socialCommitCount(t, workdir); got != before {
		t.Errorf("gitmsg/social commits = %d, want %d (unchanged)", got, before)
	}
}

func TestImportDiscussions_CommentsInOneSecondStaySeparate(t *testing.T) {
	const sameSecondJSON = `{"data":{"repository":{"discussions":{
		"nodes":[{"number":12,"title":"Busy second","body":"Start.",
		 "author":{"login":"alice","name":"Alice GraphQL"},
		 "category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-15T12:00:00Z",
		 "comments":{"nodes":[
			{"id":"DC_a","databaseId":601,"body":"First","author":{"login":"bob","name":"Bob"},
			 "createdAt":"2024-06-15T13:00:00Z",
			 "replies":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}},
			{"id":"DC_b","databaseId":602,"body":"Second","author":{"login":"bob","name":"Bob"},
			 "createdAt":"2024-06-15T13:00:00Z",
			 "replies":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		  "pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`

	workdir := newImportWorkspace(t)
	cacheDir := t.TempDir()
	socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: sameSecondJSON}
	})

	stats := runSocialImport(t, workdir, cacheDir, false)
	if stats.Comments != 2 {
		t.Fatalf("Comments = %d, want 2 (both comments of that second)", stats.Comments)
	}
	mapping, err := importpkg.ReadMapping(cacheDir, "https://github.com/acme/widgets", "")
	if err != nil {
		t.Fatalf("ReadMapping() error = %v", err)
	}
	for _, databaseID := range []string{"601", "602"} {
		if !mapping.IsMapped(importpkg.MappingKey("github", "comment", databaseID)) {
			t.Errorf("mapping is missing comment %s: %+v", databaseID, mapping.Items)
		}
	}
	byContent := commentsByContent(t, workdir, mapping.GetHash(importpkg.MappingKey("github", "post", "12")))
	if len(byContent) != 2 {
		t.Errorf("thread comments = %+v, want both", byContent)
	}
}

func TestImportDiscussions_LegacyMappingKeysStillDedupe(t *testing.T) {
	workdir := newImportWorkspace(t)
	cacheDir := t.TempDir()
	repoURL := "https://github.com/acme/widgets"
	socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: repliesDiscussionJSON}
	})

	runSocialImport(t, workdir, cacheDir, false)
	before := socialCommitCount(t, workdir)
	rekeyCommentsToLegacyIDs(t, cacheDir, repoURL, map[string]string{
		"501": "11-20240615T130000",
		"502": "11-20240615T140000",
		"503": "11-20240615T150000",
	})

	for _, update := range []bool{false, true} {
		stats := runSocialImport(t, workdir, cacheDir, update)
		if stats.Total() != 0 {
			t.Errorf("run with update=%v imported %d items, want 0 (%+v)", update, stats.Total(), stats)
		}
		if got := socialCommitCount(t, workdir); got != before {
			t.Errorf("run with update=%v left %d commits, want %d", update, got, before)
		}
	}
}

func TestImportDiscussions_ReplyFindsALegacyMappedParent(t *testing.T) {
	// The first page is what an import before this branch saw: the comment, no replies.
	const commentOnlyJSON = `{"data":{"repository":{"discussions":{
		"nodes":[{"number":21,"title":"Old thread","body":"Start.",
		 "author":{"login":"alice","name":"Alice GraphQL","email":"alice@example.com"},
		 "category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-15T12:00:00Z",
		 "comments":{"nodes":[
			{"id":"DC_p","databaseId":701,"body":"Parent comment","author":{"login":"bob"},
			 "createdAt":"2024-06-15T13:00:00Z",
			 "replies":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		  "pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`
	const withReplyJSON = `{"data":{"repository":{"discussions":{
		"nodes":[{"number":21,"title":"Old thread","body":"Start.",
		 "author":{"login":"alice","name":"Alice GraphQL","email":"alice@example.com"},
		 "category":{"name":"General","slug":"general"},
		 "createdAt":"2024-06-15T12:00:00Z",
		 "comments":{"nodes":[
			{"id":"DC_p","databaseId":701,"body":"Parent comment","author":{"login":"bob"},
			 "createdAt":"2024-06-15T13:00:00Z",
			 "replies":{"nodes":[
				{"id":"DC_c","databaseId":702,"body":"Late reply","author":{"login":"alice"},
				 "createdAt":"2024-06-15T14:00:00Z"}],
			  "pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		  "pageInfo":{"hasNextPage":false,"endCursor":null}}}],
		"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`

	workdir := newImportWorkspace(t)
	cacheDir := t.TempDir()
	repoURL := "https://github.com/acme/widgets"
	page := commentOnlyJSON
	socialRoutes(t, func([]string) ghResponse {
		return ghResponse{stdout: page}
	})

	runSocialImport(t, workdir, cacheDir, false)
	rekeyCommentsToLegacyIDs(t, cacheDir, repoURL, map[string]string{"701": "21-20240615T130000"})

	// New comments on an imported discussion arrive through --update, which plans the mapped parent again.
	page = withReplyJSON
	stats := runSocialImport(t, workdir, cacheDir, true)
	if stats.Comments != 1 || stats.Posts != 0 {
		t.Fatalf("second run = %+v, want only the reply", stats)
	}

	mapping, err := importpkg.ReadMapping(cacheDir, repoURL, "")
	if err != nil {
		t.Fatalf("ReadMapping() error = %v", err)
	}
	parentHash := mapping.GetHash(importpkg.MappingKey("github", "comment", "21-20240615T130000"))
	if parentHash == "" {
		t.Fatalf("the legacy parent entry is gone: %+v", mapping.Items)
	}
	byContent := commentsByContent(t, workdir, mapping.GetHash(importpkg.MappingKey("github", "post", "21")))
	reply, ok := byContent["Late reply"]
	if !ok {
		t.Fatalf("thread comments = %+v, want the reply", byContent)
	}
	if protocol.ParseRef(reply.ParentCommentID).Value != parentHash {
		t.Errorf("reply ParentCommentID = %q, want the legacy-mapped parent %s", reply.ParentCommentID, parentHash)
	}
}

// rekeyCommentsToLegacyIDs rewrites mapped comment IDs to the shape an import before databaseId wrote.
func rekeyCommentsToLegacyIDs(t *testing.T, cacheDir, repoURL string, legacy map[string]string) {
	t.Helper()
	mapping, err := importpkg.ReadMapping(cacheDir, repoURL, "")
	if err != nil {
		t.Fatalf("ReadMapping() error = %v", err)
	}
	for externalID, legacyID := range legacy {
		key := importpkg.MappingKey("github", "comment", externalID)
		item, ok := mapping.Items[key]
		if !ok {
			t.Fatalf("mapping is missing comment %s: %+v", externalID, mapping.Items)
		}
		delete(mapping.Items, key)
		mapping.Items[importpkg.MappingKey("github", "comment", legacyID)] = item
	}
	if err := importpkg.WriteMapping(cacheDir, repoURL, "", mapping); err != nil {
		t.Fatalf("WriteMapping() error = %v", err)
	}
}

// socialCommitCount returns how many commits the social branch holds.
func socialCommitCount(t *testing.T, workdir string) int {
	t.Helper()
	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{Branch: gitmsg.GetExtBranch(workdir, "social")})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	return len(commits)
}
