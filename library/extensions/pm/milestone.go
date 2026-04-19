// milestone.go - Milestone creation and management
package pm

import (
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

type CreateMilestoneOptions struct {
	State  State
	Due    *time.Time
	Origin *protocol.Origin
}

// CreateMilestone creates a new milestone on the PM branch.
func CreateMilestone(workdir, title, body string, opts CreateMilestoneOptions) Result[Milestone] {
	branch := gitmsg.GetExtBranch(workdir, "pm")

	state := opts.State
	if state == "" {
		state = StateOpen
	}
	content := buildMilestoneContent(title, body, state, opts.Due, "", opts.Origin)
	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[Milestone]("COMMIT_FAILED", err.Error())
	}

	repoURL := gitmsg.ResolveRepoURL(workdir)
	if err := cacheMilestoneFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Milestone]("CACHE_FAILED", err.Error())
	}

	item, err := GetPMItem(repoURL, hash, branch)
	if err != nil {
		return result.Err[Milestone]("GET_FAILED", err.Error())
	}

	return result.Ok(PMItemToMilestone(*item))
}

// GetMilestone retrieves a single milestone by reference.
func GetMilestone(milestoneRef string) Result[Milestone] {
	parsed := protocol.ParseRef(milestoneRef)
	hash := parsed.Value
	if hash == "" {
		hash = milestoneRef
	}
	item, err := GetPMItemByHashPrefix(hash, string(ItemTypeMilestone))
	if err != nil {
		return result.Err[Milestone]("NOT_FOUND", "milestone not found: "+milestoneRef)
	}
	return result.Ok(PMItemToMilestone(*item))
}

type UpdateMilestoneOptions struct {
	State  *State
	Due    *time.Time
	Title  *string
	Body   *string
	Origin *protocol.Origin
}

// UpdateMilestone edits an existing milestone using core versioning.
func UpdateMilestone(workdir, milestoneRef string, opts UpdateMilestoneOptions) Result[Milestone] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetPMItemByRef(milestoneRef, repoURL)
	if err != nil {
		return result.Err[Milestone]("NOT_FOUND", "milestone not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "pm")

	milestone := PMItemToMilestone(*existing)
	state := milestone.State
	due := milestone.Due
	title := milestone.Title
	body := milestone.Body

	if opts.State != nil {
		state = *opts.State
	}
	if opts.Due != nil {
		due = opts.Due
	}
	if opts.Title != nil {
		title = *opts.Title
	}
	if opts.Body != nil {
		body = *opts.Body
	}

	var origin *protocol.Origin
	if opts.Origin != nil {
		origin = opts.Origin
	} else {
		origin = existing.Origin
	}
	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	content := buildMilestoneContent(title, body, state, due, canonicalRef, origin)
	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[Milestone]("COMMIT_FAILED", err.Error())
	}

	if err := cacheMilestoneFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Milestone]("CACHE_FAILED", err.Error())
	}

	item, err := GetPMItem(existing.RepoURL, existing.Hash, existing.Branch)
	if err != nil {
		return result.Err[Milestone]("GET_FAILED", err.Error())
	}

	return result.Ok(PMItemToMilestone(*item))
}

// CloseMilestone changes a milestone's state to closed.
func CloseMilestone(workdir, milestoneRef string) Result[Milestone] {
	closed := StateClosed
	return UpdateMilestone(workdir, milestoneRef, UpdateMilestoneOptions{State: &closed})
}

// ReopenMilestone changes a milestone's state to open.
func ReopenMilestone(workdir, milestoneRef string) Result[Milestone] {
	open := StateOpen
	return UpdateMilestone(workdir, milestoneRef, UpdateMilestoneOptions{State: &open})
}

// CancelMilestone changes a milestone's state to canceled.
func CancelMilestone(workdir, milestoneRef string) Result[Milestone] {
	canceled := StateCancelled
	return UpdateMilestone(workdir, milestoneRef, UpdateMilestoneOptions{State: &canceled})
}

// RetractMilestone marks a milestone as retracted (deleted).
func RetractMilestone(workdir, milestoneRef string) Result[bool] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetPMItemByRef(milestoneRef, repoURL)
	if err != nil {
		return result.Err[bool]("NOT_FOUND", "milestone not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "pm")

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

// GetMilestoneIssues retrieves issues linked to a milestone.
func GetMilestoneIssues(milestoneID string, states []string) Result[[]Issue] {
	parsed := protocol.ParseRef(milestoneID)
	if parsed.Value == "" {
		return result.Err[[]Issue]("INVALID_REF", "invalid milestone reference")
	}

	q := PMQuery{
		Types:  []string{string(ItemTypeIssue)},
		States: states,
		Limit:  1000,
	}
	items, err := GetPMItems(q)
	if err != nil {
		return result.Err[[]Issue]("QUERY_FAILED", err.Error())
	}

	var issues []Issue
	for _, item := range items {
		if item.MilestoneHash.Valid && item.MilestoneHash.String == parsed.Value {
			issues = append(issues, PMItemToIssue(item))
		}
	}
	return result.Ok(issues)
}

func buildMilestoneContent(title, body string, state State, due *time.Time, editsRef string, origin *protocol.Origin) string {
	content := title
	if body != "" {
		content += "\n\n" + body
	}

	fields := map[string]string{
		"type":  string(ItemTypeMilestone),
		"state": string(state),
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if due != nil {
		fields["due"] = due.Format("2006-01-02")
	}
	protocol.ApplyOrigin(fields, origin)

	header := protocol.Header{
		Ext:        "pm",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: milestoneFieldOrder,
	}
	return protocol.FormatMessage(content, header, nil)
}

func cacheMilestoneFromCommit(workdir, repoURL, hash, branch string) error {
	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return err
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
	if msg == nil || msg.Header.Ext != "pm" {
		return nil
	}

	itemType := msg.Header.Fields["type"]
	if itemType != string(ItemTypeMilestone) {
		return nil
	}

	state := msg.Header.Fields["state"]
	if state == "" {
		state = string(StateOpen)
	}

	editsRef := msg.Header.Fields["edits"]
	isRetracted := msg.Header.Fields["retracted"] == "true"
	if editsRef != "" {
		canonical := protocol.ResolveRefWithDefaults(editsRef, repoURL, branch)
		if canonical.Hash != "" {
			if err := cache.InsertVersion(repoURL, hash, branch, canonical.RepoURL, canonical.Hash, canonical.Branch, isRetracted); err != nil {
				log.Warn("insert milestone version failed", "hash", hash, "error", err)
			}
		}
	}

	// Store pm_items with commit's own coordinates (view resolves to latest edit)
	item := PMItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  branch,
		Type:    itemType,
		State:   state,
		Due:     cache.ToNullString(msg.Header.Fields["due"]),
	}

	if err := InsertPMItem(item); err != nil {
		return err
	}
	cache.SyncEditExtensionFields([]cache.EditKey{{RepoURL: repoURL, Hash: hash, Branch: branch}})
	return nil
}
