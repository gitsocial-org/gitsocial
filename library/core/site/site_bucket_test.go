// site_bucket_test.go - the git-repo and pushed-bucket fixtures the site tests share.
package site

import (
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/objstore/membucket"
)

// The transport keys the site tests assert the site layer leaves alone; objstore owns their spelling.
const (
	bucketRefsKey = ".gitsocial/refs.json"
	infoRefsKey   = "info/refs"
	packsKey      = "objects/info/packs"
)

// gitRun runs a git command in dir with a hermetic environment (no user config,
// fixed identity) so commits are reproducible across machines. GIT_DIR is
// stripped: the helper under test exports it for its own git invocations, and
// it would otherwise redirect these commands away from dir.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_DIR=") && !strings.HasPrefix(kv, "GIT_WORK_TREE=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env,
		"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t.com",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// packTestRepo builds a repo with n commits, each adding a distinct file, and returns its directory.
func packTestRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%02d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("contents %d\n", i)), 0644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", name)
		gitRun(t, dir, "commit", "-qm", fmt.Sprintf("commit %d", i))
	}
	return dir
}

// pushPackedBucket pushes the named refs of dir through the remote helper with packing forced, and returns a client on the resulting bucket.
func pushPackedBucket(t *testing.T, dir string, refs ...string) *objstore.Client {
	t.Helper()
	bucket := membucket.New()
	srv := httptest.NewServer(bucket)
	t.Cleanup(srv.Close)
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "k")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "s")
	t.Setenv("GIT_QUIET", "1")
	t.Setenv("GITSOCIAL_S3_PACK_THRESHOLD", "1")
	gitDir := filepath.Join(dir, ".git")
	t.Setenv("GIT_DIR", gitDir)
	var batch strings.Builder
	for _, ref := range refs {
		fmt.Fprintf(&batch, "push %s:%s\n", ref, ref)
	}
	batch.WriteString("\n")
	remoteURL := "s3://" + strings.TrimPrefix(srv.URL, "http://") + "/b/"
	if err := objstore.RunHelper("", remoteURL, objstore.HelperEnv{GitDir: gitDir}, strings.NewReader(batch.String()), io.Discard, nil); err != nil {
		t.Fatalf("RunHelper: %v", err)
	}
	client, err := objstore.NewClient(objstore.Config{
		Endpoint: srv.URL, Bucket: "b", Region: "us-east-1",
		AccessKey: "k", SecretKey: "s", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}
