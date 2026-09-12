// main_test.go - package-wide test environment isolation.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// Test-only state: the one CLI build and isolated HOME every child-process test shares.
var (
	cliBuildOnce   sync.Once
	harnessRoot    string
	cliBinaryPath  string
	cliBuildOutput string
	cliBuildErr    error
	harnessHome    string
)

// TestMain strips the two variables a `gitsocial push` leaves in the
// environment of the gate it triggers (push -> git push -> pre-push hook ->
// scripts/check.sh), both of which these tests are sensitive to:
//
//   - the repo-redirecting variables, GIT_DIR above all, which git exports to
//     every hook. These tests build fixture repositories by spawning git
//     themselves, and a redirect overrides `git -C dir`, so the fixtures would
//     init, add and commit into the repository being pushed.
//   - GITSOCIAL_S3_DEFER_MAINTENANCE, which the CLI child these tests spawn
//     reads to skip its post-push bucket maintenance, hollowing out every
//     assertion about the site and refs that push is supposed to write.
//
// A test that wants either set does so itself with t.Setenv.
func TestMain(m *testing.M) {
	git.UnsetRedirectEnv()
	os.Unsetenv(git.DeferMaintenanceEnv)
	code := m.Run()
	// os.Exit skips deferred calls, so the shared build directory goes here.
	if harnessRoot != "" {
		os.RemoveAll(harnessRoot) // a failed removal leaves a temp dir, nothing more
	}
	os.Exit(code)
}

// cliBinary returns the gitsocial binary, building it once per test run.
func cliBinary(t *testing.T) string {
	t.Helper()
	cliBuildOnce.Do(buildCLIBinary)
	if cliBuildErr != nil {
		t.Fatalf("build gitsocial binary: %v\n%s", cliBuildErr, cliBuildOutput)
	}
	return cliBinaryPath
}

// buildCLIBinary builds the CLI into harnessRoot and prepares the isolated HOME.
func buildCLIBinary() {
	harnessRoot, cliBuildErr = os.MkdirTemp("", "gitsocial-cli-test-*")
	if cliBuildErr != nil {
		return
	}
	// The helper tests put this directory on PATH, so it holds the binary alone.
	binDir := filepath.Join(harnessRoot, "bin")
	harnessHome = filepath.Join(harnessRoot, "home")
	for _, dir := range []string{binDir, harnessHome} {
		if cliBuildErr = os.MkdirAll(dir, 0o755); cliBuildErr != nil {
			return
		}
	}
	cliBinaryPath = filepath.Join(binDir, "gitsocial")
	out, err := exec.Command("go", "build", "-o", cliBinaryPath, ".").CombinedOutput()
	if err != nil {
		cliBuildErr, cliBuildOutput = err, string(out)
	}
}

// fullTierOnly skips the test unless GITSOCIAL_TEST_FULL=1 (scripts/check.sh without --quick).
func fullTierOnly(t *testing.T) {
	t.Helper()
	if os.Getenv("GITSOCIAL_TEST_FULL") == "" {
		t.Skip("full tier only: GITSOCIAL_TEST_FULL=1")
	}
}
