// release_script_test.go - The release driver's dry run, which the gate exercises without credentials
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseScript_dryRunCompletes runs scripts/release.sh --dry-run end to end and fails on any abort.
func TestReleaseScript_dryRunCompletes(t *testing.T) {
	script := filepath.Join("..", "..", "scripts", "release.sh")
	cmd := exec.Command("bash", script, "--dry-run", "v9.9.9")
	// An empty GITSOCIAL_RELEASE_ENV skips the credential file, and a binary that does not exist keeps the run off the host's config.
	cmd.Env = append(os.Environ(), "GITSOCIAL_RELEASE_ENV=", "GITSOCIAL_BINARY="+filepath.Join(t.TempDir(), "gitsocial"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/release.sh --dry-run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Release v9.9.9 complete") {
		t.Errorf("dry run stopped before the end:\n%s", out)
	}
}
