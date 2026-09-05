// main_test.go - package-wide test environment isolation.
package main

import (
	"os"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
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
	os.Exit(m.Run())
}
