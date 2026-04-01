// mapping.go - ID mapping file for idempotent imports
package importpkg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// MappingFile tracks external IDs to GitSocial commit hashes.
type MappingFile struct {
	Source     string                `json:"source"`
	RepoURL    string                `json:"repo_url"`
	ImportedAt time.Time             `json:"imported_at"`
	Items      map[string]MappedItem `json:"items"`
}

// MappedItem is a single imported item's mapping.
type MappedItem struct {
	Hash      string `json:"hash"`
	Branch    string `json:"branch"`
	Type      string `json:"type"`
	UpdatedAt string `json:"updated_at,omitempty"` // platform's updated_at (RFC3339)
}

// MappingKey builds the canonical key for a mapped item.
func MappingKey(platform, itemType, externalID string) string {
	return fmt.Sprintf("%s:%s:%s", platform, itemType, externalID)
}

// ReadMapping loads an existing mapping file or returns an empty one.
func ReadMapping(cacheDir, repoURL, mapFile string) *MappingFile {
	path := resolveMappingPath(cacheDir, repoURL, mapFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return &MappingFile{Items: make(map[string]MappedItem)}
	}
	var m MappingFile
	if err := json.Unmarshal(data, &m); err != nil {
		return &MappingFile{Items: make(map[string]MappedItem)}
	}
	if m.Items == nil {
		m.Items = make(map[string]MappedItem)
	}
	return &m
}

// WriteMapping saves the mapping file to disk.
func WriteMapping(cacheDir, repoURL, mapFile string, m *MappingFile) error {
	m.ImportedAt = time.Now()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mapping: %w", err)
	}
	path := resolveMappingPath(cacheDir, repoURL, mapFile)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create import dir: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

// ResolveMappingPath returns the resolved path for the mapping file (for display).
func ResolveMappingPath(cacheDir, repoURL, mapFile string) string {
	return resolveMappingPath(cacheDir, repoURL, mapFile)
}

// IsMapped checks if an external ID has already been imported.
func (m *MappingFile) IsMapped(key string) bool {
	_, ok := m.Items[key]
	return ok
}

// Record adds a mapping entry.
func (m *MappingFile) Record(key, hash, branch, itemType string) {
	m.Items[key] = MappedItem{Hash: hash, Branch: branch, Type: itemType}
}

// GetHash returns the GitSocial commit hash for a mapped item.
func (m *MappingFile) GetHash(key string) string {
	if item, ok := m.Items[key]; ok {
		return item.Hash
	}
	return ""
}

// SetUpdatedAt records the platform's updated_at timestamp for a mapped item.
func (m *MappingFile) SetUpdatedAt(key string, t time.Time) {
	if item, ok := m.Items[key]; ok {
		item.UpdatedAt = t.Format(time.RFC3339)
		m.Items[key] = item
	}
}

// GetUpdatedAt returns the stored updated_at timestamp for a mapped item.
func (m *MappingFile) GetUpdatedAt(key string) string {
	if item, ok := m.Items[key]; ok {
		return item.UpdatedAt
	}
	return ""
}

// RebuildMapping scans extension branches for commits with origin metadata
// and rebuilds the mapping file from git history. Returns the count of recovered entries.
func RebuildMapping(workdir string, mapping *MappingFile) int {
	exts := []string{"social", "pm", "release", "review"}
	branches := make([]string, len(exts))
	for i, ext := range exts {
		branches[i] = gitmsg.GetExtBranch(workdir, ext)
	}
	recovered := 0
	for _, branch := range branches {
		commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{Branch: branch})
		if err != nil {
			continue
		}
		for _, c := range commits {
			msg := protocol.ParseMessage(c.Message)
			if msg == nil {
				continue
			}
			if _, hasEdits := msg.Header.Fields["edits"]; hasEdits {
				continue
			}
			origin := protocol.ExtractOrigin(&msg.Header)
			if origin == nil || origin.URL == "" {
				continue
			}
			itemType, externalID := reverseMapOriginURL(origin.URL)
			if itemType == "" {
				continue
			}
			if mapping.Source == "" && origin.Platform != "" {
				mapping.Source = origin.Platform
			}
			platform := origin.Platform
			if platform == "" {
				platform = mapping.Source
			}
			key := MappingKey(platform, itemType, externalID)
			if mapping.IsMapped(key) {
				continue
			}
			mapping.Record(key, c.Hash, branch, itemType)
			recovered++
		}
	}
	return recovered
}

// reverseMapOriginURL extracts item type and external ID from an origin URL path.
func reverseMapOriginURL(originURL string) (itemType, externalID string) {
	idx := strings.Index(originURL, "://")
	if idx == -1 {
		return "", ""
	}
	rest := originURL[idx+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		return "", ""
	}
	// Skip host + owner/repo (3 path segments: host/owner/repo/...)
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 4 {
		return "", ""
	}
	path := parts[3]
	switch {
	// GitHub paths
	case strings.HasPrefix(path, "milestone/"):
		return "milestone", strings.TrimPrefix(path, "milestone/")
	case strings.HasPrefix(path, "issues/"):
		return "issue", strings.TrimPrefix(path, "issues/")
	case strings.HasPrefix(path, "releases/tag/"):
		return "release", strings.TrimPrefix(path, "releases/tag/")
	case strings.HasPrefix(path, "pull/"):
		return "pr", strings.TrimPrefix(path, "pull/")
	case strings.HasPrefix(path, "discussions/"):
		suffix := strings.TrimPrefix(path, "discussions/")
		if fragIdx := strings.Index(suffix, "#discussioncomment-"); fragIdx != -1 {
			return "comment", suffix[fragIdx+len("#discussioncomment-"):]
		}
		return "post", suffix
	// GitLab paths (/-/ prefix)
	case strings.HasPrefix(path, "-/milestones/"):
		return "milestone", strings.TrimPrefix(path, "-/milestones/")
	case strings.HasPrefix(path, "-/issues/"):
		return "issue", strings.TrimPrefix(path, "-/issues/")
	case strings.HasPrefix(path, "-/releases/"):
		return "release", strings.TrimPrefix(path, "-/releases/")
	case strings.HasPrefix(path, "-/merge_requests/"):
		return "pr", strings.TrimPrefix(path, "-/merge_requests/")
	}
	return "", ""
}

// MappedExternalIDs extracts external IDs by type from the mapping for a given platform.
// Returns a map with "type:externalID" keys suitable for FetchOptions.SkipExternalIDs.
func MappedExternalIDs(mapping *MappingFile, platform string) map[string]bool {
	ids := make(map[string]bool, len(mapping.Items))
	for key := range mapping.Items {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) < 3 || parts[0] != platform {
			continue
		}
		ids[parts[1]+":"+parts[2]] = true
	}
	return ids
}

// CountMapped returns counts of previously imported items by type from the mapping file.
// Only types present in found (>= 0) are included; others are set to -1 (hidden by FormatItemCounts).
func CountMapped(mapping *MappingFile, found ItemCounts) ItemCounts {
	counts := ItemCounts{Issues: -1, PRs: -1, Releases: -1, Discussions: -1}
	var issues, prs, releases, posts int
	for key := range mapping.Items {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) < 3 {
			continue
		}
		switch parts[1] {
		case "issue":
			issues++
		case "pr":
			prs++
		case "release":
			releases++
		case "post":
			posts++
		}
	}
	if found.Issues >= 0 {
		counts.Issues = issues
	}
	if found.PRs >= 0 {
		counts.PRs = prs
	}
	if found.Releases >= 0 {
		counts.Releases = releases
	}
	if found.Discussions >= 0 {
		counts.Discussions = posts
	}
	return counts
}

func resolveMappingPath(cacheDir, repoURL, mapFile string) string {
	if mapFile != "" {
		clean := filepath.Clean(mapFile)
		if filepath.IsAbs(clean) {
			return clean
		}
		wd, _ := os.Getwd()
		return filepath.Join(wd, clean)
	}
	return filepath.Join(cacheDir, "imports", urlToSlug(repoURL)+".json")
}

// urlToSlug converts a repo URL to a filesystem-safe directory name.
func urlToSlug(url string) string {
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
