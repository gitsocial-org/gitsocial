// observe.go - Branch observation: live remote tip vs stored PR tips.
//
// After `gitsocial fetch`, RefreshOpenPRBranches walks every branch any
// open PR's head or base points at — workspace branches and registered-fork
// branches alike — and records the live remote tip in
// review_branch_observations. Observations are keyed by (repo_url, branch),
// not by PR, so a single push to `feature` updates one row and is reflected
// for every PR that targets that branch. The PR's stored head-tip /
// base-tip is part of the protocol record and only changes when someone
// runs `pr update`; observations live alongside as a transient "what the
// remote looks like right now" snapshot, used to surface notifications and
// stale-tip warnings without polluting the edit chain.
package review

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// BranchObservation is a snapshot of one (repo_url, branch) pair: the
// current remote tip (12-char) and whether the branch still exists on the
// remote.
type BranchObservation struct {
	RepoURL    string
	Branch     string
	Tip        string
	Exists     bool
	ObservedAt time.Time
}

// RefreshOpenPRBranches resolves the live remote tip of every (repo_url,
// branch) pair referenced by an open PR's head or base, across the
// workspace and any registered fork, and upserts the result into
// review_branch_observations. Idempotent and safe to call repeatedly —
// one row per (repo_url, branch) regardless of how many PRs reference it.
func RefreshOpenPRBranches(workdir string) error {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil
	}
	branch := gitmsg.GetExtBranch(workdir, "review")
	forkURLs := gitmsg.GetForks(workdir)
	res := GetPullRequestsWithForks(workspaceURL, branch, forkURLs, []string{"open"}, "", 0)
	if !res.Success {
		return errors.New(res.Error.Message)
	}
	now := time.Now()
	seen := make(map[[2]string]struct{})
	rows := make([]BranchObservation, 0, len(res.Data)*2)
	collect := func(parsed protocol.ParsedRef) {
		if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
			return
		}
		repoURL := parsed.Repository
		if repoURL == "" {
			repoURL = workspaceURL
		}
		key := [2]string{repoURL, parsed.Value}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		rows = append(rows, observeBranch(workdir, repoURL, parsed.Value, now))
	}
	for _, pr := range res.Data {
		collect(protocol.ParseRef(pr.Head))
		collect(protocol.ParseRef(pr.Base))
	}
	return upsertBranchObservations(rows)
}

// ObserveLivePR resolves both sides of a single PR through ResolveBranchTip
// and returns a per-PR view of the observation. Used by `pr show` and the
// TUI detail view to render an up-to-the-second divergence indicator
// without waiting for the next fetch-driven refresh.
func ObserveLivePR(workdir string, pr PullRequest) *PRObservation {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	baseParsed := protocol.ParseRef(pr.Base)
	headParsed := protocol.ParseRef(pr.Head)
	if baseParsed.Type != protocol.RefTypeBranch && headParsed.Type != protocol.RefTypeBranch {
		return nil
	}
	obs := &PRObservation{HeadExists: true, BaseExists: true}
	if headParsed.Type == protocol.RefTypeBranch && headParsed.Value != "" {
		obs.HeadTip, obs.HeadExists = resolveTipShortObs(workdir, workspaceURL, headParsed)
	}
	if baseParsed.Type == protocol.RefTypeBranch && baseParsed.Value != "" {
		obs.BaseTip, obs.BaseExists = resolveTipShortObs(workdir, workspaceURL, baseParsed)
	}
	return obs
}

// PRObservation is the per-PR projection of branch observations. The cache
// is keyed by branch; this struct is the answer to "for this PR, what do
// the head and base look like right now?" Used only by display paths.
type PRObservation struct {
	HeadTip    string
	HeadExists bool
	BaseTip    string
	BaseExists bool
}

// PRObservationFromCache reads stored observations for a PR's head and base
// branches. Returns nil when neither side has an observation row — caller
// (display layer) can fall back to ObserveLivePR.
func PRObservationFromCache(workspaceURL string, pr PullRequest) *PRObservation {
	headParsed := protocol.ParseRef(pr.Head)
	baseParsed := protocol.ParseRef(pr.Base)
	headRepo, headBranch := resolveRepoBranch(headParsed, workspaceURL)
	baseRepo, baseBranch := resolveRepoBranch(baseParsed, workspaceURL)
	headObs, _ := GetBranchObservation(headRepo, headBranch)
	baseObs, _ := GetBranchObservation(baseRepo, baseBranch)
	if headObs == nil && baseObs == nil {
		return nil
	}
	out := &PRObservation{HeadExists: true, BaseExists: true}
	if headObs != nil {
		out.HeadTip = headObs.Tip
		out.HeadExists = headObs.Exists
	}
	if baseObs != nil {
		out.BaseTip = baseObs.Tip
		out.BaseExists = baseObs.Exists
	}
	return out
}

func resolveRepoBranch(parsed protocol.ParsedRef, workspaceURL string) (string, string) {
	if parsed.Type != protocol.RefTypeBranch {
		return "", ""
	}
	repo := parsed.Repository
	if repo == "" {
		repo = workspaceURL
	}
	return repo, parsed.Value
}

// observeBranch resolves the live tip of (repoURL, branch) via the strict
// remote resolver and packages the result for upsert.
func observeBranch(workdir, repoURL, branch string, now time.Time) BranchObservation {
	tip, err := ResolveBranchTip(workdir, repoURL, branch)
	obs := BranchObservation{
		RepoURL:    repoURL,
		Branch:     branch,
		Exists:     err == nil && tip != "",
		ObservedAt: now,
	}
	if obs.Exists {
		if len(tip) > 12 {
			tip = tip[:12]
		}
		obs.Tip = tip
	}
	return obs
}

// resolveTipShortObs returns a 12-char tip and a bool indicating the branch
// exists on the remote. Used by ObserveLivePR to preserve the strict
// remote-only semantics expected by the display layer (deletion is
// surfaced, not masked by a local fallback).
func resolveTipShortObs(workdir, workspaceURL string, parsed protocol.ParsedRef) (string, bool) {
	tip, err := resolveTipForObservation(workdir, workspaceURL, parsed)
	if err != nil || tip == "" {
		return "", false
	}
	if len(tip) > 12 {
		tip = tip[:12]
	}
	return tip, true
}

// upsertBranchObservations writes the observation rows to the cache. A
// single transaction keeps callers from racing partial state.
func upsertBranchObservations(rows []BranchObservation) error {
	if len(rows) == 0 {
		return nil
	}
	return cache.ExecLocked(func(db *sql.DB) error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		stmt, err := tx.Prepare(`
            INSERT INTO review_branch_observations
                (repo_url, branch, tip, branch_exists, observed_at)
            VALUES (?, ?, ?, ?, ?)
            ON CONFLICT(repo_url, branch) DO UPDATE SET
                tip = excluded.tip,
                branch_exists = excluded.branch_exists,
                observed_at = excluded.observed_at`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, r := range rows {
			_, err := stmt.Exec(
				r.RepoURL, r.Branch,
				nullableTip(r.Tip),
				boolToInt(r.Exists),
				r.ObservedAt.Format(time.RFC3339),
			)
			if err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

// GetBranchObservation reads the latest observation for a (repo_url, branch)
// pair. Returns (nil, sql.ErrNoRows) when no observation has been recorded.
func GetBranchObservation(repoURL, branch string) (*BranchObservation, error) {
	if repoURL == "" || branch == "" {
		return nil, sql.ErrNoRows
	}
	return cache.QueryLocked(func(db *sql.DB) (*BranchObservation, error) {
		row := db.QueryRow(`
            SELECT repo_url, branch, tip, branch_exists, observed_at
            FROM review_branch_observations
            WHERE repo_url = ? AND branch = ?`,
			repoURL, branch)
		var obs BranchObservation
		var tip sql.NullString
		var exists int
		var observedAt string
		if err := row.Scan(&obs.RepoURL, &obs.Branch, &tip, &exists, &observedAt); err != nil {
			return nil, err
		}
		obs.Tip = tip.String
		obs.Exists = exists == 1
		obs.ObservedAt, _ = time.Parse(time.RFC3339, observedAt)
		return &obs, nil
	})
}

// nullableTip returns sql.NullString.Valid=false for empty tip strings so
// the stored value is NULL (matches "branch missing" semantics).
func nullableTip(s string) sql.NullString {
	return sql.NullString{String: s, Valid: strings.TrimSpace(s) != ""}
}
