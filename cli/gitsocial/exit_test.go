// exit_test.go - In-process runs of a failing command through the exit seam
package main

import (
	"bytes"
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
			// A fresh tree per case: each tree carries its own flag values.
			root := buildRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(append([]string{"-C", t.TempDir(), "--cache-dir", t.TempDir()}, c.args...))

			err := root.Execute()
			if code := exitCode(err); code != c.wantCode {
				t.Fatalf("exit code = %d, want %d (error %v)", code, c.wantCode, err)
			}
			line := out.String()
			if err != nil {
				line += err.Error()
			}
			if !strings.Contains(line, c.wantLine) {
				t.Errorf("output = %q, want %q", line, c.wantLine)
			}
		})
	}
}
