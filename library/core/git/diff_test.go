// diff_test.go - Tests for git diff operations and unified diff parser
package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input     string
		wantStart int
		wantCount int
	}{
		{"10,7", 10, 7},
		{"10", 10, 1},
		{"0,0", 0, 0},
		{"1,1", 1, 1},
		{"100,50", 100, 50},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			start, count := parseRange(tt.input)
			if start != tt.wantStart || count != tt.wantCount {
				t.Errorf("parseRange(%q) = (%d, %d), want (%d, %d)", tt.input, start, count, tt.wantStart, tt.wantCount)
			}
		})
	}
}

func TestParseHunkHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                                                   string
		input                                                  string
		wantOldStart, wantOldCount, wantNewStart, wantNewCount int
	}{
		{"standard", "@@ -10,7 +10,8 @@ func main()", 10, 7, 10, 8},
		{"single lines", "@@ -1 +1 @@", 1, 1, 1, 1},
		{"large range", "@@ -100,50 +200,60 @@", 100, 50, 200, 60},
		{"malformed no @@", "not a header", 0, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os, oc, ns, nc := parseHunkHeader(tt.input)
			if os != tt.wantOldStart || oc != tt.wantOldCount || ns != tt.wantNewStart || nc != tt.wantNewCount {
				t.Errorf("parseHunkHeader(%q) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
					tt.input, os, oc, ns, nc, tt.wantOldStart, tt.wantOldCount, tt.wantNewStart, tt.wantNewCount)
			}
		})
	}
}

func TestParseHunk(t *testing.T) {
	t.Parallel()
	lines := []string{
		"@@ -1,3 +1,4 @@ header",
		" context line",
		"-removed line",
		"+added line 1",
		"+added line 2",
		" another context",
	}
	hunk, nextIdx := parseHunk(lines, 0)
	if hunk.OldStart != 1 || hunk.OldCount != 3 || hunk.NewStart != 1 || hunk.NewCount != 4 {
		t.Errorf("hunk range = (%d,%d,%d,%d), want (1,3,1,4)", hunk.OldStart, hunk.OldCount, hunk.NewStart, hunk.NewCount)
	}
	if len(hunk.Lines) != 5 {
		t.Fatalf("len(Lines) = %d, want 5", len(hunk.Lines))
	}
	if hunk.Lines[0].Type != LineContext {
		t.Errorf("line 0 type = %d, want LineContext", hunk.Lines[0].Type)
	}
	if hunk.Lines[1].Type != LineRemoved {
		t.Errorf("line 1 type = %d, want LineRemoved", hunk.Lines[1].Type)
	}
	if hunk.Lines[2].Type != LineAdded {
		t.Errorf("line 2 type = %d, want LineAdded", hunk.Lines[2].Type)
	}
	if nextIdx != 6 {
		t.Errorf("nextIdx = %d, want 6", nextIdx)
	}
}

func TestParseHunk_noNewlineMarker(t *testing.T) {
	t.Parallel()
	lines := []string{
		"@@ -1,1 +1,1 @@",
		"+new content",
		"\\ No newline at end of file",
	}
	hunk, _ := parseHunk(lines, 0)
	if len(hunk.Lines) != 1 {
		t.Errorf("len(Lines) = %d, want 1 (backslash line should be skipped)", len(hunk.Lines))
	}
}

func TestParseHunk_emptyLine(t *testing.T) {
	t.Parallel()
	lines := []string{
		"@@ -1,2 +1,2 @@",
		" first",
		"",
	}
	hunk, _ := parseHunk(lines, 0)
	if len(hunk.Lines) != 2 {
		t.Fatalf("len(Lines) = %d, want 2", len(hunk.Lines))
	}
	if hunk.Lines[1].Type != LineContext {
		t.Errorf("empty line should be LineContext, got %d", hunk.Lines[1].Type)
	}
}

func TestParseFileDiff_modified(t *testing.T) {
	t.Parallel()
	lines := []string{
		"diff --git a/file.go b/file.go",
		"index abc..def 100644",
		"--- a/file.go",
		"+++ b/file.go",
		"@@ -1,1 +1,1 @@",
		"-old",
		"+new",
	}
	fd, nextIdx := parseFileDiff(lines, 0)
	if fd.OldPath != "file.go" || fd.NewPath != "file.go" {
		t.Errorf("paths = (%q, %q), want (file.go, file.go)", fd.OldPath, fd.NewPath)
	}
	if fd.Status != DiffStatusModified {
		t.Errorf("status = %q, want DiffStatusModified", fd.Status)
	}
	if len(fd.Hunks) != 1 {
		t.Errorf("len(Hunks) = %d, want 1", len(fd.Hunks))
	}
	if nextIdx != 7 {
		t.Errorf("nextIdx = %d, want 7", nextIdx)
	}
}

func TestParseFileDiff_added(t *testing.T) {
	t.Parallel()
	lines := []string{
		"diff --git a/new.go b/new.go",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/new.go",
		"@@ -0,0 +1,1 @@",
		"+content",
	}
	fd, _ := parseFileDiff(lines, 0)
	if fd.Status != DiffStatusAdded {
		t.Errorf("status = %q, want DiffStatusAdded", fd.Status)
	}
}

func TestParseFileDiff_deleted(t *testing.T) {
	t.Parallel()
	lines := []string{
		"diff --git a/old.go b/old.go",
		"deleted file mode 100644",
		"--- a/old.go",
		"+++ /dev/null",
		"@@ -1,1 +0,0 @@",
		"-content",
	}
	fd, _ := parseFileDiff(lines, 0)
	if fd.Status != DiffStatusDeleted {
		t.Errorf("status = %q, want DiffStatusDeleted", fd.Status)
	}
}

func TestParseFileDiff_renamed(t *testing.T) {
	t.Parallel()
	lines := []string{
		"diff --git a/old.go b/new.go",
		"similarity index 100%",
		"rename from old.go",
		"rename to new.go",
	}
	fd, _ := parseFileDiff(lines, 0)
	if fd.Status != DiffStatusRenamed {
		t.Errorf("status = %q, want DiffStatusRenamed", fd.Status)
	}
	if fd.OldPath != "old.go" || fd.NewPath != "new.go" {
		t.Errorf("paths = (%q, %q), want (old.go, new.go)", fd.OldPath, fd.NewPath)
	}
}

func TestParseFileDiff_binary(t *testing.T) {
	t.Parallel()
	lines := []string{
		"diff --git a/image.png b/image.png",
		"Binary files a/image.png and b/image.png differ",
	}
	fd, _ := parseFileDiff(lines, 0)
	if !fd.Binary {
		t.Error("Binary should be true")
	}
}

func TestParseDiff(t *testing.T) {
	t.Parallel()
	output := `diff --git a/file1.go b/file1.go
index abc..def 100644
--- a/file1.go
+++ b/file1.go
@@ -1,1 +1,1 @@
-old1
+new1
diff --git a/file2.go b/file2.go
new file mode 100644
--- /dev/null
+++ b/file2.go
@@ -0,0 +1,1 @@
+content2`

	diffs := parseDiff(output)
	if len(diffs) != 2 {
		t.Fatalf("len(diffs) = %d, want 2", len(diffs))
	}
	if diffs[0].NewPath != "file1.go" {
		t.Errorf("diffs[0].NewPath = %q", diffs[0].NewPath)
	}
	if diffs[1].Status != DiffStatusAdded {
		t.Errorf("diffs[1].Status = %q, want DiffStatusAdded", diffs[1].Status)
	}
}

func TestParseDiff_empty(t *testing.T) {
	t.Parallel()
	diffs := parseDiff("")
	if len(diffs) != 0 {
		t.Errorf("len(diffs) = %d, want 0", len(diffs))
	}
}

func TestGetDiff(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create a file and commit
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello\n"), 0644)
	ExecGit(dir, []string{"add", "test.txt"})
	ExecGit(dir, []string{"commit", "-m", "add file"})
	base, _ := ReadRef(dir, "HEAD")

	// Modify and commit
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("world\n"), 0644)
	ExecGit(dir, []string{"add", "test.txt"})
	ExecGit(dir, []string{"commit", "-m", "modify file"})
	head, _ := ReadRef(dir, "HEAD")

	diffs, err := GetDiff(dir, base, head)
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("len(diffs) = %d, want 1", len(diffs))
	}
	if diffs[0].NewPath != "test.txt" {
		t.Errorf("NewPath = %q, want test.txt", diffs[0].NewPath)
	}
}

func TestGetDiff_noDiff(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	head, _ := ReadRef(dir, "HEAD")

	diffs, err := GetDiff(dir, head, head)
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("len(diffs) = %d, want 0 for same ref", len(diffs))
	}
}

func TestGetFileDiff(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb\n"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "add files"})
	base, _ := ReadRef(dir, "HEAD")

	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa modified\n"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb modified\n"), 0644)
	ExecGit(dir, []string{"add", "."})
	ExecGit(dir, []string{"commit", "-m", "modify files"})
	head, _ := ReadRef(dir, "HEAD")

	diff, err := GetFileDiff(dir, base, head, "a.txt")
	if err != nil {
		t.Fatalf("GetFileDiff() error = %v", err)
	}
	if diff == nil {
		t.Fatal("GetFileDiff() returned nil")
	}
	if diff.NewPath != "a.txt" {
		t.Errorf("NewPath = %q, want a.txt", diff.NewPath)
	}
}

func TestGetFileDiff_noDiff(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	head, _ := ReadRef(dir, "HEAD")

	diff, err := GetFileDiff(dir, head, head, "nonexistent.txt")
	if err != nil {
		t.Fatalf("GetFileDiff() error = %v", err)
	}
	if diff != nil {
		t.Error("GetFileDiff() should return nil for no diff")
	}
}

func TestGetFileContent(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "content.txt"), []byte("file contents here"), 0644)
	ExecGit(dir, []string{"add", "content.txt"})
	ExecGit(dir, []string{"commit", "-m", "add content"})

	content, err := GetFileContent(dir, "HEAD", "content.txt")
	if err != nil {
		t.Fatalf("GetFileContent() error = %v", err)
	}
	if content != "file contents here" {
		t.Errorf("GetFileContent() = %q, want %q", content, "file contents here")
	}
}

func TestGetDiffStats(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "stats.txt"), []byte("line1\nline2\nline3\n"), 0644)
	ExecGit(dir, []string{"add", "stats.txt"})
	ExecGit(dir, []string{"commit", "-m", "add file"})
	base, _ := ReadRef(dir, "HEAD")

	os.WriteFile(filepath.Join(dir, "stats.txt"), []byte("line1\nmodified\nline3\nnew line\n"), 0644)
	ExecGit(dir, []string{"add", "stats.txt"})
	ExecGit(dir, []string{"commit", "-m", "modify file"})
	head, _ := ReadRef(dir, "HEAD")

	stats, err := GetDiffStats(dir, base, head)
	if err != nil {
		t.Fatalf("GetDiffStats() error = %v", err)
	}
	if stats.Files != 1 {
		t.Errorf("Files = %d, want 1", stats.Files)
	}
	if stats.Added == 0 {
		t.Error("Added should be > 0")
	}
	if stats.Removed == 0 {
		t.Error("Removed should be > 0")
	}
}

func TestGetDiffStats_empty(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	head, _ := ReadRef(dir, "HEAD")

	stats, err := GetDiffStats(dir, head, head)
	if err != nil {
		t.Fatalf("GetDiffStats() error = %v", err)
	}
	if stats.Files != 0 {
		t.Errorf("Files = %d, want 0", stats.Files)
	}
}

func TestGetDiff_error(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := GetDiff(dir, "nonexistent1", "nonexistent2")
	if err == nil {
		t.Error("GetDiff should error for invalid refs")
	}
}

func TestGetFileDiff_error(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := GetFileDiff(dir, "nonexistent1", "nonexistent2", "file.txt")
	if err == nil {
		t.Error("GetFileDiff should error for invalid refs")
	}
}

func TestGetFileContent_notFound(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := GetFileContent(dir, "HEAD", "nonexistent.txt")
	if err == nil {
		t.Error("GetFileContent should error for nonexistent file")
	}
}

func TestGetDiffStats_error(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, err := GetDiffStats(dir, "nonexistent1", "nonexistent2")
	if err == nil {
		t.Error("GetDiffStats should error for invalid refs")
	}
}

func TestParseFileDiff_indexLine(t *testing.T) {
	t.Parallel()
	lines := []string{
		"diff --git a/file.go b/file.go",
		"index abc1234..def5678 100644",
		"--- a/file.go",
		"+++ b/file.go",
		"@@ -1,1 +1,2 @@",
		" existing",
		"+new line",
	}
	fd, _ := parseFileDiff(lines, 0)
	if fd.NewPath != "file.go" {
		t.Errorf("NewPath = %q, want file.go", fd.NewPath)
	}
}

func TestParseHunkHeader_malformed(t *testing.T) {
	t.Parallel()
	os, oc, ns, nc := parseHunkHeader("@@@ invalid")
	if os != 0 || oc != 0 || ns != 0 || nc != 0 {
		t.Errorf("malformed header should return zeros, got (%d,%d,%d,%d)", os, oc, ns, nc)
	}
}
