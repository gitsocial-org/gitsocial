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
