// inherits.go - Per-project list of memo source repos this workspace inherits from.
//
// "Inherited" sources contribute memos that the project considers binding —
// memos that govern this codebase's policies. Mirrors the per-element fork
// layout (one ref per URL under refs/gitmsg/memo/inherits/<urlHash>) so
// concurrent adds across clones don't collide.
//
// AddInherit also ensures the URL is in a managed `memo-inherits` social list
// with --all-branches so the inherited memos are actually fetched. The
// inherits ref set remains the source of truth for tier classification — the
// list is a side-channel that drives the fetch pipeline.
package memo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

const (
	inheritsRefPrefix = "refs/gitmsg/memo/inherits/"
	// InheritsListID is the social list memo manages on behalf of `inherit add`.
	// Surfaces in `social list ls` so users can see which sources are being
	// followed for memo inheritance; `inherit remove` cleans up its entries.
	InheritsListID   = "memo-inherits"
	inheritsListName = "Memo Inherits (auto-managed)"
)

// inheritRefPath returns the per-URL ref name for a normalized memo source URL.
func inheritRefPath(normalizedURL string) string {
	h := sha256.Sum256([]byte(normalizedURL))
	return inheritsRefPrefix + hex.EncodeToString(h[:6])
}

// ListInherits returns the URLs of every memo source the workspace inherits from.
func ListInherits(workdir string) []string {
	refs, err := git.ListRefs(workdir, "memo/inherits/")
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
		if url := strings.TrimSpace(msg); url != "" {
			out = append(out, url)
		}
	}
	return out
}

// AddInherit registers a memo source URL for the workspace and ensures the URL
// is in the managed `memo-inherits` social list with --all-branches so fetch
// picks up the source's gitmsg/memo commits. Idempotent.
//
// Returns true when the inherits ref was newly written, false when the URL was
// already registered. List-side errors are logged but don't fail the call —
// the inherits ref is the source of truth; the list is opportunistic plumbing.
func AddInherit(workdir, url string) Result[bool] {
	normalized := protocol.NormalizeURL(url)
	if normalized == "" {
		return result.Err[bool]("INVALID_ARGS", "invalid URL: "+url)
	}
	ref := inheritRefPath(normalized)
	alreadyInherited := false
	if _, err := git.ReadRef(workdir, ref); err == nil {
		alreadyInherited = true
	}
	if !alreadyInherited {
		hash, err := git.CreateCommitTree(workdir, normalized+"\n", "")
		if err != nil {
			return result.Err[bool]("COMMIT_FAILED", fmt.Sprintf("create inherit ref: %s", err))
		}
		if err := git.WriteRef(workdir, ref, hash); err != nil {
			return result.Err[bool]("REF_WRITE_FAILED", err.Error())
		}
	}
	ensureInheritsListMember(workdir, normalized)
	return result.Ok(!alreadyInherited)
}

// RemoveInherit deletes a memo source URL from both the inherits ref set and
// the managed `memo-inherits` list. Idempotent.
func RemoveInherit(workdir, url string) Result[bool] {
	normalized := protocol.NormalizeURL(url)
	if normalized == "" {
		return result.Ok(false)
	}
	ref := inheritRefPath(normalized)
	if _, err := git.ReadRef(workdir, ref); err != nil {
		// Not in inherits — still try to clean up the list in case of partial state.
		removeFromInheritsList(workdir, normalized)
		return result.Ok(false)
	}
	if err := git.DeleteRef(workdir, ref); err != nil {
		return result.Err[bool]("REF_DELETE_FAILED", err.Error())
	}
	removeFromInheritsList(workdir, normalized)
	return result.Ok(true)
}

// ensureInheritsListMember adds the URL to the memo-inherits social list with
// --all-branches, creating the list on first use. Best-effort: failures are
// logged but don't abort the caller.
func ensureInheritsListMember(workdir, normalizedURL string) {
	if existing := social.GetList(workdir, InheritsListID); !existing.Success || existing.Data == nil {
		if res := social.CreateList(workdir, InheritsListID, inheritsListName); !res.Success {
			log.Debug("memo inherits list create failed", "error", res.Error.Message)
			return
		}
	}
	res := social.AddRepositoryToList(workdir, InheritsListID, normalizedURL, "", true)
	if !res.Success && res.Error.Code != "REPOSITORY_EXISTS" {
		log.Debug("memo inherits list add failed", "url", normalizedURL, "error", res.Error.Message)
	}
}

// removeFromInheritsList strips the URL from the memo-inherits list.
// Best-effort: missing list / missing entry are silent.
func removeFromInheritsList(workdir, normalizedURL string) {
	existing := social.GetList(workdir, InheritsListID)
	if !existing.Success || existing.Data == nil {
		return
	}
	res := social.RemoveRepositoryFromList(workdir, InheritsListID, normalizedURL)
	if !res.Success && res.Error.Code != "REPOSITORY_NOT_FOUND" {
		log.Debug("memo inherits list remove failed", "url", normalizedURL, "error", res.Error.Message)
	}
}

// IsInherited reports whether the given URL is registered as an inherited
// memo source for the workspace.
func IsInherited(workdir, url string) bool {
	normalized := protocol.NormalizeURL(url)
	if normalized == "" {
		return false
	}
	_, err := git.ReadRef(workdir, inheritRefPath(normalized))
	return err == nil
}

// InheritStatus describes one inherited source's freshness: whether it's
// followed (so fetch picks it up) and when it was last fetched. Returned by
// InheritsStatus and surfaced by `gitsocial memo status`.
type InheritStatus struct {
	URL       string    `json:"url"`
	Followed  bool      `json:"followed"`
	LastFetch time.Time `json:"last_fetch"`
}

// InheritsStatus reports freshness for every inherited memo source the
// workspace knows about. Joins the inherits ref set with core_repositories
// (where `gitsocial fetch` records `last_fetch`). An unfollowed URL is a
// legacy state — `inherit add` auto-follows since the §3.3 fix, but
// pre-existing inherits may not be in any list.
func InheritsStatus(workdir string) []InheritStatus {
	urls := ListInherits(workdir)
	if len(urls) == 0 {
		return nil
	}
	repos, _ := cache.GetRepositories()
	repoMap := make(map[string]cache.Repository, len(repos))
	for _, r := range repos {
		repoMap[r.URL] = r
	}
	out := make([]InheritStatus, 0, len(urls))
	for _, u := range urls {
		s := InheritStatus{URL: u}
		if r, ok := repoMap[u]; ok {
			s.Followed = true
			if r.LastFetch.Valid {
				if t, err := time.Parse(time.RFC3339, r.LastFetch.String); err == nil {
					s.LastFetch = t
				}
			}
		}
		out = append(out, s)
	}
	return out
}
