// site_fixture_test.go - the full-tier build of the showcase site fixture

package site

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fullTierOnly skips the test unless GITSOCIAL_TEST_FULL=1 (scripts/check.sh without --quick).
func fullTierOnly(t *testing.T) {
	t.Helper()
	if os.Getenv("GITSOCIAL_TEST_FULL") == "" {
		t.Skip("full tier only: GITSOCIAL_TEST_FULL=1")
	}
}

// TestSiteFixtureBuild runs sitetest/fixture.sh and asserts the served manifest it names.
func TestSiteFixtureBuild(t *testing.T) {
	fullTierOnly(t)
	script := filepath.Join("sitetest", "fixture.sh")
	// Read the script so the test cache keys on its content, as the fixture stamp does.
	if _, err := os.ReadFile(script); err != nil {
		t.Fatalf("read %s: %v", script, err)
	}
	out, err := exec.Command("bash", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}
	manifest := filepath.Join("sitetest", ".fixture", "served", "thread-demo", ".gitsocial", "refs.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("served manifest: %v", err)
	}
}
