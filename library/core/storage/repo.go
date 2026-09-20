// repo.go - Bare repository storage management for cached remote data
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// GetStorageDir returns the storage directory path for a repository.
func GetStorageDir(baseDir, repoURL string) string {
	h := sha256.Sum256([]byte(repoURL))
	hash := hex.EncodeToString(h[:8])
	name := urlToDirectoryName(repoURL)
	return filepath.Join(baseDir, "repositories", name+"-"+hash)
}

// urlToDirectoryName converts a URL to a safe directory name.
func urlToDirectoryName(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimSuffix(url, ".git")
	url = strings.ReplaceAll(url, "/", "-")
	url = strings.ReplaceAll(url, ":", "-")
	if len(url) > 50 {
		url = url[:50]
	}
	return url
}

type EnsureOptions struct {
	IsPersistent bool
	Force        bool
}

// EnsureRepository creates a bare clone if it doesn't exist: the directory is named from
// repoURL, the identity, and git fetches from address, the spelling the user gave.
// An empty address serves on create alone and leaves an existing remote as it was set.
func EnsureRepository(baseDir, repoURL, address, branch string, opts *EnsureOptions) (string, error) {
	if opts == nil {
		opts = &EnsureOptions{}
	}

	storageDir := GetStorageDir(baseDir, repoURL)
	if _, err := os.Stat(storageDir); err == nil && !opts.Force {
		if address != "" {
			if err := git.EnsureRemote(storageDir, "upstream", address); err != nil {
				slog.Debug("set upstream url", "dir", storageDir, "address", address, "error", err)
			}
		}
		return storageDir, nil
	}
	if address == "" {
		address = repoURL
	}

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return "", fmt.Errorf("create storage dir: %w", err)
	}

	_, err := git.ExecGit(storageDir, []string{"init", "--bare"})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("init bare repo: %w", err)
	}

	_, err = git.ExecGit(storageDir, []string{"remote", "add", "upstream", address})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("add remote: %w", err)
	}

	_, err = git.ExecGit(storageDir, []string{
		"config", "remote.upstream.partialclonefilter", "blob:none",
	})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("set partial clone filter: %w", err)
	}

	_, err = git.ExecGit(storageDir, []string{
		"config", "remote.upstream.pushurl", "",
	})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("disable push: %w", err)
	}

	_, err = git.ExecGit(storageDir, []string{
		"config", "gitmsg.branch", branch,
	})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("set branch config: %w", err)
	}

	persistent := "0"
	if opts.IsPersistent {
		persistent = "1"
	}
	_, err = git.ExecGit(storageDir, []string{
		"config", "gitmsg.persistent", persistent,
	})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("set persistent config: %w", err)
	}

	return storageDir, nil
}

type FetchOptions struct {
	Since string
	Depth int
}

// FetchRepository fetches the gitmsg/* branches and refs, plus the named branch, or every branch when branch is "*".
func FetchRepository(storageDir string, branch string, opts *FetchOptions) error {
	branchRefspec := "+refs/heads/gitmsg/*:refs/heads/gitmsg/*"
	args := []string{"fetch", "upstream", branchRefspec}

	if opts != nil {
		if opts.Since != "" {
			args = append(args, "--shallow-since="+opts.Since)
		} else if opts.Depth > 0 {
			args = append(args, fmt.Sprintf("--depth=%d", opts.Depth))
		}
	}

	args = append(args, "--no-tags")

	_, err := git.ExecGit(storageDir, args)
	if err != nil {
		if opts != nil && opts.Since != "" {
			args = []string{"fetch", "upstream", branchRefspec, "--depth=100", "--no-tags"}
			_, err = git.ExecGit(storageDir, args)
		}
	}

	// Fetch gitmsg refs (extension configs, lists)
	if _, fetchErr := git.ExecGit(storageDir, []string{
		"fetch", "upstream",
		"+refs/gitmsg/*:refs/gitmsg/*",
		"--no-tags",
	}); fetchErr != nil {
		slog.Debug("fetch gitmsg refs", "error", fetchErr, "dir", storageDir)
	}

	if branch == "*" {
		// Follow all branches: fetch all refs/heads/*
		allArgs := []string{"fetch", "upstream", "+refs/heads/*:refs/heads/*", "--no-tags"}
		if opts != nil && opts.Since != "" {
			allArgs = append(allArgs, "--shallow-since="+opts.Since)
		}
		if _, fetchErr := git.ExecGit(storageDir, allArgs); fetchErr != nil {
			slog.Debug("fetch all branches", "error", fetchErr, "dir", storageDir)
		}
	} else {
		// GITMSG.md 3.4: the entry names the branch, and no reader opens a remote's config to find one.
		if branch != "" && !strings.HasPrefix(branch, "gitmsg/") {
			defaultRefspec := fmt.Sprintf("+refs/heads/%s:refs/heads/%s", branch, branch)
			defaultArgs := []string{"fetch", "upstream", defaultRefspec, "--no-tags"}
			if opts != nil && opts.Since != "" {
				defaultArgs = append(defaultArgs, "--shallow-since="+opts.Since)
			}
			if _, fetchErr := git.ExecGit(storageDir, defaultArgs); fetchErr != nil {
				slog.Debug("fetch default branch", "error", fetchErr, "branch", branch)
			}
		}
	}

	return err
}
