// errors.go - Git operation error types and detection
package git

import "errors"

var (
	ErrGitExec            = errors.New("git command failed")
	ErrGitInit            = errors.New("failed to initialize git repository")
	ErrGitCommit          = errors.New("failed to create commit")
	ErrGitRef             = errors.New("git reference error")
	ErrGitRemote          = errors.New("git remote error")
	ErrBranch             = errors.New("branch error")
	ErrCheckout           = errors.New("checkout failed")
	ErrUncommittedChanges = errors.New("uncommitted changes")
	ErrNoChanges          = errors.New("no changes to commit")
	ErrDetachedHead       = errors.New("cannot push from detached HEAD")
	ErrDiverged           = errors.New("branch has diverged")
)

type GitError struct {
	Op     string
	Args   []string
	Err    error
	Stderr string
	Code   int
}

func (e *GitError) Error() string {
	if e.Stderr != "" {
		return e.Op + ": " + e.Stderr
	}
	if e.Err != nil {
		return e.Op + ": " + e.Err.Error()
	}
	return e.Op + ": unknown error"
}

func (e *GitError) Unwrap() error {
	return e.Err
}
