// exec.go - Git command execution and output handling
package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
)

// testIsolationEnv neutralizes the host's global/system git config when running
// under `go test`, so tests don't inherit per-machine state like signing keys,
// aliases, or includeIf overrides. (Credential-helper dialog suppression is
// handled separately by GCM_INTERACTIVE=Never on every invocation.)
func testIsolationEnv() []string {
	if !testing.Testing() {
		return nil
	}
	return []string{
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	}
}

var gitTimeout = 30 * time.Second

// SetTimeout overrides the default git command timeout.
func SetTimeout(d time.Duration) { gitTimeout = d }

// ExecFunc executes a git command and returns the result.
type ExecFunc func(ctx context.Context, workdir string, args []string) (*ExecResult, error)

var (
	executorMu sync.RWMutex
	executor   ExecFunc = DefaultExec
)

// SetExecutor replaces the git executor for testing. Returns a restore function.
func SetExecutor(fn ExecFunc) func() {
	executorMu.Lock()
	prev := executor
	executor = fn
	executorMu.Unlock()
	return func() {
		executorMu.Lock()
		executor = prev
		executorMu.Unlock()
	}
}

// DefaultExec is the real git executor that spawns a subprocess.
func DefaultExec(ctx context.Context, workdir string, args []string) (*ExecResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
	cmd.Env = append(append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		// Git Credential Manager: don't pop GUI dialogs for fresh auth — cached
		// tokens still work, so private-repo fetches the user already
		// authenticated to keep functioning. Prevents surprise sign-in dialogs
		// when gitsocial fetches external repos the user has no creds for.
		"GCM_INTERACTIVE=Never",
	), testIsolationEnv()...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	if err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		log.Debug("git command failed", "args", args, "duration_ms", duration.Milliseconds(), "exit_code", exitCode)
		return nil, &GitError{
			Op:     "git " + strings.Join(args, " "),
			Args:   args,
			Err:    ErrGitExec,
			Stderr: strings.TrimSpace(stderr.String()),
			Code:   exitCode,
		}
	}

	log.Debug("git command", "args", args, "duration_ms", duration.Milliseconds())
	return &ExecResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}, nil
}

// ExecGit executes a git command and returns stdout/stderr.
func ExecGit(workdir string, args []string) (*ExecResult, error) {
	return ExecGitContext(context.Background(), workdir, args)
}

// ExecGitContext executes a git command with context for cancellation.
func ExecGitContext(ctx context.Context, workdir string, args []string) (*ExecResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, gitTimeout)
		defer cancel()
	}
	executorMu.RLock()
	fn := executor
	executorMu.RUnlock()
	return fn(ctx, workdir, args)
}

// execGitSimple executes a git command and returns only stdout.
func execGitSimple(workdir string, args []string) (string, error) {
	result, err := ExecGit(workdir, args)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

// execGitWithStdin executes a git command with data piped to stdin and returns stdout.
func execGitWithStdin(workdir string, args []string, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
	cmd.Env = append(append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	), testIsolationEnv()...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &GitError{
			Op:     "git " + strings.Join(args, " "),
			Args:   args,
			Err:    ErrGitExec,
			Stderr: strings.TrimSpace(stderr.String()),
		}
	}
	return strings.TrimSpace(stdout.String()), nil
}
