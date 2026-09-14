// forks_test.go - Tests for the fork registry refs
package gitmsg

import (
	"errors"
	"strings"
	"testing"
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
