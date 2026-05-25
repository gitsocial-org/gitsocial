// sessions.go - Memo session lifecycle (init, list, gc, push, fetch)
package memo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// SessionInfo describes a session bare repo on disk.
//
// `Path` carries the absolute filesystem path so internal callers (gc, sync)
// can act on it directly; MarshalJSON tilde-collapses Path on serialization
// so JSON exports don't leak the absolute home path.
type SessionInfo struct {
	ID          string
	Path        string
	LastUsed    time.Time
	HasRemote   bool
	MemoCount   int                  // active (non-retracted, non-edit) memos in the session
	RecentMemos []SessionMemoSummary // up to 3 most-recent active memos
}

// MarshalJSON tilde-collapses Path so JSON exports don't leak the OS username.
func (s SessionInfo) MarshalJSON() ([]byte, error) {
	type alias SessionInfo
	view := alias(s)
	view.Path = collapseHomePath(view.Path)
	return json.Marshal(view)
}

// SessionMemoSummary captures the bits of a recent memo that the session
// picker shows inline (subject, author, timestamp).
type SessionMemoSummary struct {
	Subject   string
	Author    string
	Timestamp time.Time
}

// InitSession creates (or resumes) the session bare repo for the given id.
// When id is empty it is resolved via env/setting/auto-gen. workspaceURL,
// when non-empty, is recorded so subsequent ListSessions(workspaceURL)
// calls can scope to this workspace. Returns the resolved id.
func InitSession(id, workspaceURL string) Result[string] {
	_, resolved, err := EnsureSessionRepo(id, workspaceURL)
	if err != nil {
		return result.Err[string]("SESSION_INIT_FAILED", err.Error())
	}
	return result.Ok(resolved)
}

// ListSessions returns session bare repos whose recorded workspace-url matches
// workspaceURL (plus untagged legacy repos). Passing "" disables the filter —
// used by gc and other admin paths that operate across workspaces. Each
// session is synced to the cache before its stats are read.
func ListSessions(workspaceURL string) Result[[]SessionInfo] {
	dir, err := SessionDir()
	if err != nil {
		return result.Err[[]SessionInfo]("SESSION_DIR_FAILED", err.Error())
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return result.Ok([]SessionInfo{})
	}
	paths := listSessionReposForWorkspace(workspaceURL)
	sessions := make([]SessionInfo, 0, len(paths))
	for _, path := range paths {
		count, recents := sessionStats(path)
		sessions = append(sessions, SessionInfo{
			ID:          filepath.Base(path),
			Path:        path,
			LastUsed:    sessionLastUsed(path),
			HasRemote:   hasRemote(path),
			MemoCount:   count,
			RecentMemos: recents,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUsed.After(sessions[j].LastUsed)
	})
	return result.Ok(sessions)
}

// GCSession deletes a session bare repo by id, and clears every cache row
// keyed to its `local:<path>` URL so the deleted session leaves no trace.
func GCSession(id string) Result[bool] {
	if id == "" {
		return result.Err[bool]("INVALID_ARGS", "session id required")
	}
	path, err := SessionRepoPath(id)
	if err != nil {
		return result.Err[bool]("SESSION_DIR_FAILED", err.Error())
	}
	if !git.BareRepoExists(path) {
		return result.Err[bool]("NOT_FOUND", "session not found: "+id)
	}
	repoURL := LocalRepoURL(path)
	if err := os.RemoveAll(path); err != nil {
		return result.Err[bool]("GC_FAILED", err.Error())
	}
	if err := cache.ResetRepositoryData(repoURL); err != nil {
		return result.Err[bool]("CACHE_CLEANUP_FAILED", err.Error())
	}
	lastSyncedTip.Delete(path + "\x00" + MemoBranch)
	return result.Ok(true)
}

// GCSessionsOlderThan deletes session bare repos that haven't been touched
// within the given duration. Operates across all workspaces (admin cleanup).
// Cache rows for each deleted session are removed alongside the bare repo.
// Returns the ids deleted.
func GCSessionsOlderThan(d time.Duration) Result[[]string] {
	listed := ListSessions("")
	if !listed.Success {
		return result.Err[[]string](listed.Error.Code, listed.Error.Message)
	}
	cutoff := time.Now().Add(-d)
	var deleted []string
	for _, s := range listed.Data {
		if s.LastUsed.After(cutoff) {
			continue
		}
		repoURL := LocalRepoURL(s.Path)
		if err := os.RemoveAll(s.Path); err != nil {
			return result.Err[[]string]("GC_FAILED", err.Error())
		}
		if err := cache.ResetRepositoryData(repoURL); err != nil {
			return result.Err[[]string]("CACHE_CLEANUP_FAILED", err.Error())
		}
		lastSyncedTip.Delete(s.Path + "\x00" + MemoBranch)
		deleted = append(deleted, s.ID)
	}
	return result.Ok(deleted)
}

// PushSession pushes a session's gitmsg/memo to its remote, auto-merging on
// divergent histories.
func PushSession(id string) Result[bool] {
	if id == "" {
		return result.Err[bool]("INVALID_ARGS", "session id required")
	}
	path, err := SessionRepoPath(id)
	if err != nil {
		return result.Err[bool]("SESSION_DIR_FAILED", err.Error())
	}
	return pushBareRepoMemos(path, "NOT_FOUND", "session not found: "+id)
}

// FetchSession pulls a session's gitmsg/memo from its remote, auto-merging on
// divergence, and re-syncs the cache.
func FetchSession(id string) Result[bool] {
	if id == "" {
		return result.Err[bool]("INVALID_ARGS", "session id required")
	}
	path, err := SessionRepoPath(id)
	if err != nil {
		return result.Err[bool]("SESSION_DIR_FAILED", err.Error())
	}
	return fetchBareRepoMemos(path, "NOT_FOUND", "session not found: "+id)
}

// PushPersonal pushes the personal gitmsg/memo to its remote, auto-merging on
// divergent histories.
func PushPersonal() Result[bool] {
	path, err := settings.PersonalRepoPath()
	if err != nil {
		return result.Err[bool]("PERSONAL_DIR_FAILED", err.Error())
	}
	return pushBareRepoMemos(path, "NOT_INITIALIZED",
		"personal repo not initialized; run `gitsocial personal init`")
}

// FetchPersonal pulls from the personal remote, auto-merging on divergence,
// and re-syncs the cache.
func FetchPersonal() Result[bool] {
	path, err := settings.PersonalRepoPath()
	if err != nil {
		return result.Err[bool]("PERSONAL_DIR_FAILED", err.Error())
	}
	return fetchBareRepoMemos(path, "NOT_INITIALIZED",
		"personal repo not initialized; run `gitsocial personal init`")
}

// pushBareRepoMemos pushes gitmsg/memo from a bare tier repo with auto-merge.
// notFoundCode/notFoundMsg parameterize the missing-repo error for the
// personal-vs-session caller.
func pushBareRepoMemos(path, notFoundCode, notFoundMsg string) Result[bool] {
	if !git.BareRepoExists(path) {
		return result.Err[bool](notFoundCode, notFoundMsg)
	}
	if !hasRemote(path) {
		return result.Err[bool]("NO_REMOTE", "no remote configured")
	}
	if err := gitmsg.PushBranchWithMerge(path, MemoBranch); err != nil {
		return result.Err[bool]("PUSH_FAILED", err.Error())
	}
	return result.Ok(true)
}

// fetchBareRepoMemos fetches gitmsg/memo with auto-merge and resyncs the cache.
func fetchBareRepoMemos(path, notFoundCode, notFoundMsg string) Result[bool] {
	if !git.BareRepoExists(path) {
		return result.Err[bool](notFoundCode, notFoundMsg)
	}
	if !hasRemote(path) {
		return result.Err[bool]("NO_REMOTE", "no remote configured")
	}
	if err := gitmsg.FetchAndMergeBranch(path, MemoBranch); err != nil {
		return result.Err[bool]("FETCH_FAILED", err.Error())
	}
	if err := SyncTierRepoToCache(path); err != nil {
		return result.Err[bool]("CACHE_FAILED", err.Error())
	}
	return result.Ok(true)
}

// sessionStats returns the active-memo count and a summary of the most recent
// (up to 3) active memos for a session bare repo. Syncs the repo into the
// cache first so the stats reflect what `memo list` would show.
// Best-effort: errors result in a zero count and empty summaries.
func sessionStats(path string) (int, []SessionMemoSummary) {
	if err := SyncTierRepoToCache(path); err != nil {
		return 0, nil
	}
	repoURL := LocalRepoURL(path)
	items, err := GetMemoItems(MemoQuery{
		RepoURL: repoURL,
		Branch:  MemoBranch,
	})
	if err != nil {
		return 0, nil
	}
	if len(items) == 0 {
		return 0, nil
	}
	limit := 3
	if len(items) < limit {
		limit = len(items)
	}
	recents := make([]SessionMemoSummary, 0, limit)
	for i := 0; i < limit; i++ {
		subj := splitSubjectOnly(items[i].Content)
		author := items[i].AuthorName
		if author == "" {
			author = items[i].AuthorEmail
		}
		recents = append(recents, SessionMemoSummary{
			Subject:   subj,
			Author:    author,
			Timestamp: items[i].Timestamp,
		})
	}
	return len(items), recents
}

// splitSubjectOnly returns just the first non-empty line as a subject.
func splitSubjectOnly(content string) string {
	for _, line := range splitLines(content) {
		if line != "" {
			return line
		}
	}
	return ""
}

// splitLines is a small helper that avoids pulling strings.Split into the
// hot path of the picker render.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// sessionLastUsed returns the timestamp of the most recent commit on
// gitmsg/memo in the session bare repo. Falls back to the directory mtime for
// freshly-init'd sessions (no commits yet) or when git can't read the branch.
//
// Directly using os.Stat(path).ModTime() is fragile: backups, `cp -p`,
// filesystem migrations, and cache-dir restores all bump the mtime without
// reflecting any memo activity. Commit timestamps are authoritative — they
// track actual writes to the session.
func sessionLastUsed(path string) time.Time {
	out, err := git.ExecGit(path, []string{
		"log", "-1", "--format=%cI", "refs/heads/" + MemoBranch,
	})
	if err == nil {
		raw := strings.TrimSpace(out.Stdout)
		if raw != "" {
			if t, perr := time.Parse(time.RFC3339, raw); perr == nil {
				return t
			}
		}
	}
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// hasRemote checks whether the bare repo at path has an origin remote configured.
func hasRemote(path string) bool {
	out, err := git.ExecGit(path, []string{"config", "--get", "remote.origin.url"})
	if err != nil {
		return false
	}
	return out.Stdout != ""
}

// FormatAge produces a short human-readable age for a session timestamp.
func FormatAge(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
