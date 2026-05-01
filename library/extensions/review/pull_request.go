// pull_request.go - Pull request creation and management
package review

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
)

type CreatePROptions struct {
	Base      string
	BaseTip   string
	Head      string
	HeadTip   string
	DependsOn []string
	Closes    []string
	Reviewers []string
	Labels    []string
	MergeBase string
	MergeHead string
	Draft     bool
	Origin    *protocol.Origin
	// AllowUnpublishedHead skips the "head branch resolvable" check. Set this
	// only when the caller has a reason to record a tip-less PR (e.g., tests
	// that don't exercise branch resolution). Production callers should leave
	// this false so unpushed branches are caught early.
	AllowUnpublishedHead bool
}

// CreatePR creates a new pull request on the review branch.
func CreatePR(workdir, subject, body string, opts CreatePROptions) Result[PullRequest] {
	branch := gitmsg.GetExtBranch(workdir, "review")
	repoURL := gitmsg.ResolveRepoURL(workdir)
	opts.Base = protocol.EnsureBranchRef(opts.Base)
	opts.Head = protocol.EnsureBranchRef(opts.Head)
	opts.Base = protocol.LocalizeRef(opts.Base, repoURL)
	opts.Head = protocol.LocalizeRef(opts.Head, repoURL)
	for i, ref := range opts.DependsOn {
		opts.DependsOn[i] = protocol.LocalizeRef(ref, repoURL)
	}
	for i, ref := range opts.Closes {
		opts.Closes[i] = protocol.LocalizeRef(ref, repoURL)
	}

	// Auto-resolve branch tips if not already set
	if opts.BaseTip == "" {
		if tip, err := resolveTipForWrite(workdir, repoURL, protocol.ParseRef(opts.Base)); err == nil && len(tip) >= 12 {
			opts.BaseTip = tip[:12]
		}
	}
	if opts.HeadTip == "" {
		if tip, err := resolveTipForWrite(workdir, repoURL, protocol.ParseRef(opts.Head)); err == nil && len(tip) >= 12 {
			opts.HeadTip = tip[:12]
		}
	}

	if opts.Head != "" && opts.HeadTip == "" && !opts.AllowUnpublishedHead {
		return result.Err[PullRequest]("HEAD_NOT_FOUND",
			fmt.Sprintf("head branch %q not found on origin or locally — push it first?", opts.Head))
	}

	content := buildPRContent(subject, body, opts, "")
	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[PullRequest]("COMMIT_FAILED", err.Error())
	}
	if err := cacheReviewFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[PullRequest]("CACHE_FAILED", err.Error())
	}

	item, err := GetReviewItem(repoURL, hash, branch)
	if err != nil {
		return result.Err[PullRequest]("GET_FAILED", err.Error())
	}
	return result.Ok(ReviewItemToPullRequest(*item))
}

// GetPR retrieves a single pull request by reference.
func GetPR(prRef string) Result[PullRequest] {
	// Try direct indexed lookup first (covers full refs and full hashes)
	item, err := GetReviewItemByRef(prRef, "")
	if err == nil && item.Type == string(ItemTypePullRequest) {
		return result.Ok(ReviewItemToPullRequest(*item))
	}
	// Fall back to prefix scan for short hashes
	items, err := GetReviewItems(ReviewQuery{
		Types: []string{string(ItemTypePullRequest)},
		Limit: 1000,
	})
	if err != nil {
		return result.Err[PullRequest]("QUERY_FAILED", err.Error())
	}
	for _, item := range items {
		pr := ReviewItemToPullRequest(item)
		if pr.ID == prRef || strings.HasPrefix(pr.ID, prRef) {
			return result.Ok(pr)
		}
		parsed := protocol.ParseRef(pr.ID)
		if parsed.Value != "" && strings.HasPrefix(parsed.Value, prRef) {
			return result.Ok(pr)
		}
	}
	return result.Err[PullRequest]("NOT_FOUND", "pull request not found: "+prRef)
}

type UpdatePROptions struct {
	State     *PRState
	Draft     *bool
	Base      *string
	BaseTip   *string
	Head      *string
	HeadTip   *string
	DependsOn *[]string
	Closes    *[]string
	Reviewers *[]string
	Labels    *[]string
	Subject   *string
	Body      *string
	MergeBase *string
	MergeHead *string
	Origin    *protocol.Origin
}

// UpdatePR edits an existing pull request using core versioning.
func UpdatePR(workdir, prRef string, opts UpdatePROptions) Result[PullRequest] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[PullRequest]("NOT_FOUND", "pull request not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "review")

	pr := ReviewItemToPullRequest(*existing)
	createOpts := CreatePROptions{
		Base:      pr.Base,
		BaseTip:   pr.BaseTip,
		Head:      pr.Head,
		HeadTip:   pr.HeadTip,
		DependsOn: pr.DependsOn,
		Closes:    pr.Closes,
		Reviewers: pr.Reviewers,
		Labels:    pr.Labels,
		Draft:     pr.IsDraft,
		// Preserve merge-base / merge-head when editing an already-merged
		// PR (e.g., subject tweak). The opts overrides below take effect
		// only when the caller explicitly passes them.
		MergeBase: pr.MergeBase,
		MergeHead: pr.MergeHead,
	}
	if opts.Origin != nil {
		createOpts.Origin = opts.Origin
	} else {
		createOpts.Origin = existing.Origin
	}
	subject := pr.Subject
	body := pr.Body
	state := pr.State

	if opts.State != nil {
		state = *opts.State
	}
	if opts.Draft != nil {
		createOpts.Draft = *opts.Draft
	}
	if opts.Base != nil {
		createOpts.Base = *opts.Base
	}
	if opts.BaseTip != nil {
		createOpts.BaseTip = *opts.BaseTip
	}
	if opts.Head != nil {
		createOpts.Head = *opts.Head
	}
	if opts.HeadTip != nil {
		createOpts.HeadTip = *opts.HeadTip
	}
	if opts.DependsOn != nil {
		createOpts.DependsOn = *opts.DependsOn
	}
	if opts.Closes != nil {
		createOpts.Closes = *opts.Closes
	}
	if opts.Reviewers != nil {
		createOpts.Reviewers = *opts.Reviewers
	}
	if opts.Labels != nil {
		createOpts.Labels = *opts.Labels
	}
	if opts.MergeBase != nil {
		createOpts.MergeBase = *opts.MergeBase
	}
	if opts.MergeHead != nil {
		createOpts.MergeHead = *opts.MergeHead
	}
	if opts.Subject != nil {
		subject = *opts.Subject
	}
	if opts.Body != nil {
		body = *opts.Body
	}

	// GITREVIEW.md §1.5: state="merged" edits MUST carry merge-base and
	// merge-head. Reject the transition at the API boundary so direct
	// UpdatePR callers can't bypass MergePR's pre-flight checks.
	if state == PRStateMerged {
		if createOpts.MergeBase == "" {
			return result.Err[PullRequest]("MERGE_INCOMPLETE",
				"cannot record state=merged without merge-base")
		}
		if createOpts.MergeHead == "" {
			return result.Err[PullRequest]("MERGE_INCOMPLETE",
				"cannot record state=merged without merge-head")
		}
	}

	// Normalize and localize refs before committing
	createOpts.Base = protocol.EnsureBranchRef(createOpts.Base)
	createOpts.Head = protocol.EnsureBranchRef(createOpts.Head)
	createOpts.Base = protocol.LocalizeRef(createOpts.Base, repoURL)
	createOpts.Head = protocol.LocalizeRef(createOpts.Head, repoURL)
	for i, ref := range createOpts.DependsOn {
		createOpts.DependsOn[i] = protocol.LocalizeRef(ref, repoURL)
	}
	for i, ref := range createOpts.Closes {
		createOpts.Closes[i] = protocol.LocalizeRef(ref, repoURL)
	}

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	content := buildPRContentWithState(subject, body, createOpts, canonicalRef, state, existing.References)

	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[PullRequest]("COMMIT_FAILED", err.Error())
	}

	if err := cacheReviewFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[PullRequest]("CACHE_FAILED", err.Error())
	}

	item, err := GetReviewItem(existing.RepoURL, existing.Hash, existing.Branch)
	if err != nil {
		return result.Err[PullRequest]("GET_FAILED", err.Error())
	}
	return result.Ok(ReviewItemToPullRequest(*item))
}

// MergePR merges the head branch into the base branch, then updates PR state to merged.
func MergePR(workdir, prRef string, strategy MergeStrategy) Result[PullRequest] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[PullRequest]("NOT_FOUND", "pull request not found")
	}
	pr := ReviewItemToPullRequest(*existing)
	if pr.State != PRStateOpen {
		return result.Err[PullRequest]("INVALID_STATE", fmt.Sprintf("cannot merge: pull request is %s", pr.State))
	}
	if pr.IsDraft {
		return result.Err[PullRequest]("DRAFT_PR", "cannot merge: pull request is a draft")
	}

	// Reject merge if the PR's base branch explicitly targets a different repository
	if baseRepo := protocol.ParseRef(pr.Base).Repository; baseRepo != "" && baseRepo != repoURL {
		return result.Err[PullRequest]("INVALID_TARGET", "cannot merge: pull request targets a different repository")
	}

	// Enforce merge ordering: all depends-on targets must be merged first
	for _, depRef := range pr.DependsOn {
		depResult := GetPR(depRef)
		if !depResult.Success {
			return result.Err[PullRequest]("UNMET_DEPENDENCY",
				fmt.Sprintf("cannot merge: dependency %s not found", depRef))
		}
		if depResult.Data.State != PRStateMerged {
			return result.Err[PullRequest]("UNMET_DEPENDENCY",
				fmt.Sprintf("cannot merge: dependency \"%s\" is %s (must be merged first)", depResult.Data.Subject, depResult.Data.State))
		}
	}

	// Copy fork PR to upstream review branch so the merge record is self-contained
	if existing.RepoURL != repoURL {
		branch := gitmsg.GetExtBranch(workdir, "review")
		copyOpts := CreatePROptions{
			Base:      protocol.LocalizeRef(protocol.EnsureBranchRef(pr.Base), repoURL),
			Head:      protocol.LocalizeRef(protocol.EnsureBranchRef(pr.Head), repoURL),
			DependsOn: pr.DependsOn,
			Closes:    pr.Closes,
			Reviewers: pr.Reviewers,
		}
		copyOpts.Origin = existing.Origin
		for i, ref := range copyOpts.Closes {
			copyOpts.Closes[i] = protocol.LocalizeRef(ref, repoURL)
		}
		forkRef := protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch)
		refs := []protocol.Ref{{
			Ext:      "review",
			Author:   pr.Author.Name,
			Email:    pr.Author.Email,
			Time:     pr.Timestamp.Format(time.RFC3339),
			Ref:      forkRef,
			V:        "0.1.0",
			Fields:   map[string]string{"type": string(ItemTypePullRequest)},
			Metadata: protocol.QuoteContent(joinSubjectBody(pr.Subject, pr.Body)),
		}}
		content := buildPRContentWithState(pr.Subject, pr.Body, copyOpts, "", PRStateOpen, refs)
		hash, err := git.CreateCommitOnBranch(workdir, branch, content)
		if err != nil {
			return result.Err[PullRequest]("COMMIT_FAILED", "copy fork PR: "+err.Error())
		}
		if err := cacheReviewFromCommit(workdir, repoURL, hash, branch); err != nil {
			return result.Err[PullRequest]("CACHE_FAILED", "cache copied PR: "+err.Error())
		}
		prRef = hash
	}

	// Snapshot merge-base and head hash before the merge (lost after FF merge)
	baseName := protocol.ParseRef(pr.Base).Value
	headParsed := protocol.ParseRef(pr.Head)
	headBranch := headParsed.Value
	headSourceURL := headParsed.Repository
	if headSourceURL == "" {
		headSourceURL = repoURL
	}

	// merge-base / merge-head are REQUIRED by GITREVIEW.md §1.5 on
	// state="merged" edits — they're the only durable record of the merged
	// commit range once the head branch is deleted. Refuse early if either
	// PR ref is malformed; the merge can't produce a complete record.
	if baseName == "" {
		return result.Err[PullRequest]("MERGE_INCOMPLETE",
			"cannot merge: base ref missing or not a branch ref")
	}
	if headBranch == "" {
		return result.Err[PullRequest]("MERGE_INCOMPLETE",
			"cannot merge: head ref missing or not a branch ref")
	}

	// Fetch the PR's head into a temporary local branch for merging,
	// regardless of whether it lives in the workspace or a fork. This makes
	// the merge use whatever the live remote tip is — same source of truth
	// as ResolveBranchTip — and lets the merge primitives operate against
	// uniform refs/heads/<temp> on both paths.
	const tempHeadBranch = "_gitmsg-merge-tmp"
	if err := fetchHeadIntoTemp(workdir, headSourceURL, headBranch, tempHeadBranch); err != nil {
		return result.Err[PullRequest]("HEAD_NOT_FOUND",
			fmt.Sprintf("cannot fetch head branch %q from %s: %s", headBranch, headSourceURL, err))
	}
	defer func() { _, _ = git.ExecGit(workdir, []string{"branch", "-D", tempHeadBranch}) }()
	headName := tempHeadBranch
	if _, err := git.ReadRef(workdir, baseName); err != nil {
		return result.Err[PullRequest]("BASE_NOT_FOUND",
			fmt.Sprintf("base branch %q not found locally", baseName))
	}

	// Compute merge-base / merge-head BEFORE running the merge, so a failure
	// here aborts cleanly without leaving a half-merged state. Both fields
	// must be present in the merged-state edit per spec §1.5.
	mergeBaseFull, err := git.GetMergeBase(workdir, baseName, headName)
	if err != nil || mergeBaseFull == "" {
		errMsg := "no common ancestor"
		if err != nil {
			errMsg = err.Error()
		}
		return result.Err[PullRequest]("MERGE_INCOMPLETE",
			fmt.Sprintf("cannot compute merge-base for %s..%s: %s", baseName, headBranch, errMsg))
	}
	mergeHeadFull, err := git.ReadRef(workdir, headName)
	if err != nil || mergeHeadFull == "" {
		errMsg := "ref not found"
		if err != nil {
			errMsg = err.Error()
		}
		return result.Err[PullRequest]("MERGE_INCOMPLETE",
			fmt.Sprintf("cannot resolve merge-head from %s: %s", headBranch, errMsg))
	}
	mergeBase := mergeBaseFull
	if len(mergeBase) > 12 {
		mergeBase = mergeBase[:12]
	}
	mergeHead := mergeHeadFull
	if len(mergeHead) > 12 {
		mergeHead = mergeHead[:12]
	}

	// Perform actual git merge of head into base
	var mergeErr error
	switch strategy {
	case MergeStrategySquash:
		_, mergeErr = git.SquashMerge(workdir, baseName, headName, pr.Subject)
	case MergeStrategyRebase:
		_, mergeErr = git.RebaseMerge(workdir, baseName, headName)
	case MergeStrategyMerge:
		_, mergeErr = git.ForceMerge(workdir, baseName, headName)
	default:
		_, mergeErr = git.MergeBranches(workdir, baseName, headName)
	}
	if mergeErr != nil {
		return result.Err[PullRequest]("MERGE_FAILED", mergeErr.Error())
	}

	merged := PRStateMerged
	opts := UpdatePROptions{
		State:     &merged,
		MergeBase: &mergeBase,
		MergeHead: &mergeHead,
	}
	// Capture branch tips at merge time
	if tip, err := git.ReadRef(workdir, baseName); err == nil && len(tip) >= 12 {
		baseTip := tip[:12]
		opts.BaseTip = &baseTip
	}
	if tip, err := git.ReadRef(workdir, headName); err == nil && len(tip) >= 12 {
		headTip := tip[:12]
		opts.HeadTip = &headTip
	}
	res := UpdatePR(workdir, prRef, opts)
	if !res.Success {
		return res
	}

	// Auto-close PM issues referenced in closes
	if len(res.Data.Closes) > 0 {
		// Refresh PM cache so we don't auto-close on stale state — a
		// teammate may have retracted or already closed the issue since
		// this PR was created. The fetch is bounded to the workspace's
		// gitmsg/pm branch and is fast in practice.
		if err := pm.SyncWorkspaceToCache(workdir); err != nil {
			log.Warn("auto-close: PM sync failed; cache may be stale",
				"error", err)
		}
		for _, closeRef := range res.Data.Closes {
			closeResult := pm.CloseIssue(workdir, closeRef)
			if !closeResult.Success {
				log.Warn("auto-close issue failed",
					"ref", closeRef,
					"code", closeResult.Error.Code,
					"error", closeResult.Error.Message)
			}
		}
	}

	// Auto-retarget dependent PRs: if another open PR depends on this one
	// and its base matches the merged PR's head, retarget it to this PR's base
	retargetDependents(workdir, res.Data)

	return res
}

// ClosePR sets a pull request's state to closed.
func ClosePR(workdir, prRef string) Result[PullRequest] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[PullRequest]("NOT_FOUND", "pull request not found")
	}
	pr := ReviewItemToPullRequest(*existing)
	if pr.State != PRStateOpen {
		return result.Err[PullRequest]("INVALID_STATE", fmt.Sprintf("cannot close: pull request is %s", pr.State))
	}

	closed := PRStateClosed
	return UpdatePR(workdir, prRef, UpdatePROptions{State: &closed})
}

// MarkReady removes the draft flag from a pull request.
func MarkReady(workdir, prRef string) Result[PullRequest] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[PullRequest]("NOT_FOUND", "pull request not found")
	}
	pr := ReviewItemToPullRequest(*existing)
	if pr.State != PRStateOpen {
		return result.Err[PullRequest]("INVALID_STATE", fmt.Sprintf("cannot mark ready: pull request is %s", pr.State))
	}
	if !pr.IsDraft {
		return result.Err[PullRequest]("NOT_DRAFT", "pull request is not a draft")
	}
	draft := false
	return UpdatePR(workdir, prRef, UpdatePROptions{Draft: &draft})
}

// ConvertToDraft sets the draft flag on a pull request.
func ConvertToDraft(workdir, prRef string) Result[PullRequest] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[PullRequest]("NOT_FOUND", "pull request not found")
	}
	pr := ReviewItemToPullRequest(*existing)
	if pr.State != PRStateOpen {
		return result.Err[PullRequest]("INVALID_STATE", fmt.Sprintf("cannot convert to draft: pull request is %s", pr.State))
	}
	if pr.IsDraft {
		return result.Err[PullRequest]("ALREADY_DRAFT", "pull request is already a draft")
	}
	draft := true
	return UpdatePR(workdir, prRef, UpdatePROptions{Draft: &draft})
}

// UpdatePRTips resolves current base/head branch tips and creates an edit with
// updated tips. Returns the existing PR unchanged when nothing has moved
// (avoids edit-storm noise on the gitmsg/review branch). Returns an error if
// either branch can no longer be resolved — silent no-op edits used to be
// emitted for deleted branches, masking the problem.
func UpdatePRTips(workdir, prRef string) Result[PullRequest] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[PullRequest]("NOT_FOUND", "pull request not found")
	}
	pr := ReviewItemToPullRequest(*existing)
	if pr.State != PRStateOpen {
		return result.Err[PullRequest]("INVALID_STATE", fmt.Sprintf("cannot update: pull request is %s", pr.State))
	}
	opts := UpdatePROptions{}
	if baseParsed := protocol.ParseRef(pr.Base); baseParsed.Value != "" {
		tip, err := resolveTipForWrite(workdir, repoURL, baseParsed)
		if err != nil {
			return result.Err[PullRequest]("BASE_UNRESOLVED",
				fmt.Sprintf("cannot resolve base %q: %s", pr.Base, err))
		}
		if len(tip) >= 12 {
			baseTip := tip[:12]
			if baseTip != pr.BaseTip {
				opts.BaseTip = &baseTip
			}
		}
	}
	if headParsed := protocol.ParseRef(pr.Head); headParsed.Value != "" {
		tip, err := resolveTipForWrite(workdir, repoURL, headParsed)
		if err != nil {
			return result.Err[PullRequest]("HEAD_UNRESOLVED",
				fmt.Sprintf("cannot resolve head %q: %s", pr.Head, err))
		}
		if len(tip) >= 12 {
			headTip := tip[:12]
			if headTip != pr.HeadTip {
				opts.HeadTip = &headTip
			}
		}
	}
	if opts.BaseTip == nil && opts.HeadTip == nil {
		return result.Ok(pr)
	}
	return UpdatePR(workdir, prRef, opts)
}

// SyncPRBranch updates the head branch with changes from the base branch.
func SyncPRBranch(workdir, prRef, strategy string) Result[PullRequest] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[PullRequest]("NOT_FOUND", "pull request not found")
	}
	pr := ReviewItemToPullRequest(*existing)
	if pr.State != PRStateOpen {
		return result.Err[PullRequest]("INVALID_STATE", fmt.Sprintf("cannot sync: pull request is %s", pr.State))
	}
	baseName := protocol.ParseRef(pr.Base).Value
	headName := protocol.ParseRef(pr.Head).Value
	if baseName == "" || headName == "" {
		return result.Err[PullRequest]("INVALID_REFS", "base or head branch not set")
	}
	switch strategy {
	case "merge":
		if _, err := git.MergeBranches(workdir, headName, baseName); err != nil {
			return result.Err[PullRequest]("SYNC_FAILED", err.Error())
		}
	default:
		if _, err := git.RebaseBranch(workdir, baseName, headName); err != nil {
			return result.Err[PullRequest]("SYNC_FAILED", err.Error())
		}
	}
	return UpdatePRTips(workdir, prRef)
}

// RetractPR marks a pull request as retracted.
func RetractPR(workdir, prRef string) Result[bool] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetReviewItemByRef(prRef, repoURL)
	if err != nil {
		return result.Err[bool]("NOT_FOUND", "pull request not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "review")

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	content := buildRetractContent(canonicalRef)

	_, err = git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[bool]("COMMIT_FAILED", err.Error())
	}
	return result.Ok(true)
}

// ReviewConfig holds review extension configuration.
type ReviewConfig struct {
	Version       string `json:"version"`
	Branch        string `json:"branch,omitempty"`
	RequireReview bool   `json:"require-review,omitempty"`
}

// SaveReviewConfig saves the review extension configuration.
func SaveReviewConfig(workdir string, config ReviewConfig) error {
	if config.Version == "" {
		config.Version = "0.1.0"
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	ref := "refs/gitmsg/review/config"
	var parent string
	if existing, err := git.ReadRef(workdir, ref); err == nil {
		parent = existing
	}
	hash, err := git.CreateCommitTree(workdir, string(data), parent)
	if err != nil {
		return err
	}
	if err := git.WriteRef(workdir, ref, hash); err != nil {
		return err
	}
	gitmsg.InvalidateExtConfig(workdir, "review")
	return nil
}

// GetReviewConfig reads the review extension configuration.
func GetReviewConfig(workdir string) ReviewConfig {
	configMap, err := gitmsg.ReadExtConfig(workdir, "review")
	if err != nil || configMap == nil {
		return ReviewConfig{Branch: "gitmsg/review"}
	}
	var config ReviewConfig
	if v, ok := configMap["version"].(string); ok {
		config.Version = v
	}
	if v, ok := configMap["branch"].(string); ok {
		config.Branch = v
	}
	if v, ok := configMap["require-review"].(bool); ok {
		config.RequireReview = v
	}
	if config.Branch == "" {
		config.Branch = "gitmsg/review"
	}
	return config
}

// GetForks returns the list of registered fork URLs (delegates to core).
func GetForks(workdir string) []string {
	return gitmsg.GetForks(workdir)
}

// AddFork registers a fork URL (delegates to core).
func AddFork(workdir, forkURL string) error {
	return gitmsg.AddFork(workdir, forkURL)
}

// AddForks registers multiple fork URLs (delegates to core).
func AddForks(workdir string, forkURLs []string) (int, error) {
	return gitmsg.AddForks(workdir, forkURLs)
}

// RemoveFork removes a fork URL (delegates to core).
func RemoveFork(workdir, forkURL string) error {
	return gitmsg.RemoveFork(workdir, forkURL)
}

// fetchHeadIntoTemp populates refs/heads/<tempBranch> in workdir with the
// tip of <branch> on <sourceURL>. For the workspace's own URL, the source
// is the origin tracking ref (refs/remotes/<remote>/<branch>) when one of
// the workdir's git remotes points at sourceURL — falls back to the local
// refs/heads/<branch>. For any other URL, runs `git fetch <sourceURL>
// +refs/heads/<branch>:refs/heads/<tempBranch>`. Either way, the merge
// primitives downstream see a consistent refs/heads/<tempBranch>.
func fetchHeadIntoTemp(workdir, sourceURL, branch, tempBranch string) error {
	normalized := protocol.NormalizeURL(sourceURL)
	if remoteName := findRemoteForURL(workdir, normalized); remoteName != "" {
		// Local repo already has the data — copy the remote tracking ref
		// into the temp branch (preferred) or the local branch if no
		// tracking ref exists.
		if tip, err := git.ReadRef(workdir, "refs/remotes/"+remoteName+"/"+branch); err == nil && tip != "" {
			return git.WriteRef(workdir, "refs/heads/"+tempBranch, tip)
		}
		if tip, err := git.ReadRef(workdir, branch); err == nil && tip != "" {
			return git.WriteRef(workdir, "refs/heads/"+tempBranch, tip)
		}
		return fmt.Errorf("branch %q not found locally", branch)
	}
	refspec := fmt.Sprintf("+refs/heads/%s:refs/heads/%s", branch, tempBranch)
	_, err := git.ExecGit(workdir, []string{"fetch", sourceURL, refspec, "--no-tags"})
	return err
}

// retargetDependents finds open PRs that depend on the merged PR and retargets
// their base from the merged PR's head to the merged PR's base.
func retargetDependents(workdir string, merged PullRequest) {
	dependents := GetDependents(merged.Repository, merged.Branch, extractRefHash(merged.ID))
	for _, dep := range dependents {
		if dep.State != PRStateOpen {
			continue
		}
		// Only retarget if dependent's base matches the merged PR's head
		if dep.Base != merged.Head {
			continue
		}
		newBase := merged.Base
		UpdatePR(workdir, dep.ID, UpdatePROptions{Base: &newBase})
		log.Debug("auto-retarget dependent PR", "pr", dep.Subject, "new_base", newBase)
	}
}

// extractRefHash extracts the hash from a ref string like "#commit:abc123@branch".
func extractRefHash(ref string) string {
	parsed := protocol.ParseRef(ref)
	return parsed.Value
}

func buildPRContent(subject, body string, opts CreatePROptions, editsRef string) string {
	return buildPRContentWithState(subject, body, opts, editsRef, PRStateOpen, nil)
}

func buildPRContentWithState(subject, body string, opts CreatePROptions, editsRef string, state PRState, refs []protocol.Ref) string {
	content := subject
	if body != "" {
		content += "\n\n" + body
	}

	fields := map[string]string{
		"type":  string(ItemTypePullRequest),
		"state": string(state),
	}
	if opts.Draft {
		fields["draft"] = "true"
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if opts.Base != "" {
		fields["base"] = opts.Base
	}
	if len(opts.DependsOn) > 0 {
		fields["depends-on"] = strings.Join(opts.DependsOn, ",")
	}
	if len(opts.Closes) > 0 {
		fields["closes"] = strings.Join(opts.Closes, ",")
	}
	if opts.Head != "" {
		fields["head"] = opts.Head
	}
	if len(opts.Reviewers) > 0 {
		fields["reviewers"] = strings.Join(opts.Reviewers, ",")
	}
	if len(opts.Labels) > 0 {
		fields["labels"] = strings.Join(opts.Labels, ",")
	}
	if opts.BaseTip != "" {
		fields["base-tip"] = opts.BaseTip
	}
	if opts.HeadTip != "" {
		fields["head-tip"] = opts.HeadTip
	}
	if opts.MergeBase != "" {
		fields["merge-base"] = opts.MergeBase
	}
	if opts.MergeHead != "" {
		fields["merge-head"] = opts.MergeHead
	}
	protocol.ApplyOrigin(fields, opts.Origin)

	header := protocol.Header{
		Ext:        "review",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: prFieldOrder,
	}
	return protocol.FormatMessage(content, header, refs)
}

func buildRetractContent(editsRef string) string {
	header := protocol.Header{
		Ext: "review",
		V:   "0.1.0",
		Fields: map[string]string{
			"edits":     editsRef,
			"retracted": "true",
		},
	}
	return protocol.FormatMessage("", header, nil)
}

func cacheReviewFromCommit(workdir, repoURL, hash, branch string) error {
	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return err
	}
	if commit == nil {
		return fmt.Errorf("commit not found: %s", hash)
	}

	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      branch,
		AuthorName:  commit.Author,
		AuthorEmail: commit.Email,
		Message:     commit.Message,
		Timestamp:   commit.Timestamp,
	}}); err != nil {
		return fmt.Errorf("insert commit: %w", err)
	}

	msg := protocol.ParseMessage(commit.Message)
	if msg == nil || msg.Header.Ext != "review" {
		return nil
	}

	processReviewCommit(*commit, msg, repoURL, branch)
	return nil
}
