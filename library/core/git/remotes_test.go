// remotes_test.go - Tests for remote repository management
package git

import (
	"testing"
)

func TestListRemotes(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/user/repo.git"})

	remotes, err := ListRemotes(dir)
	if err != nil {
		t.Fatalf("ListRemotes() error = %v", err)
	}
	if len(remotes) != 1 {
		t.Fatalf("len(remotes) = %d, want 1", len(remotes))
	}
	if remotes[0].Name != "origin" {
		t.Errorf("Name = %q, want origin", remotes[0].Name)
	}
	if remotes[0].URL != "https://github.com/user/repo.git" {
		t.Errorf("URL = %q", remotes[0].URL)
	}
}

func TestListRemotes_noRemotes(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	remotes, err := ListRemotes(dir)
	if err != nil {
		t.Fatalf("ListRemotes() error = %v", err)
	}
	if len(remotes) != 0 {
		t.Errorf("len(remotes) = %d, want 0", len(remotes))
	}
}

func TestListRemotes_multiple(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/user/repo.git"})
	ExecGit(dir, []string{"remote", "add", "upstream", "https://github.com/org/repo.git"})

	remotes, err := ListRemotes(dir)
	if err != nil {
		t.Fatalf("ListRemotes() error = %v", err)
	}
	if len(remotes) != 2 {
		t.Errorf("len(remotes) = %d, want 2", len(remotes))
	}
}

func TestGetOriginURL(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/user/repo.git"})

	url := GetOriginURL(dir)
	if url != "https://github.com/user/repo.git" {
		t.Errorf("GetOriginURL() = %q", url)
	}
}

func TestGetOriginURL_noOrigin(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "upstream", "https://github.com/org/repo.git"})

	url := GetOriginURL(dir)
	if url != "https://github.com/org/repo.git" {
		t.Errorf("GetOriginURL() should fallback to first remote, got %q", url)
	}
}

func TestGetOriginURL_noRemotes(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	url := GetOriginURL(dir)
	if url != "" {
		t.Errorf("GetOriginURL() = %q, want empty", url)
	}
}

func TestPushRemote_configOverHeuristic(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	// origin is s3, so the heuristic would pick origin. A configured remote
	// must win over that.
	ExecGit(dir, []string{"remote", "add", "origin", "s3://s3.example.com/bucket/repo"})
	ExecGit(dir, []string{"remote", "add", "backup", "s3://s3.example.com/other/repo"})
	ExecGit(dir, []string{"config", "gitsocial.pushRemote", "backup"})

	if got := PushRemote(dir); got != "backup" {
		t.Errorf("PushRemote() = %q, want backup (config overrides heuristic)", got)
	}
}

func TestPushRemote_missingConfiguredFallsBack(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	// Configured name doesn't exist as a remote; must fall through to the
	// heuristic (origin, which is s3 here).
	ExecGit(dir, []string{"remote", "add", "origin", "s3://s3.example.com/bucket/repo"})
	ExecGit(dir, []string{"config", "gitsocial.pushRemote", "ghost"})

	if got := PushRemote(dir); got != "origin" {
		t.Errorf("PushRemote() = %q, want origin (heuristic fallback for missing config)", got)
	}
}

func TestConfiguredPushRemote(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	if got := ConfiguredPushRemote(dir); got != "" {
		t.Errorf("ConfiguredPushRemote() = %q, want empty when unset", got)
	}
	ExecGit(dir, []string{"config", "gitsocial.pushRemote", "backup"})
	if got := ConfiguredPushRemote(dir); got != "backup" {
		t.Errorf("ConfiguredPushRemote() = %q, want backup", got)
	}
}

func TestSetConfiguredPushRemotes_multiValued(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	if got := ConfiguredPushRemotes(dir); got != nil {
		t.Errorf("ConfiguredPushRemotes() = %v, want nil when unset", got)
	}
	if err := SetConfiguredPushRemotes(dir, []string{"r2", "s3"}); err != nil {
		t.Fatalf("SetConfiguredPushRemotes: %v", err)
	}
	got := ConfiguredPushRemotes(dir)
	if len(got) != 2 || got[0] != "r2" || got[1] != "s3" {
		t.Fatalf("ConfiguredPushRemotes() = %v, want [r2 s3]", got)
	}
	// The first value stays available via the singular accessor (a plain --get
	// would error on a multi-valued key).
	if first := ConfiguredPushRemote(dir); first != "r2" {
		t.Errorf("ConfiguredPushRemote() = %q, want r2 (first value)", first)
	}
	// Replacing with a shorter list drops the stale trailing values.
	if err := SetConfiguredPushRemotes(dir, []string{"only"}); err != nil {
		t.Fatalf("SetConfiguredPushRemotes replace: %v", err)
	}
	if got := ConfiguredPushRemotes(dir); len(got) != 1 || got[0] != "only" {
		t.Fatalf("after replace, ConfiguredPushRemotes() = %v, want [only]", got)
	}
	// An empty list unsets the key entirely.
	if err := SetConfiguredPushRemotes(dir, nil); err != nil {
		t.Fatalf("SetConfiguredPushRemotes unset: %v", err)
	}
	if got := ConfiguredPushRemotes(dir); got != nil {
		t.Errorf("after unset, ConfiguredPushRemotes() = %v, want nil", got)
	}
}

func TestPushRemotes_resolution(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "s3://s3.example.com/bucket/repo"})
	ExecGit(dir, []string{"remote", "add", "r2", "s3://r2.example.com/bucket/repo"})
	// No config: falls back to the single-element heuristic.
	if got := PushRemotes(dir); len(got) != 1 || got[0] != "origin" {
		t.Fatalf("PushRemotes() unconfigured = %v, want [origin]", got)
	}
	// Configured multi-valued: every existing configured remote, in order.
	if err := SetConfiguredPushRemotes(dir, []string{"r2", "origin"}); err != nil {
		t.Fatalf("SetConfiguredPushRemotes: %v", err)
	}
	if got := PushRemotes(dir); len(got) != 2 || got[0] != "r2" || got[1] != "origin" {
		t.Fatalf("PushRemotes() configured = %v, want [r2 origin]", got)
	}
	// A configured name that isn't a real remote is dropped; if none survive, the
	// heuristic single-element fallback stands.
	if err := SetConfiguredPushRemotes(dir, []string{"ghost", "origin"}); err != nil {
		t.Fatalf("SetConfiguredPushRemotes ghost: %v", err)
	}
	if got := PushRemotes(dir); len(got) != 1 || got[0] != "origin" {
		t.Fatalf("PushRemotes() with a ghost = %v, want [origin]", got)
	}
}

func TestGetRemoteDefaultBranch_fallback(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// With no valid remote, should fall back to "main"
	branch := GetRemoteDefaultBranch(dir, "https://invalid.example.com/none.git")
	if branch != "main" {
		t.Errorf("GetRemoteDefaultBranch() = %q, want main (fallback)", branch)
	}
}

func TestFetchRemote_noRemote(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	err := FetchRemote(dir, "origin", nil)
	if err == nil {
		t.Error("FetchRemote() should error when remote doesn't exist")
	}
}

func TestFetchRemote_withOptions(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	ExecGit(dir, []string{"remote", "add", "origin", "https://invalid.example.com/repo.git"})

	// Will fail (invalid remote) but tests option building
	err := FetchRemote(dir, "origin", &FetchOptions{
		ShallowSince: "2025-01-01",
		Depth:        10,
		Branch:       "main",
		Jobs:         4,
	})
	if err == nil {
		t.Error("FetchRemote() should error for invalid remote URL")
	}
}
