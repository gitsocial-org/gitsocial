// lfs.go - Git LFS pointer generation and object storage
package git

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const lfsPointerPrefix = "version https://git-lfs.github.com/spec/v1\n"

// IsLFSAvailable checks if git-lfs is installed.
func IsLFSAvailable() bool {
	_, err := ExecGit(".", []string{"lfs", "version"})
	return err == nil
}

// FormatLFSPointer generates LFS pointer content for a given oid and size.
func FormatLFSPointer(oid string, size int64) []byte {
	return []byte(fmt.Sprintf("%soid sha256:%s\nsize %d\n", lfsPointerPrefix, oid, size))
}

// StoreLFSObject writes content to the local LFS object store.
func StoreLFSObject(workdir, oid string, data []byte) error {
	gitDir, err := execGitSimple(workdir, []string{"rev-parse", "--git-dir"})
	if err != nil {
		return fmt.Errorf("resolve git dir: %w", err)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(workdir, gitDir)
	}
	dir := filepath.Join(gitDir, "lfs", "objects", oid[:2], oid[2:4])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create lfs object dir: %w", err)
	}
	objPath := filepath.Join(dir, oid)
	if _, err := os.Stat(objPath); err == nil {
		return nil // already exists
	}
	if err := os.WriteFile(objPath, data, 0o444); err != nil {
		return fmt.Errorf("write lfs object: %w", err)
	}
	return nil
}

// ReadLFSObject reads content from the local LFS object store.
func ReadLFSObject(workdir, oid string) ([]byte, error) {
	gitDir, err := execGitSimple(workdir, []string{"rev-parse", "--git-dir"})
	if err != nil {
		return nil, fmt.Errorf("resolve git dir: %w", err)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(workdir, gitDir)
	}
	objPath := filepath.Join(gitDir, "lfs", "objects", oid[:2], oid[2:4], oid)
	data, err := os.ReadFile(objPath)
	if err != nil {
		return nil, fmt.Errorf("read lfs object: %w", err)
	}
	return data, nil
}

// IsLFSPointer checks whether content is a Git LFS pointer.
func IsLFSPointer(data []byte) bool {
	return bytes.HasPrefix(data, []byte(lfsPointerPrefix))
}

// GetUnpushedLFSCount returns the number of LFS objects not yet pushed to origin.
func GetUnpushedLFSCount(workdir string) int {
	if !IsLFSAvailable() {
		return 0
	}
	result, err := ExecGit(workdir, []string{"lfs", "push", "--dry-run", "origin", "--all"})
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.HasPrefix(line, "push ") {
			count++
		}
	}
	return count
}

// PushLFS pushes all LFS objects to origin, including those on gitmsg refs.
func PushLFS(workdir string) (int, error) {
	if !IsLFSAvailable() {
		return 0, fmt.Errorf("git-lfs not installed")
	}
	count := GetUnpushedLFSCount(workdir)
	if count == 0 {
		return 0, nil
	}
	if _, err := ExecGit(workdir, []string{"lfs", "push", "origin", "--all"}); err != nil {
		return 0, fmt.Errorf("lfs push --all: %w", err)
	}
	refsResult, err := ExecGit(workdir, []string{"for-each-ref", "--format=%(refname)", "refs/gitmsg/"})
	if err == nil {
		for _, ref := range strings.Split(refsResult.Stdout, "\n") {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				_, _ = ExecGit(workdir, []string{"lfs", "push", "origin", ref})
			}
		}
	}
	return count, nil
}

// ParseLFSPointer extracts the oid and size from LFS pointer content.
func ParseLFSPointer(data []byte) (oid string, size int64, ok bool) {
	if !IsLFSPointer(data) {
		return "", 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "oid sha256:") {
			oid = strings.TrimPrefix(line, "oid sha256:")
		} else if strings.HasPrefix(line, "size ") {
			size, _ = strconv.ParseInt(strings.TrimPrefix(line, "size "), 10, 64)
		}
	}
	return oid, size, oid != "" && size > 0
}
