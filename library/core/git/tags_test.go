// tags_test.go - Tag listing, the display order and the commit count
package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListTags reads a lightweight and an annotated tag with the author and date of each, in display order.
func TestListTags(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	if _, err := ExecGit(dir, []string{"tag", "v0.1.0"}); err != nil {
		t.Fatalf("tag: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0644)
	ExecGit(dir, []string{"add", "a.txt"})
	ExecGit(dir, []string{"commit", "-m", "second"})
	if _, err := ExecGit(dir, []string{"-c", "user.name=Tagger", "-c", "user.email=tagger@test.com", "tag", "-a", "v1.0.0", "-m", "release"}); err != nil {
		t.Fatalf("annotated tag: %v", err)
	}
	tags, err := ListTags(dir)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 2 || tags[0].Name != "v1.0.0" || tags[1].Name != "v0.1.0" {
		t.Fatalf("tags = %+v, want v1.0.0 then v0.1.0", tags)
	}
	head, _ := execGitSimple(dir, []string{"rev-parse", "HEAD"})
	head = strings.TrimSpace(head)
	if tags[0].Commit != head || tags[0].Author != "Tagger" || tags[0].Email != "tagger@test.com" || tags[0].Time == 0 || tags[0].SHA == head {
		t.Errorf("annotated tag = %+v, want the tagger, the tag date and the peeled commit", tags[0])
	}
	if tags[1].Author != "Test User" || tags[1].Commit != tags[1].SHA {
		t.Errorf("lightweight tag = %+v, want the commit author and sha", tags[1])
	}
	if n, err := CountCommits(dir, tags[0].Commit, tags[1].Commit); err != nil || n != 1 {
		t.Errorf("CountCommits since v0.1.0 = %d, %v, want 1", n, err)
	}
	if n, err := CountCommits(dir, tags[1].Commit, ""); err != nil || n != 1 {
		t.Errorf("CountCommits of the history = %d, %v, want 1", n, err)
	}
}

// TestTagOrder puts versions first and highest, a plain version before its pre-release, hash-suffix ties by date, and names last.
func TestTagOrder(t *testing.T) {
	t.Parallel()
	names := []string{"nightly", "v1.2.0-rc.1", "v1.2.0", "v1.10.0", "v0.9.0.abcdef1", "v0.9.0.1234567", "build"}
	times := []int64{0, 0, 0, 0, 100, 200, 0}
	got := make([]string, 0, len(names))
	for _, i := range TagOrder(names, times) {
		got = append(got, names[i])
	}
	want := []string{"v1.10.0", "v1.2.0", "v1.2.0-rc.1", "v0.9.0.1234567", "v0.9.0.abcdef1", "nightly", "build"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// TestParseTagRef rejects a tag on a tree and a short line.
func TestParseTagRef(t *testing.T) {
	t.Parallel()
	if _, ok := ParseTagRef("short"); ok {
		t.Error("a short line parsed")
	}
	line := "t\x00sha\x00tree\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
	if _, ok := ParseTagRef(line); ok {
		t.Error("a tag on a tree parsed")
	}
}
