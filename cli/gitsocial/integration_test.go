// integration_test.go - Binary-level CLI tests: JSON output shape, exit codes, flag validation
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce   sync.Once
	binaryPath  string
	binaryErr   error
	harnessHome string
)

// cliBinary builds the gitsocial binary once per test run.
func cliBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gitsocial-cli-test-*")
		if err != nil {
			binaryErr = err
			return
		}
		binaryPath = filepath.Join(dir, "gitsocial")
		out, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput()
		if err != nil {
			binaryErr = err
			binaryPath = string(out)
			return
		}
		harnessHome, binaryErr = os.MkdirTemp("", "gitsocial-cli-home-*")
	})
	if binaryErr != nil {
		t.Fatalf("build gitsocial binary: %v\n%s", binaryErr, binaryPath)
	}
	return binaryPath
}

// runCLI executes the binary in dir with an isolated HOME and per-call cache
// dir, returning stdout, stderr, and the exit code.
func runCLI(t *testing.T, dir, cacheDir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(cliBinary(t), append([]string{"-C", dir, "--cache-dir", cacheDir}, args...)...)
	cmd.Env = append(os.Environ(),
		"HOME="+harnessHome,
		"XDG_CONFIG_HOME="+filepath.Join(harnessHome, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(harnessHome, ".cache"),
		"GIT_TERMINAL_PROMPT=0",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return stdout.String(), stderr.String(), code
}

// initCLITestRepo creates a git repo with an initial commit and origin remote.
func initCLITestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "cli-test@test.com"},
		{"config", "user.name", "CLI Test"},
		{"commit", "--allow-empty", "-m", "init"},
		{"remote", "add", "origin", "https://github.com/test/cli-repo.git"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestCLI_notARepo_exitCode(t *testing.T) {
	dir := t.TempDir()
	_, _, code := runCLI(t, dir, t.TempDir(), "pm", "init")
	if code != ExitNotRepo {
		t.Errorf("pm init outside a repo: exit %d, want %d", code, ExitNotRepo)
	}
}

func TestCLI_unknownCommand_exitCode(t *testing.T) {
	_, stderr, code := runCLI(t, t.TempDir(), t.TempDir(), "no-such-command")
	if code == 0 {
		t.Error("unknown command should exit non-zero")
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("stderr = %q, want mention of unknown command", stderr)
	}
}

func TestCLI_pmIssueRoundTrip_JSON(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()

	stdout, stderr, code := runCLI(t, dir, cacheDir, "--json", "pm", "init")
	if code != 0 {
		t.Fatalf("pm init: exit %d\n%s%s", code, stdout, stderr)
	}
	var initOut map[string]string
	if err := json.Unmarshal([]byte(stdout), &initOut); err != nil {
		t.Fatalf("pm init --json output is not JSON: %v\n%s", err, stdout)
	}
	if initOut["status"] != "initialized" || initOut["branch"] != "gitmsg/pm" {
		t.Errorf("pm init JSON = %v", initOut)
	}

	stdout, stderr, code = runCLI(t, dir, cacheDir, "--json", "pm", "issue", "create", "Test issue subject", "--labels", "kind/bug")
	if code != 0 {
		t.Fatalf("issue create: exit %d\n%s%s", code, stdout, stderr)
	}
	var created struct {
		ID      string
		Subject string
		State   string
	}
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("issue create --json output is not JSON: %v\n%s", err, stdout)
	}
	if created.ID == "" || created.Subject != "Test issue subject" || created.State != "open" {
		t.Errorf("issue create JSON = %+v", created)
	}

	stdout, stderr, code = runCLI(t, dir, cacheDir, "--json", "pm", "issue", "list")
	if code != 0 {
		t.Fatalf("issue list: exit %d\n%s%s", code, stdout, stderr)
	}
	var issues []struct {
		ID      string
		Subject string
		State   string
		Labels  []struct{ Scope, Value string }
	}
	if err := json.Unmarshal([]byte(stdout), &issues); err != nil {
		t.Fatalf("issue list --json output is not a JSON array: %v\n%s", err, stdout)
	}
	if len(issues) != 1 {
		t.Fatalf("issue list returned %d issues, want 1\n%s", len(issues), stdout)
	}
	if issues[0].Subject != "Test issue subject" || issues[0].State != "open" {
		t.Errorf("listed issue = %+v", issues[0])
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0].Scope != "kind" || issues[0].Labels[0].Value != "bug" {
		t.Errorf("listed labels = %+v, want kind/bug", issues[0].Labels)
	}

	stdout, stderr, code = runCLI(t, dir, cacheDir, "--json", "pm", "issue", "show", created.ID)
	if code != 0 {
		t.Fatalf("issue show: exit %d\n%s%s", code, stdout, stderr)
	}
	var shown struct{ ID, Subject string }
	if err := json.Unmarshal([]byte(stdout), &shown); err != nil {
		t.Fatalf("issue show --json output is not JSON: %v\n%s", err, stdout)
	}
	if shown.Subject != "Test issue subject" {
		t.Errorf("shown issue = %+v", shown)
	}
}

func TestCLI_pmIssueShow_notFound(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()
	runCLI(t, dir, cacheDir, "pm", "init")
	_, stderr, code := runCLI(t, dir, cacheDir, "pm", "issue", "show", "#commit:000000000000")
	if code != ExitError {
		t.Errorf("issue show missing: exit %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("stderr = %q, want mention of not found", stderr)
	}
}

func TestCLI_socialPostRoundTrip_JSON(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()

	if _, stderr, code := runCLI(t, dir, cacheDir, "social", "init"); code != 0 {
		t.Fatalf("social init: exit %d\n%s", code, stderr)
	}
	stdout, stderr, code := runCLI(t, dir, cacheDir, "--json", "social", "post", "Hello from the integration test")
	if code != 0 {
		t.Fatalf("social post: exit %d\n%s%s", code, stdout, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Errorf("social post --json output is not JSON:\n%s", stdout)
	}
}

func TestCLI_importInvalidFlags(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()

	cases := []struct {
		args    []string
		wantMsg string
	}{
		{[]string{"import", "--labels", "bogus", "https://github.com/test/x"}, "--labels"},
		{[]string{"import", "--state", "opened", "https://github.com/test/x"}, "--state"},
		{[]string{"import", "--host", "guthib", "https://github.com/test/x"}, "--host"},
	}
	for _, c := range cases {
		_, stderr, code := runCLI(t, dir, cacheDir, c.args...)
		if code == 0 {
			t.Errorf("%v: exit 0, want non-zero", c.args)
		}
		if !strings.Contains(stderr, c.wantMsg) || !strings.Contains(stderr, "valid:") {
			t.Errorf("%v: stderr = %q, want invalid-flag error naming %s and valid values", c.args, stderr, c.wantMsg)
		}
	}
}

func TestCLI_statusJSON(t *testing.T) {
	dir := initCLITestRepo(t)
	stdout, stderr, code := runCLI(t, dir, t.TempDir(), "--json", "status")
	if code != 0 {
		t.Fatalf("status: exit %d\n%s%s", code, stdout, stderr)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("status --json output is not JSON: %v\n%s", err, stdout)
	}
	if len(status) == 0 {
		t.Error("status --json returned an empty object")
	}
}

func TestCLI_remoteDefault_setAndShow(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()

	// No config yet: reports the heuristic resolution (origin here).
	stdout, stderr, code := runCLI(t, dir, cacheDir, "remote", "default")
	if code != 0 {
		t.Fatalf("remote default (show): exit %d\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "heuristic: origin") {
		t.Errorf("remote default show = %q, want mention of heuristic: origin", stdout)
	}

	// Add a second remote and set it as the default.
	cmd := exec.Command("git", "-C", dir, "remote", "add", "backup", "s3://s3.example.com/bucket/repo")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add backup: %v\n%s", err, out)
	}
	_, stderr, code = runCLI(t, dir, cacheDir, "remote", "default", "backup")
	if code != 0 {
		t.Fatalf("remote default backup: exit %d\n%s", code, stderr)
	}

	// Now it reports the configured name (not the heuristic).
	stdout, _, code = runCLI(t, dir, cacheDir, "remote", "default")
	if code != 0 {
		t.Fatalf("remote default (show configured): exit %d", code)
	}
	if strings.TrimSpace(stdout) != "backup" {
		t.Errorf("remote default show = %q, want backup", strings.TrimSpace(stdout))
	}
}

func TestCLI_remoteDefault_missingRemoteErrors(t *testing.T) {
	dir := initCLITestRepo(t)
	_, stderr, code := runCLI(t, dir, t.TempDir(), "remote", "default", "ghost")
	if code == 0 {
		t.Error("remote default with a nonexistent remote should exit non-zero")
	}
	if !strings.Contains(stderr, "ghost") {
		t.Errorf("stderr = %q, want mention of the missing remote", stderr)
	}
}

// initCLIRepoWithBareRemote creates a git repo whose "origin" points at a fresh
// local bare remote (a non-s3 path, so the site step is skipped), with the
// initial commit already pushed. Returns the work dir.
func initCLIRepoWithBareRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bare := t.TempDir()
	for _, args := range [][]string{
		{"init", "--bare", "-b", "main", bare},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "cli-test@test.com"},
		{"config", "user.name", "CLI Test"},
		{"commit", "--allow-empty", "-m", "init"},
		{"remote", "add", "origin", bare},
		{"push", "origin", "main"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// TestCLI_push_noSiteOnNonS3Remote: `push --no-site` to a non-s3 remote reports
// the site as skipped and exits 0.
func TestCLI_push_noSiteOnNonS3Remote(t *testing.T) {
	dir := initCLIRepoWithBareRemote(t)
	cacheDir := t.TempDir()
	if _, stderr, code := runCLI(t, dir, cacheDir, "social", "init"); code != 0 {
		t.Fatalf("social init: exit %d\n%s", code, stderr)
	}
	if _, stderr, code := runCLI(t, dir, cacheDir, "social", "post", "hello"); code != 0 {
		t.Fatalf("social post: exit %d\n%s", code, stderr)
	}

	stdout, stderr, code := runCLI(t, dir, cacheDir, "push", "--no-site")
	if code != 0 {
		t.Fatalf("push --no-site: exit %d\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Site: skipped (--no-site)") {
		t.Errorf("push stdout = %q, want Site: skipped (--no-site)", stdout)
	}
}

// TestCLI_push_dryRunJSON: `push --dry-run --json` emits the combined result and
// touches nothing.
func TestCLI_push_dryRunJSON(t *testing.T) {
	dir := initCLIRepoWithBareRemote(t)
	cacheDir := t.TempDir()
	if _, stderr, code := runCLI(t, dir, cacheDir, "social", "init"); code != 0 {
		t.Fatalf("social init: exit %d\n%s", code, stderr)
	}
	if _, stderr, code := runCLI(t, dir, cacheDir, "social", "post", "hello"); code != 0 {
		t.Fatalf("social post: exit %d\n%s", code, stderr)
	}

	stdout, stderr, code := runCLI(t, dir, cacheDir, "--json", "push", "--dry-run")
	if code != 0 {
		t.Fatalf("push --dry-run --json: exit %d\n%s%s", code, stdout, stderr)
	}
	var res struct {
		Push struct {
			Remote string `json:"remote"`
		} `json:"push"`
		Site struct {
			Published bool `json:"published"`
		} `json:"site"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("push --dry-run --json output is not JSON: %v\n%s", err, stdout)
	}
	if res.Push.Remote != "origin" {
		t.Errorf("push.remote = %q, want origin", res.Push.Remote)
	}
	if res.Site.Published {
		t.Error("dry-run should not publish a site")
	}
}

// TestCLI_push_multiRemote_oneFailingContinues: `push broken good` reports the
// failing remote and exits non-zero, but the healthy remote still receives the
// data (a failed remote must not block the others).
func TestCLI_push_multiRemote_oneFailingContinues(t *testing.T) {
	dir := t.TempDir()
	good := t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", "-b", "main", good).CombinedOutput(); err != nil {
		t.Fatalf("init good bare: %v\n%s", err, out)
	}
	broken := t.TempDir() // an empty dir, not a git repo → pushes to it fail
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "cli-test@test.com"},
		{"config", "user.name", "CLI Test"},
		{"commit", "--allow-empty", "-m", "init"},
		{"remote", "add", "good", good},
		{"remote", "add", "broken", broken},
		{"push", "good", "main"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cacheDir := t.TempDir()
	if _, stderr, code := runCLI(t, dir, cacheDir, "social", "init"); code != 0 {
		t.Fatalf("social init: exit %d\n%s", code, stderr)
	}
	if _, stderr, code := runCLI(t, dir, cacheDir, "social", "post", "hello"); code != 0 {
		t.Fatalf("social post: exit %d\n%s", code, stderr)
	}

	// broken first: it must fail, yet the good remote still gets the push.
	stdout, stderr, code := runCLI(t, dir, cacheDir, "push", "--no-site", "broken", "good")
	if code == 0 {
		t.Errorf("a failing remote must make push exit non-zero\n%s%s", stdout, stderr)
	}
	if out, err := exec.Command("git", "-C", good, "rev-parse", "--verify", "refs/heads/gitmsg/social").CombinedOutput(); err != nil {
		t.Errorf("good remote did not receive the push despite a failing peer: %v\n%s", err, out)
	}
}
