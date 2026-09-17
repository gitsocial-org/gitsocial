// main_test.go - package-wide test environment isolation and the in-process CLI runner.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// Test-only state: the isolated HOME every run uses and the one CLI build the child-process tests share.
var (
	cliBuildOnce   sync.Once
	harnessRoot    string
	cliBinaryPath  string
	cliBuildOutput string
	cliBuildErr    error
	harnessHome    string
	hostHome       string
)

// TestMain points HOME and the XDG paths at an isolated directory, then strips
// the two variables a `gitsocial push` leaves in the environment of the gate it
// triggers (push -> git push -> pre-push hook -> scripts/check.sh), both of
// which these tests are sensitive to:
//
//   - the repo-redirecting variables, GIT_DIR above all, which git exports to
//     every hook. These tests build fixture repositories by spawning git
//     themselves, and a redirect overrides `git -C dir`, so the fixtures would
//     init, add and commit into the repository being pushed.
//   - GITSOCIAL_S3_DEFER_MAINTENANCE, which the CLI reads to skip its post-push
//     bucket maintenance, hollowing out every assertion about the site and refs
//     that push is supposed to write.
//
// A test that wants either set does so itself with t.Setenv.
func TestMain(m *testing.M) {
	git.UnsetRedirectEnv()
	os.Unsetenv(git.DeferMaintenanceEnv)
	if err := setupHarnessHome(); err != nil {
		fmt.Fprintf(os.Stderr, "harness home: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	// os.Exit skips deferred calls, so the shared harness directory goes here.
	if harnessRoot != "" {
		os.RemoveAll(harnessRoot) // a failed removal leaves a temp dir, nothing more
	}
	os.Exit(code)
}

// setupHarnessHome creates the harness directory and points the process environment at it.
func setupHarnessHome() error {
	root, err := os.MkdirTemp("", "gitsocial-cli-test-*")
	if err != nil {
		return err
	}
	harnessRoot = root
	harnessHome = filepath.Join(harnessRoot, "home")
	hostHome = os.Getenv("HOME")
	if err := os.MkdirAll(harnessHome, 0o755); err != nil {
		return err
	}
	for key, value := range map[string]string{
		"HOME":                harnessHome,
		"XDG_CONFIG_HOME":     filepath.Join(harnessHome, ".config"),
		"XDG_CACHE_HOME":      filepath.Join(harnessHome, ".cache"),
		"GIT_TERMINAL_PROMPT": "0",
	} {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

// runInProcess runs the command tree in the test process and returns stdout, stderr and the exit code.
func runInProcess(t *testing.T, dir, cacheDir string, args ...string) (string, string, int) {
	t.Helper()
	return runInProcessStdin(t, dir, cacheDir, "", args...)
}

// runInProcessStdin is runInProcess with stdin read from a string.
func runInProcessStdin(t *testing.T, dir, cacheDir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	// The cache is a singleton, so --cache-dir only lands on a fresh open.
	cache.Reset()
	outWriter, readStdout := capturedStream(t)
	errWriter, readStderr := capturedStream(t)
	realStdout, realStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outWriter, errWriter

	// A fresh tree per run: the global flag vars are package level.
	root := buildRootCmd()
	root.SetOut(outWriter)
	root.SetErr(errWriter)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"-C", dir, "--cache-dir", cacheDir}, args...))
	cmd, err := root.ExecuteC()
	// main prints what a command did not: cobra's own errors carry no exit code.
	var coded exitError
	if err != nil && !errors.As(err, &coded) {
		fmt.Fprintln(errWriter, cmd.ErrPrefix(), err)
		fmt.Fprintln(errWriter, cmd.UsageString())
	}

	os.Stdout, os.Stderr = realStdout, realStderr
	return readStdout(), readStderr(), exitCode(err)
}

// capturedStream returns a pipe to write to and a function returning what was written.
func capturedStream(t *testing.T) (*os.File, func() string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	done := make(chan string, 1)
	go func() {
		var buf strings.Builder
		_, _ = io.Copy(&buf, reader) // a read error ends the copy; the test reads what arrived
		done <- buf.String()
	}()
	return writer, func() string {
		_ = writer.Close()
		text := <-done
		_ = reader.Close()
		return text
	}
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

// buildCLIBinary builds the CLI into the harness directory.
func buildCLIBinary() {
	// The helper tests put this directory on PATH, so it holds the binary alone.
	binDir := filepath.Join(harnessRoot, "bin")
	if cliBuildErr = os.MkdirAll(binDir, 0o755); cliBuildErr != nil {
		return
	}
	cliBinaryPath = filepath.Join(binDir, "gitsocial")
	build := exec.Command("go", "build", "-o", cliBinaryPath, ".")
	// The build cache and the module cache live under the host's HOME.
	build.Env = append(os.Environ(), "HOME="+hostHome)
	out, err := build.CombinedOutput()
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
