// credentials_test.go - Binary-level tests for `config credentials`
// set/list/remove against the harness's isolated XDG_CONFIG_HOME.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runCLIStdin is runCLI with a piped stdin (the credentials `set` input path).
func runCLIStdin(t *testing.T, dir, cacheDir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(cliBinary(t), append([]string{"-C", dir, "--cache-dir", cacheDir}, args...)...)
	cmd.Env = append(os.Environ(),
		"HOME="+harnessHome,
		"XDG_CONFIG_HOME="+filepath.Join(harnessHome, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(harnessHome, ".cache"),
		"GIT_TERMINAL_PROMPT=0",
	)
	cmd.Stdin = strings.NewReader(stdin)
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

// TestCLI_credentials_setListRemove drives the full lifecycle: set for a remote
// name (resolved to its endpoint host), set for a bare host, masked list,
// remove, and remove-of-absent failing.
func TestCLI_credentials_setListRemove(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()
	host := "creds-test.example.com"
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "r2creds", "s3://"+host+"/bucket/repo").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	// set via a remote name, keys piped as two stdin lines.
	stdout, stderr, code := runCLIStdin(t, dir, cacheDir, "AKIATESTKEY\nsecret-value\n", "config", "credentials", "set", "r2creds")
	if code != 0 {
		t.Fatalf("credentials set: exit %d\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, host) || !strings.Contains(stdout, "AKIA…") {
		t.Errorf("set output = %q, want the resolved host and a masked key", stdout)
	}

	// set via a bare endpoint host.
	if _, stderr, code := runCLIStdin(t, dir, cacheDir, "BAREKEY12\nbare-secret\n", "config", "credentials", "set", "bare-test.example.com:9000"); code != 0 {
		t.Fatalf("credentials set (bare host): exit %d\n%s", code, stderr)
	}

	// The file lands under the isolated XDG_CONFIG_HOME with 0600.
	path := filepath.Join(harnessHome, ".config", "gitsocial", "credentials.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("credentials file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("credentials file mode = %o, want 0600", info.Mode().Perm())
	}

	// list shows hosts with masked keys, never the secret.
	stdout, _, code = runCLI(t, dir, cacheDir, "config", "credentials", "list")
	if code != 0 {
		t.Fatalf("credentials list: exit %d", code)
	}
	if !strings.Contains(stdout, host+" = AKIA…") || !strings.Contains(stdout, "bare-test.example.com:9000 = BARE…") {
		t.Errorf("list output = %q, want both hosts with masked keys", stdout)
	}
	if strings.Contains(stdout, "secret-value") || strings.Contains(stdout, "AKIATESTKEY") {
		t.Errorf("list output leaks key material: %q", stdout)
	}

	// remove drops the entry; a second remove fails.
	if _, stderr, code := runCLI(t, dir, cacheDir, "config", "credentials", "remove", host); code != 0 {
		t.Fatalf("credentials remove: exit %d\n%s", code, stderr)
	}
	stdout, _, _ = runCLI(t, dir, cacheDir, "config", "credentials", "list")
	if strings.Contains(stdout, host) {
		t.Errorf("removed host still listed: %q", stdout)
	}
	if _, _, code := runCLI(t, dir, cacheDir, "config", "credentials", "remove", host); code == 0 {
		t.Error("removing an absent host should exit non-zero")
	}
	// Leave the harness config clean for other tests.
	runCLI(t, dir, cacheDir, "config", "credentials", "remove", "bare-test.example.com:9000")
}

// TestCLI_credentials_set_missingInput: fewer than two stdin lines fails.
func TestCLI_credentials_set_missingInput(t *testing.T) {
	dir := initCLITestRepo(t)
	if _, stderr, code := runCLIStdin(t, dir, t.TempDir(), "only-one-line\n", "config", "credentials", "set", "host-only.example.com"); code == 0 {
		t.Errorf("set with one stdin line should fail\n%s", stderr)
	}
}
