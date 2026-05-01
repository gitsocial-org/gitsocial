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
	"sync"
	"time"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
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

var legacyListsMigrated sync.Map // "workdir\x00ext\x00name" → bool

// refPath returns the git ref path for a list's metadata. Stored under
// `<name>/_meta` so the parent `<name>/` namespace can host items.
func refPath(extension, name string) string {
	return fmt.Sprintf("refs/gitmsg/%s/lists/%s%s", extension, name, metaSubpath)
}

// legacyRefPath returns the pre-migration ref path (single-ref layout
// where members were embedded in the metadata commit).
func legacyRefPath(extension, name string) string {
	return fmt.Sprintf("refs/gitmsg/%s/lists/%s", extension, name)
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
		// Each ref under lists/ is one of:
		//   <name>           (legacy single-ref layout)
		//   <name>/_meta     (new metadata ref)
		//   <name>/items/<h> (new member ref)
		// Take the first path segment as the list name in all cases.
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

// ReadList reads a list's metadata and member set. Members come from
// per-element refs; on first read of a legacy list (single ref with
// embedded Repositories array), this deletes the legacy ref, writes the
// new metadata-only ref, and splits members into per-element refs.
func ReadList(workdir, extension, name string) (*ListData, error) {
	migrateLegacyList(workdir, extension, name)
	data := readListMetadata(workdir, extension, name)
	if data == nil {
		return nil, nil
	}
	data.Repositories = readListMembers(workdir, extension, name)
	return data, nil
}

// WriteList writes the list's metadata. The Repositories field is
// ignored — members are stored as per-element refs via AddListMember /
// RemoveListMember. Callers passing a non-empty Repositories slice will
// have it stripped from the persisted metadata; this preserves backward
// compatibility for any caller that round-trips a ListData through
// ReadList → WriteList without intending to mutate the member set.
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

// HasListMember reports whether a member ref is registered in the list.
func HasListMember(workdir, extension, name, memberRef string) bool {
	if memberRef == "" {
		return false
	}
	itemRef := itemRefPath(extension, name, memberRef)
	_, err := git.ReadRef(workdir, itemRef)
	return err == nil
}

// DeleteList removes a list's metadata ref and all of its member refs.
// Also clears any pre-migration legacy ref at the same name.
func DeleteList(workdir, extension, name string) error {
	for _, member := range readListMembers(workdir, extension, name) {
		_ = RemoveListMember(workdir, extension, name, member)
	}
	_ = git.DeleteRef(workdir, legacyRefPath(extension, name))
	return git.DeleteRef(workdir, refPath(extension, name))
}

// FindListAdditionTime finds when a member was added by reading the
// item-ref's commit timestamp. Falls back to scanning the legacy
// metadata commit history if no per-element ref exists (transitional).
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

// readListMetadata reads the metadata commit at the list's head ref.
// Falls back to the legacy single-ref layout when the new _meta ref is
// missing — this is the read-side support for transitions in flight.
// Returns nil when neither ref resolves; never returns an error
// (unreadable refs are treated as missing, with debug/warn log lines).
func readListMetadata(workdir, extension, name string) *ListData {
	for _, ref := range []string{refPath(extension, name), legacyRefPath(extension, name)} {
		hash, err := git.ReadRef(workdir, ref)
		if err != nil {
			continue
		}
		msg, err := git.GetCommitMessage(workdir, hash)
		if err != nil {
			slog.Debug("read list commit message", "error", err, "extension", extension, "name", name)
			continue
		}
		var data ListData
		if err := json.Unmarshal([]byte(strings.TrimSpace(msg)), &data); err != nil {
			slog.Warn("list data JSON parse", "error", err, "extension", extension, "name", name)
			continue
		}
		return &data
	}
	return nil
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

// migrateLegacyList converts a legacy single-ref list to the new
// metadata + per-member layout. Idempotent: returns early if the new
// `_meta` ref already exists, or if no legacy ref is present.
//
// Order matters: git refuses to create refs under `<name>/...` while
// `<name>` itself exists as a ref, so the legacy parent ref must be
// deleted before writing the new metadata and item refs.
func migrateLegacyList(workdir, extension, name string) {
	key := workdir + "\x00" + extension + "\x00" + name
	if _, done := legacyListsMigrated.Load(key); done {
		return
	}
	defer legacyListsMigrated.Store(key, true)
	legacyRef := legacyRefPath(extension, name)
	hash, err := git.ReadRef(workdir, legacyRef)
	if err != nil {
		return
	}
	if _, err := git.ReadRef(workdir, refPath(extension, name)); err == nil {
		// Both old and new exist — should not happen, but trust the new
		// layout and just delete the legacy.
		_ = git.DeleteRef(workdir, legacyRef)
		return
	}
	msg, err := git.GetCommitMessage(workdir, hash)
	if err != nil {
		return
	}
	var data ListData
	if err := json.Unmarshal([]byte(strings.TrimSpace(msg)), &data); err != nil {
		return
	}
	members := data.Repositories
	if err := git.DeleteRef(workdir, legacyRef); err != nil {
		return
	}
	data.Repositories = nil
	_ = WriteList(workdir, extension, name, data)
	for _, member := range members {
		_ = AddListMember(workdir, extension, name, member)
	}
}
