// tree_test.go - Tree listing, object types and the binary and LFS tests
package git

import (
	"testing"
)

// treeFixture commits a nested tree on main and returns the repository.
func treeFixture(t *testing.T) string {
	t.Helper()
	dir := initTestRepo(t)
	if _, err := CommitFiles(dir, "refs/heads/main", "tree", map[string][]byte{
		"zeta.txt": []byte("zeta\n"),
		"alpha.go": []byte("package alpha\n"),
	}); err != nil {
		t.Fatalf("CommitFiles: %v", err)
	}
	// mktree takes one level, so the subtree is added with the plumbing that reads an index.
	if _, err := ExecGit(dir, []string{"read-tree", "main"}); err != nil {
		t.Fatalf("read-tree: %v", err)
	}
	blob, err := execGitWithStdin(dir, []string{"hash-object", "-w", "--stdin"}, "nested\n")
	if err != nil {
		t.Fatalf("hash-object: %v", err)
	}
	if _, err := ExecGit(dir, []string{"update-index", "--add", "--cacheinfo", "100644," + blob + ",src/inner.txt"}); err != nil {
		t.Fatalf("update-index: %v", err)
	}
	tree, err := execGitSimple(dir, []string{"write-tree"})
	if err != nil {
		t.Fatalf("write-tree: %v", err)
	}
	commit, err := execGitSimple(dir, []string{"commit-tree", tree, "-p", "main", "-m", "nested"})
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	if _, err := ExecGit(dir, []string{"update-ref", "refs/heads/main", commit}); err != nil {
		t.Fatalf("update-ref: %v", err)
	}
	return dir
}

// TestListTree lists subtrees first, then blobs by name with their sizes, at the root and in a subtree.
func TestListTree(t *testing.T) {
	t.Parallel()
	dir := treeFixture(t)
	entries, err := ListTree(dir, "main", "")
	if err != nil {
		t.Fatalf("ListTree: %v", err)
	}
	if len(entries) != 3 || entries[0].Name != "src" || entries[0].Type != "tree" || entries[1].Name != "alpha.go" || entries[2].Name != "zeta.txt" {
		t.Fatalf("entries = %+v, want src, alpha.go, zeta.txt", entries)
	}
	if entries[1].Size != int64(len("package alpha\n")) || entries[1].Type != "blob" {
		t.Errorf("alpha.go = %+v, want a blob of its size", entries[1])
	}
	inner, err := ListTree(dir, "main", "src/")
	if err != nil {
		t.Fatalf("ListTree(src): %v", err)
	}
	if len(inner) != 1 || inner[0].Name != "inner.txt" {
		t.Errorf("src = %+v, want inner.txt", inner)
	}
	if _, err := ListTree(dir, "main", "missing"); err == nil {
		t.Error("a missing path should fail")
	}
}

// TestObjectType tells a tree from a blob, and the root is a tree.
func TestObjectType(t *testing.T) {
	t.Parallel()
	dir := treeFixture(t)
	for path, want := range map[string]string{"": "tree", "src": "tree", "src/inner.txt": "blob"} {
		got, err := ObjectType(dir, "main", path)
		if err != nil || got != want {
			t.Errorf("ObjectType(%q) = %q, %v, want %q", path, got, err, want)
		}
	}
}

// TestIsBinaryContent marks content with a NUL as binary.
func TestIsBinaryContent(t *testing.T) {
	t.Parallel()
	if IsBinaryContent("plain text\n") || !IsBinaryContent("a\x00b") {
		t.Error("the NUL test is wrong")
	}
}
