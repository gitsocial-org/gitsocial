// s3_helper_test.go - End-to-end s3:// remote helper tests: git clone and push
// via the built-in helper against an in-process S3 fixture
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// s3Fixture is a minimal path-style S3 server: GET (with ETag),
// conditional PUT (If-Match / If-None-Match: *), DELETE, and ListObjectsV2.
// It stores keys under /<bucket>/<key> and requires a SigV4-shaped
// Authorization header (shape only; it does not re-derive the signature).
type s3Fixture struct {
	bucket  string
	mu      sync.Mutex
	objects map[string][]byte
	// noConditional simulates a bucket that silently ignores conditional
	// headers (the MinIO-style gap the push probe must catch).
	noConditional bool
	// createOnlyCAS simulates Ceph RGW (DigitalOcean Spaces): If-None-Match: *
	// is enforced, but any If-Match PUT is rejected even with a matching ETag.
	createOnlyCAS bool
	// contendKey/contendValue inject one concurrent writer: the first
	// conditional PUT to contendKey finds the key rewritten underneath it.
	contendKey   string
	contendValue []byte
	contended    bool
	// blockKey, when set, holds every PUT to that bucket-relative key until
	// unblock is closed — used to freeze post-push site maintenance while the
	// test observes that git already got its ref-update report.
	blockKey string
	unblock  chan struct{}
}

func etagFor(data []byte) string {
	return fmt.Sprintf("%q", fmt.Sprintf("%x", sha256.Sum256(data))[:32])
}

func (f *s3Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=") || !strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") {
		http.Error(w, "missing or malformed SigV4 authorization", http.StatusForbidden)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/"+f.bucket)
	path = strings.TrimPrefix(path, "/")
	// Freeze a designated maintenance PUT (outside the fixture lock, so reads
	// keep flowing) until the test unblocks it.
	f.mu.Lock()
	blockKey, unblock := f.blockKey, f.unblock
	f.mu.Unlock()
	if r.Method == http.MethodPut && blockKey != "" && path == blockKey && unblock != nil {
		<-unblock
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if sum := fmt.Sprintf("%x", sha256.Sum256(body)); sum != r.Header.Get("x-amz-content-sha256") {
			http.Error(w, "payload hash mismatch", http.StatusBadRequest)
			return
		}
		if !f.noConditional {
			ifMatch := r.Header.Get("If-Match")
			ifNone := r.Header.Get("If-None-Match")
			if f.createOnlyCAS && ifMatch != "" {
				http.Error(w, "precondition failed", http.StatusPreconditionFailed)
				return
			}
			if path == f.contendKey && !f.contended && (ifMatch != "" || ifNone == "*") {
				f.objects[path] = f.contendValue
				f.contended = true
			}
			current, exists := f.objects[path]
			if ifNone == "*" && exists {
				http.Error(w, "precondition failed", http.StatusPreconditionFailed)
				return
			}
			if ifMatch != "" && (!exists || etagFor(current) != ifMatch) {
				http.Error(w, "precondition failed", http.StatusPreconditionFailed)
				return
			}
		}
		f.objects[path] = body
		w.Header().Set("ETag", etagFor(body))
		return
	case http.MethodDelete:
		delete(f.objects, path)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Query().Get("list-type") == "2" {
		prefix := r.URL.Query().Get("prefix")
		type object struct {
			Key string `xml:"Key"`
		}
		var result struct {
			XMLName  xml.Name `xml:"ListBucketResult"`
			Contents []object
		}
		keys := make([]string, 0, len(f.objects))
		for k := range f.objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			result.Contents = append(result.Contents, object{Key: k})
		}
		w.Header().Set("Content-Type", "application/xml")
		_ = xml.NewEncoder(w).Encode(result)
		return
	}
	data, ok := f.objects[path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("ETag", etagFor(data))
	_, _ = w.Write(data)
}

// object reads a key under the fixture lock (test-side accessor).
func (f *s3Fixture) object(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objects[key]
	return data, ok
}

// putObject writes a key under the fixture lock (test-side setter).
func (f *s3Fixture) putObject(key string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = data
}

// uploadRepo copies a repo's loose objects and refs into the fixture using the
// bucket layout (objects/xx/38-hex, one key per ref, HEAD).
func uploadRepo(t *testing.T, fixture *s3Fixture, repoDir, prefix string) {
	t.Helper()
	objectsDir := filepath.Join(repoDir, ".git", "objects")
	entries, err := os.ReadDir(objectsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, fan := range entries {
		if !fan.IsDir() || len(fan.Name()) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(objectsDir, fan.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			data, err := os.ReadFile(filepath.Join(objectsDir, fan.Name(), file.Name()))
			if err != nil {
				t.Fatal(err)
			}
			fixture.putObject(prefix+"objects/"+fan.Name()+"/"+file.Name(), data)
		}
	}
	out, err := exec.Command("git", "-C", repoDir, "for-each-ref", "--format=%(refname) %(objectname)").Output()
	if err != nil {
		t.Fatal(err)
	}
	refCount := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, sha, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		fixture.putObject(prefix+name, []byte(sha+"\n"))
		refCount++
	}
	if refCount == 0 {
		t.Fatal("source repo has no refs to upload")
	}
	fixture.putObject(prefix+"HEAD", []byte("ref: refs/heads/main\n"))
}

// s3AliasEnv exposes the given binary as the s3 remote helper via git's
// env-config alias mechanism — helper discovery works because git spawns
// helpers as the subcommand `git remote-s3`, which resolves aliases. This is
// the same mechanism production uses (no shim files, no PATH changes).
func s3AliasEnv(binary string) []string {
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=alias.remote-s3",
		`GIT_CONFIG_VALUE_0=!"` + binary + `" __git-remote-s3`,
	}
}

// s3HelperEnv builds the environment for git to reach the fixture through the
// built-in helper: the helper alias, the dev endpoint override (http +
// path-style), and creds.
func s3HelperEnv(t *testing.T, serverURL string, baseEnv []string) []string {
	t.Helper()
	env := append(append([]string(nil), baseEnv...), s3AliasEnv(cliBinary(t))...)
	return append(env,
		"GITSOCIAL_S3_ENDPOINT="+serverURL,
		"GITSOCIAL_S3_PATH_STYLE=1",
		"AWS_ACCESS_KEY_ID=test-access-key",
		"AWS_SECRET_ACCESS_KEY=test-secret-key",
	)
}

// s3FixtureRemote builds a canonical host-form remote URL for the fixture
// server (its host:port is the endpoint host; the scheme and addressing come
// from the GITSOCIAL_S3_ENDPOINT/PATH_STYLE env override).
func s3FixtureRemote(serverURL, bucketAndPrefix string) string {
	return "s3://" + strings.TrimPrefix(serverURL, "http://") + "/" + bucketAndPrefix
}

// gitIn runs git in dir with the helper shim on PATH and fixture env, failing the test on error.
func gitIn(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestS3Helper_cloneRoundTrip(t *testing.T) {
	// Source repo: a commit on main, a gitmsg/social branch with a GitMsg
	// message, and a per-element state ref — the shapes that must round-trip.
	src := initCLITestRepo(t)
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("s3 spike\n"), 0644); err != nil {
		t.Fatal(err)
	}
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	gitIn(t, src, baseEnv, "add", "README.md")
	gitIn(t, src, baseEnv, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test", "commit", "-m", "add readme")
	gitIn(t, src, baseEnv, "branch", "gitmsg/social", "main")
	gitIn(t, src, baseEnv, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test",
		"commit", "--allow-empty", "-m", "Hello from S3\n\nGitMsg: ext=\"social\" v=\"0.1.0\" type=\"post\"", "--", ".")
	// Move the post commit onto gitmsg/social where it belongs.
	postSHA := strings.TrimSpace(gitIn(t, src, baseEnv, "rev-parse", "HEAD"))
	gitIn(t, src, baseEnv, "update-ref", "refs/heads/gitmsg/social", postSHA)
	gitIn(t, src, baseEnv, "reset", "--hard", "HEAD~1")
	mainSHA := strings.TrimSpace(gitIn(t, src, baseEnv, "rev-parse", "main"))
	gitIn(t, src, baseEnv, "update-ref", "refs/gitmsg/core/forks/deadbeef", mainSHA)

	// Serve it from the in-process S3 fixture.
	fixture := &s3Fixture{bucket: "spike-bucket", objects: map[string][]byte{}}
	server := httptest.NewServer(fixture)
	defer server.Close()
	uploadRepo(t, fixture, src, "myrepo/")

	env := s3HelperEnv(t, server.URL, baseEnv)

	// The acceptance bar: clone via s3:// and fetch the gitmsg refs.
	workdir := t.TempDir()
	gitIn(t, workdir, env, "clone", s3FixtureRemote(server.URL, "spike-bucket/myrepo"), "clone")
	clone := filepath.Join(workdir, "clone")
	gitIn(t, clone, env, "fetch", "origin", "refs/gitmsg/*:refs/gitmsg/*")

	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "main")); got != mainSHA {
		t.Errorf("cloned main = %s, want %s", got, mainSHA)
	}
	readme, err := os.ReadFile(filepath.Join(clone, "README.md"))
	if err != nil || string(readme) != "s3 spike\n" {
		t.Errorf("README content = %q (err %v)", readme, err)
	}
	// Custom refs round-trip: the social branch (SHA-exact, message intact)
	// and the per-element state ref.
	remoteSocial := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "refs/remotes/origin/gitmsg/social"))
	if remoteSocial != postSHA {
		t.Errorf("cloned gitmsg/social = %s, want %s (SHA must not be rewritten)", remoteSocial, postSHA)
	}
	msg := gitIn(t, clone, env, "log", "-1", "--format=%B", remoteSocial)
	if !strings.Contains(msg, `GitMsg: ext="social"`) {
		t.Errorf("GitMsg trailer lost in transit: %q", msg)
	}
	forkRef := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "refs/gitmsg/core/forks/deadbeef"))
	if forkRef != mainSHA {
		t.Errorf("state ref = %s, want %s", forkRef, mainSHA)
	}
	// Object store integrity.
	gitIn(t, clone, env, "fsck", "--strict")
}

// TestS3Helper_pushRoundTrip covers the push side: push a repo (branches, an
// annotated tag, gitmsg branches and state refs) into an empty bucket, clone
// it back, push an incremental commit, fetch it, and delete a ref.
func TestS3Helper_pushRoundTrip(t *testing.T) {
	src := initCLITestRepo(t)
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	commitEnv := []string{"-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test"}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("pushed via s3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, baseEnv, "add", "README.md")
	gitIn(t, src, baseEnv, append(commitEnv, "commit", "-m", "add readme")...)
	gitIn(t, src, baseEnv, "branch", "gitmsg/social", "main")
	gitIn(t, src, baseEnv, "switch", "gitmsg/social")
	gitIn(t, src, baseEnv, append(commitEnv, "commit", "--allow-empty", "-m", "A post\n\nGitMsg: ext=\"social\" v=\"0.1.0\" type=\"post\"")...)
	gitIn(t, src, baseEnv, "switch", "main")
	gitIn(t, src, baseEnv, append(commitEnv, "tag", "-a", "v1.0.0", "-m", "release v1.0.0")...)
	mainSHA := strings.TrimSpace(gitIn(t, src, baseEnv, "rev-parse", "main"))
	socialSHA := strings.TrimSpace(gitIn(t, src, baseEnv, "rev-parse", "gitmsg/social"))
	tagSHA := strings.TrimSpace(gitIn(t, src, baseEnv, "rev-parse", "v1.0.0"))
	gitIn(t, src, baseEnv, "update-ref", "refs/gitmsg/core/forks/cafe0001", mainSHA)

	// Empty bucket; everything below arrives via push.
	fixture := &s3Fixture{bucket: "push-bucket", objects: map[string][]byte{}}
	server := httptest.NewServer(fixture)
	defer server.Close()
	env := s3HelperEnv(t, server.URL, baseEnv)
	remote := s3FixtureRemote(server.URL, "push-bucket/myrepo")

	gitIn(t, src, env, "push", remote, "main", "gitmsg/social", "v1.0.0")
	gitIn(t, src, env, "push", remote, "refs/gitmsg/core/forks/cafe0001")

	// Clone it back and verify every ref shape survived.
	workdir := t.TempDir()
	gitIn(t, workdir, env, "clone", remote, "clone")
	clone := filepath.Join(workdir, "clone")
	gitIn(t, clone, env, "fetch", "origin", "refs/gitmsg/*:refs/gitmsg/*")
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "main")); got != mainSHA {
		t.Errorf("cloned main = %s, want %s", got, mainSHA)
	}
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "refs/remotes/origin/gitmsg/social")); got != socialSHA {
		t.Errorf("cloned gitmsg/social = %s, want %s", got, socialSHA)
	}
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "v1.0.0")); got != tagSHA {
		t.Errorf("cloned tag = %s, want %s (annotated tag object must round-trip)", got, tagSHA)
	}
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "refs/gitmsg/core/forks/cafe0001")); got != mainSHA {
		t.Errorf("state ref = %s, want %s", got, mainSHA)
	}
	gitIn(t, clone, env, "fsck", "--strict")

	// Incremental push: only the new objects should need to travel.
	if err := os.WriteFile(filepath.Join(src, "CHANGES.md"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, env, "add", "CHANGES.md")
	gitIn(t, src, env, append(commitEnv, "commit", "-m", "second commit")...)
	newSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	gitIn(t, src, env, "push", remote, "main")
	gitIn(t, clone, env, "fetch", "origin")
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "refs/remotes/origin/main")); got != newSHA {
		t.Errorf("fetched incremental main = %s, want %s", got, newSHA)
	}

	// Ref deletion.
	gitIn(t, src, env, "push", remote, ":refs/gitmsg/core/forks/cafe0001")
	if _, ok := fixture.object("myrepo/refs/gitmsg/core/forks/cafe0001"); ok {
		t.Error("deleted ref key still present in bucket")
	}
}

// TestS3Helper_pushReportsBeforeSiteMaintenance is the regression for the
// end-of-push hang: on a site-enabled bucket the helper must flush its per-ref
// status report (gitremote-helpers(7): status lines + blank line) to git BEFORE
// running post-push site-artifact maintenance. If maintenance ran inside the
// status exchange, a slow/blocked maintenance pass would leave git idle waiting
// on a report whose refs already landed — the observed multi-ref-push deadlock.
// Here maintenance is frozen indefinitely; the push must still report success
// promptly (proving the report preceded maintenance), after which we unblock.
func TestS3Helper_pushReportsBeforeSiteMaintenance(t *testing.T) {
	src := initCLITestRepo(t)
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("report first\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, baseEnv, "add", "README.md")
	gitIn(t, src, baseEnv, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test", "commit", "-m", "readme")
	gitIn(t, src, baseEnv, "branch", "gitmsg/social", "main")
	gitIn(t, src, baseEnv, "branch", "feature/x", "main")

	fixture := &s3Fixture{bucket: "report-bucket", objects: map[string][]byte{}, unblock: make(chan struct{})}
	// Mark the bucket as site-enabled so post-push maintenance runs, and freeze
	// its first write (the refs manifest) so maintenance cannot progress.
	fixture.putObject("myrepo/.gitsocial/site/version", []byte("test\n"))
	fixture.mu.Lock()
	fixture.blockKey = "myrepo/.gitsocial/site/refs.json"
	fixture.mu.Unlock()
	server := httptest.NewServer(fixture)
	defer server.Close()
	env := s3HelperEnv(t, server.URL, baseEnv)
	remote := s3FixtureRemote(server.URL, "report-bucket/myrepo")

	// Run the multi-ref push; capture stderr live (git prints the ref report to
	// stderr). Maintenance is frozen, so a correct helper still reports promptly.
	cmd := exec.Command("git", "-C", src, "push", remote, "main", "gitmsg/social", "feature/x")
	cmd.Env = env
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Scan stderr incrementally — the report lines must arrive while the process
	// is still alive (maintenance frozen), so this must NOT wait for EOF/exit.
	reported := make(chan string, 1)
	go func() {
		var seen strings.Builder
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			seen.WriteString(scanner.Text())
			seen.WriteByte('\n')
			if strings.Contains(seen.String(), "main") &&
				strings.Contains(seen.String(), "gitmsg/social") &&
				strings.Contains(seen.String(), "feature/x") {
				reported <- seen.String()
				return
			}
		}
	}()
	// The report must arrive while maintenance is frozen forever. A pre-fix helper
	// only reports after maintenance (which never completes), so this times out.
	select {
	case <-reported:
		// Every ref must have landed on the bucket before the report.
		for _, key := range []string{
			"myrepo/refs/heads/main",
			"myrepo/refs/heads/gitmsg/social",
			"myrepo/refs/heads/feature/x",
		} {
			if _, ok := fixture.object(key); !ok {
				t.Errorf("ref %s not written before report", key)
			}
		}
	case <-time.After(20 * time.Second):
		close(fixture.unblock) // let the helper unwind so the process can exit
		cmd.Wait()
		t.Fatal("push did not report ref status while site maintenance was frozen — status exchange is blocked behind maintenance (the end-of-push hang)")
	}
	// Unblock maintenance and let the push finish cleanly.
	close(fixture.unblock)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("push exited non-zero after maintenance unblocked: %v", err)
	}
}

// gitInErr runs git expecting failure; returns combined output.
func gitInErr(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("git %v: expected failure, got success\n%s", args, out)
	}
	return string(out)
}

// pushTestRepo creates a repo with one commit on main and returns (dir, env,
// fixture, remote URL) wired to a fresh fixture bucket.
func pushTestRepo(t *testing.T) (string, []string, *s3Fixture, string) {
	t.Helper()
	src := initCLITestRepo(t)
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("cas tests\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, baseEnv, "add", "README.md")
	gitIn(t, src, baseEnv, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test", "commit", "-m", "base")
	fixture := &s3Fixture{bucket: "cas-bucket", objects: map[string][]byte{}}
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	env := s3HelperEnv(t, server.URL, baseEnv)
	return src, env, fixture, s3FixtureRemote(server.URL, "cas-bucket/repo")
}

func commitIn(t *testing.T, dir string, env []string, file, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(msg+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, env, "add", file)
	gitIn(t, dir, env, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test", "commit", "-m", msg)
}

// TestS3Helper_nonFastForwardRejected: a stale writer must not clobber a ref
// another writer advanced; a force push may.
func TestS3Helper_nonFastForwardRejected(t *testing.T) {
	src, env, _, remote := pushTestRepo(t)
	gitIn(t, src, env, "push", remote, "main")

	// Writer B clones and advances main.
	workdir := t.TempDir()
	gitIn(t, workdir, env, "clone", remote, "b")
	cloneB := filepath.Join(workdir, "b")
	commitIn(t, cloneB, env, "b.txt", "from B")
	gitIn(t, cloneB, env, "push", remote, "main")
	bSHA := strings.TrimSpace(gitIn(t, cloneB, env, "rev-parse", "main"))

	// Stale writer A diverges and pushes without fetching: rejected (B's tip
	// is unknown locally).
	commitIn(t, src, env, "a.txt", "from A")
	out := gitInErr(t, src, env, "push", remote, "main")
	if !strings.Contains(out, "fetch first") {
		t.Errorf("stale push output = %q, want 'fetch first' rejection", out)
	}
	// After fetching, still divergent: non-fast-forward.
	gitIn(t, src, env, "fetch", remote, "refs/heads/main:refs/remotes/origin/main")
	out = gitInErr(t, src, env, "push", remote, "main")
	if !strings.Contains(out, "non-fast-forward") {
		t.Errorf("divergent push output = %q, want non-fast-forward rejection", out)
	}
	// Force push wins.
	gitIn(t, src, env, "push", "--force", remote, "main")
	aSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	if aSHA == bSHA {
		t.Fatal("test setup broken: A and B produced the same commit")
	}
	gitIn(t, cloneB, env, "fetch", "origin")
	if got := strings.TrimSpace(gitIn(t, cloneB, env, "rev-parse", "refs/remotes/origin/main")); got != aSHA {
		t.Errorf("after force push remote main = %s, want %s", got, aSHA)
	}
}

// TestS3Helper_casRetryOnContention: a concurrent fast-forwardable write lands
// between the helper's read and write; the CAS loop must retry and converge.
func TestS3Helper_casRetryOnContention(t *testing.T) {
	src, env, fixture, remote := pushTestRepo(t)
	gitIn(t, src, env, "push", remote, "main")
	oldSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))

	commitIn(t, src, env, "next.txt", "next")
	newSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))

	// Inject: first conditional PUT to the ref key finds it rewritten (same
	// old value, new ETag) — a torn-write race the CAS must absorb.
	fixture.mu.Lock()
	fixture.contendKey = "repo/refs/heads/main"
	fixture.contendValue = []byte(oldSHA + "\n")
	fixture.mu.Unlock()

	gitIn(t, src, env, "push", remote, "main")
	fixture.mu.Lock()
	contended := fixture.contended
	fixture.mu.Unlock()
	if !contended {
		t.Fatal("contention hook never fired; CAS path untested")
	}
	if data, _ := fixture.object("repo/refs/heads/main"); strings.TrimSpace(string(data)) != newSHA {
		t.Errorf("ref after contended push = %q, want %s", strings.TrimSpace(string(data)), newSHA)
	}
}

// TestS3Helper_forceWithLease: git conveys --force-with-lease to the helper via
// the remote-helper cas option. A matching lease authorizes a fast-forward push
// (behaves as normal) and a non-fast-forward rewrite; a lease invalidated by a
// concurrent writer BETWEEN the helper's read and its CAS write rejects with
// git's "stale info" (the write-time re-check, not just the client-side one);
// a lease stale already at list time is rejected by git client-side.
func TestS3Helper_forceWithLease(t *testing.T) {
	src, env, fixture, remote := pushTestRepo(t)
	gitIn(t, src, env, "push", remote, "main")
	baseSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))

	// Lease on a plain fast-forward: behaves as a normal push.
	commitIn(t, src, env, "ff.txt", "fast forward")
	ffSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	gitIn(t, src, env, "push", "--force-with-lease=main:"+baseSHA, remote, "main")
	if data, _ := fixture.object("repo/refs/heads/main"); strings.TrimSpace(string(data)) != ffSHA {
		t.Fatalf("ref after fast-forward lease push = %q, want %s", strings.TrimSpace(string(data)), ffSHA)
	}

	// Rewrite history: plain push is rejected non-fast-forward, the matching
	// lease authorizes it.
	gitIn(t, src, env, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test",
		"commit", "--amend", "-m", "rewritten")
	rewrittenSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	out := gitInErr(t, src, env, "push", remote, "main")
	if !strings.Contains(out, "non-fast-forward") {
		t.Fatalf("plain push after rewrite = %q, want non-fast-forward rejection", out)
	}
	gitIn(t, src, env, "push", "--force-with-lease=main:"+ffSHA, remote, "main")
	if data, _ := fixture.object("repo/refs/heads/main"); strings.TrimSpace(string(data)) != rewrittenSHA {
		t.Errorf("ref after lease push = %q, want %s", strings.TrimSpace(string(data)), rewrittenSHA)
	}

	// Concurrent writer moves the ref between the helper's read and its CAS
	// write: the 412 retry re-reads, sees the lease no longer holds, and rejects
	// with "stale info" instead of retrying past it.
	gitIn(t, src, env, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test",
		"commit", "--amend", "-m", "rewritten again")
	concurrentSHA := strings.Repeat("beef", 10)
	fixture.mu.Lock()
	fixture.contendKey = "repo/refs/heads/main"
	fixture.contendValue = []byte(concurrentSHA + "\n")
	fixture.mu.Unlock()
	out = gitInErr(t, src, env, "push", "--force-with-lease=main:"+rewrittenSHA, remote, "main")
	if !strings.Contains(out, "stale info") {
		t.Errorf("write-time stale lease output = %q, want stale info rejection", out)
	}
	fixture.mu.Lock()
	contended := fixture.contended
	fixture.mu.Unlock()
	if !contended {
		t.Fatal("contention hook never fired; write-time lease path untested")
	}
	if data, _ := fixture.object("repo/refs/heads/main"); strings.TrimSpace(string(data)) != concurrentSHA {
		t.Errorf("concurrent writer's ref = %q, want it preserved as %s", strings.TrimSpace(string(data)), concurrentSHA)
	}

	// Lease stale already at list time: git rejects client-side (same phrasing).
	out = gitInErr(t, src, env, "push", "--force-with-lease=main:"+rewrittenSHA, remote, "main")
	if !strings.Contains(out, "stale info") {
		t.Errorf("list-time stale lease output = %q, want stale info rejection", out)
	}
}

// TestS3Helper_generationForceWithLease: the cas lease holds in generation mode
// — a matching lease authorizes a rewrite by advancing the chain, and a
// concurrent generation taken between list and create rejects with stale info.
func TestS3Helper_generationForceWithLease(t *testing.T) {
	src, env, fixture, remote := generationTestRepo(t)
	gitIn(t, src, env, "push", remote, "main")
	baseSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))

	gitIn(t, src, env, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test",
		"commit", "--amend", "-m", "rewritten")
	rewrittenSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	out := gitInErr(t, src, env, "push", remote, "main")
	if !strings.Contains(out, "non-fast-forward") {
		t.Fatalf("plain push after rewrite = %q, want non-fast-forward rejection", out)
	}
	gitIn(t, src, env, "push", "--force-with-lease=main:"+baseSHA, remote, "main")
	if data, _ := fixture.object("repo/refs/heads/main/.gen/0000000002"); strings.TrimSpace(string(data)) != rewrittenSHA {
		t.Errorf("generation 2 after lease push = %q, want %s", strings.TrimSpace(string(data)), rewrittenSHA)
	}

	// A concurrent writer takes the next generation between the helper's list
	// and its create: the retry re-reads the new tip, the lease no longer holds.
	gitIn(t, src, env, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test",
		"commit", "--amend", "-m", "rewritten again")
	concurrentSHA := strings.Repeat("feed", 10)
	fixture.mu.Lock()
	fixture.contendKey = "repo/refs/heads/main/.gen/0000000003"
	fixture.contendValue = []byte(concurrentSHA + "\n")
	fixture.mu.Unlock()
	out = gitInErr(t, src, env, "push", "--force-with-lease=main:"+rewrittenSHA, remote, "main")
	if !strings.Contains(out, "stale info") {
		t.Errorf("write-time stale lease output = %q, want stale info rejection", out)
	}
	if data, _ := fixture.object("repo/refs/heads/main/.gen/0000000003"); strings.TrimSpace(string(data)) != concurrentSHA {
		t.Errorf("concurrent writer's generation = %q, want it preserved as %s", strings.TrimSpace(string(data)), concurrentSHA)
	}
}

// TestS3Helper_pushRejectsNonCASBucket: a bucket that ignores conditional
// headers must fail the probe before any ref is written.
func TestS3Helper_pushRejectsNonCASBucket(t *testing.T) {
	src, env, fixture, remote := pushTestRepo(t)
	fixture.mu.Lock()
	fixture.noConditional = true
	fixture.mu.Unlock()

	out := gitInErr(t, src, env, "push", remote, "main")
	if !strings.Contains(out, "does not enforce conditional writes") {
		t.Errorf("push output = %q, want loud conditional-write rejection", out)
	}
	if _, ok := fixture.object("repo/refs/heads/main"); ok {
		t.Error("ref was written despite failed conditional-write probe")
	}
}

// TestS3Helper_refModeMarkerWritten: the probed mode is recorded in the
// bucket marker (the fixture supports full conditional writes → etag).
func TestS3Helper_refModeMarkerWritten(t *testing.T) {
	src, env, fixture, remote := pushTestRepo(t)
	gitIn(t, src, env, "push", remote, "main")
	if data, ok := fixture.object("repo/.gitsocial/ref-mode"); !ok || strings.TrimSpace(string(data)) != "etag" {
		t.Errorf("ref-mode marker = %q, want %q", strings.TrimSpace(string(data)), "etag")
	}
	if _, ok := fixture.object("repo/.gitsocial/cas-probe"); ok {
		t.Error("probe key not cleaned up")
	}
}

// generationTestRepo wires pushTestRepo to a create-only-CAS fixture (the
// DigitalOcean Spaces / Ceph RGW shape).
func generationTestRepo(t *testing.T) (string, []string, *s3Fixture, string) {
	t.Helper()
	src, env, fixture, remote := pushTestRepo(t)
	fixture.mu.Lock()
	fixture.createOnlyCAS = true
	fixture.mu.Unlock()
	return src, env, fixture, remote
}

// TestS3Helper_generationPushRoundTrip: on a create-only-CAS bucket the probe
// selects generation mode; pushes, clones, GC, and deletion all round-trip.
func TestS3Helper_generationPushRoundTrip(t *testing.T) {
	src, env, fixture, remote := generationTestRepo(t)
	gitIn(t, src, env, "branch", "gitmsg/social", "main")
	mainSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	gitIn(t, src, env, "update-ref", "refs/gitmsg/core/forks/feed0001", mainSHA)

	gitIn(t, src, env, "push", remote, "main", "gitmsg/social")
	gitIn(t, src, env, "push", remote, "refs/gitmsg/core/forks/feed0001")
	if data, ok := fixture.object("repo/.gitsocial/ref-mode"); !ok || strings.TrimSpace(string(data)) != "generation" {
		t.Fatalf("ref-mode marker = %q, want %q", strings.TrimSpace(string(data)), "generation")
	}
	if data, ok := fixture.object("repo/refs/heads/main/.gen/0000000001"); !ok || strings.TrimSpace(string(data)) != mainSHA {
		t.Errorf("generation 1 = %q, want %s", strings.TrimSpace(string(data)), mainSHA)
	}
	if _, ok := fixture.object("repo/refs/heads/main"); ok {
		t.Error("generation mode must not write plain ref keys")
	}

	// Clone resolves the chain; marker removal proves reads are structural.
	fixture.mu.Lock()
	delete(fixture.objects, "repo/.gitsocial/ref-mode")
	fixture.mu.Unlock()
	workdir := t.TempDir()
	gitIn(t, workdir, env, "clone", remote, "clone")
	clone := filepath.Join(workdir, "clone")
	gitIn(t, clone, env, "fetch", "origin", "refs/gitmsg/*:refs/gitmsg/*")
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "main")); got != mainSHA {
		t.Errorf("cloned main = %s, want %s", got, mainSHA)
	}
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "refs/gitmsg/core/forks/feed0001")); got != mainSHA {
		t.Errorf("state ref = %s, want %s", got, mainSHA)
	}
	gitIn(t, clone, env, "fsck", "--strict")
	fixture.mu.Lock()
	fixture.objects["repo/.gitsocial/ref-mode"] = []byte("generation\n")
	fixture.mu.Unlock()

	// Two incremental pushes: gen 2 and 3; GC must leave exactly gens 2 and 3.
	commitIn(t, src, env, "second.md", "second")
	gitIn(t, src, env, "push", remote, "main")
	commitIn(t, src, env, "third.md", "third")
	thirdSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	gitIn(t, src, env, "push", remote, "main")
	if _, ok := fixture.object("repo/refs/heads/main/.gen/0000000001"); ok {
		t.Error("generation 1 not garbage-collected")
	}
	if _, ok := fixture.object("repo/refs/heads/main/.gen/0000000002"); !ok {
		t.Error("predecessor generation 2 must be kept")
	}
	if data, ok := fixture.object("repo/refs/heads/main/.gen/0000000003"); !ok || strings.TrimSpace(string(data)) != thirdSHA {
		t.Errorf("generation 3 = %q, want %s", strings.TrimSpace(string(data)), thirdSHA)
	}
	gitIn(t, clone, env, "fetch", "origin")
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "refs/remotes/origin/main")); got != thirdSHA {
		t.Errorf("fetched incremental main = %s, want %s", got, thirdSHA)
	}

	// Ref deletion clears the whole chain.
	gitIn(t, src, env, "push", remote, ":refs/gitmsg/core/forks/feed0001")
	if _, ok := fixture.object("repo/refs/gitmsg/core/forks/feed0001/.gen/0000000001"); ok {
		t.Error("deleted ref's generation chain still present")
	}
}

// TestS3Helper_generationContention: a concurrent writer takes the next
// generation between the helper's list and create; the loop must converge.
func TestS3Helper_generationContention(t *testing.T) {
	src, env, fixture, remote := generationTestRepo(t)
	gitIn(t, src, env, "push", remote, "main")
	oldSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))

	commitIn(t, src, env, "next.txt", "next")
	newSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))

	// Inject: the first create of generation 2 finds it already taken with a
	// fast-forwardable value; the helper must re-list and take generation 3.
	fixture.mu.Lock()
	fixture.contendKey = "repo/refs/heads/main/.gen/0000000002"
	fixture.contendValue = []byte(oldSHA + "\n")
	fixture.mu.Unlock()

	gitIn(t, src, env, "push", remote, "main")
	fixture.mu.Lock()
	contended := fixture.contended
	fixture.mu.Unlock()
	if !contended {
		t.Fatal("contention hook never fired; generation CAS path untested")
	}
	if data, _ := fixture.object("repo/refs/heads/main/.gen/0000000003"); strings.TrimSpace(string(data)) != newSHA {
		t.Errorf("generation 3 after contended push = %q, want %s", strings.TrimSpace(string(data)), newSHA)
	}
}

// TestS3Helper_generationNonFastForward: the fast-forward rules hold in
// generation mode exactly as in etag mode.
func TestS3Helper_generationNonFastForward(t *testing.T) {
	src, env, _, remote := generationTestRepo(t)
	gitIn(t, src, env, "push", remote, "main")

	workdir := t.TempDir()
	gitIn(t, workdir, env, "clone", remote, "b")
	cloneB := filepath.Join(workdir, "b")
	commitIn(t, cloneB, env, "b.txt", "from B")
	gitIn(t, cloneB, env, "push", remote, "main")

	commitIn(t, src, env, "a.txt", "from A")
	out := gitInErr(t, src, env, "push", remote, "main")
	if !strings.Contains(out, "fetch first") {
		t.Errorf("stale push output = %q, want 'fetch first' rejection", out)
	}
	gitIn(t, src, env, "fetch", remote, "refs/heads/main:refs/remotes/origin/main")
	out = gitInErr(t, src, env, "push", remote, "main")
	if !strings.Contains(out, "non-fast-forward") {
		t.Errorf("divergent push output = %q, want non-fast-forward rejection", out)
	}
	gitIn(t, src, env, "push", "--force", remote, "main")
	aSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	gitIn(t, cloneB, env, "fetch", "origin")
	if got := strings.TrimSpace(gitIn(t, cloneB, env, "rev-parse", "refs/remotes/origin/main")); got != aSHA {
		t.Errorf("after force push remote main = %s, want %s", got, aSHA)
	}
}

// TestS3Helper_rejectsNonCanonicalURLs: the only accepted s3 URL is the
// canonical host form; bucket-only authorities and query params fail loudly.
func TestS3Helper_rejectsNonCanonicalURLs(t *testing.T) {
	src, env, _, remote := pushTestRepo(t)

	out := gitInErr(t, src, env, "push", "s3://url-bucket/repo", "main")
	if !strings.Contains(out, "must name the endpoint host") {
		t.Errorf("bucket-only URL output = %q, want endpoint-host rejection", out)
	}
	out = gitInErr(t, src, env, "push", remote+"?path-style=1", "main")
	if !strings.Contains(out, "take no parameters") {
		t.Errorf("param URL output = %q, want no-parameters rejection", out)
	}
}

// TestS3Helper_zeroSetup: a workspace whose origin is an s3:// URL works
// through gitsocial with NO alias in the environment, no shim, and no global
// config — core/git's exec layer injects the helper alias itself.
func TestS3Helper_zeroSetup(t *testing.T) {
	fixture := &s3Fixture{bucket: "zero-bucket", objects: map[string][]byte{}}
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("GITSOCIAL_S3_ENDPOINT", server.URL)
	t.Setenv("GITSOCIAL_S3_PATH_STYLE", "1")

	src := initCLITestRepo(t)
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	gitIn(t, src, baseEnv, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test",
		"commit", "--allow-empty", "-m", "A post\n\nGitMsg: ext=\"social\" v=\"0.1.0\" type=\"post\"")
	postSHA := strings.TrimSpace(gitIn(t, src, baseEnv, "rev-parse", "HEAD"))
	gitIn(t, src, baseEnv, "update-ref", "refs/heads/gitmsg/social", postSHA)
	gitIn(t, src, baseEnv, "reset", "--hard", "HEAD~1")
	remote := s3FixtureRemote(server.URL, "zero-bucket/repo")
	gitIn(t, src, baseEnv, "remote", "set-url", "origin", remote)

	stdout, stderr, code := runCLI(t, src, t.TempDir(), "push")
	if code != 0 {
		t.Fatalf("gitsocial push against s3 origin: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if data, ok := fixture.object("repo/refs/heads/gitmsg/social"); !ok || strings.TrimSpace(string(data)) != postSHA {
		t.Errorf("pushed gitmsg/social = %q, want %s", strings.TrimSpace(string(data)), postSHA)
	}
	// The pre-run hook records the local alias so the user's own git works too.
	out := gitIn(t, src, baseEnv, "config", "--local", "--get", "alias.remote-s3")
	if !strings.Contains(out, "__git-remote-s3") {
		t.Errorf("local alias after gitsocial run = %q, want the s3 helper alias", strings.TrimSpace(out))
	}
}

// TestS3Helper_cloneCommand: gitsocial clone needs zero setup, derives a clean
// directory from the s3 URL, and leaves the repo self-sufficient — the local
// alias lets PLAIN git (helper binary on PATH as `gitsocial`) fetch afterwards.
func TestS3Helper_cloneCommand(t *testing.T) {
	src := initCLITestRepo(t)
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	commitIn(t, src, baseEnv, "README.md", "clone me")
	mainSHA := strings.TrimSpace(gitIn(t, src, baseEnv, "rev-parse", "main"))
	fixture := &s3Fixture{bucket: "clone-bucket", objects: map[string][]byte{}}
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	uploadRepo(t, fixture, src, "myrepo/")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("GITSOCIAL_S3_ENDPOINT", server.URL)
	t.Setenv("GITSOCIAL_S3_PATH_STYLE", "1")

	workdir := t.TempDir()
	remote := s3FixtureRemote(server.URL, "clone-bucket/myrepo")
	stdout, stderr, code := runCLI(t, workdir, t.TempDir(), "clone", remote)
	if code != 0 {
		t.Fatalf("gitsocial clone: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	clone := filepath.Join(workdir, "myrepo")
	if got := strings.TrimSpace(gitIn(t, clone, baseEnv, "rev-parse", "main")); got != mainSHA {
		t.Errorf("cloned main = %s, want %s", got, mainSHA)
	}
	out := gitIn(t, clone, baseEnv, "config", "--local", "--get", "alias.remote-s3")
	if strings.TrimSpace(out) != "!gitsocial __git-remote-s3" {
		t.Errorf("local alias = %q, want %q", strings.TrimSpace(out), "!gitsocial __git-remote-s3")
	}
	// Plain git, no injected alias env: the binary is on PATH as `gitsocial`
	// (its build name), exactly the production shape.
	plainEnv := append(append([]string(nil), baseEnv...),
		"PATH="+filepath.Dir(cliBinary(t))+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWS_ACCESS_KEY_ID=test-access-key",
		"AWS_SECRET_ACCESS_KEY=test-secret-key",
		"GITSOCIAL_S3_ENDPOINT="+server.URL,
		"GITSOCIAL_S3_PATH_STYLE=1")
	gitIn(t, clone, plainEnv, "fetch", "origin")
}

// TestS3Helper_customAliasPreserved: a user-set local alias is never overwritten.
func TestS3Helper_customAliasPreserved(t *testing.T) {
	src := initCLITestRepo(t)
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	gitIn(t, src, baseEnv, "remote", "set-url", "origin", "s3://minio.example.com/some-bucket/repo")
	gitIn(t, src, baseEnv, "config", "--local", "alias.remote-s3", "!custom-helper")

	_, _, _ = runCLI(t, src, t.TempDir(), "status")
	out := gitIn(t, src, baseEnv, "config", "--local", "--get", "alias.remote-s3")
	if strings.TrimSpace(out) != "!custom-helper" {
		t.Errorf("custom alias overwritten: %q", strings.TrimSpace(out))
	}
}

// TestS3Helper_timelineFromS3Repo: an s3 repo works as a timeline source —
// gitsocial fetch pulls its gitmsg/social branch into the cache and the post
// shows up in the timeline.
func TestS3Helper_timelineFromS3Repo(t *testing.T) {
	src := initCLITestRepo(t)
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	commitIn(t, src, baseEnv, "README.md", "s3 timeline source")
	gitIn(t, src, baseEnv, "-c", "user.email=cli-test@test.com", "-c", "user.name=CLI Test",
		"commit", "--allow-empty", "-m", "Hello from S3 timeline\n\nGitMsg: ext=\"social\" v=\"0.1.0\" type=\"post\"")
	postSHA := strings.TrimSpace(gitIn(t, src, baseEnv, "rev-parse", "HEAD"))
	gitIn(t, src, baseEnv, "update-ref", "refs/heads/gitmsg/social", postSHA)
	gitIn(t, src, baseEnv, "reset", "--hard", "HEAD~1")

	fixture := &s3Fixture{bucket: "timeline-bucket", objects: map[string][]byte{}}
	server := httptest.NewServer(fixture)
	t.Cleanup(server.Close)
	uploadRepo(t, fixture, src, "feedrepo/")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	// Short-form URL + env config: the read-path shape (identity stays the
	// short form; a canonical https URL would be the production shape).
	t.Setenv("GITSOCIAL_S3_ENDPOINT", server.URL)
	t.Setenv("GITSOCIAL_S3_PATH_STYLE", "1")

	// Real UX flow: the timeline shows repos from lists, so create a list,
	// add the s3 repo, then fetch the list. The workspace path must be
	// symlink-resolved: lists are keyed by the git root's physical path.
	workspace, err := filepath.EvalSymlinks(initCLITestRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	remote := s3FixtureRemote(server.URL, "timeline-bucket/feedrepo")
	for _, args := range [][]string{
		{"social", "init"},
		{"social", "list", "create", "s3feed"},
		{"social", "list", "add", "s3feed", remote},
		{"fetch"},
	} {
		stdout, stderr, code := runCLI(t, workspace, cacheDir, args...)
		if code != 0 {
			t.Fatalf("gitsocial %v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
		}
		t.Logf("gitsocial %v:\n%s%s", args, stdout, stderr)
	}
	stdout, stderr, code := runCLI(t, workspace, cacheDir, "social", "timeline")
	if code != 0 {
		t.Fatalf("gitsocial social timeline: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Hello from S3 timeline") {
		t.Errorf("timeline output missing the s3 repo's post:\n%s", stdout)
	}
}

// TestS3Helper_realBucket runs the full push→clone round trip against a REAL
// provider bucket. Opt-in: skipped unless GITSOCIAL_S3_TEST_HOST and
// GITSOCIAL_S3_TEST_BUCKET are set:
//
//	DO:  GITSOCIAL_S3_TEST_HOST=<region>.digitaloceanspaces.com
//	AWS: GITSOCIAL_S3_TEST_HOST=s3.<region>.amazonaws.com
//	R2:  GITSOCIAL_S3_TEST_HOST=<account-id>.r2.cloudflarestorage.com
//
// plus credentials in env. Test keys live under a unique prefix and are
// deleted afterwards.
func TestS3Helper_realBucket(t *testing.T) {
	host := os.Getenv("GITSOCIAL_S3_TEST_HOST")
	bucket := os.Getenv("GITSOCIAL_S3_TEST_BUCKET")
	if host == "" || bucket == "" {
		t.Skip("set GITSOCIAL_S3_TEST_HOST + GITSOCIAL_S3_TEST_BUCKET (plus creds) to run against a real bucket")
	}
	_, region, ok := protocol.S3HostInfo(host)
	if !ok {
		region = "us-east-1"
	}
	client, err := objstore.NewClient(objstore.Config{
		Endpoint: "https://" + host, Region: region, Bucket: bucket,
	})
	if err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("gitsocial-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	remote := "s3://" + host + "/" + bucket + "/" + prefix
	t.Cleanup(func() {
		keys, err := client.List(prefix + "/")
		if err != nil {
			t.Logf("cleanup list failed: %v (keys under %s/ may remain)", err, prefix)
			return
		}
		for _, key := range keys {
			if err := client.Delete(key); err != nil {
				t.Logf("cleanup delete %s failed: %v", key, err)
			}
		}
	})

	// Helper alias in env; provider config + creds pass through from the real env.
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	env = append(env, s3AliasEnv(cliBinary(t))...)

	src := initCLITestRepo(t)
	commitIn(t, src, env, "README.md", "real bucket round trip")
	gitIn(t, src, env, "branch", "gitmsg/social", "main")
	mainSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	gitIn(t, src, env, "update-ref", "refs/gitmsg/core/forks/feed0001", mainSHA)

	// Push (exercises the conditional-write probe + CAS against the provider).
	gitIn(t, src, env, "push", remote, "main", "gitmsg/social")
	gitIn(t, src, env, "push", remote, "refs/gitmsg/core/forks/feed0001")
	// Incremental push exercises If-Match CAS on an existing ref.
	commitIn(t, src, env, "second.md", "second commit")
	newSHA := strings.TrimSpace(gitIn(t, src, env, "rev-parse", "main"))
	gitIn(t, src, env, "push", remote, "main")

	workdir := t.TempDir()
	gitIn(t, workdir, env, "clone", remote, "clone")
	clone := filepath.Join(workdir, "clone")
	gitIn(t, clone, env, "fetch", "origin", "refs/gitmsg/*:refs/gitmsg/*")
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "main")); got != newSHA {
		t.Errorf("cloned main = %s, want %s", got, newSHA)
	}
	if got := strings.TrimSpace(gitIn(t, clone, env, "rev-parse", "refs/gitmsg/core/forks/feed0001")); got != mainSHA {
		t.Errorf("state ref = %s, want %s", got, mainSHA)
	}
	gitIn(t, clone, env, "fsck", "--strict")
	t.Logf("provider round trip OK: remote=%s region=%s", remote, region)
}
