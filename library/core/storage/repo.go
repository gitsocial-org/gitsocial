// repo.go - Bare repository storage management for cached remote data
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/git"
)

// GetStorageDir returns the storage directory path for a repository.
func GetStorageDir(baseDir, repoURL string) string {
	h := sha256.Sum256([]byte(repoURL))
	hash := hex.EncodeToString(h[:2])
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

// EnsureRepository creates a bare clone if it doesn't exist.
func EnsureRepository(baseDir, repoURL, branch string, opts *EnsureOptions) (string, error) {
	if opts == nil {
		opts = &EnsureOptions{}
	}

	storageDir := GetStorageDir(baseDir, repoURL)

	if _, err := os.Stat(storageDir); err == nil && !opts.Force {
		return storageDir, nil
	}

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create storage dir: %w", err)
	}

	_, err := git.ExecGit(storageDir, []string{"init", "--bare"})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("failed to init bare repo: %w", err)
	}

	_, err = git.ExecGit(storageDir, []string{"remote", "add", "upstream", repoURL})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("failed to add remote: %w", err)
	}

	_, err = git.ExecGit(storageDir, []string{
		"config", "remote.upstream.partialclonefilter", "blob:none",
	})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("failed to set partial clone filter: %w", err)
	}

	_, err = git.ExecGit(storageDir, []string{
		"config", "remote.upstream.pushurl", "",
	})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("failed to disable push: %w", err)
	}

	_, err = git.ExecGit(storageDir, []string{
		"config", "gitmsg.branch", branch,
	})
	if err != nil {
		os.RemoveAll(storageDir)
		return "", fmt.Errorf("failed to set branch config: %w", err)
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
		return "", fmt.Errorf("failed to set persistent config: %w", err)
	}

	return storageDir, nil
}

type FetchOptions struct {
	Since string
	Depth int
}

// FetchRepository fetches all gitmsg/* branches and refs from upstream into the bare repo.
// Also fetches custom-named branches discovered from extension configs.
// When branch is "*", fetches all branches from upstream.
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
		// Fetch custom-named branches from extension configs (outside gitmsg/ namespace)
		for _, b := range discoverCustomBranches(storageDir) {
			fetchArgs := []string{"fetch", "upstream", b, "--no-tags"}
			if opts != nil && opts.Since != "" {
				fetchArgs = append(fetchArgs, "--shallow-since="+opts.Since)
			}
			if _, fetchErr := git.ExecGit(storageDir, fetchArgs); fetchErr != nil {
				slog.Debug("fetch custom branch", "error", fetchErr, "branch", b)
			}
		}

		// Fetch the configured default branch (e.g., "main") if not covered by gitmsg/* refspecs
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

// discoverCustomBranches reads extension configs from refs/gitmsg/*/config
// and returns branch names that fall outside the gitmsg/ namespace.
func discoverCustomBranches(storageDir string) []string {
	result, err := git.ExecGit(storageDir, []string{
		"for-each-ref", "--format=%(refname)", "refs/gitmsg/",
	})
	if err != nil || result.Stdout == "" {
		return nil
	}

	var branches []string
	for _, ref := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if !strings.HasSuffix(ref, "/config") {
			continue
		}
		msg, err := git.ExecGit(storageDir, []string{"show", "-s", "--format=%B", ref})
		if err != nil || msg.Stdout == "" {
			continue
		}
		var config map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(msg.Stdout)), &config); err != nil {
			continue
		}
		if b, ok := config["branch"].(string); ok && b != "" && !strings.HasPrefix(b, "gitmsg/") {
			branches = append(branches, b)
		}
	}
	return branches
}
