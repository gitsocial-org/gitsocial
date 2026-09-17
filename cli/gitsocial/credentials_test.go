// credentials_test.go - `config credentials` set/list/remove against a per-test XDG_CONFIG_HOME
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// credentialsConfigHome points the credentials file at a directory of this test's own.
func credentialsConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// TestCLI_credentials_setListRemove drives the full lifecycle: set for a remote
// name (resolved to its endpoint host), set for a bare host, masked list,
// remove, and remove-of-absent failing.
func TestCLI_credentials_setListRemove(t *testing.T) {
	configHome := credentialsConfigHome(t)
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()
	host := "creds-test.example.com"
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "r2creds", "s3://"+host+"/bucket/repo").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	// set via a remote name, keys piped as two stdin lines.
	stdout, stderr, code := runInProcessStdin(t, dir, cacheDir, "AKIATESTKEY\nsecret-value\n", "config", "credentials", "set", "r2creds")
	if code != 0 {
		t.Fatalf("credentials set: exit %d\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, host) || !strings.Contains(stdout, "AKIA…") {
		t.Errorf("set output = %q, want the resolved host and a masked key", stdout)
	}

	// set via a bare endpoint host.
	if _, stderr, code := runInProcessStdin(t, dir, cacheDir, "BAREKEY12\nbare-secret\n", "config", "credentials", "set", "bare-test.example.com:9000"); code != 0 {
		t.Fatalf("credentials set (bare host): exit %d\n%s", code, stderr)
	}

	// The file lands under this test's XDG_CONFIG_HOME with 0600.
	info, err := os.Stat(filepath.Join(configHome, "gitsocial", "credentials.json"))
	if err != nil {
		t.Fatalf("credentials file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("credentials file mode = %o, want 0600", info.Mode().Perm())
	}

	// list shows hosts with masked keys, never the secret.
	stdout, _, code = runInProcess(t, dir, cacheDir, "config", "credentials", "list")
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
	if _, stderr, code := runInProcess(t, dir, cacheDir, "config", "credentials", "remove", host); code != 0 {
		t.Fatalf("credentials remove: exit %d\n%s", code, stderr)
	}
	stdout, _, _ = runInProcess(t, dir, cacheDir, "config", "credentials", "list")
	if strings.Contains(stdout, host) {
		t.Errorf("removed host still listed: %q", stdout)
	}
	if _, _, code := runInProcess(t, dir, cacheDir, "config", "credentials", "remove", host); code == 0 {
		t.Error("removing an absent host should exit non-zero")
	}
}

// TestCLI_credentials_set_missingInput: fewer than two stdin lines fails.
func TestCLI_credentials_set_missingInput(t *testing.T) {
	credentialsConfigHome(t)
	dir := initCLITestRepo(t)
	if _, stderr, code := runInProcessStdin(t, dir, t.TempDir(), "only-one-line\n", "config", "credentials", "set", "host-only.example.com"); code == 0 {
		t.Errorf("set with one stdin line should fail\n%s", stderr)
	}
}
