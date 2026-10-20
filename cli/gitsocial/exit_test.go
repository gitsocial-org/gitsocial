// exit_test.go - In-process runs of a failing command through the exit seam
package main

import (
	"strings"
	"testing"
)

// TestCLI_inProcessFailure_exitCodes runs failing commands in the test process
// and reads the exit code and the error line off the command tree.
func TestCLI_inProcessFailure_exitCodes(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantLine string
	}{
		{"not a repository", []string{"pm", "init"}, ExitNotRepo, "error: not a git repository"},
		{"unknown flag", []string{"status", "--no-such-flag"}, ExitError, "unknown flag: --no-such-flag"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, code := runInProcess(t, t.TempDir(), t.TempDir(), c.args...)
			if code != c.wantCode {
				t.Fatalf("exit code = %d, want %d", code, c.wantCode)
			}
			if line := out + errOut; !strings.Contains(line, c.wantLine) {
				t.Errorf("output = %q, want %q", line, c.wantLine)
			}
		})
	}
}
