// acceptances_test.go - Tests for the acceptance predicate
package cache

import "testing"

// TestHasAcceptance asserts the predicate is false until RecordAcceptance, and only for the edit recorded.
func TestHasAcceptance(t *testing.T) {
	setupTestDB(t)
	const repoURL, hash, branch = "https://github.com/test/repo", "abc123def456", "gitmsg/pm"

	has, err := HasAcceptance(repoURL, hash, branch)
	if err != nil {
		t.Fatalf("HasAcceptance() error = %v", err)
	}
	if has {
		t.Error("HasAcceptance() = true for an edit that was never accepted")
	}

	if err := RecordAcceptance(repoURL, hash, branch); err != nil {
		t.Fatalf("RecordAcceptance() error = %v", err)
	}
	has, err = HasAcceptance(repoURL, hash, branch)
	if err != nil {
		t.Fatalf("HasAcceptance() error = %v", err)
	}
	if !has {
		t.Error("HasAcceptance() = false after RecordAcceptance")
	}

	other, err := HasAcceptance(repoURL, "999999999999", branch)
	if err != nil {
		t.Fatalf("HasAcceptance() error = %v", err)
	}
	if other {
		t.Error("HasAcceptance() = true for another edit in the same repository")
	}
}
