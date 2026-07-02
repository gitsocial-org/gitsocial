// versions.go - PR version history, range-diff, and version-aware reviews
package review

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

type PRVersion struct {
	Number      int       `json:"number"`
	Label       string    `json:"label"`
	CommitHash  string    `json:"commit_hash"`
	RepoURL     string    `json:"repo_url"`
	Branch      string    `json:"branch"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email"`
	Timestamp   time.Time `json:"timestamp"`
	Subject     string    `json:"subject,omitempty"`
	Body        string    `json:"body,omitempty"`
	BaseTip     string    `json:"base_tip,omitempty"`
	HeadTip     string    `json:"head_tip,omitempty"`
	State       PRState   `json:"state"`
	IsRetracted bool      `json:"is_retracted,omitempty"`
}

// GetPRVersions retrieves all versions of a PR ordered by timestamp ascending (oldest first).
func GetPRVersions(prRef, workspaceURL string) Result[[]PRVersion] {
	parsed := protocol.ParseRef(prRef)
	if parsed.Value == "" {
		// Try as a short hash via GetPR
		pr := GetPR(prRef)
		if !pr.Success {
			return result.Err[[]PRVersion]("NOT_FOUND", "pull request not found: "+prRef)
		}
		parsed = protocol.ParseRef(pr.Data.ID)
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = workspaceURL
	}
	commitHash := parsed.Value
	branch := parsed.Branch
	if branch == "" {
		branch = "gitmsg/review"
	}

	canonicalRepoURL, canonicalHash, canonicalBranch, err := cache.ResolveToCanonical(repoURL, commitHash, branch)
	if err != nil {
		return result.Err[[]PRVersion]("RESOLVE_FAILED", err.Error())
	}

	type rawVersion struct {
		RepoURL     string
		Hash        string
		Branch      string
		AuthorName  string
		AuthorEmail string
		Message     string
		Timestamp   string
		Edits       sql.NullString
	}

	rows, err := cache.QueryLocked(func(db *sql.DB) ([]rawVersion, error) {
		query := `
			SELECT repo_url, hash, branch, author_name, author_email, message, timestamp, edits
			FROM core_commits
			WHERE repo_url = ? AND hash = ? AND branch = ?
			UNION ALL
			SELECT c.repo_url, c.hash, c.branch, c.author_name, c.author_email, c.message, c.timestamp, c.edits
			FROM core_commits c
			JOIN core_commits_version v ON v.edit_repo_url = c.repo_url AND v.edit_hash = c.hash AND v.edit_branch = c.branch
			WHERE v.canonical_repo_url = ? AND v.canonical_hash = ? AND v.canonical_branch = ?
			ORDER BY timestamp ASC`
		dbRows, err := db.Query(query, canonicalRepoURL, canonicalHash, canonicalBranch, canonicalRepoURL, canonicalHash, canonicalBranch)
		if err != nil {
			return nil, err
		}
		defer dbRows.Close()
		var results []rawVersion
		for dbRows.Next() {
			var r rawVersion
			if err := dbRows.Scan(&r.RepoURL, &r.Hash, &r.Branch, &r.AuthorName, &r.AuthorEmail, &r.Message, &r.Timestamp, &r.Edits); err != nil {
				return nil, err
			}
			results = append(results, r)
		}
		return results, dbRows.Err()
	})
	if err != nil {
		return result.Err[[]PRVersion]("QUERY_FAILED", err.Error())
	}
	if len(rows) == 0 {
		return result.Err[[]PRVersion]("NOT_FOUND", "no versions found")
	}

	versions := make([]PRVersion, 0, len(rows))
	for i, r := range rows {
		ts, _ := time.Parse(time.RFC3339, r.Timestamp)
		v := PRVersion{
			Number:      i,
			CommitHash:  r.Hash,
			RepoURL:     r.RepoURL,
			Branch:      r.Branch,
			AuthorName:  r.AuthorName,
			AuthorEmail: r.AuthorEmail,
			Timestamp:   ts,
		}
		if msg := protocol.ParseMessage(r.Message); msg != nil {
			v.BaseTip = msg.Header.Fields["base-tip"]
			v.HeadTip = msg.Header.Fields["head-tip"]
			v.State = PRState(msg.Header.Fields["state"])
			v.IsRetracted = msg.Header.Fields["retracted"] == "true"
		}
		body := protocol.ExtractCleanContent(r.Message)
		if idx := indexOfNewline(body); idx >= 0 {
			v.Subject = body[:idx]
			v.Body = body[idx+1:]
		} else {
			v.Subject = body
		}
		// Labels: 0=original, last=latest, middle=v1,v2,...
		switch {
		case i == 0:
			v.Label = "original"
		case i == len(rows)-1:
			v.Label = "latest"
		default:
			v.Label = fmt.Sprintf("v%d", i)
		}
		// Single version gets "original" only
		if len(rows) == 1 {
			v.Label = "original"
		}
		versions = append(versions, v)
	}
	return result.Ok(versions)
}

// ComparePRVersions returns a git range-diff between two PR versions.
func ComparePRVersions(workdir, cacheDir, prRef string, fromVersion, toVersion int) Result[string] {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	res := GetPRVersions(prRef, workspaceURL)
	if !res.Success {
		return result.Err[string](res.Error.Code, res.Error.Message)
	}
	versions := res.Data
	if fromVersion < 0 || fromVersion >= len(versions) {
		return result.Err[string]("INVALID_VERSION", fmt.Sprintf("from version %d out of range (0-%d)", fromVersion, len(versions)-1))
	}
	if toVersion < 0 || toVersion >= len(versions) {
		return result.Err[string]("INVALID_VERSION", fmt.Sprintf("to version %d out of range (0-%d)", toVersion, len(versions)-1))
	}
	from := versions[fromVersion]
	to := versions[toVersion]
	if from.BaseTip == "" || from.HeadTip == "" {
		return result.Err[string]("MISSING_TIPS", fmt.Sprintf("version %d has no base-tip/head-tip", fromVersion))
	}
	if to.BaseTip == "" || to.HeadTip == "" {
		return result.Err[string]("MISSING_TIPS", fmt.Sprintf("version %d has no base-tip/head-tip", toVersion))
	}
	// Identical tips mean the edit between these versions changed only metadata
	// or the description, not code — no range-diff to show.
	if from.BaseTip == to.BaseTip && from.HeadTip == to.HeadTip {
		return result.Ok("")
	}
	// The tips are raw commit hashes. For a fork PR the head commits live in the
	// fork, not the workspace, so fetch both sides into a fork repo (the same
	// resolution the files-changed diff uses) when they aren't all local.
	rd := workdir
	if !tipsPresent(workdir, from, to) {
		if pr := GetPR(prRef); pr.Success {
			if ctx := ResolveDiffContext(workdir, cacheDir, pr.Data.Base, pr.Data.Head); ctx.Workdir != "" {
				rd = ctx.Workdir
			}
		}
	}
	if !tipsPresent(rd, from, to) {
		return result.Err[string]("TIPS_UNAVAILABLE", "could not resolve version tips locally (fork PR commits may be unavailable); use the files-changed view (d) to see the current diff")
	}
	output, err := git.RangeDiff(rd, from.BaseTip, from.HeadTip, to.BaseTip, to.HeadTip)
	if err != nil {
		return result.Err[string]("RANGE_DIFF_FAILED", err.Error())
	}
	return result.Ok(output)
}

// tipsPresent reports whether all four version tips resolve as commits in repo.
func tipsPresent(repo string, from, to PRVersion) bool {
	return git.CommitExists(repo, from.BaseTip) && git.CommitExists(repo, from.HeadTip) &&
		git.CommitExists(repo, to.BaseTip) && git.CommitExists(repo, to.HeadTip)
}

type VersionAwareReview struct {
	ReviewerName    string      `json:"reviewer_name"`
	ReviewerEmail   string      `json:"reviewer_email"`
	State           ReviewState `json:"state"`
	ReviewedAt      time.Time   `json:"reviewed_at"`
	ReviewedVersion int         `json:"reviewed_version"`
	ReviewedLabel   string      `json:"reviewed_label"`
	CurrentVersion  int         `json:"current_version"`
	CurrentLabel    string      `json:"current_label"`
	HeadChanged     bool        `json:"head_changed"`
	CodeChanged     bool        `json:"code_changed"`
	Stale           bool        `json:"stale"`
}

// GetVersionAwareReviews computes per-reviewer review staleness against PR versions.
func GetVersionAwareReviews(workdir, prRef string) Result[[]VersionAwareReview] {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	vRes := GetPRVersions(prRef, workspaceURL)
	if !vRes.Success {
		return result.Err[[]VersionAwareReview](vRes.Error.Code, vRes.Error.Message)
	}
	versions := vRes.Data
	if len(versions) == 0 {
		return result.Ok([]VersionAwareReview{})
	}

	// Get the canonical PR to fetch feedback
	prRes := GetPR(prRef)
	if !prRes.Success {
		return result.Err[[]VersionAwareReview](prRes.Error.Code, prRes.Error.Message)
	}
	pr := prRes.Data
	feedbackRes := GetFeedbackForPR(pr.Repository, extractHashFromID(pr.ID), pr.Branch)
	if !feedbackRes.Success {
		return result.Err[[]VersionAwareReview](feedbackRes.Error.Code, feedbackRes.Error.Message)
	}

	currentVersion := len(versions) - 1
	latestHeadTip := versions[currentVersion].HeadTip

	// Deduplicate: keep each reviewer's latest stateful feedback
	type reviewerInfo struct {
		name  string
		email string
		state ReviewState
		ts    time.Time
	}
	latestByAuthor := map[string]reviewerInfo{}
	for _, f := range feedbackRes.Data {
		if f.ReviewState == "" {
			continue
		}
		prev, exists := latestByAuthor[f.Author.Email]
		if !exists || f.Timestamp.After(prev.ts) {
			latestByAuthor[f.Author.Email] = reviewerInfo{
				name:  f.Author.Name,
				email: f.Author.Email,
				state: f.ReviewState,
				ts:    f.Timestamp,
			}
		}
	}

	var reviews []VersionAwareReview
	for _, info := range latestByAuthor {
		// Find which version was current when feedback was given
		reviewedVersion := 0
		for i, v := range versions {
			if !v.Timestamp.After(info.ts) {
				reviewedVersion = i
			}
		}
		rv := versions[reviewedVersion]
		headChanged := rv.HeadTip != latestHeadTip
		codeChanged := headChanged
		if headChanged && rv.BaseTip != "" && rv.HeadTip != "" && versions[currentVersion].BaseTip != "" && latestHeadTip != "" {
			if equal, err := git.PatchesEqual(workdir, rv.BaseTip, rv.HeadTip, versions[currentVersion].BaseTip, latestHeadTip); err == nil {
				codeChanged = !equal
			} else {
				// Degraded staleness: without the patch comparison (commits not
				// local, etc.) a pure rebase reads as a code change.
				log.Debug("PatchesEqual failed; treating head move as code change",
					"reviewedHead", rv.HeadTip, "latestHead", latestHeadTip, "error", err)
			}
		}
		stale := codeChanged && reviewedVersion < currentVersion
		reviews = append(reviews, VersionAwareReview{
			ReviewerName:    info.name,
			ReviewerEmail:   info.email,
			State:           info.state,
			ReviewedAt:      info.ts,
			ReviewedVersion: reviewedVersion,
			ReviewedLabel:   rv.Label,
			CurrentVersion:  currentVersion,
			CurrentLabel:    versions[currentVersion].Label,
			HeadChanged:     headChanged,
			CodeChanged:     codeChanged,
			Stale:           stale,
		})
	}
	return result.Ok(reviews)
}

// indexOfNewline returns the index of the first newline in s, or -1 if none.
func indexOfNewline(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}

// extractHashFromID extracts the hash portion from a PR ID ref.
func extractHashFromID(id string) string {
	parsed := protocol.ParseRef(id)
	if parsed.Value != "" {
		return parsed.Value
	}
	return id
}
