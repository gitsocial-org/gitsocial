// serve_test.go - tests for gitsocial serve: its environment, its bucket and its exit
package main

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// lockedBuffer collects the output of a child across goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends under the lock.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String reads under the lock.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// serveChild is one running serve: the process, its output and the URL it serves.
type serveChild struct {
	cmd     *exec.Cmd
	stdout  *lockedBuffer
	stderr  *lockedBuffer
	url     string
	waited  chan error
	stopped bool
}

// serveRepo creates a workspace with an https origin and one post, and returns it with its cache directory.
func serveRepo(t *testing.T) (dir, cacheDir string) {
	t.Helper()
	dir, cacheDir = initCLITestRepo(t), t.TempDir()
	for _, args := range [][]string{{"social", "init"}, {"social", "post", "hello"}} {
		if _, stderr, code := runCLI(t, dir, cacheDir, args...); code != 0 {
			t.Fatalf("%v: exit %d\n%s", args, code, stderr)
		}
	}
	return dir, cacheDir
}

// startServe runs serve on an ephemeral port and returns once it prints the serving line.
func startServe(t *testing.T, dir, cacheDir string) *serveChild {
	t.Helper()
	c := &serveChild{stdout: &lockedBuffer{}, stderr: &lockedBuffer{}, waited: make(chan error, 1)}
	c.cmd = exec.Command(cliBinary(t), "-C", dir, "--cache-dir", cacheDir, "serve", "--addr", "127.0.0.1:0")
	c.cmd.Stdout, c.cmd.Stderr = c.stdout, c.stderr
	if err := c.cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	go func() { c.waited <- c.cmd.Wait() }()
	t.Cleanup(func() { stopServe(t, c) })
	waitFor(t, 2*time.Minute, "the serving line", func() bool {
		select {
		case err := <-c.waited:
			c.waited <- err
			t.Fatalf("serve exited before serving: %v\n%s", err, c.stderr.String())
		default:
		}
		_, after, ok := strings.Cut(c.stdout.String(), "serving ")
		if ok {
			c.url = strings.TrimSpace(strings.SplitN(after, "\n", 2)[0])
		}
		return ok
	})
	return c
}

// stopServe interrupts serve as Ctrl-C does and waits for a clean exit; a second call does nothing.
func stopServe(t *testing.T, c *serveChild) {
	t.Helper()
	if c.stopped {
		return
	}
	c.stopped = true
	_ = c.cmd.Process.Signal(os.Interrupt)
	select {
	case err := <-c.waited:
		if err != nil {
			t.Errorf("serve exit: %v\n%s", err, c.stderr.String())
		}
	case <-time.After(2 * time.Minute):
		_ = c.cmd.Process.Kill()
		t.Errorf("serve did not stop on interrupt\n%s", c.stderr.String())
	}
}

// waitFor polls cond every 100 ms until it holds or the timeout passes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// httpGet returns the status and the decoded body of one GET; the shell is stored brotli-compressed.
func httpGet(t *testing.T, target string) (int, string) {
	t.Helper()
	resp, err := http.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if resp.Header.Get("Content-Encoding") == "br" {
		if body, err = objstore.BrotliDecompress(body); err != nil {
			t.Fatalf("decode %s: %v", target, err)
		}
	}
	return resp.StatusCode, string(body)
}

// gitOut runs git in dir and returns its output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git.ExecGit(dir, args)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out.Stdout
}

// serveBucketDir returns the directory the serve bucket keeps the workspace's keys under.
func serveBucketDir(t *testing.T, dir, cacheDir string) string {
	t.Helper()
	root, err := git.GetRootDir(dir)
	if err != nil {
		t.Fatalf("root of %s: %v", dir, err)
	}
	return filepath.Join(cacheDir, "serve", fetch.URLHash(root), serveBucket, filepath.Base(root))
}

// TestServeEnv_AppendsConfig: the serve remote joins the GIT_CONFIG_* entries already in the environment, and both stay in effect.
func TestServeEnv_AppendsConfig(t *testing.T) {
	t.Parallel()
	dir := initCLITestRepo(t)
	base := append(os.Environ(), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=Existing Entry")
	env := git.AppendConfigEnv(base, serveConfigPairs("s3://127.0.0.1:1/gitsocial/repo")...)
	for key, want := range map[string]string{
		"user.name":                    "Existing Entry",
		"remote.gitsocial-serve.url":   "s3://127.0.0.1:1/gitsocial/repo",
		"remote.gitsocial-serve.fetch": "+refs/heads/*:refs/remotes/gitsocial-serve/*",
		"gitsocial.pushSite":           "true",
	} {
		cmd := exec.Command("git", "-C", dir, "config", "--get", key)
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git config --get %s: %v", key, err)
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// TestServe_LoopbackDefault: the default address is loopback, an unspecified address answers on loopback, and the served URL is on 127.0.0.1.
func TestServe_LoopbackDefault(t *testing.T) {
	t.Parallel()
	host, _, err := net.SplitHostPort(newServeCmd().Flags().Lookup("addr").DefValue)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		t.Fatalf("default --addr host = %q, want loopback", host)
	}
	if got := serveHost(&net.TCPAddr{IP: net.IPv4zero, Port: 9}); got != "127.0.0.1:9" {
		t.Errorf("serveHost(0.0.0.0:9) = %q, want 127.0.0.1:9", got)
	}
	dir, cacheDir := serveRepo(t)
	c := startServe(t, dir, cacheDir)
	u, err := url.Parse(c.url)
	if err != nil || u.Hostname() != "127.0.0.1" {
		t.Fatalf("serving %q, want a 127.0.0.1 URL", c.url)
	}
	if status, body := httpGet(t, c.url); status != 200 || !strings.Contains(body, "<title>gitsocial</title>") {
		t.Errorf("GET %s = %d, want 200 with the app shell", c.url, status)
	}
}

// TestServe_NoConfigChange: a run leaves the git config, the remotes and the push resolution as they were.
func TestServe_NoConfigChange(t *testing.T) {
	t.Parallel()
	dir, cacheDir := serveRepo(t)
	configBefore := gitOut(t, dir, "config", "--local", "--list")
	remotesBefore, reasonBefore := git.ResolvePushRemotes(dir)
	c := startServe(t, dir, cacheDir)
	stopServe(t, c)
	if configAfter := gitOut(t, dir, "config", "--local", "--list"); configAfter != configBefore {
		t.Errorf("git config changed:\nbefore:\n%s\nafter:\n%s", configBefore, configAfter)
	}
	remotesAfter, reasonAfter := git.ResolvePushRemotes(dir)
	if strings.Join(remotesAfter, ",") != strings.Join(remotesBefore, ",") || reasonAfter != reasonBefore {
		t.Errorf("push resolution = %v (%s), want %v (%s)", remotesAfter, reasonAfter, remotesBefore, reasonBefore)
	}
	if len(remotesBefore) != 1 || remotesBefore[0] != "origin" || reasonBefore != git.PushOrigin {
		t.Errorf("push resolution = %v (%s), want origin by the heuristic", remotesBefore, reasonBefore)
	}
	if remotes := gitOut(t, dir, "remote"); strings.Contains(remotes, serveRemote) {
		t.Errorf("git remote lists %q:\n%s", serveRemote, remotes)
	}
}

// TestServe_NoPages: with pages and a url configured, the served site has the shell and no HTML page.
func TestServe_NoPages(t *testing.T) {
	t.Parallel()
	dir, cacheDir := serveRepo(t)
	for _, kv := range [][2]string{{"publish", "true"}, {"url", "https://example.org/"}, {"pages", "true"}} {
		if _, stderr, code := runCLI(t, dir, cacheDir, "config", "site", "set", kv[0], kv[1]); code != 0 {
			t.Fatalf("config site set %s: exit %d\n%s", kv[0], code, stderr)
		}
	}
	c := startServe(t, dir, cacheDir)
	status, body := httpGet(t, c.url)
	if status != 200 || !strings.Contains(body, "<title>gitsocial</title>") || strings.Contains(body, `id="gs-page"`) {
		t.Errorf("GET %s = %d, want the app shell and not a page", c.url, status)
	}
	bucket := serveBucketDir(t, dir, cacheDir)
	err := filepath.WalkDir(bucket, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, bucket+string(filepath.Separator)))
		if rel == "index.html" || strings.HasPrefix(rel, ".gitsocial/") {
			return nil
		}
		if strings.HasSuffix(rel, ".html") || strings.HasPrefix(rel, "i/") || strings.HasPrefix(rel, "sitemap") {
			t.Errorf("page %s is in the serve bucket", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", bucket, err)
	}
}

// TestServe_portInUse: a taken port ends serve with a message that names --addr.
func TestServe_portInUse(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	dir := initCLITestRepo(t)
	err = runServe(context.Background(), &Config{WorkDir: dir, CacheDir: t.TempDir()}, ln.Addr().String(), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--addr") {
		t.Fatalf("runServe on a taken port = %v, want an error naming --addr", err)
	}
}
