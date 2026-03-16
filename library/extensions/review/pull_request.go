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
	Closes    []string
	Reviewers []string
	Labels    []string
	MergeBase string
	MergeHead string
	Draft     bool
	Origin    *protocol.Origin
}

// CreatePR creates a new pull request on the review branch.
func CreatePR(workdir, subject, body string, opts CreatePROptions) Result[PullRequest] {
	branch := gitmsg.GetExtBranch(workdir, "review")
	repoURL := gitmsg.ResolveRepoURL(workdir)
	opts.Base = protocol.EnsureBranchRef(opts.Base)
	opts.Head = protocol.EnsureBranchRef(opts.Head)
	opts.Base = protocol.LocalizeRef(opts.Base, repoURL)
	opts.Head = protocol.LocalizeRef(opts.Head, repoURL)
	for i, ref := range opts.Closes {
		opts.Closes[i] = protocol.LocalizeRef(ref, repoURL)
	}

	// Auto-resolve branch tips if not already set
	if opts.BaseTip == "" {
		baseParsed := protocol.ParseRef(opts.Base)
		if baseParsed.Value != "" {
			tip, err := resolveRefTip(workdir, repoURL, baseParsed)
			if err == nil && len(tip) >= 12 {
				opts.BaseTip = tip[:12]
			}
		}
	}
	if opts.HeadTip == "" {
		headParsed := protocol.ParseRef(opts.Head)
		if headParsed.Value != "" {
			tip, err := resolveRefTip(workdir, repoURL, headParsed)
			if err == nil && len(tip) >= 12 {
				opts.HeadTip = tip[:12]
			}
		}
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
		Closes:    pr.Closes,
		Reviewers: pr.Reviewers,
		Labels:    pr.Labels,
		Draft:     pr.IsDraft,
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

	// Normalize and localize refs before committing
	createOpts.Base = protocol.EnsureBranchRef(createOpts.Base)
	createOpts.Head = protocol.EnsureBranchRef(createOpts.Head)
	createOpts.Base = protocol.LocalizeRef(createOpts.Base, repoURL)
	createOpts.Head = protocol.LocalizeRef(createOpts.Head, repoURL)
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

	// Copy fork PR to upstream review branch so the merge record is self-contained
	if existing.RepoURL != repoURL {
		branch := gitmsg.GetExtBranch(workdir, "review")
		copyOpts := CreatePROptions{
			Base:      protocol.LocalizeRef(protocol.EnsureBranchRef(pr.Base), repoURL),
			Head:      protocol.LocalizeRef(protocol.EnsureBranchRef(pr.Head), repoURL),
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
	headName := headParsed.Value

	// Fetch remote head branch into a temporary local branch for merging
	headRemote := headParsed.Repository != "" && headParsed.Repository != repoURL
	if headRemote && headName != "" {
		refspec := fmt.Sprintf("+refs/heads/%s:refs/heads/_gitmsg-merge-tmp", headName)
		if _, err := git.ExecGit(workdir, []string{"fetch", headParsed.Repository, refspec, "--no-tags"}); err != nil {
			return result.Err[PullRequest]("FETCH_FAILED",
				fmt.Sprintf("cannot fetch remote branch %s: %s", headName, err))
		}
		headName = "_gitmsg-merge-tmp"
		defer func() { _, _ = git.ExecGit(workdir, []string{"branch", "-D", "_gitmsg-merge-tmp"}) }()
	}
	var mergeBase, mergeHead string
	if baseName != "" && headName != "" {
		mergeBase, _ = git.GetMergeBase(workdir, baseName, headName)
		if len(mergeBase) > 12 {
			mergeBase = mergeBase[:12]
		}
		mergeHead, _ = git.ReadRef(workdir, headName)
		if len(mergeHead) > 12 {
			mergeHead = mergeHead[:12]
		}
	}

	// Perform actual git merge of head into base
	if baseName != "" && headName != "" {
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
	}

	merged := PRStateMerged
	opts := UpdatePROptions{State: &merged}
	if mergeBase != "" {
		opts.MergeBase = &mergeBase
	}
	if mergeHead != "" {
		opts.MergeHead = &mergeHead
	}
	// Capture branch tips at merge time
	var baseTip, headTip string
	if baseName != "" {
		if tip, err := git.ReadRef(workdir, baseName); err == nil && len(tip) >= 12 {
			baseTip = tip[:12]
			opts.BaseTip = &baseTip
		}
	}
	if headName != "" {
		if tip, err := git.ReadRef(workdir, headName); err == nil && len(tip) >= 12 {
			headTip = tip[:12]
			opts.HeadTip = &headTip
		}
	}
	res := UpdatePR(workdir, prRef, opts)
	if !res.Success {
		return res
	}

	// Auto-close PM issues referenced in closes
	for _, closeRef := range res.Data.Closes {
		closeResult := pm.CloseIssue(workdir, closeRef)
		if !closeResult.Success {
			log.Debug("auto-close issue failed", "ref", closeRef, "error", closeResult.Error.Message)
		}
	}

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

// UpdatePRTips resolves current base/head branch tips and creates an edit with updated tips.
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
		if tip, err := resolveRefTip(workdir, repoURL, baseParsed); err == nil && len(tip) >= 12 {
			baseTip := tip[:12]
			opts.BaseTip = &baseTip
		}
	}
	if headParsed := protocol.ParseRef(pr.Head); headParsed.Value != "" {
		if tip, err := resolveRefTip(workdir, repoURL, headParsed); err == nil && len(tip) >= 12 {
			headTip := tip[:12]
			opts.HeadTip = &headTip
		}
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
	Version       string   `json:"version"`
	Branch        string   `json:"branch,omitempty"`
	RequireReview bool     `json:"require-review,omitempty"`
	Forks         []string `json:"forks,omitempty"`
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
	if v, ok := configMap["forks"].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				config.Forks = append(config.Forks, s)
			}
		}
	}
	if config.Branch == "" {
		config.Branch = "gitmsg/review"
	}
	return config
}

// GetForks returns the list of registered fork URLs for the workspace.
func GetForks(workdir string) []string {
	return GetReviewConfig(workdir).Forks
}

// AddFork registers a fork URL in the review config.
func AddFork(workdir, forkURL string) error {
	forkURL = protocol.NormalizeURL(forkURL)
	config := GetReviewConfig(workdir)
	for _, f := range config.Forks {
		if f == forkURL {
			return nil
		}
	}
	config.Forks = append(config.Forks, forkURL)
	return SaveReviewConfig(workdir, config)
}

// AddForks registers multiple fork URLs in a single config save.
func AddForks(workdir string, forkURLs []string) (int, error) {
	config := GetReviewConfig(workdir)
	existing := map[string]bool{}
	for _, f := range config.Forks {
		existing[f] = true
	}
	added := 0
	for _, u := range forkURLs {
		u = protocol.NormalizeURL(u)
		if existing[u] {
			continue
		}
		existing[u] = true
		config.Forks = append(config.Forks, u)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	return added, SaveReviewConfig(workdir, config)
}

// RemoveFork removes a fork URL from the review config.
func RemoveFork(workdir, forkURL string) error {
	forkURL = protocol.NormalizeURL(forkURL)
	config := GetReviewConfig(workdir)
	filtered := make([]string, 0, len(config.Forks))
	for _, f := range config.Forks {
		if f != forkURL {
			filtered = append(filtered, f)
		}
	}
	config.Forks = filtered
	return SaveReviewConfig(workdir, config)
}

// resolveRefTip reads the branch tip hash, using ls-remote for cross-fork refs.
func resolveRefTip(workdir, repoURL string, parsed protocol.ParsedRef) (string, error) {
	if parsed.Repository != "" && parsed.Repository != repoURL {
		return git.ReadRemoteRef(workdir, parsed.Repository, parsed.Value)
	}
	return git.ReadRef(workdir, parsed.Value)
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
