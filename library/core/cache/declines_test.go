// declines_test.go - Tests for the decline predicate
package cache

import "testing"

// TestHasDecline asserts the predicate is false until RecordDecline, and only for the edit recorded.
func TestHasDecline(t *testing.T) {
	setupTestDB(t)
	const repoURL, hash, branch = "https://github.com/test/repo", "abc123def456", "gitmsg/pm"

	has, err := HasDecline(repoURL, hash, branch)
	if err != nil {
		t.Fatalf("HasDecline() error = %v", err)
	}
	if has {
		t.Error("HasDecline() = true for an edit that was never declined")
	}

	if err := RecordDecline(repoURL, hash, branch); err != nil {
		t.Fatalf("RecordDecline() error = %v", err)
	}
	has, err = HasDecline(repoURL, hash, branch)
	if err != nil {
		t.Fatalf("HasDecline() error = %v", err)
	}
	if !has {
		t.Error("HasDecline() = false after RecordDecline")
	}

	other, err := HasDecline(repoURL, "999999999999", branch)
	if err != nil {
		t.Fatalf("HasDecline() error = %v", err)
	}
	if other {
		t.Error("HasDecline() = true for another edit in the same repository")
	}
}
