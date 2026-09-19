// push_test.go - tests for the push flow: remote resolution, the site gate and the result shape.
package client

import (
	"strings"
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

// TestResolveRemotes: named remotes win, else the configured defaults, else the heuristic.
func TestResolveRemotes(t *testing.T) {
	work := setupWork(t, t.TempDir())
	if _, err := git.ExecGit(work, []string{"remote", "add", "backup", "s3://s3.example.com/b/p"}); err != nil {
		t.Fatalf("add backup: %v", err)
	}
	got, reason := ResolveRemotes(work, []string{"backup"})
	if len(got) != 1 || got[0] != "backup" || reason != git.PushConfigured {
		t.Errorf("ResolveRemotes named = %v (%q), want [backup] configured", got, reason)
	}
	if got, reason := ResolveRemotes(work, nil); len(got) != 1 || got[0] != "backup" || reason != git.PushS3 {
		t.Errorf("ResolveRemotes unconfigured = %v (%q), want [backup] s3", got, reason)
	}
	if err := git.SetConfiguredPushRemotes(work, []string{"backup", "origin"}); err != nil {
		t.Fatalf("SetConfiguredPushRemotes: %v", err)
	}
	got, reason = ResolveRemotes(work, nil)
	if len(got) != 2 || got[0] != "backup" || got[1] != "origin" || reason != git.PushConfigured {
		t.Errorf("ResolveRemotes configured = %v (%q), want [backup origin] configured", got, reason)
	}
	if err := git.SetConfiguredPushRemotes(work, []string{"ghost"}); err != nil {
		t.Fatalf("SetConfiguredPushRemotes ghost: %v", err)
	}
	if got, reason := ResolveRemotes(work, nil); len(got) != 1 || got[0] != "backup" || reason != git.PushStale {
		t.Errorf("ResolveRemotes stale = %v (%q), want [backup] stale", got, reason)
	}
}

// TestPushAll_continuesPastFailure: a failed remote stops neither the next one nor the error.
func TestPushAll_continuesPastFailure(t *testing.T) {
	good := t.TempDir()
	if err := git.EnsureBareRepo(good); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, good)
	if _, err := git.ExecGit(work, []string{"remote", "add", "broken", t.TempDir()}); err != nil {
		t.Fatalf("add broken: %v", err)
	}
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "a post"); err != nil {
		t.Fatalf("commit on branch: %v", err)
	}

	results, err := PushAll(work, []string{"broken", "origin"}, Options{}, nil, nil, nil)
	if err == nil {
		t.Fatal("PushAll with a broken remote should return an error")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error = %v, want it to name the broken remote", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (the remote that worked)", len(results))
	}
	if results[0].Push == nil || results[0].Push.Remote != "origin" {
		t.Errorf("result = %+v, want origin's push result", results[0].Push)
	}
}

// TestPush_nonS3RemoteSkipsSite: a non-s3 remote sends data and skips
// the site step (nothing to serve a site from).
func TestPush_nonS3RemoteSkipsSite(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	git.ExecGit(work, []string{"push", "origin", "main"})
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "a post"); err != nil {
		t.Fatalf("commit on branch: %v", err)
	}

	res, err := Push(work, "origin", Options{}, nil, nil)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.Site.Published {
		t.Error("non-s3 remote should not publish a site")
	}
	if res.Site.Skipped != "non-s3 remote" {
		t.Errorf("Site.Skipped = %q, want %q", res.Site.Skipped, "non-s3 remote")
	}
	if res.Push == nil || res.Push.Commits == 0 {
		t.Errorf("data push should have sent the gitmsg/social commit, got %+v", res.Push)
	}
}

// TestPush_noSiteOptOut: --no-site skips the site with the right reason even
// on what would otherwise be an s3 remote path.
func TestPush_noSiteOptOut(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	git.ExecGit(work, []string{"push", "origin", "main"})

	res, err := Push(work, "origin", Options{NoSite: true}, nil, nil)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.Site.Published {
		t.Error("--no-site should not publish a site")
	}
	if res.Site.Skipped != "--no-site" {
		t.Errorf("Site.Skipped = %q, want --no-site", res.Site.Skipped)
	}
}

// TestPush_siteOnlyNonS3Errors: an explicit --site-only against a non-s3
// remote is an error (a plain push would just skip the site), and pushes no
// data even where a full push would have.
func TestPush_siteOnlyNonS3Errors(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	git.ExecGit(work, []string{"push", "origin", "main"})
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "a post"); err != nil {
		t.Fatalf("commit on branch: %v", err)
	}

	if _, err := Push(work, "origin", Options{SiteOnly: true}, nil, nil); err == nil {
		t.Error("site-only push to a non-s3 remote should error")
	}
	out, _ := git.ExecGit(remote, []string{"branch", "--list", "gitmsg/social"})
	if out != nil && out.Stdout != "" {
		t.Errorf("site-only push sent data: %q", out.Stdout)
	}
}

// TestPush_siteOnlyMissingRemoteErrors: --site-only names a remote that is
// not configured — explicit request, loud failure.
func TestPush_siteOnlyMissingRemoteErrors(t *testing.T) {
	work := setupWork(t, t.TempDir())
	if _, err := Push(work, "nosuch", Options{SiteOnly: true}, nil, nil); err == nil {
		t.Error("site-only push to a missing remote should error")
	}
}

// TestPush_siteOnlyDryRun: a site-only dry run stays offline and reports the
// site skipped rather than erroring.
func TestPush_siteOnlyDryRun(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)

	res, err := Push(work, "origin", Options{SiteOnly: true, DryRun: true}, nil, nil)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.Site.Published || res.Site.Skipped != "dry-run" {
		t.Errorf("Site = %+v, want skipped dry-run", res.Site)
	}
}

// TestPush_dryRunSkipsSite: a dry run touches nothing, including the site.
func TestPush_dryRunSkipsSite(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	git.ExecGit(work, []string{"push", "origin", "main"})

	res, err := Push(work, "origin", Options{DryRun: true}, nil, nil)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.Site.Published {
		t.Error("dry-run should not publish a site")
	}
}

// TestPush_emptyBoot: a fresh bare remote is reported as the first-push
// (bootstrap) case; after the first push, a second is not.
func TestPush_emptyBoot(t *testing.T) {
	remote := t.TempDir()
	if err := git.EnsureBareRepo(remote); err != nil {
		t.Fatalf("init remote: %v", err)
	}
	work := setupWork(t, remote)
	// Push main directly so the reason-based gitmsg push has a base, but the
	// remote is still empty at the moment Push probes it.
	if _, err := git.CreateCommitOnBranch(work, "gitmsg/social", "a post"); err != nil {
		t.Fatalf("commit on branch: %v", err)
	}

	res, err := Push(work, "origin", Options{}, nil, nil)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !res.EmptyBoot {
		t.Error("first push to a fresh bare remote should set EmptyBoot")
	}

	res2, err := Push(work, "origin", Options{}, nil, nil)
	if err != nil {
		t.Fatalf("second Push: %v", err)
	}
	if res2.EmptyBoot {
		t.Error("second push should not report EmptyBoot")
	}
}
