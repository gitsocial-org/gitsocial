// lfs_test.go - Tests for Git LFS pointers, the object store and the batch API
package git

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFormatLFSPointer pins the exact bytes written, which are the three lines
// the LFS v1 pointer format requires, each newline-terminated.
func TestFormatLFSPointer(t *testing.T) {
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	got := string(FormatLFSPointer(oid, 12345))
	want := "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize 12345\n"
	if got != want {
		t.Errorf("FormatLFSPointer() =\n%q\nwant\n%q", got, want)
	}
	if !IsLFSPointer([]byte(got)) {
		t.Error("IsLFSPointer() = false for freshly formatted pointer")
	}
}

// TestLFSPointerRoundTrip checks that formatting then parsing returns the same
// oid and size, across the sizes a real artifact takes.
func TestLFSPointerRoundTrip(t *testing.T) {
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	for _, size := range []int64{1, 42, 1024, 1 << 30, 1<<62 - 1} {
		gotOID, gotSize, ok := ParseLFSPointer(FormatLFSPointer(oid, size))
		if !ok {
			t.Errorf("size %d: ParseLFSPointer() ok = false", size)
			continue
		}
		if gotOID != oid {
			t.Errorf("size %d: oid = %q, want %q", size, gotOID, oid)
		}
		if gotSize != size {
			t.Errorf("size %d: size = %d", size, gotSize)
		}
	}
}

// TestParseLFSPointerRejects covers the inputs that must not be read as a
// pointer, so unrelated file content is never mistaken for one.
func TestParseLFSPointerRejects(t *testing.T) {
	const oidLine = "oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\n"
	tests := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"plain text", "just a regular file\n"},
		{"version line missing", oidLine + "size 10\n"},
		{"version line not first", "\n" + lfsPointerPrefix + oidLine + "size 10\n"},
		{"different spec version", "version https://git-lfs.github.com/spec/v2\n" + oidLine + "size 10\n"},
		{"version line without newline", strings.TrimSuffix(lfsPointerPrefix, "\n")},
		{"no oid", lfsPointerPrefix + "size 10\n"},
		{"empty oid", lfsPointerPrefix + "oid sha256:\nsize 10\n"},
		{"no size", lfsPointerPrefix + oidLine},
		{"non-numeric size", lfsPointerPrefix + oidLine + "size abc\n"},
		{"negative size", lfsPointerPrefix + oidLine + "size -5\n"},
		{"unsupported hash algorithm", lfsPointerPrefix + "oid sha1:abc123\nsize 10\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oid, size, ok := ParseLFSPointer([]byte(tt.data))
			if ok {
				t.Errorf("ParseLFSPointer() ok = true (oid %q, size %d), want false", oid, size)
			}
		})
	}
}

// TestParseLFSPointerTolerates covers well-formed pointers that carry more than
// the two fields we read, which the LFS spec permits.
func TestParseLFSPointerTolerates(t *testing.T) {
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	tests := []struct {
		name string
		data string
	}{
		{"extra key", lfsPointerPrefix + "ext-0-shalink sha256:deadbeef\noid sha256:" + oid + "\nsize 7\n"},
		{"no trailing newline", lfsPointerPrefix + "oid sha256:" + oid + "\nsize 7"},
		{"trailing blank lines", lfsPointerPrefix + "oid sha256:" + oid + "\nsize 7\n\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOID, gotSize, ok := ParseLFSPointer([]byte(tt.data))
			if !ok || gotOID != oid || gotSize != 7 {
				t.Errorf("ParseLFSPointer() = %q, %d, %v; want %q, 7, true", gotOID, gotSize, ok, oid)
			}
		})
	}
}

// TestParseLFSPointerZeroSize documents that an empty file does not round-trip:
// FormatLFSPointer writes a valid `size 0` pointer, but ParseLFSPointer treats
// any non-positive size as "not a pointer", so callers fall back to raw content.
func TestParseLFSPointerZeroSize(t *testing.T) {
	const oid = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	data := FormatLFSPointer(oid, 0)
	if !IsLFSPointer(data) {
		t.Fatal("IsLFSPointer() = false for a size-0 pointer")
	}
	if _, _, ok := ParseLFSPointer(data); ok {
		t.Error("ParseLFSPointer() now accepts size 0; update this test and the callers that rely on the fallback")
	}
}

// TestIsLFSPointer checks the prefix test on its own, since it gates every read
// path that decides whether a file is content or a pointer.
func TestIsLFSPointer(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"valid prefix", lfsPointerPrefix + "oid sha256:abc\nsize 1\n", true},
		{"prefix only", lfsPointerPrefix, true},
		{"empty", "", false},
		{"prefix without newline", strings.TrimSuffix(lfsPointerPrefix, "\n"), false},
		{"prefix later in the file", "# notes\n" + lfsPointerPrefix, false},
		{"leading whitespace", " " + lfsPointerPrefix, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLFSPointer([]byte(tt.data)); got != tt.want {
				t.Errorf("IsLFSPointer() = %v, want %v", got, tt.want)
			}
		})
	}
}

// FuzzParseLFSPointer checks the pointer parser against FormatLFSPointer.
func FuzzParseLFSPointer(f *testing.F) {
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	seeds := []string{
		lfsPointerPrefix + "oid sha256:" + oid + "\nsize 12345\n",
		lfsPointerPrefix + "oid sha256:" + oid + "\nsize 0\n",
		lfsPointerPrefix + "size 12345\noid sha256:" + oid + "\n",
		lfsPointerPrefix + "oid sha256:" + oid + "\nsize 99999999999999999999\n",
		lfsPointerPrefix + "ext-0-shalink sha256:deadbeef\noid sha256:" + oid + "\nsize 7\n",
		lfsPointerPrefix + "oid sha1:abc123\nsize 10\n",
		lfsPointerPrefix,
		"version https://git-lfs.github.com/spec/v2\noid sha256:" + oid + "\nsize 10\n",
		"a regular file\n",
		"",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		gotOID, gotSize, ok := ParseLFSPointer(data)
		// Content without the LFS v1 version line reads as no pointer at all.
		if !IsLFSPointer(data) {
			if ok || gotOID != "" || gotSize != 0 {
				t.Fatalf("ParseLFSPointer() = %q, %d, %v for content that is not a pointer", gotOID, gotSize, ok)
			}
			return
		}
		if !ok {
			return
		}
		// An accepted pointer carries a non-empty oid and a positive size.
		if gotOID == "" || gotSize <= 0 {
			t.Fatalf("ParseLFSPointer() accepted oid %q and size %d", gotOID, gotSize)
		}
		// A re-formatted pointer parses back to the same oid and size.
		againOID, againSize, againOK := ParseLFSPointer(FormatLFSPointer(gotOID, gotSize))
		if !againOK || againOID != gotOID || againSize != gotSize {
			t.Errorf("round trip = %q, %d, %v; want %q, %d, true", againOID, againSize, againOK, gotOID, gotSize)
		}
	})
}

// TestStoreAndReadLFSObject checks that a stored object reads back byte for byte.
func TestStoreAndReadLFSObject(t *testing.T) {
	dir := initTestRepo(t)
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	payload := []byte("binary artifact content")

	if err := StoreLFSObject(dir, oid, payload); err != nil {
		t.Fatalf("StoreLFSObject() error = %v", err)
	}
	got, err := ReadLFSObject(dir, oid)
	if err != nil {
		t.Fatalf("ReadLFSObject() error = %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("ReadLFSObject() = %q, want %q", got, payload)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "lfs", "objects", oid[:2], oid[2:4], oid)); err != nil {
		t.Errorf("object is not at the sharded path: %v", err)
	}
}

// TestStoreLFSObject_keepsTheFirstWrite checks that a second store of the same oid succeeds.
func TestStoreLFSObject_keepsTheFirstWrite(t *testing.T) {
	dir := initTestRepo(t)
	const oid = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	if err := StoreLFSObject(dir, oid, []byte("first")); err != nil {
		t.Fatalf("first StoreLFSObject() error = %v", err)
	}
	if err := StoreLFSObject(dir, oid, []byte("second")); err != nil {
		t.Fatalf("second StoreLFSObject() error = %v", err)
	}
	got, err := ReadLFSObject(dir, oid)
	if err != nil {
		t.Fatalf("ReadLFSObject() error = %v", err)
	}
	if string(got) != "first" {
		t.Errorf("ReadLFSObject() = %q, want the content of the first write", got)
	}
}

// TestReadLFSObject_missing checks that an absent object reports an error.
func TestReadLFSObject_missing(t *testing.T) {
	dir := initTestRepo(t)
	if _, err := ReadLFSObject(dir, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Error("ReadLFSObject() error = nil for an object that was never stored")
	}
}

// TestStoreLFSObject_outsideRepository checks that a directory with no git dir reports an error.
func TestStoreLFSObject_outsideRepository(t *testing.T) {
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	if err := StoreLFSObject(t.TempDir(), oid, []byte("x")); err == nil {
		t.Error("StoreLFSObject() error = nil outside a repository")
	}
}

// TestBuildLFSBatchURL checks the batch endpoint built from each remote URL shape.
func TestBuildLFSBatchURL(t *testing.T) {
	tests := []struct{ name, repoURL, want string }{
		{"plain https", "https://github.com/user/repo", "https://github.com/user/repo.git/info/lfs/objects/batch"},
		{"already dot git", "https://github.com/user/repo.git", "https://github.com/user/repo.git/info/lfs/objects/batch"},
		{"trailing slash", "https://github.com/user/repo/", "https://github.com/user/repo.git/info/lfs/objects/batch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildLFSBatchURL(tt.repoURL); got != tt.want {
				t.Errorf("buildLFSBatchURL(%q) = %q, want %q", tt.repoURL, got, tt.want)
			}
		})
	}
}

// lfsBatchServer serves one batch response and one download payload.
func lfsBatchServer(t *testing.T, batch func(downloadURL string) any, payload []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/download", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
		if err := json.NewEncoder(w).Encode(batch(server.URL + "/download")); err != nil {
			t.Errorf("encode batch response: %v", err)
		}
	})
	return server
}

// TestFetchLFSObject checks that a batch response with a download action yields the object.
func TestFetchLFSObject(t *testing.T) {
	payload := []byte("downloaded artifact")
	server := lfsBatchServer(t, func(downloadURL string) any {
		return map[string]any{"objects": []map[string]any{{
			"oid":     "abc",
			"size":    len(payload),
			"actions": map[string]any{"download": map[string]any{"href": downloadURL, "header": map[string]string{"X-Test": "1"}}},
		}}}
	}, payload)

	got, err := FetchLFSObject(server.URL+"/user/repo", "abc", int64(len(payload)))
	if err != nil {
		t.Fatalf("FetchLFSObject() error = %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("FetchLFSObject() = %q, want %q", got, payload)
	}
}

// TestFetchLFSObject_serverFailures checks the batch responses that yield no object.
func TestFetchLFSObject_serverFailures(t *testing.T) {
	tests := []struct {
		name    string
		batch   func(string) any
		wantErr string
	}{
		{
			name:    "no objects",
			batch:   func(string) any { return map[string]any{"objects": []map[string]any{}} },
			wantErr: "no objects",
		},
		{
			name: "object error",
			batch: func(string) any {
				return map[string]any{"objects": []map[string]any{{"oid": "abc", "error": map[string]any{"code": 404, "message": "not found"}}}}
			},
			wantErr: "not found",
		},
		{
			name: "no download action",
			batch: func(string) any {
				return map[string]any{"objects": []map[string]any{{"oid": "abc", "actions": map[string]any{}}}}
			},
			wantErr: "no download action",
		},
		{
			name:    "not an object",
			batch:   func(string) any { return "" },
			wantErr: "decode lfs batch response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := lfsBatchServer(t, tt.batch, nil)
			_, err := FetchLFSObject(server.URL+"/user/repo", "abc", 3)
			if err == nil {
				t.Fatal("FetchLFSObject() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

// TestFetchLFSObject_batchStatus checks that a non-200 batch response reports its status.
func TestFetchLFSObject_batchStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer server.Close()

	_, err := FetchLFSObject(server.URL+"/user/repo", "abc", 3)
	if err == nil || !strings.Contains(err.Error(), "410") {
		t.Errorf("FetchLFSObject() error = %v, want it to report status 410", err)
	}
}

// TestFetchLFSObject_downloadStatus checks that a failing download reports its status.
func TestFetchLFSObject_downloadStatus(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/download", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"objects":[{"oid":"abc","actions":{"download":{"href":"` + server.URL + `/download"}}}]}`))
	})

	_, err := FetchLFSObject(server.URL+"/user/repo", "abc", 3)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("FetchLFSObject() error = %v, want it to report status 500", err)
	}
}

// TestFetchLFSObject_unreachable checks that a dead endpoint reports a request error.
func TestFetchLFSObject_unreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	if _, err := FetchLFSObject(url+"/user/repo", "abc", 3); err == nil {
		t.Error("FetchLFSObject() error = nil against a closed server")
	}
}

// TestGetGitCredentials_unparseableURL checks that a malformed URL reports no credentials.
func TestGetGitCredentials_unparseableURL(t *testing.T) {
	if _, _, ok := getGitCredentials("http://[::1"); ok {
		t.Error("getGitCredentials() ok = true for an unparseable URL")
	}
}

// TestDownloadsDir checks that the reported directory exists.
func TestDownloadsDir(t *testing.T) {
	dir := DownloadsDir()
	if dir == "" {
		t.Fatal("DownloadsDir() = empty")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("DownloadsDir() = %q, which does not exist: %v", dir, err)
	}
	if !info.IsDir() {
		t.Errorf("DownloadsDir() = %q, which is not a directory", dir)
	}
}

// TestIsLFSAvailable checks that the probe follows the lfs version command.
func TestIsLFSAvailable(t *testing.T) {
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "lfs" {
			return &ExecResult{Stdout: "git-lfs/3.5.1"}, nil, true
		}
		return nil, nil, false
	}))
	available := IsLFSAvailable()
	restore()
	if !available {
		t.Error("IsLFSAvailable() = false when git lfs version succeeds")
	}

	restore = SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "lfs" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()
	if IsLFSAvailable() {
		t.Error("IsLFSAvailable() = true when git lfs version fails")
	}
}

// TestGetUnpushedLFSCount checks the count read from the dry run output.
func TestGetUnpushedLFSCount(t *testing.T) {
	const dryRun = "push 111 => file/one\npush 222 => file/two\nskip 333\n"
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 2 && args[0] == "lfs" && args[1] == "push" {
			return &ExecResult{Stdout: dryRun}, nil, true
		}
		if len(args) > 0 && args[0] == "lfs" {
			return &ExecResult{}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	if got := GetUnpushedLFSCount(t.TempDir()); got != 2 {
		t.Errorf("GetUnpushedLFSCount() = %d, want 2", got)
	}
}

// TestGetUnpushedLFSCount_lfsMissing checks that a missing git-lfs counts nothing.
func TestGetUnpushedLFSCount_lfsMissing(t *testing.T) {
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "lfs" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	if got := GetUnpushedLFSCount(t.TempDir()); got != 0 {
		t.Errorf("GetUnpushedLFSCount() = %d, want 0", got)
	}
}

// TestPushLFS checks that the push covers the objects on gitmsg refs too.
func TestPushLFS(t *testing.T) {
	var pushed []string
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		switch {
		case len(args) > 2 && args[0] == "lfs" && args[1] == "push" && args[2] == "--dry-run":
			return &ExecResult{Stdout: "push 111 => one\n"}, nil, true
		case len(args) > 1 && args[0] == "lfs" && args[1] == "push":
			pushed = append(pushed, strings.Join(args[2:], " "))
			return &ExecResult{}, nil, true
		case len(args) > 0 && args[0] == "lfs":
			return &ExecResult{}, nil, true
		case len(args) > 0 && args[0] == "for-each-ref":
			return &ExecResult{Stdout: "refs/gitmsg/social/lists/reading\n\n"}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	count, err := PushLFS(t.TempDir())
	if err != nil {
		t.Fatalf("PushLFS() error = %v", err)
	}
	if count != 1 {
		t.Errorf("PushLFS() = %d, want 1", count)
	}
	want := []string{"origin --all", "origin refs/gitmsg/social/lists/reading"}
	if strings.Join(pushed, "|") != strings.Join(want, "|") {
		t.Errorf("pushed %v, want %v", pushed, want)
	}
}

// TestPushLFS_nothingToPush checks that an empty dry run pushes nothing.
func TestPushLFS_nothingToPush(t *testing.T) {
	calls := 0
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 1 && args[0] == "lfs" && args[1] == "push" {
			calls++
			return &ExecResult{}, nil, true
		}
		if len(args) > 0 && args[0] == "lfs" {
			return &ExecResult{}, nil, true
		}
		return nil, nil, false
	}))
	defer restore()

	count, err := PushLFS(t.TempDir())
	if err != nil {
		t.Fatalf("PushLFS() error = %v", err)
	}
	if count != 0 || calls != 1 {
		t.Errorf("PushLFS() = %d after %d lfs push calls, want 0 after the dry run alone", count, calls)
	}
}

// TestPushLFS_lfsMissing checks that a missing git-lfs reports an error.
func TestPushLFS_lfsMissing(t *testing.T) {
	restore := SetExecutor(mockExecutor(func(args []string) (*ExecResult, error, bool) {
		if len(args) > 0 && args[0] == "lfs" {
			return nil, errMock, true
		}
		return nil, nil, false
	}))
	defer restore()

	if _, err := PushLFS(t.TempDir()); err == nil {
		t.Error("PushLFS() error = nil without git-lfs")
	}
}
