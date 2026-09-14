// observe.go - Branch observation: the live remote tip of every open pull request's branches
package review

import (
	"database/sql"
	"errors"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// branchObservation is a snapshot of one (repo_url, branch) pair.
type branchObservation struct {
	RepoURL    string
	Branch     string
	Tip        string
	Exists     bool
	ObservedAt time.Time
}

// refreshOpenPRBranches records the live remote tip of every branch an open pull request names.
func refreshOpenPRBranches(workdir string) error {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	if workspaceURL == "" {
		return nil
	}
	branch := gitmsg.GetExtBranch(workdir, "review")
	addresses := gitmsg.ForkAddresses(workdir)
	forkURLs := slices.Sorted(maps.Keys(addresses))
	res := GetPullRequestsWithForks(workspaceURL, branch, forkURLs, []string{"open"}, "", 0)
	if !res.Success {
		return errors.New(res.Error.Message)
	}
	now := time.Now()
	seen := make(map[[2]string]struct{})
	rows := make([]branchObservation, 0, len(res.Data)*2)
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
		rows = append(rows, observeBranch(workdir, repoURL, parsed.Value, now, addresses))
	}
	for _, pr := range res.Data {
		collect(protocol.ParseRef(pr.Head))
		collect(protocol.ParseRef(pr.Base))
	}
	return upsertBranchObservations(rows)
}

// ObserveLivePR resolves both sides of one pull request against the remote, now.
func ObserveLivePR(workdir string, pr PullRequest) *PRObservation {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	baseParsed := protocol.ParseRef(pr.Base)
	headParsed := protocol.ParseRef(pr.Head)
	if baseParsed.Type != protocol.RefTypeBranch && headParsed.Type != protocol.RefTypeBranch {
		return nil
	}
	obs := &PRObservation{HeadExists: true, BaseExists: true}
	addresses := gitmsg.ForkAddresses(workdir)
	if headParsed.Type == protocol.RefTypeBranch && headParsed.Value != "" {
		obs.HeadTip, obs.HeadExists = resolveTipShortObs(workdir, workspaceURL, headParsed, addresses)
	}
	if baseParsed.Type == protocol.RefTypeBranch && baseParsed.Value != "" {
		obs.BaseTip, obs.BaseExists = resolveTipShortObs(workdir, workspaceURL, baseParsed, addresses)
	}
	return obs
}

// PRObservation is what one pull request's head and base look like on their remotes.
type PRObservation struct {
	HeadTip    string
	HeadExists bool
	BaseTip    string
	BaseExists bool
}

// PRObservationFromCache reads the stored observations for a pull request, nil when it has none.
func PRObservationFromCache(workspaceURL string, pr PullRequest) *PRObservation {
	headParsed := protocol.ParseRef(pr.Head)
	baseParsed := protocol.ParseRef(pr.Base)
	headRepo, headBranch := refRepoAndBranch(headParsed, workspaceURL)
	baseRepo, baseBranch := refRepoAndBranch(baseParsed, workspaceURL)
	headObs, _ := getBranchObservation(headRepo, headBranch)
	baseObs, _ := getBranchObservation(baseRepo, baseBranch)
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

// observeBranch resolves the live tip of (repoURL, branch) for upsert.
func observeBranch(workdir, repoURL, branch string, now time.Time, addresses map[string]string) branchObservation {
	tip, err := resolveBranchTip(workdir, repoURL, branch, addresses)
	obs := branchObservation{
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

// resolveTipShortObs returns a 12-character remote tip and whether the branch exists.
func resolveTipShortObs(workdir, workspaceURL string, parsed protocol.ParsedRef, addresses map[string]string) (string, bool) {
	tip, err := resolveTipForObservation(workdir, workspaceURL, parsed, addresses)
	if err != nil || tip == "" {
		return "", false
	}
	if len(tip) > 12 {
		tip = tip[:12]
	}
	return tip, true
}

// upsertBranchObservations writes the observation rows in one transaction.
func upsertBranchObservations(rows []branchObservation) error {
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

// getBranchObservation reads one observation, returning sql.ErrNoRows when none was recorded.
func getBranchObservation(repoURL, branch string) (*branchObservation, error) {
	if repoURL == "" || branch == "" {
		return nil, sql.ErrNoRows
	}
	return cache.QueryLocked(func(db *sql.DB) (*branchObservation, error) {
		row := db.QueryRow(`
            SELECT repo_url, branch, tip, branch_exists, observed_at
            FROM review_branch_observations
            WHERE repo_url = ? AND branch = ?`,
			repoURL, branch)
		var obs branchObservation
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

// nullableTip stores an empty tip as NULL, which is how a missing branch reads.
func nullableTip(s string) sql.NullString {
	return sql.NullString{String: s, Valid: strings.TrimSpace(s) != ""}
}

// LocalKnownBranches lists a repository's branches seen in observations or on an open pull request.
func LocalKnownBranches(repoURL string) []string {
	if repoURL == "" {
		return nil
	}
	branches, _ := cache.QueryLocked(func(db *sql.DB) ([]string, error) {
		set := make(map[string]struct{})
		obsRows, err := db.Query(`
            SELECT branch FROM review_branch_observations
            WHERE repo_url = ? AND branch_exists = 1`, repoURL)
		if err == nil {
			for obsRows.Next() {
				var b string
				if obsRows.Scan(&b) == nil && b != "" {
					set[b] = struct{}{}
				}
			}
			obsRows.Close()
		}
		refRows, err := db.Query(`
            SELECT base FROM review_items WHERE state = 'open'
            UNION
            SELECT head FROM review_items WHERE state = 'open'`)
		if err == nil {
			for refRows.Next() {
				var ref string
				if refRows.Scan(&ref) != nil || ref == "" {
					continue
				}
				parsed := protocol.ParseRef(ref)
				if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
					continue
				}
				if parsed.Repository == repoURL {
					set[parsed.Value] = struct{}{}
				}
			}
			refRows.Close()
		}
		out := make([]string, 0, len(set))
		for b := range set {
			out = append(out, b)
		}
		sort.Strings(out)
		return out, nil
	})
	return branches
}

// IsHeadUnpushed reports whether the pull request's head tip is missing from its remote.
func IsHeadUnpushed(workdir string, pr PullRequest) bool {
	if pr.State != PRStateOpen || pr.HeadTip == "" {
		return false
	}
	parsed := protocol.ParseRef(pr.Head)
	if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
		return false
	}
	repoURL := parsed.Repository
	wsURL := gitmsg.ResolveRepoURL(workdir)
	if repoURL == "" {
		repoURL = wsURL
	}
	obs, err := getBranchObservation(repoURL, parsed.Value)
	if err != nil || obs == nil || !obs.Exists {
		return false
	}
	if obs.Tip == "" || obs.Tip == pr.HeadTip {
		return false
	}
	if repoURL == wsURL {
		unpushed, err := git.GetUnpushedCommits(workdir, parsed.Value)
		if err != nil {
			return false
		}
		_, ahead := unpushed[pr.HeadTip]
		return ahead
	}
	return true
}

// UnpushedHeadBranches counts the unpushed commits on each workspace head an open pull request names.
func UnpushedHeadBranches(workdir, remote string) (map[string]int, error) {
	wsURL := gitmsg.ResolveRepoURL(workdir)
	if wsURL == "" {
		return nil, nil
	}
	branch := gitmsg.GetExtBranch(workdir, "review")
	res := GetPullRequests(wsURL, branch, []string{"open"}, "", 0)
	if !res.Success {
		return nil, errors.New(res.Error.Message)
	}
	out := make(map[string]int)
	seen := make(map[string]bool)
	for _, pr := range res.Data {
		parsed := protocol.ParseRef(pr.Head)
		if parsed.Type != protocol.RefTypeBranch || parsed.Value == "" {
			continue
		}
		if parsed.Repository != "" && parsed.Repository != wsURL {
			continue
		}
		if seen[parsed.Value] {
			continue
		}
		seen[parsed.Value] = true
		unpushed, err := git.UnpushedOnBranch(workdir, parsed.Value, remote)
		if err != nil || len(unpushed) == 0 {
			continue
		}
		out[parsed.Value] = len(unpushed)
	}
	return out, nil
}
