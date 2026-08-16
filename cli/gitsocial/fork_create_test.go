// fork_create_test.go - Tests for `gitsocial fork create`: blobless clone, the
// origin/upstream layout, the forge fork API (gh stubbed), and the thin push
// keys an s3 destination records.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initForkUpstream creates a source repository with one commit and returns its path.
func initForkUpstream(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "upstream@test.com"},
		{"config", "user.name", "Upstream"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// gitConfigValue reads a local git config value from a repository.
func gitConfigValue(t *testing.T, repoDir, key string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoDir, "config", "--local", "--get", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// runForkCLI runs the built binary in dir with an isolated HOME, a stub
// directory prepended to PATH, and extra environment entries.
func runForkCLI(t *testing.T, dir string, stubDir string, extraEnv []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(cliBinary(t), append([]string{"-C", dir, "--cache-dir", t.TempDir()}, args...)...)
	home := t.TempDir()
	path := os.Getenv("PATH")
	if stubDir != "" {
		path = stubDir + string(os.PathListSeparator) + path
	}
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"PATH="+path,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Fork Test",
		"GIT_AUTHOR_EMAIL=fork@test.com",
		"GIT_COMMITTER_NAME=Fork Test",
		"GIT_COMMITTER_EMAIL=fork@test.com",
		"GITLAB_TOKEN=",
		"GITLAB_PRIVATE_TOKEN=",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
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

// writeGHStub writes a gh shim whose behavior is driven by the environment:
// GH_STUB_AUTH=fail makes `gh auth status` fail; every fork call is appended
// to GH_STUB_LOG.
func writeGHStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1 $2" in
  "auth status")
    [ "$GH_STUB_AUTH" = "fail" ] && { echo "You are not logged into any GitHub hosts" >&2; exit 1; }
    exit 0 ;;
  "api user")
    echo "${GH_STUB_LOGIN:-tester}"; exit 0 ;;
  "repo fork")
    echo "$@" >> "$GH_STUB_LOG"
    echo "✓ Created fork ${GH_STUB_LOGIN:-tester}/repo"
    exit 0 ;;
esac
echo "gh stub: unexpected call: $@" >&2
exit 1
`
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write gh stub: %v", err)
	}
	return dir
}

func TestForkCreate_bloblessCloneAndRemoteLayout(t *testing.T) {
	upstream := "file://" + initForkUpstream(t)
	dest := "s3://s3.example.com/bucket/my-fork"
	workdir := t.TempDir()

	stdout, stderr, code := runForkCLI(t, workdir, "", nil, "fork", "create", upstream, "--to", dest, "my-fork")
	if code != 0 {
		t.Fatalf("fork create: exit %d\n%s%s", code, stdout, stderr)
	}
	repoDir := filepath.Join(workdir, "my-fork")

	if got := gitConfigValue(t, repoDir, "remote.upstream.partialclonefilter"); got != "blob:none" {
		t.Errorf("remote.upstream.partialclonefilter = %q, want blob:none", got)
	}
	if got := gitConfigValue(t, repoDir, "remote.upstream.promisor"); got != "true" {
		t.Errorf("remote.upstream.promisor = %q, want true (the source stays the promisor after the rename)", got)
	}
	if got := gitConfigValue(t, repoDir, "remote.upstream.url"); got != upstream {
		t.Errorf("remote.upstream.url = %q, want %q", got, upstream)
	}
	if got := gitConfigValue(t, repoDir, "remote.origin.url"); got != dest {
		t.Errorf("remote.origin.url = %q, want %q", got, dest)
	}
	if got := gitConfigValue(t, repoDir, "remote.origin.gitsocial-thin"); got != "true" {
		t.Errorf("remote.origin.gitsocial-thin = %q, want true", got)
	}
	if got := gitConfigValue(t, repoDir, "remote.origin.gitsocial-upstream"); got != upstream {
		t.Errorf("remote.origin.gitsocial-upstream = %q, want %q", got, upstream)
	}

	forks, _, code := runForkCLI(t, repoDir, "", nil, "fork", "list")
	if code != 0 || !strings.Contains(forks, upstream) {
		t.Errorf("fork list = %q (exit %d), want it to register %s", forks, code, upstream)
	}
}

func TestForkCreate_fullSkipsThinKey(t *testing.T) {
	upstream := "file://" + initForkUpstream(t)
	workdir := t.TempDir()

	if stdout, stderr, code := runForkCLI(t, workdir, "", nil, "fork", "create", upstream, "--to", "s3://s3.example.com/bucket/my-fork", "--full", "my-fork"); code != 0 {
		t.Fatalf("fork create --full: exit %d\n%s%s", code, stdout, stderr)
	}
	repoDir := filepath.Join(workdir, "my-fork")
	if got := gitConfigValue(t, repoDir, "remote.origin.gitsocial-thin"); got != "" {
		t.Errorf("remote.origin.gitsocial-thin = %q, want it unset with --full", got)
	}
	if got := gitConfigValue(t, repoDir, "remote.origin.gitsocial-upstream"); got != upstream {
		t.Errorf("remote.origin.gitsocial-upstream = %q, want %q", got, upstream)
	}
}

func TestForkCreate_filterIgnoredStillClones(t *testing.T) {
	// A plain local path clones without the wire protocol, so git ignores the
	// filter entirely — the same shape as a server that cannot honor it.
	upstream := initForkUpstream(t)
	workdir := t.TempDir()

	stdout, stderr, code := runForkCLI(t, workdir, "", nil, "fork", "create", upstream, "--to", "s3://s3.example.com/bucket/my-fork", "my-fork")
	if code != 0 {
		t.Fatalf("fork create against a filter-ignoring source: exit %d\n%s%s", code, stdout, stderr)
	}
	repoDir := filepath.Join(workdir, "my-fork")
	if out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output(); err != nil || len(out) == 0 {
		t.Errorf("clone has no HEAD: %v", err)
	}
}

func TestForkCreate_noFilterClonesEverything(t *testing.T) {
	upstream := "file://" + initForkUpstream(t)
	workdir := t.TempDir()

	if stdout, stderr, code := runForkCLI(t, workdir, "", nil, "fork", "create", upstream, "--to", "s3://s3.example.com/bucket/my-fork", "--no-filter", "my-fork"); code != 0 {
		t.Fatalf("fork create --no-filter: exit %d\n%s%s", code, stdout, stderr)
	}
	repoDir := filepath.Join(workdir, "my-fork")
	if got := gitConfigValue(t, repoDir, "remote.upstream.partialclonefilter"); got != "" {
		t.Errorf("remote.upstream.partialclonefilter = %q, want unset with --no-filter", got)
	}
}

func TestForkCreate_githubUsesForkAPI(t *testing.T) {
	upstream := initForkUpstream(t)
	stubDir := writeGHStub(t)
	logFile := filepath.Join(t.TempDir(), "gh.log")
	// Rewrite the GitHub URL to the local source so the flow is exercised
	// end to end without a network: the fork API is stubbed, the clone is not.
	gitConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gitConfig, []byte("[url \"file://"+upstream+"\"]\n\tinsteadOf = https://github.com/octo/repo\n"), 0o644); err != nil {
		t.Fatalf("write gitconfig: %v", err)
	}
	env := []string{"GH_STUB_LOG=" + logFile, "GIT_CONFIG_GLOBAL=" + gitConfig, "GIT_CONFIG_NOSYSTEM=1"}
	workdir := t.TempDir()

	stdout, stderr, code := runForkCLI(t, workdir, stubDir, env, "fork", "create", "https://github.com/octo/repo", "--to", "https://github.com/tester/repo", "my-fork")
	if code != 0 {
		t.Fatalf("fork create --to github: exit %d\n%s%s", code, stdout, stderr)
	}
	logged, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("gh stub was never called for the fork: %v", err)
	}
	call := strings.TrimSpace(string(logged))
	for _, want := range []string{"repo fork", "https://github.com/octo/repo", "--clone=false", "--remote=false"} {
		if !strings.Contains(call, want) {
			t.Errorf("gh call = %q, want it to contain %q", call, want)
		}
	}
	if strings.Contains(call, "--org") {
		t.Errorf("gh call = %q, want no --org when the fork lands in the authenticated account", call)
	}

	repoDir := filepath.Join(workdir, "my-fork")
	if got := gitConfigValue(t, repoDir, "remote.origin.url"); got != "https://github.com/tester/repo" {
		t.Errorf("remote.origin.url = %q, want the created fork", got)
	}
	// A forge fork transfers nothing: no push, and no thin bookkeeping.
	if got := gitConfigValue(t, repoDir, "remote.origin.gitsocial-thin"); got != "" {
		t.Errorf("remote.origin.gitsocial-thin = %q, want unset for a forge destination", got)
	}
	out, err := exec.Command("git", "-C", repoDir, "for-each-ref", "--format=%(refname)", "refs/remotes/origin").Output()
	if err != nil {
		t.Fatalf("for-each-ref: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("origin has tracking refs %q, want none (nothing is pushed)", out)
	}
}

func TestForkCreate_githubUnauthenticatedFallsBackToInstructions(t *testing.T) {
	stubDir := writeGHStub(t)
	workdir := t.TempDir()

	stdout, stderr, code := runForkCLI(t, workdir, stubDir, []string{"GH_STUB_AUTH=fail"}, "fork", "create", "https://github.com/octo/repo")
	if code == 0 {
		t.Fatalf("fork create with an unauthenticated gh should not succeed\n%s%s", stdout, stderr)
	}
	combined := stdout + stderr
	for _, want := range []string{"Cannot create the fork automatically", "https://github.com/octo/repo/fork", "gitsocial fork create https://github.com/octo/repo --to"} {
		if !strings.Contains(combined, want) {
			t.Errorf("output = %q, want it to contain %q", combined, want)
		}
	}
	if entries, err := os.ReadDir(workdir); err != nil || len(entries) != 0 {
		t.Errorf("workdir entries = %v (err %v), want nothing cloned", entries, err)
	}
}

func TestParseGitHubForkSlug(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"✓ Created fork tester/repo\n", "tester/repo"},
		{"! tester/repo already exists\n", "tester/repo"},
		{"", ""},
		{"some unrelated output\n", ""},
	}
	for _, tt := range tests {
		if got := parseGitHubForkSlug(tt.output); got != tt.want {
			t.Errorf("parseGitHubForkSlug(%q) = %q, want %q", tt.output, got, tt.want)
		}
	}
}
