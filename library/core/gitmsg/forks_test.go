// forks_test.go - Tests for the fork registry refs
package gitmsg

import (
	"errors"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// TestRemoveFork_registered removes a fork that was added.
func TestRemoveFork_registered(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	const forkURL = "https://example.com/alice/repo"

	if err := AddFork(dir, forkURL); err != nil {
		t.Fatalf("AddFork() error = %v", err)
	}
	if err := RemoveFork(dir, forkURL); err != nil {
		t.Fatalf("RemoveFork() error = %v", err)
	}
	if forks := GetForks(dir); len(forks) != 0 {
		t.Errorf("GetForks() = %v, want none", forks)
	}
}

// TestRemoveFork_bySpelling removes a fork named by a spelling other than the stored one.
func TestRemoveFork_bySpelling(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	if err := AddFork(dir, "https://example.com/alice/repo.git"); err != nil {
		t.Fatalf("AddFork() error = %v", err)
	}
	if err := RemoveFork(dir, "git@example.com:alice/repo"); err != nil {
		t.Fatalf("RemoveFork() by another spelling: %v", err)
	}
	if forks := GetForks(dir); len(forks) != 0 {
		t.Errorf("GetForks() = %v, want none", forks)
	}
}

// TestAddFork_keepsTheAddress reads a fork back as an identity and fetches it as written.
func TestAddFork_keepsTheAddress(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	const address = "https://example.com/alice/repo.git"
	const identity = "https://example.com/alice/repo"

	if err := AddFork(dir, address); err != nil {
		t.Fatalf("AddFork() error = %v", err)
	}
	// Another spelling of a registered fork is a no-op, and leaves the address alone.
	if err := AddFork(dir, "git@example.com:alice/repo"); err != nil {
		t.Fatalf("AddFork() second spelling: %v", err)
	}
	forks := GetForks(dir)
	if len(forks) != 1 || forks[0] != identity {
		t.Fatalf("GetForks() = %v, want [%s]", forks, identity)
	}
	if got := ForkAddresses(dir)[identity]; got != address {
		t.Errorf("ForkAddresses()[%s] = %q, want %q", identity, got, address)
	}
}

// TestRemoveFork_unregistered reports a URL that was never registered.
func TestRemoveFork_unregistered(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	const forkURL = "https://example.com/nobody/repo"

	err := RemoveFork(dir, forkURL)
	if !errors.Is(err, ErrForkNotRegistered) {
		t.Fatalf("RemoveFork() error = %v, want ErrForkNotRegistered", err)
	}
	if !strings.Contains(err.Error(), forkURL) {
		t.Errorf("RemoveFork() error = %q, want it to name %q", err, forkURL)
	}
}

// TestRemoveFork_invalidURL reports a URL that normalizes to nothing.
func TestRemoveFork_invalidURL(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	if err := RemoveFork(dir, ""); err == nil {
		t.Error("RemoveFork() error = nil, want an error for an empty URL")
	}
}

// TestRemoveFork_reKeyed lists once and removes a fork whose ref was written under an older identity rule.
func TestRemoveFork_reKeyed(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	const address = "https://example.com/alice/repo/"
	const identity = "https://example.com/alice/repo"

	// The ref sits where the address itself hashes, not where its identity does.
	hash, err := git.CreateCommitTree(dir, address+"\n", "")
	if err != nil {
		t.Fatalf("create fork ref commit: %v", err)
	}
	if err := git.WriteRef(dir, forkRefPath(address), hash); err != nil {
		t.Fatalf("write fork ref: %v", err)
	}
	if forks := GetForks(dir); len(forks) != 1 || forks[0] != identity {
		t.Fatalf("GetForks() = %v, want [%s]", forks, identity)
	}
	if err := AddFork(dir, identity); err != nil {
		t.Fatalf("AddFork() error = %v", err)
	}
	if forks := GetForks(dir); len(forks) != 1 {
		t.Fatalf("GetForks() after a re-add = %v, want the identity once", forks)
	}
	if err := RemoveFork(dir, address); err != nil {
		t.Fatalf("RemoveFork() error = %v", err)
	}
	if forks := GetForks(dir); len(forks) != 0 {
		t.Errorf("GetForks() = %v, want none", forks)
	}
}
