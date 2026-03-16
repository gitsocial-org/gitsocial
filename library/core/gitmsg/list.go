// list.go - List data storage and retrieval from git refs
package gitmsg

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

type ListData struct {
	Version      string   `json:"version"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Repositories []string `json:"repositories"`
}

// refPath returns the git ref path for a list.
func refPath(extension, name string) string {
	return fmt.Sprintf("refs/gitmsg/%s/lists/%s", extension, name)
}

// EnumerateLists returns all list names for an extension.
func EnumerateLists(workdir, extension string) ([]string, error) {
	refs, err := git.ListRefs(workdir, fmt.Sprintf("%s/lists/", extension))
	if err != nil {
		return nil, err
	}

	prefix := fmt.Sprintf("%s/lists/", extension)
	var names []string
	for _, ref := range refs {
		name := strings.TrimPrefix(ref, prefix)
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// ReadList reads a list's data from its git ref.
func ReadList(workdir, extension, name string) (*ListData, error) {
	ref := refPath(extension, name)

	hash, err := git.ReadRef(workdir, ref)
	if err != nil {
		return nil, nil // ref not found is expected for uninitialized lists
	}

	msg, err := git.GetCommitMessage(workdir, hash)
	if err != nil {
		slog.Debug("read list commit message", "error", err, "extension", extension, "name", name)
		return nil, nil
	}

	var data ListData
	if err := json.Unmarshal([]byte(strings.TrimSpace(msg)), &data); err != nil {
		slog.Warn("list data JSON parse", "error", err, "extension", extension, "name", name)
		return nil, nil
	}

	return &data, nil
}

// WriteList writes a list's data to its git ref.
func WriteList(workdir, extension, name string, data ListData) error {
	ref := refPath(extension, name)

	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal list: %w", err)
	}

	var parent string
	if existingHash, err := git.ReadRef(workdir, ref); err == nil {
		parent = existingHash
	}

	commitHash, err := git.CreateCommitTree(workdir, string(content), parent)
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}

	if err := git.WriteRef(workdir, ref, commitHash); err != nil {
		return fmt.Errorf("failed to write ref: %w", err)
	}

	return nil
}

// DeleteList removes a list by deleting its git ref.
func DeleteList(workdir, extension, name string) error {
	ref := refPath(extension, name)
	return git.DeleteRef(workdir, ref)
}

// FindListAdditionTime finds when a repo was added to a list.
func FindListAdditionTime(workdir, extension, listName, targetURL string) (time.Time, string, bool) {
	ref := refPath(extension, listName)
	hash, err := git.ReadRef(workdir, ref)
	if err != nil {
		return time.Time{}, "", false
	}

	commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{
		Branch: hash,
		Limit:  100,
	})
	if err != nil || len(commits) == 0 {
		return time.Time{}, "", false
	}

	normalizedTarget := protocol.NormalizeURL(targetURL)
	var firstFound time.Time
	var foundHash string

	for i := len(commits) - 1; i >= 0; i-- {
		commit := commits[i]
		var data ListData
		if err := json.Unmarshal([]byte(strings.TrimSpace(commit.Message)), &data); err != nil {
			continue
		}

		for _, repoRef := range data.Repositories {
			parts := strings.Split(repoRef, "#branch:")
			url := protocol.NormalizeURL(parts[0])
			if url == normalizedTarget {
				firstFound = commit.Timestamp
				foundHash = commit.Hash
				break
			}
		}
		if !firstFound.IsZero() {
			break
		}
	}

	return firstFound, foundHash, !firstFound.IsZero()
}
