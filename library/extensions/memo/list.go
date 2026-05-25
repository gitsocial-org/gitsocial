// list.go - High-level memo listing across tiers with sensible defaults
package memo

import (
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// ListOptions configures cross-tier memo listing.
type ListOptions struct {
	Tier            Tier   // empty merges across all tiers (excluding external unless IncludeExternal)
	IncludeSessions string // "" = current only, "all" = every session, "<id>" = one session
	IncludeExpired  bool
	OnlyExpired     bool
	IncludeExternal bool // include external (incidentally-followed) repos in the default merge
	Labels          []string
	Limit           int
}

// ListMemos returns memos visible from the workspace using the documented
// defaults: current session + personal + project + inherited (external hidden),
// ordered by tier retrieval rank then priority label then recency.
func ListMemos(workdir string, opts ListOptions) Result[[]Memo] {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	personalURL := ""
	if path, err := settings.PersonalRepoPath(); err == nil && git.BareRepoExists(path) {
		personalURL = LocalRepoURL(path)
	}
	inheritedURLs := ListInherits(workdir)

	q := MemoQuery{
		WorkspaceURL:   workspaceURL,
		PersonalURL:    personalURL,
		InheritedURLs:  inheritedURLs,
		Branch:         MemoBranch,
		Labels:         opts.Labels,
		Limit:          opts.Limit,
		IncludeExpired: opts.IncludeExpired,
		OnlyExpired:    opts.OnlyExpired,
	}

	switch opts.IncludeSessions {
	case "":
		q.SessionID = ResolveSessionID()
	case "all":
		q.IncludeAllSessions = true
	default:
		q.SessionID = opts.IncludeSessions
	}

	switch {
	case opts.Tier != "":
		q.Tier = opts.Tier
		q.TierRepoURLs = repoURLsForTier(opts.Tier, workspaceURL, personalURL, inheritedURLs, q.SessionID, q.IncludeAllSessions)
		if opts.Tier != TierExternal && len(q.TierRepoURLs) == 0 {
			return result.Ok([]Memo{})
		}
	case !opts.IncludeExternal:
		// Default merge: include session/personal/project/inherited and exclude
		// the external long-tail. Sessions surface via repo_url LIKE the session
		// dir prefix; the rest are explicit URLs.
		q.TierRepoURLs = defaultVisibleTierURLs(workspaceURL, personalURL, inheritedURLs, q.SessionID, q.IncludeAllSessions)
		if len(q.TierRepoURLs) == 0 {
			return result.Ok([]Memo{})
		}
	}
	// IncludeExternal && Tier == "": leave TierRepoURLs unset so every cached
	// gitmsg/memo row surfaces alongside the local tiers.

	return GetMemos(q)
}

// defaultVisibleTierURLs returns the repo_url set for the default merge:
// current session(s) + personal + project + every inherited URL.
func defaultVisibleTierURLs(workspaceURL, personalURL string, inheritedURLs []string, sessionID string, allSessions bool) []string {
	urls := repoURLsForTier(TierSession, workspaceURL, personalURL, inheritedURLs, sessionID, allSessions)
	if workspaceURL != "" {
		urls = append(urls, workspaceURL)
	}
	if personalURL != "" {
		urls = append(urls, personalURL)
	}
	urls = append(urls, inheritedURLs...)
	return urls
}

// repoURLsForTier returns the repo_url set for a single requested tier.
func repoURLsForTier(t Tier, workspaceURL, personalURL string, inheritedURLs []string, sessionID string, allSessions bool) []string {
	switch t {
	case TierProject:
		if workspaceURL == "" {
			return nil
		}
		return []string{workspaceURL}
	case TierPersonal:
		if personalURL == "" {
			return nil
		}
		return []string{personalURL}
	case TierInherited:
		if len(inheritedURLs) == 0 {
			return nil
		}
		out := make([]string, len(inheritedURLs))
		copy(out, inheritedURLs)
		return out
	case TierSession:
		var urls []string
		if allSessions {
			for _, p := range listSessionReposForWorkspace(workspaceURL) {
				urls = append(urls, LocalRepoURL(p))
			}
			return urls
		}
		if sessionID == "" {
			sessionID = ResolveSessionID()
		}
		if path, err := SessionRepoPath(sessionID); err == nil && git.BareRepoExists(path) {
			urls = append(urls, LocalRepoURL(path))
		}
		return urls
	case TierExternal:
		// External = anything not in the local tier set or the inherited set.
		// We can't enumerate followed repos here without coupling to core/cache;
		// the caller gets every non-(workspace, personal, session, inherited)
		// row via the tier predicate built at query time. Returning nil here
		// signals "no IN-clause restriction" and the WHERE NOT IN is added
		// below.
		return nil
	}
	return nil
}
