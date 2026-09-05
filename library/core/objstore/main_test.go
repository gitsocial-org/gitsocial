// main_test.go - package-wide test environment isolation.
package objstore

import (
	"os"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// TestMain strips the two classes of environment this package's tests are
// sensitive to, both of which a `gitsocial push` puts in the environment of the
// gate it triggers (push -> git push -> pre-push hook -> scripts/check.sh):
//
//   - the repo-redirecting variables, GIT_DIR above all, which git exports to
//     every hook. They override `git -C dir`, so a fixture building a temp repo
//     would init, add and commit into the repository being pushed instead.
//   - GITSOCIAL_S3_DEFER_MAINTENANCE, which postPushMaintenance reads to skip
//     its work for all but the last transfer of a multi-push run, and which
//     would silently hollow out every test that asserts on that work.
//
// A test that wants either set does so itself with t.Setenv.
func TestMain(m *testing.M) {
	git.UnsetRedirectEnv()
	os.Unsetenv(git.DeferMaintenanceEnv)
	os.Exit(m.Run())
}
