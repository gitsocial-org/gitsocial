// list.go - List storage with per-element member refs.
//
// Each list's metadata (id, name, version) lives at
// `refs/gitmsg/<ext>/lists/<name>/_meta`. Each member lives at
// `refs/gitmsg/<ext>/lists/<name>/items/<refHash>` whose commit message
// is the full member ref (e.g., `https://github.com/x/y#branch:main`).
// The `<name>/` segment is reserved as a directory in git's ref
// namespace — refs can't coexist with a same-named ref tree, so the
// metadata can't live at the parent ref `lists/<name>` itself. Splitting
// members across refs eliminates the silent-data-loss path where two
// clones editing the same list contended on a single ref — concurrent
// adds either land on different item-refs (no collision) or the same
// item-ref with identical content (idempotent push).
package gitmsg

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

type ListData struct {
	Version      string   `json:"version"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Repositories []string `json:"repositories,omitempty"`
}

const (
	itemsSubpath = "/items/"
	metaSubpath  = "/_meta"
)

// refPath returns the git ref path for a list's metadata. Stored under
// `<name>/_meta` so the parent `<name>/` namespace can host items.
func refPath(extension, name string) string {
	return fmt.Sprintf("refs/gitmsg/%s/lists/%s%s", extension, name, metaSubpath)
}

// itemRefPath returns the per-member ref name for a list.
func itemRefPath(extension, name, memberRef string) string {
	h := sha256.Sum256([]byte(memberRef))
	return fmt.Sprintf("refs/gitmsg/%s/lists/%s%s%s",
		extension, name, itemsSubpath, hex.EncodeToString(h[:6]))
}

// EnumerateLists returns all list names for an extension. Walks the
// `lists/` namespace and extracts the list-name segment, filtering
// duplicates from the items/<hash> and _meta children.
func EnumerateLists(workdir, extension string) ([]string, error) {
	refs, err := git.ListRefs(workdir, fmt.Sprintf("%s/lists/", extension))
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%s/lists/", extension)
	seen := make(map[string]struct{})
	var names []string
	for _, ref := range refs {
		stripped := strings.TrimPrefix(ref, prefix)
		if stripped == "" {
			continue
		}
		// Every ref under lists/ is <name>/_meta or <name>/items/<h>, so the first segment is the name.
		if i := strings.Index(stripped, "/"); i >= 0 {
			stripped = stripped[:i]
		}
		if _, ok := seen[stripped]; ok {
			continue
		}
		seen[stripped] = struct{}{}
		names = append(names, stripped)
	}
	return names, nil
}

// ReadList reads a list's metadata and its member set from the per-element refs.
func ReadList(workdir, extension, name string) (*ListData, error) {
	data := readListMetadata(workdir, extension, name)
	if data == nil {
		return nil, nil
	}
	data.Repositories = readListMembers(workdir, extension, name)
	return data, nil
}

// WriteList writes the list's metadata; Repositories is stripped, since members live in per-element refs.
func WriteList(workdir, extension, name string, data ListData) error {
	data.Repositories = nil
	ref := refPath(extension, name)
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal list metadata: %w", err)
	}
	var parent string
	if existingHash, err := git.ReadRef(workdir, ref); err == nil {
		parent = existingHash
	}
	commitHash, err := git.CreateCommitTree(workdir, string(content), parent)
	if err != nil {
		return fmt.Errorf("create list metadata commit: %w", err)
	}
	return git.WriteRef(workdir, ref, commitHash)
}

// AddListMember registers a member ref (e.g., `https://x/y#branch:main`)
// in the named list. Idempotent across clones: same memberRef → same
// item-ref name and content → no collision on push.
func AddListMember(workdir, extension, name, memberRef string) error {
	if memberRef == "" {
		return fmt.Errorf("empty member ref")
	}
	itemRef := itemRefPath(extension, name, memberRef)
	if _, err := git.ReadRef(workdir, itemRef); err == nil {
		return nil
	}
	hash, err := git.CreateCommitTree(workdir, memberRef+"\n", "")
	if err != nil {
		return fmt.Errorf("create list-member commit: %w", err)
	}
	return git.WriteRef(workdir, itemRef, hash)
}

// RemoveListMember removes a member ref from the list. Idempotent —
// removing a nonexistent member is a no-op.
func RemoveListMember(workdir, extension, name, memberRef string) error {
	if memberRef == "" {
		return nil
	}
	itemRef := itemRefPath(extension, name, memberRef)
	if _, err := git.ReadRef(workdir, itemRef); err != nil {
		return nil
	}
	return git.DeleteRef(workdir, itemRef)
}

// DeleteList removes a list's metadata ref and all of its member refs.
func DeleteList(workdir, extension, name string) error {
	for _, member := range readListMembers(workdir, extension, name) {
		_ = RemoveListMember(workdir, extension, name, member)
	}
	return git.DeleteRef(workdir, refPath(extension, name))
}

// FindListAdditionTime finds when a member was added by reading the item-ref's commit timestamp.
func FindListAdditionTime(workdir, extension, listName, targetURL string) (time.Time, string, bool) {
	normalizedTarget := protocol.NormalizeURL(targetURL)
	for _, memberRef := range readListMembers(workdir, extension, listName) {
		parts := strings.Split(memberRef, "#branch:")
		if protocol.NormalizeURL(parts[0]) != normalizedTarget {
			continue
		}
		itemRef := itemRefPath(extension, listName, memberRef)
		hash, err := git.ReadRef(workdir, itemRef)
		if err != nil {
			continue
		}
		commits, err := git.GetCommits(workdir, &git.GetCommitsOptions{
			Branch: hash,
			Limit:  1,
		})
		if err != nil || len(commits) == 0 {
			continue
		}
		return commits[0].Timestamp, commits[0].Hash, true
	}
	return time.Time{}, "", false
}

// readListMetadata reads the metadata commit at the list's _meta ref, or nil when it does not resolve.
func readListMetadata(workdir, extension, name string) *ListData {
	hash, err := git.ReadRef(workdir, refPath(extension, name))
	if err != nil {
		return nil
	}
	msg, err := git.GetCommitMessage(workdir, hash)
	if err != nil {
		slog.Debug("read list commit message", "error", err, "extension", extension, "name", name)
		return nil
	}
	var data ListData
	if err := json.Unmarshal([]byte(strings.TrimSpace(msg)), &data); err != nil {
		slog.Warn("list data JSON parse", "error", err, "extension", extension, "name", name)
		return nil
	}
	return &data
}

// readListMembers returns the member refs for a list by reading every
// per-element ref under <list>/items/.
func readListMembers(workdir, extension, name string) []string {
	prefix := fmt.Sprintf("%s/lists/%s%s", extension, name, itemsSubpath)
	refs, err := git.ListRefs(workdir, prefix)
	if err != nil || len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		hash, err := git.ReadRef(workdir, "refs/gitmsg/"+ref)
		if err != nil {
			continue
		}
		msg, err := git.GetCommitMessage(workdir, hash)
		if err != nil {
			continue
		}
		member := strings.TrimSpace(msg)
		if member != "" {
			out = append(out, member)
		}
	}
	return out
}
