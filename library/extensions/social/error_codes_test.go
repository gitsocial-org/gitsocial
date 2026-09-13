// error_codes_test.go - Error codes the social post and list write paths return at the Result boundary
package social

import (
	"testing"
)

// TestCreatePost_commitError asserts COMMIT_ERROR when the workdir holds no repository.
func TestCreatePost_commitError(t *testing.T) {
	setupTestDB(t)

	res := CreatePost(t.TempDir(), "a post with nowhere to land", nil)
	if res.Success || res.Error.Code != "COMMIT_ERROR" {
		t.Errorf("CreatePost() outside a repository = %+v, want COMMIT_ERROR", res)
	}
}

// TestCreateList_gitError asserts GIT_ERROR when the list metadata ref cannot be written.
func TestCreateList_gitError(t *testing.T) {
	setupTestDB(t)

	res := CreateList(t.TempDir(), "reading", "Reading")
	if res.Success || res.Error.Code != "GIT_ERROR" {
		t.Errorf("CreateList() outside a repository = %+v, want GIT_ERROR", res)
	}
}
