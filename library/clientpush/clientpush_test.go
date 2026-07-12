// clientpush_test.go - Orchestration tests for the publish (data + site) flow.
// Site publication over a real bucket is covered by core/objstore; here we
// exercise the orchestrator's decisions: remote resolution, the site-gate
// (non-s3 / opt-out / dry-run skip), and combined-result shape.
package clientpush

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// setupWork creates a working repo with one commit on main and an origin bare
// remote at the given URL (a local path = non-s3).
func setupWork(t *testing.T, originURL string) string {
	t.Helper()
	work := t.TempDir()
	if err := git.Init(work, "main"); err != nil {
		t.Fatalf("init work: %v", err)
	}
	for _, kv := range [][2]string{{"user.name", "Tester"}, {"user.email", "t@example.com"}} {
		git.ExecGit(work, []string{"config", kv[0], kv[1]})
	}
	git.CreateCommit(work, git.CommitOptions{Message: "init", AllowEmpty: true})
	if _, err := git.ExecGit(work, []string{"remote", "add", "origin", originURL}); err != nil {
		t.Fatalf("add origin: %v", err)
	}
	return work
}

// TestResolveRemote_explicitWins: an explicit remote beats the heuristic.
func TestResolveRemote_explicitWins(t *testing.T) {
	work := setupWork(t, t.TempDir())
	if got := ResolveRemote(work, "backup"); got != "backup" {
		t.Errorf("ResolveRemote explicit = %q, want backup", got)
	}
	if got := ResolveRemote(work, ""); got != "origin" {
		t.Errorf("ResolveRemote empty = %q, want origin (heuristic)", got)
	}
}

// TestPublish_nonS3RemoteSkipsSite: a non-s3 remote publishes data but silently
// skips the site step (nothing to serve a site from).
func TestPublish_nonS3RemoteSkipsSite(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	git.ExecGit(work, []string{"push", "origin", "main"})
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "a post"); err != nil {
		t.Fatalf("commit on branch: %v", err)
	}

	res, err := Publish(work, Options{}, nil, nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Site.Published {
		t.Error("non-s3 remote should not publish a site")
	}
	if res.Site.Skipped != "non-s3 remote" {
		t.Errorf("Site.Skipped = %q, want %q", res.Site.Skipped, "non-s3 remote")
	}
	if res.Push == nil || res.Push.Commits == 0 {
		t.Errorf("data push should have published the gitmsg/social commit, got %+v", res.Push)
	}
}

// TestPublish_noSiteOptOut: --no-site skips the site with the right reason even
// on what would otherwise be an s3 remote path.
func TestPublish_noSiteOptOut(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	git.ExecGit(work, []string{"push", "origin", "main"})

	res, err := Publish(work, Options{NoSite: true}, nil, nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Site.Published {
		t.Error("--no-site should not publish a site")
	}
	if res.Site.Skipped != "--no-site" {
		t.Errorf("Site.Skipped = %q, want --no-site", res.Site.Skipped)
	}
}

// TestPublish_dryRunSkipsSite: a dry run touches nothing, including the site.
func TestPublish_dryRunSkipsSite(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	git.ExecGit(work, []string{"push", "origin", "main"})

	res, err := Publish(work, Options{DryRun: true}, nil, nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if res.Site.Published {
		t.Error("dry-run should not publish a site")
	}
}

// TestPublish_emptyBoot: a fresh bare remote is reported as the first-publish
// (bootstrap) case; after publishing, a second publish is not.
func TestPublish_emptyBoot(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	// Push main directly so the reason-based gitmsg push has a base, but the
	// remote is still empty at the moment Publish probes it.
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "a post"); err != nil {
		t.Fatalf("commit on branch: %v", err)
	}

	res, err := Publish(work, Options{}, nil, nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !res.EmptyBoot {
		t.Error("first publish to a fresh bare remote should set EmptyBoot")
	}

	res2, err := Publish(work, Options{}, nil, nil)
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	if res2.EmptyBoot {
		t.Error("second publish should not report EmptyBoot")
	}
}
