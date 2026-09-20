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

// TestCLI_extStatus_warnsIgnoredBranch checks the warning status and init print.
func TestCLI_extStatus_warnsIgnoredBranch(t *testing.T) {
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()
	if _, stderr, code := runInProcess(t, dir, cacheDir, "pm", "init"); code != 0 {
		t.Fatalf("pm init: exit %d\n%s", code, stderr)
	}
	if _, stderr, code := runInProcess(t, dir, cacheDir, "pm", "config", "set", "branch", "feat/pm"); code != 0 {
		t.Fatalf("pm config set branch: exit %d\n%s", code, stderr)
	}

	want := "warning: pm ignores the configured branch feat/pm: run git branch -m feat/pm gitmsg/pm"
	_, stderr, code := runInProcess(t, dir, cacheDir, "pm", "status")
	if code != 0 {
		t.Fatalf("pm status: exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, want) {
		t.Errorf("pm status stderr = %q, want %q", stderr, want)
	}
	if _, stderr, _ = runInProcess(t, dir, cacheDir, "pm", "init"); !strings.Contains(stderr, want) {
		t.Errorf("pm init stderr = %q, want %q", stderr, want)
	}
}
