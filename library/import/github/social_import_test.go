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
			{"id":"DC_top","body":"Top comment","author":{"login":"bob"},
			 "createdAt":"2024-06-15T13:00:00Z",
			 "replies":{"nodes":[
				{"id":"DC_r1","body":"First reply","author":{"login":"alice"},
				 "createdAt":"2024-06-15T14:00:00Z"},
				{"id":"DC_r2","body":"Reply to the reply","author":{"login":"bob"},
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
func runSocialImport(t *testing.T, workdir, cacheDir string) importpkg.Stats {
	t.Helper()
	counts := importpkg.ItemCounts{Issues: -1, PRs: -1, Releases: -1, Discussions: 1}
	stats, err := importpkg.Run(New("acme", "widgets"), importpkg.Options{
		WorkDir:    workdir,
		RepoURL:    "https://github.com/acme/widgets",
		CacheDir:   cacheDir,
		Extensions: []string{"social"},
		Counts:     &counts,
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

	stats := runSocialImport(t, workdir, cacheDir)
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

	runSocialImport(t, workdir, cacheDir)
	before := socialCommitCount(t, workdir)

	stats := runSocialImport(t, workdir, cacheDir)
	if stats.Total() != 0 {
		t.Errorf("second run imported %d items, want 0 (%+v)", stats.Total(), stats)
	}
	if got := socialCommitCount(t, workdir); got != before {
		t.Errorf("gitmsg/social commits = %d, want %d (unchanged)", got, before)
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
