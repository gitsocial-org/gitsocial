// ext_branch_test.go - The fixed content branch of pm, review and release
package main

import (
	"strings"
	"testing"
)

// TestCLI_extInit_noBranchFlag checks that only social still takes -b.
func TestCLI_extInit_noBranchFlag(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()
	for _, ext := range []string{"pm", "review", "release"} {
		_, stderr, code := runInProcess(t, dir, cacheDir, ext, "init", "-b", "other")
		if code == 0 {
			t.Errorf("%s init -b should be refused", ext)
		}
		if !strings.Contains(stderr, "unknown shorthand flag") {
			t.Errorf("%s init -b stderr = %q, want an unknown-flag error", ext, stderr)
		}
	}
	if _, stderr, code := runInProcess(t, dir, cacheDir, "social", "init", "-b", "gitmsg/social"); code != 0 {
		t.Errorf("social init -b: exit %d\n%s", code, stderr)
	}
}

// TestCLI_extInit_writesNoBranchKey checks that init leaves the branch key out of the config.
func TestCLI_extInit_writesNoBranchKey(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()
	for _, ext := range []string{"pm", "review", "release"} {
		if _, stderr, code := runInProcess(t, dir, cacheDir, ext, "init"); code != 0 {
			t.Fatalf("%s init: exit %d\n%s", ext, code, stderr)
		}
		stdout, stderr, code := runInProcess(t, dir, cacheDir, ext, "config", "list")
		if code != 0 {
			t.Fatalf("%s config list: exit %d\n%s", ext, code, stderr)
		}
		if strings.Contains(stdout, "branch") {
			t.Errorf("%s config list = %q, want no branch key", ext, stdout)
		}
		if !strings.Contains(stdout, "version") {
			t.Errorf("%s config list = %q, want a version key", ext, stdout)
		}
	}
}
