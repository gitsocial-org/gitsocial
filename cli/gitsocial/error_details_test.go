// error_details_test.go - The error line carries the details after the message
package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_errorLine_carriesDetails fetches an unreachable repository and reads
// the underlying git error off the command's error line.
func TestCLI_errorLine_carriesDetails(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	missing := "file://" + filepath.Join(t.TempDir(), "missing.git")

	out, errOut, code := runInProcess(t, dir, t.TempDir(), "fetch", missing)
	if code != ExitError {
		t.Fatalf("exit code = %d, want %d", code, ExitError)
	}
	line := out + errOut
	if !strings.Contains(line, "error: fetch repository: ") {
		t.Fatalf("output = %q, want the details after the message", line)
	}
	if strings.Contains(line, "error: fetch repository\n") {
		t.Errorf("output = %q, want no bare message line", line)
	}
}
