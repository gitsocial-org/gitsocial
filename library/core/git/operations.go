// operations.go - Core git operations for commits, refs, and repository state
package git

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	recordSep = "\x1E"
	unitSep   = "\x1F"
	emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
)

// IsRepository checks if the given directory is a git repository.
func IsRepository(workdir string) bool {
	_, err := ExecGit(workdir, []string{"rev-parse", "--git-dir"})
	return err == nil
}

// GetRootDir returns the root directory of the git repository.
func GetRootDir(workdir string) (string, error) {
	result, err := ExecGit(workdir, []string{"rev-parse", "--show-toplevel"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// GetUserEmail returns the configured git user email.
func GetUserEmail(workdir string) string {
	result, err := ExecGit(workdir, []string{"config", "user.email"})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// GetUserName returns the configured git user name.
func GetUserName(workdir string) string {
	result, err := ExecGit(workdir, []string{"config", "user.name"})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// GetGitConfig reads a git config value.
func GetGitConfig(workdir, key string) string {
	result, err := ExecGit(workdir, []string{"config", key})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// CreateSignedCommitTree creates a signed commit with empty tree, returning the full hash.
func CreateSignedCommitTree(workdir, message, parent string) (string, error) {
	args := []string{"commit-tree", emptyTree, "-m", message, "-S"}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	hash, err := execGitSimple(workdir, args)
	if err != nil {
		return "", fmt.Errorf("signed commit-tree: %w", err)
	}
	return strings.TrimSpace(hash), nil
}

// VerifyCommitSignature checks the signature on a commit.
// Returns the raw output from git verify-commit (good-sig info) or error if unsigned/invalid.
func VerifyCommitSignature(workdir, hash string) (string, error) {
	result, err := ExecGit(workdir, []string{"verify-commit", "--raw", hash})
	if err != nil {
		return "", fmt.Errorf("verify-commit %s: %w", hash, err)
	}
	// verify-commit outputs signature info to stderr
	output := result.Stderr
	if output == "" {
		output = result.Stdout
	}
	return output, nil
}

// GetCommitSignerKey extracts the signing key fingerprint from a commit.
// For SSH: returns the key fingerprint. For GPG: returns the key ID.
func GetCommitSignerKey(workdir, hash string) (format string, key string, err error) {
	result, err := ExecGit(workdir, []string{"log", "-1", "--format=%GK%n%GG", hash})
	if err != nil {
		return "", "", fmt.Errorf("get signer key: %w", err)
	}
	lines := strings.SplitN(strings.TrimSpace(result.Stdout), "\n", 2)
	if len(lines) == 0 || lines[0] == "" {
		return "", "", fmt.Errorf("commit %s is not signed", hash)
	}
	keyID := strings.TrimSpace(lines[0])
	sigInfo := ""
	if len(lines) > 1 {
		sigInfo = lines[1]
	}
	if strings.Contains(sigInfo, "ssh") || strings.HasPrefix(keyID, "SHA256:") {
		return "ssh", keyID, nil
	}
	return "gpg", keyID, nil
}

// signerKeyBatchSize bounds how many hashes are passed to a single `git log
// --no-walk` invocation. Large repos (kernel-scale: 1M+ commits) would exceed
// OS argv limits if all hashes were passed at once.
const signerKeyBatchSize = 500

// GetCommitSignerKeys returns a map of commit hash → signer key for the given hashes.
// Unsigned commits are omitted from the result. Internally batches the lookup so
// argv stays well under platform limits regardless of input size.
func GetCommitSignerKeys(workdir string, hashes []string) (map[string]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(hashes))
	for start := 0; start < len(hashes); start += signerKeyBatchSize {
		end := start + signerKeyBatchSize
		if end > len(hashes) {
			end = len(hashes)
		}
		batch := hashes[start:end]
		args := make([]string, 0, 3+len(batch))
		args = append(args, "log", "--no-walk", "--format=%H"+unitSep+"%GK")
		args = append(args, batch...)
		result, err := ExecGit(workdir, args)
		if err != nil {
			return nil, fmt.Errorf("batch get signer keys (chunk %d-%d): %w", start, end, err)
		}
		for _, line := range strings.Split(result.Stdout, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, unitSep, 2)
			if len(parts) != 2 {
				continue
			}
			fullHash := strings.TrimSpace(parts[0])
			key := strings.TrimSpace(parts[1])
			if fullHash == "" || key == "" {
				continue
			}
			out[fullHash] = key
			if len(fullHash) >= 12 {
				out[fullHash[:12]] = key
			}
		}
	}
	return out, nil
}

// Init initializes a new git repository in the given directory.
func Init(workdir string, initialBranch string) error {
	args := []string{"init"}
	if initialBranch != "" {
		args = append(args, "-b", initialBranch)
	}
	_, err := ExecGit(workdir, args)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrGitInit, err)
	}
	return nil
}

type GetCommitsOptions struct {
	Branch      string
	Limit       int
	Since       *time.Time
	Until       *time.Time
	All         bool
	IncludeRefs []string
}

// GetCommits retrieves commits from the repository with filtering options.
func GetCommits(workdir string, opts *GetCommitsOptions) ([]Commit, error) {
	if opts == nil {
		opts = &GetCommitsOptions{}
	}

	format := fmt.Sprintf("%s%%h%s%%cd%s%%an%s%%ae%s%%B%s%%S", recordSep, unitSep, unitSep, unitSep, unitSep, unitSep)
	args := []string{"log"}

	if opts.All {
		args = append(args, "--exclude=refs/gitmsg/config", "--exclude=HEAD", "--exclude=refs/remotes/*/HEAD", "--all")
	} else {
		branch := opts.Branch
		if branch == "" {
			branch = "HEAD"
		}
		_, err := ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", branch})
		if err == nil {
			args = append(args, branch)
		}
		if len(opts.IncludeRefs) > 0 {
			args = append(args, opts.IncludeRefs...)
		}
		if len(args) == 1 {
			return []Commit{}, nil
		}
	}

	if opts.Limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", opts.Limit))
	}
	args = append(args,
		fmt.Sprintf("--format=%s", format),
		"--abbrev=12",
		"--no-merges",
		"--date=iso-strict",
	)

	if opts.Since != nil {
		args = append(args, fmt.Sprintf("--since=%s", opts.Since.Format("2006-01-02")))
	}
	if opts.Until != nil {
		until := opts.Until.Add(24 * time.Hour)
		args = append(args, fmt.Sprintf("--until=%s", until.Format("2006-01-02")))
	}

	timeout := gitTimeout
	if opts.Limit == 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := ExecGitContext(ctx, workdir, args)
	if err != nil {
		slog.Debug("get commits", "error", err, "workdir", workdir)
		return []Commit{}, nil
	}

	entries := strings.Split(result.Stdout, recordSep)
	commits := make([]Commit, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.Split(entry, unitSep)
		if len(parts) < 6 {
			continue
		}

		timestamp, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
		if err != nil {
			slog.Debug("parse commit timestamp", "error", err, "hash", strings.TrimSpace(parts[0]))
		}
		commits = append(commits, Commit{
			Hash:      strings.TrimSpace(parts[0]),
			Timestamp: timestamp,
			Author:    strings.TrimSpace(parts[2]),
			Email:     strings.TrimSpace(parts[3]),
			Message:   parts[4],
			Refname:   strings.TrimSpace(parts[5]),
		})
	}

	return commits, nil
}

// CreateCommit creates a new commit and returns its hash.
func CreateCommit(workdir string, opts CommitOptions) (string, error) {
	if opts.Parent != "" {
		args := []string{"commit-tree", emptyTree, "-m", opts.Message, "-p", opts.Parent}
		hash, err := execGitSimple(workdir, args)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrGitCommit, err)
		}
		return strings.TrimSpace(hash), nil
	}

	result, err := ExecGit(workdir, []string{"status", "--porcelain"})
	if err != nil {
		return "", err
	}

	if !opts.AllowEmpty && result.Stdout == "" {
		return "", ErrNoChanges
	}

	_, err = ExecGit(workdir, []string{"add", "-A"})
	if err != nil {
		return "", err
	}

	args := []string{"commit", "-m", opts.Message}
	if opts.AllowEmpty {
		args = append(args, "--allow-empty")
	}

	_, err = ExecGit(workdir, args)
	if err != nil {
		return "", err
	}

	hash, err := execGitSimple(workdir, []string{"rev-parse", "--short=12", "HEAD"})
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(hash), nil
}

// CreateCommitOnBranch creates an empty commit directly on a branch.
func CreateCommitOnBranch(workdir, branch, message string) (string, error) {
	branchRef := "refs/heads/" + branch

	result, err := ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", branchRef})
	if err == nil && result.Stdout != "" {
		parentHash := strings.TrimSpace(result.Stdout)
		args := []string{"commit-tree", emptyTree, "-m", message, "-p", parentHash}
		commitHash, err := execGitSimple(workdir, args)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrGitCommit, err)
		}
		commitHash = strings.TrimSpace(commitHash)

		_, err = ExecGit(workdir, []string{"update-ref", branchRef, commitHash})
		if err != nil {
			return "", fmt.Errorf("%w: failed to update branch reference", ErrGitCommit)
		}

		shortHash, err := execGitSimple(workdir, []string{"rev-parse", "--short=12", commitHash})
		if err != nil {
			return commitHash[:12], nil
		}
		return strings.TrimSpace(shortHash), nil
	}

	commitHash, err := execGitSimple(workdir, []string{"commit-tree", emptyTree, "-m", message})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrGitCommit, err)
	}
	commitHash = strings.TrimSpace(commitHash)

	_, err = ExecGit(workdir, []string{"update-ref", branchRef, commitHash})
	if err != nil {
		return "", fmt.Errorf("%w: failed to create branch reference", ErrGitCommit)
	}

	shortHash, err := execGitSimple(workdir, []string{"rev-parse", "--short=12", commitHash})
	if err != nil {
		return commitHash[:12], nil
	}
	return strings.TrimSpace(shortHash), nil
}

// ReadRef reads the value of a git reference.
func ReadRef(workdir, ref string) (string, error) {
	return execGitSimple(workdir, []string{"rev-parse", "--short=12", ref})
}

// WriteRef updates a git reference to point to a value.
func WriteRef(workdir, ref, value string) error {
	_, err := ExecGit(workdir, []string{"update-ref", ref, value})
	return err
}

// GetCurrentBranch returns the name of the current branch.
func GetCurrentBranch(workdir string) (string, error) {
	return execGitSimple(workdir, []string{"rev-parse", "--abbrev-ref", "HEAD"})
}

// GetCommit retrieves a single commit by its hash.
func GetCommit(workdir, hash string) (*Commit, error) {
	commits, err := GetCommits(workdir, &GetCommitsOptions{
		Branch: hash,
		Limit:  1,
	})
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, nil
	}
	return &commits[0], nil
}

// GetUnpushedCommits returns hashes of commits not yet pushed to origin.
func GetUnpushedCommits(workdir, branch string) (map[string]struct{}, error) {
	originRef := "origin/" + branch
	_, err := ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", originRef})
	if err != nil {
		slog.Debug("no remote tracking branch", "branch", branch)
		return map[string]struct{}{}, nil
	}

	result, err := ExecGit(workdir, []string{"rev-list", "--abbrev-commit", "--abbrev=12", originRef + ".." + branch})
	if err != nil {
		slog.Debug("rev-list unpushed", "error", err, "branch", branch)
		return map[string]struct{}{}, nil
	}

	hashes := make(map[string]struct{})
	for _, h := range strings.Split(result.Stdout, "\n") {
		h = strings.TrimSpace(h)
		if h != "" {
			hashes[h] = struct{}{}
		}
	}
	return hashes, nil
}

// GetAllCommitHashes returns all commit hashes in the repository (all refs, excluding gitmsg/config).
func GetAllCommitHashes(workdir string) (map[string]bool, error) {
	result, err := ExecGit(workdir, []string{"rev-list", "--abbrev-commit", "--abbrev=12", "--exclude=refs/gitmsg/config", "--all"})
	if err != nil {
		return nil, fmt.Errorf("rev-list all: %w", err)
	}
	hashes := make(map[string]bool)
	for _, h := range strings.Split(result.Stdout, "\n") {
		h = strings.TrimSpace(h)
		if h != "" {
			hashes[h] = true
		}
	}
	return hashes, nil
}

// GetMergeBase returns the best common ancestor between two branches.
func GetMergeBase(workdir, base, head string) (string, error) {
	out, err := execGitSimple(workdir, []string{"merge-base", base, head})
	if err != nil {
		return "", fmt.Errorf("merge-base %s %s: %w", base, head, err)
	}
	return strings.TrimSpace(out), nil
}

// MergeBranches merges the head branch into the base branch.
// If the current branch is the base, uses git merge directly. Otherwise uses plumbing commands.
func MergeBranches(workdir, base, head string) (string, error) {
	current, _ := GetCurrentBranch(workdir)

	if current == base {
		_, err := ExecGit(workdir, []string{"merge", "--no-edit", head})
		if err != nil {
			if _, abortErr := ExecGit(workdir, []string{"merge", "--abort"}); abortErr != nil {
				slog.Warn("merge abort failed", "error", abortErr, "base", base, "head", head)
			}
			return "", fmt.Errorf("merge conflicts between %s and %s", base, head)
		}
		hash, err := execGitSimple(workdir, []string{"rev-parse", "HEAD"})
		if err != nil {
			return "", fmt.Errorf("resolve HEAD after merge: %w", err)
		}
		return strings.TrimSpace(hash), nil
	}

	// Check if fast-forward is possible
	_, err := ExecGit(workdir, []string{"merge-base", "--is-ancestor", "refs/heads/" + base, "refs/heads/" + head})
	if err == nil {
		headHash, err := execGitSimple(workdir, []string{"rev-parse", "refs/heads/" + head})
		if err != nil {
			return "", fmt.Errorf("resolve head branch: %w", err)
		}
		headHash = strings.TrimSpace(headHash)
		_, err = ExecGit(workdir, []string{"update-ref", "refs/heads/" + base, headHash})
		if err != nil {
			return "", fmt.Errorf("fast-forward %s: %w", base, err)
		}
		return headHash, nil
	}

	// Non-fast-forward: 3-way merge via plumbing
	treeResult, err := ExecGit(workdir, []string{"merge-tree", "--write-tree", "refs/heads/" + base, "refs/heads/" + head})
	if err != nil {
		return "", fmt.Errorf("merge conflicts between %s and %s", base, head)
	}
	tree := strings.TrimSpace(treeResult.Stdout)

	baseHash, err := execGitSimple(workdir, []string{"rev-parse", "refs/heads/" + base})
	if err != nil {
		return "", fmt.Errorf("resolve base branch: %w", err)
	}
	headHash, err := execGitSimple(workdir, []string{"rev-parse", "refs/heads/" + head})
	if err != nil {
		return "", fmt.Errorf("resolve head branch: %w", err)
	}

	msg := fmt.Sprintf("Merge branch '%s' into %s", head, base)
	commitResult, err := ExecGit(workdir, []string{
		"commit-tree", tree,
		"-p", strings.TrimSpace(baseHash),
		"-p", strings.TrimSpace(headHash),
		"-m", msg,
	})
	if err != nil {
		return "", fmt.Errorf("create merge commit: %w", err)
	}
	hash := strings.TrimSpace(commitResult.Stdout)

	_, err = ExecGit(workdir, []string{"update-ref", "refs/heads/" + base, hash})
	if err != nil {
		return "", fmt.Errorf("update %s ref: %w", base, err)
	}
	return hash, nil
}

// ListLocalBranches returns local branch names sorted by most recent commit, excluding gitmsg internal branches.
func ListLocalBranches(workdir string) ([]string, error) {
	result, err := ExecGit(workdir, []string{"for-each-ref", "--format=%(refname:short)", "--sort=-committerdate", "refs/heads/"})
	if err != nil {
		return nil, fmt.Errorf("list local branches: %w", err)
	}
	if result.Stdout == "" {
		return nil, nil
	}
	lines := strings.Split(result.Stdout, "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "gitmsg/") {
			continue
		}
		branches = append(branches, name)
	}
	return branches, nil
}

// ListRefs returns all refs in a gitmsg namespace.
func ListRefs(workdir, namespace string) ([]string, error) {
	cleanNamespace := strings.TrimPrefix(namespace, "refs/gitmsg/")
	pattern := "refs/gitmsg/"
	if cleanNamespace != "" {
		pattern = "refs/gitmsg/" + cleanNamespace
	}

	result, err := ExecGit(workdir, []string{"for-each-ref", "--format=%(refname)", pattern})
	if err != nil {
		slog.Debug("list refs", "error", err, "pattern", pattern)
		return []string{}, nil
	}

	if result.Stdout == "" {
		return []string{}, nil
	}

	var refs []string
	for _, ref := range strings.Split(result.Stdout, "\n") {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			refs = append(refs, strings.TrimPrefix(ref, "refs/gitmsg/"))
		}
	}
	return refs, nil
}

// ValidatePushPreconditions checks if push is safe to perform.
func ValidatePushPreconditions(workdir, remoteName, branch string) error {
	if remoteName == "" {
		remoteName = "origin"
	}

	_, err := ExecGit(workdir, []string{"symbolic-ref", "-q", "HEAD"})
	if err != nil {
		return ErrDetachedHead
	}

	result, err := ExecGit(workdir, []string{"remote"})
	if err != nil {
		return fmt.Errorf("%w: failed to list remotes", ErrGitRemote)
	}

	remotes := strings.Split(result.Stdout, "\n")
	found := false
	for _, r := range remotes {
		if strings.TrimSpace(r) == remoteName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: no '%s' remote configured", ErrGitRemote, remoteName)
	}

	_, err = ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", "refs/heads/" + branch})
	if err != nil {
		return fmt.Errorf("%w: branch '%s' does not exist locally", ErrBranch, branch)
	}

	originBranchRef := remoteName + "/" + branch
	_, err = ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", originBranchRef})
	if err == nil {
		result, err := ExecGit(workdir, []string{"rev-list", "--left-right", "--count", originBranchRef + "..." + branch})
		if err == nil && result.Stdout != "" {
			parts := strings.Split(result.Stdout, "\t")
			if len(parts) == 2 {
				var behind, ahead int
				if _, err := fmt.Sscanf(parts[0], "%d", &behind); err != nil {
					slog.Debug("parse behind count", "error", err, "value", parts[0])
				}
				if _, err := fmt.Sscanf(parts[1], "%d", &ahead); err != nil {
					slog.Debug("parse ahead count", "error", err, "value", parts[1])
				}
				if behind > 0 && ahead > 0 {
					return fmt.Errorf("%w: branch '%s' has diverged (%d ahead, %d behind %s)",
						ErrDiverged, branch, ahead, behind, remoteName)
				}
			}
		}
	}

	return nil
}

// GetUpstreamBranch returns the upstream tracking branch name.
func GetUpstreamBranch(workdir, localBranch string) (string, error) {
	args := []string{"rev-parse", "--abbrev-ref"}
	if localBranch != "" {
		args = append(args, localBranch+"@{upstream}")
	} else {
		args = append(args, "@{upstream}")
	}
	return execGitSimple(workdir, args)
}

// GetCommitMessage returns the full message of a commit.
func GetCommitMessage(workdir, hash string) (string, error) {
	return execGitSimple(workdir, []string{"log", "-1", "--format=%B", hash})
}

// GetCommitRange retrieves commits in the range base..head.
// Uses --first-parent to exclude upstream commits brought in via merges
// (e.g., when a fork merged upstream master into their PR branch).
func GetCommitRange(workdir, base, head string) ([]Commit, error) {
	format := fmt.Sprintf("%s%%h%s%%cd%s%%an%s%%ae%s%%B%s%%S", recordSep, unitSep, unitSep, unitSep, unitSep, unitSep)
	args := []string{
		"log",
		base + ".." + head,
		fmt.Sprintf("--format=%s", format),
		"--abbrev=12",
		"--no-merges",
		"--first-parent",
		"--date=iso-strict",
	}
	result, err := ExecGit(workdir, args)
	if err != nil {
		slog.Debug("get commit range", "error", err, "base", base, "head", head)
		return []Commit{}, nil
	}
	entries := strings.Split(result.Stdout, recordSep)
	commits := make([]Commit, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, unitSep)
		if len(parts) < 6 {
			continue
		}
		timestamp, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[1]))
		if err != nil {
			slog.Debug("parse commit timestamp", "error", err, "hash", strings.TrimSpace(parts[0]))
		}
		commits = append(commits, Commit{
			Hash:      strings.TrimSpace(parts[0]),
			Timestamp: timestamp,
			Author:    strings.TrimSpace(parts[2]),
			Email:     strings.TrimSpace(parts[3]),
			Message:   parts[4],
			Refname:   strings.TrimSpace(parts[5]),
		})
	}
	return commits, nil
}

// DeleteRef removes a git reference.
func DeleteRef(workdir, ref string) error {
	_, err := ExecGit(workdir, []string{"update-ref", "-d", ref})
	return err
}

// CreateCommitTree creates a commit using commit-tree with empty tree.
func CreateCommitTree(workdir, message, parent string) (string, error) {
	args := []string{"commit-tree", emptyTree, "-m", message}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	hash, err := execGitSimple(workdir, args)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}

// BranchExists checks if a branch exists in the repository.
func BranchExists(workdir, branch string) bool {
	_, err := ExecGit(workdir, []string{"rev-parse", "--verify", "refs/heads/" + branch})
	return err == nil
}

// GetDefaultBranch returns the repository's default branch name.
func GetDefaultBranch(workdir string) (string, error) {
	// Try HEAD first
	out, err := execGitSimple(workdir, []string{"symbolic-ref", "--short", "HEAD"})
	if err == nil && out != "" {
		return strings.TrimSpace(out), nil
	}
	// Fallback to common defaults
	if BranchExists(workdir, "main") {
		return "main", nil
	}
	if BranchExists(workdir, "master") {
		return "master", nil
	}
	return "main", nil
}

// CommitFiles creates a commit with real file content on a ref.
func CommitFiles(workdir, ref, message string, files map[string][]byte) (string, error) {
	var mktreeInput strings.Builder
	for name, data := range files {
		blobHash, err := execGitWithStdin(workdir, []string{"hash-object", "-w", "--stdin"}, string(data))
		if err != nil {
			return "", fmt.Errorf("hash-object %s: %w", name, err)
		}
		fmt.Fprintf(&mktreeInput, "100644 blob %s\t%s\n", blobHash, name)
	}
	treeHash, err := execGitWithStdin(workdir, []string{"mktree"}, mktreeInput.String())
	if err != nil {
		return "", fmt.Errorf("mktree: %w", err)
	}
	args := []string{"commit-tree", treeHash, "-m", message}
	parentHash, err := execGitSimple(workdir, []string{"rev-parse", "--verify", ref})
	if err == nil && parentHash != "" {
		args = append(args, "-p", strings.TrimSpace(parentHash))
	}
	commitHash, err := execGitSimple(workdir, args)
	if err != nil {
		return "", fmt.Errorf("commit-tree: %w", err)
	}
	commitHash = strings.TrimSpace(commitHash)
	if _, err := ExecGit(workdir, []string{"update-ref", ref, commitHash}); err != nil {
		return "", fmt.Errorf("update-ref %s: %w", ref, err)
	}
	shortHash, err := execGitSimple(workdir, []string{"rev-parse", "--short=12", commitHash})
	if err != nil {
		if len(commitHash) >= 12 {
			return commitHash[:12], nil
		}
		return commitHash, nil
	}
	return strings.TrimSpace(shortHash), nil
}

// RangeDiff runs git range-diff between two commit ranges.
func RangeDiff(workdir, oldBase, oldHead, newBase, newHead string) (string, error) {
	result, err := ExecGit(workdir, []string{"range-diff", oldBase + ".." + oldHead, newBase + ".." + newHead})
	if err != nil {
		return "", fmt.Errorf("range-diff: %w", err)
	}
	return result.Stdout, nil
}

// PatchesEqual uses range-diff to check if two patch series have identical content.
// Returns true if the patches are the same (e.g. pure rebase), false if code changed.
// Falls back to false (assume changed) if comparison fails.
func PatchesEqual(workdir, base1, head1, base2, head2 string) (bool, error) {
	output, err := RangeDiff(workdir, base1, head1, base2, head2)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(output) == "" {
		return true, nil
	}
	for _, line := range strings.Split(output, "\n") {
		if len(line) == 0 || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			op := parts[2]
			if op == "!" || op == "<" || op == ">" {
				return false, nil
			}
		}
	}
	return true, nil
}

// SquashMerge squashes all head commits into one commit on base.
func SquashMerge(workdir, base, head, message string) (string, error) {
	treeResult, err := ExecGit(workdir, []string{"merge-tree", "--write-tree", "refs/heads/" + base, "refs/heads/" + head})
	if err != nil {
		return "", fmt.Errorf("merge conflicts between %s and %s", base, head)
	}
	tree := strings.TrimSpace(treeResult.Stdout)
	baseHash, err := execGitSimple(workdir, []string{"rev-parse", "refs/heads/" + base})
	if err != nil {
		return "", fmt.Errorf("resolve base branch: %w", err)
	}
	commitResult, err := ExecGit(workdir, []string{
		"commit-tree", tree,
		"-p", strings.TrimSpace(baseHash),
		"-m", message,
	})
	if err != nil {
		return "", fmt.Errorf("create squash commit: %w", err)
	}
	hash := strings.TrimSpace(commitResult.Stdout)
	_, err = ExecGit(workdir, []string{"update-ref", "refs/heads/" + base, hash})
	if err != nil {
		return "", fmt.Errorf("update %s ref: %w", base, err)
	}
	return hash, nil
}

// RebaseMerge replays head commits onto base, then fast-forwards base.
func RebaseMerge(workdir, base, head string) (string, error) {
	ancestor, err := GetMergeBase(workdir, "refs/heads/"+base, "refs/heads/"+head)
	if err != nil {
		return "", fmt.Errorf("find merge base: %w", err)
	}
	result, err := ExecGit(workdir, []string{"rev-list", "--reverse", ancestor + ".." + "refs/heads/" + head})
	if err != nil {
		return "", fmt.Errorf("list commits: %w", err)
	}
	commits := splitNonEmpty(result.Stdout)
	if len(commits) == 0 {
		return "", fmt.Errorf("no commits to rebase")
	}
	baseHash, err := execGitSimple(workdir, []string{"rev-parse", "refs/heads/" + base})
	if err != nil {
		return "", fmt.Errorf("resolve base: %w", err)
	}
	parent := strings.TrimSpace(baseHash)
	for _, commitHash := range commits {
		msg, err := GetCommitMessage(workdir, commitHash)
		if err != nil {
			return "", fmt.Errorf("get commit message %s: %w", commitHash, err)
		}
		treeResult, err := ExecGit(workdir, []string{"merge-tree", "--write-tree", "--merge-base=" + commitHash + "^", parent, commitHash})
		if err != nil {
			return "", fmt.Errorf("merge-tree for commit %s: %w", commitHash, err)
		}
		tree := strings.TrimSpace(treeResult.Stdout)
		commitResult, err := ExecGit(workdir, []string{
			"commit-tree", tree,
			"-p", parent,
			"-m", strings.TrimSpace(msg),
		})
		if err != nil {
			return "", fmt.Errorf("commit-tree for %s: %w", commitHash, err)
		}
		parent = strings.TrimSpace(commitResult.Stdout)
	}
	_, err = ExecGit(workdir, []string{"update-ref", "refs/heads/" + base, parent})
	if err != nil {
		return "", fmt.Errorf("update %s ref: %w", base, err)
	}
	return parent, nil
}

// ForceMerge creates a merge commit even when fast-forward is possible.
func ForceMerge(workdir, base, head string) (string, error) {
	treeResult, err := ExecGit(workdir, []string{"merge-tree", "--write-tree", "refs/heads/" + base, "refs/heads/" + head})
	if err != nil {
		return "", fmt.Errorf("merge conflicts between %s and %s", base, head)
	}
	tree := strings.TrimSpace(treeResult.Stdout)
	baseHash, err := execGitSimple(workdir, []string{"rev-parse", "refs/heads/" + base})
	if err != nil {
		return "", fmt.Errorf("resolve base branch: %w", err)
	}
	headHash, err := execGitSimple(workdir, []string{"rev-parse", "refs/heads/" + head})
	if err != nil {
		return "", fmt.Errorf("resolve head branch: %w", err)
	}
	msg := fmt.Sprintf("Merge branch '%s' into %s", head, base)
	commitResult, err := ExecGit(workdir, []string{
		"commit-tree", tree,
		"-p", strings.TrimSpace(baseHash),
		"-p", strings.TrimSpace(headHash),
		"-m", msg,
	})
	if err != nil {
		return "", fmt.Errorf("create merge commit: %w", err)
	}
	hash := strings.TrimSpace(commitResult.Stdout)
	_, err = ExecGit(workdir, []string{"update-ref", "refs/heads/" + base, hash})
	if err != nil {
		return "", fmt.Errorf("update %s ref: %w", base, err)
	}
	return hash, nil
}

// RebaseBranch replays head commits onto base, updating head ref (not base).
func RebaseBranch(workdir, base, head string) (string, error) {
	ancestor, err := GetMergeBase(workdir, "refs/heads/"+base, "refs/heads/"+head)
	if err != nil {
		return "", fmt.Errorf("find merge base: %w", err)
	}
	result, err := ExecGit(workdir, []string{"rev-list", "--reverse", ancestor + ".." + "refs/heads/" + head})
	if err != nil {
		return "", fmt.Errorf("list commits: %w", err)
	}
	commits := splitNonEmpty(result.Stdout)
	if len(commits) == 0 {
		return "", fmt.Errorf("no commits to rebase")
	}
	baseHash, err := execGitSimple(workdir, []string{"rev-parse", "refs/heads/" + base})
	if err != nil {
		return "", fmt.Errorf("resolve base: %w", err)
	}
	parent := strings.TrimSpace(baseHash)
	for _, commitHash := range commits {
		msg, err := GetCommitMessage(workdir, commitHash)
		if err != nil {
			return "", fmt.Errorf("get commit message %s: %w", commitHash, err)
		}
		treeResult, err := ExecGit(workdir, []string{"merge-tree", "--write-tree", "--merge-base=" + commitHash + "^", parent, commitHash})
		if err != nil {
			return "", fmt.Errorf("merge-tree for commit %s: %w", commitHash, err)
		}
		tree := strings.TrimSpace(treeResult.Stdout)
		commitResult, err := ExecGit(workdir, []string{
			"commit-tree", tree,
			"-p", parent,
			"-m", strings.TrimSpace(msg),
		})
		if err != nil {
			return "", fmt.Errorf("commit-tree for %s: %w", commitHash, err)
		}
		parent = strings.TrimSpace(commitResult.Stdout)
	}
	_, err = ExecGit(workdir, []string{"update-ref", "refs/heads/" + head, parent})
	if err != nil {
		return "", fmt.Errorf("update %s ref: %w", head, err)
	}
	return parent, nil
}

// GetBehindCount returns the number of commits head is behind base.
func GetBehindCount(workdir, base, head string) (int, error) {
	out, err := execGitSimple(workdir, []string{"rev-list", "--count", head + ".." + base})
	if err != nil {
		return 0, fmt.Errorf("rev-list count: %w", err)
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d", &count); err != nil {
		slog.Debug("parse behind count", "error", err, "output", out)
	}
	return count, nil
}

func splitNonEmpty(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// GetAuthorIdentity returns the configured git user name and email.
func GetAuthorIdentity(workdir string) (name, email string, err error) {
	n, err := execGitSimple(workdir, []string{"config", "user.name"})
	if err != nil {
		return "", "", fmt.Errorf("get user.name: %w", err)
	}
	e, err := execGitSimple(workdir, []string{"config", "user.email"})
	if err != nil {
		return "", "", fmt.Errorf("get user.email: %w", err)
	}
	return strings.TrimSpace(n), strings.TrimSpace(e), nil
}

// FastImportCommits creates multiple empty-tree commits on a branch via git fast-import.
// Returns the 12-char short hash of each created commit.
func FastImportCommits(workdir, branch string, messages []string) ([]string, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	name, email, err := GetAuthorIdentity(workdir)
	if err != nil {
		return nil, fmt.Errorf("get author identity: %w", err)
	}
	branchRef := "refs/heads/" + branch
	var branchTip string
	result, err := ExecGit(workdir, []string{"rev-parse", "--verify", "--quiet", branchRef})
	if err == nil && result.Stdout != "" {
		branchTip = strings.TrimSpace(result.Stdout)
	}
	now := time.Now().Unix()
	var stream strings.Builder
	for i, msg := range messages {
		fmt.Fprintf(&stream, "commit %s\n", branchRef)
		fmt.Fprintf(&stream, "mark :%d\n", i+1)
		fmt.Fprintf(&stream, "committer %s <%s> %d +0000\n", name, email, now+int64(i))
		fmt.Fprintf(&stream, "data %d\n%s\n", len(msg), msg)
		if i == 0 {
			if branchTip != "" {
				fmt.Fprintf(&stream, "from %s\n", branchTip)
			}
		} else {
			fmt.Fprintf(&stream, "from :%d\n", i)
		}
		stream.WriteString("\n")
	}
	marksFile, err := os.CreateTemp("", "gitmsg-marks-*")
	if err != nil {
		return nil, fmt.Errorf("create marks file: %w", err)
	}
	marksPath := marksFile.Name()
	if err := marksFile.Close(); err != nil {
		slog.Debug("close marks file", "error", err)
	}
	defer func() {
		if err := os.Remove(marksPath); err != nil && !os.IsNotExist(err) {
			slog.Debug("remove marks file", "error", err)
		}
	}()
	_, err = execGitWithStdin(workdir, []string{
		"fast-import", "--force", "--quiet",
		"--export-marks=" + marksPath,
	}, stream.String())
	if err != nil {
		return nil, fmt.Errorf("fast-import: %w", err)
	}
	marksData, err := os.ReadFile(marksPath)
	if err != nil {
		return nil, fmt.Errorf("read marks: %w", err)
	}
	hashes := make([]string, 0, len(messages))
	for _, line := range strings.Split(string(marksData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		sha := strings.TrimSpace(parts[1])
		if len(sha) >= 12 {
			hashes = append(hashes, sha[:12])
		}
	}
	if len(hashes) != len(messages) {
		return nil, fmt.Errorf("fast-import: expected %d hashes, got %d", len(messages), len(hashes))
	}
	return hashes, nil
}

// CreateOrphanBranch creates a new orphan branch with an initial empty commit.
func CreateOrphanBranch(workdir, branch string) error {
	// Create initial commit on orphan branch
	hash, err := CreateCommitTree(workdir, "Initialize "+branch+" branch", "")
	if err != nil {
		return err
	}
	return WriteRef(workdir, "refs/heads/"+branch, hash)
}
