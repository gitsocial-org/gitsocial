// sprint.go - Sprint creation and management
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

type CreateSprintOptions struct {
	State  SprintState
	Start  time.Time
	End    time.Time
	Origin *protocol.Origin
}

// CreateSprint creates a new sprint on the PM branch.
func CreateSprint(workdir, title, body string, opts CreateSprintOptions) Result[Sprint] {
	branch := gitmsg.GetExtBranch(workdir, "pm")

	state := opts.State
	if state == "" {
		state = SprintStatePlanned
	}
	content := buildSprintContent(title, body, state, opts.Start, opts.End, "", opts.Origin)
	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[Sprint]("COMMIT_FAILED", err.Error())
	}

	repoURL := gitmsg.ResolveRepoURL(workdir)
	if err := cacheSprintFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Sprint]("CACHE_FAILED", err.Error())
	}

	item, err := GetPMItem(repoURL, hash, branch)
	if err != nil {
		return result.Err[Sprint]("GET_FAILED", err.Error())
	}

	return result.Ok(PMItemToSprint(*item))
}

// GetSprint retrieves a single sprint by reference.
func GetSprint(sprintRef string) Result[Sprint] {
	parsed := protocol.ParseRef(sprintRef)
	hash := parsed.Value
	if hash == "" {
		hash = sprintRef
	}
	item, err := GetPMItemByHashPrefix(hash, string(ItemTypeSprint))
	if err != nil {
		return result.Err[Sprint]("NOT_FOUND", "sprint not found: "+sprintRef)
	}
	return result.Ok(PMItemToSprint(*item))
}

type UpdateSprintOptions struct {
	State  *SprintState
	Start  *time.Time
	End    *time.Time
	Title  *string
	Body   *string
	Origin *protocol.Origin
}

// UpdateSprint edits an existing sprint using core versioning.
func UpdateSprint(workdir, sprintRef string, opts UpdateSprintOptions) Result[Sprint] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetPMItemByRef(sprintRef, repoURL)
	if err != nil {
		return result.Err[Sprint]("NOT_FOUND", "sprint not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "pm")

	sprint := PMItemToSprint(*existing)
	state := sprint.State
	start := sprint.Start
	end := sprint.End
	title := sprint.Title
	body := sprint.Body

	if opts.State != nil {
		state = *opts.State
	}
	if opts.Start != nil {
		start = *opts.Start
	}
	if opts.End != nil {
		end = *opts.End
	}
	if opts.Title != nil {
		title = *opts.Title
	}
	if opts.Body != nil {
		body = *opts.Body
	}

	origin := existing.Origin
	if opts.Origin != nil {
		origin = opts.Origin
	}

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	content := buildSprintContent(title, body, state, start, end, canonicalRef, origin)
	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[Sprint]("COMMIT_FAILED", err.Error())
	}

	if err := cacheSprintFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Sprint]("CACHE_FAILED", err.Error())
	}

	item, err := GetPMItem(existing.RepoURL, existing.Hash, existing.Branch)
	if err != nil {
		return result.Err[Sprint]("GET_FAILED", err.Error())
	}

	return result.Ok(PMItemToSprint(*item))
}

// ActivateSprint sets a sprint to active state.
func ActivateSprint(workdir, sprintRef string) Result[Sprint] {
	active := SprintStateActive
	return UpdateSprint(workdir, sprintRef, UpdateSprintOptions{State: &active})
}

// CompleteSprint sets a sprint to completed state.
func CompleteSprint(workdir, sprintRef string) Result[Sprint] {
	completed := SprintStateCompleted
	return UpdateSprint(workdir, sprintRef, UpdateSprintOptions{State: &completed})
}

// CancelSprint sets a sprint to canceled state.
func CancelSprint(workdir, sprintRef string) Result[Sprint] {
	canceled := SprintStateCancelled
	return UpdateSprint(workdir, sprintRef, UpdateSprintOptions{State: &canceled})
}

// RetractSprint marks a sprint as retracted (deleted).
func RetractSprint(workdir, sprintRef string) Result[bool] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetPMItemByRef(sprintRef, repoURL)
	if err != nil {
		return result.Err[bool]("NOT_FOUND", "sprint not found")
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

// GetSprintIssues retrieves issues linked to a sprint.
func GetSprintIssues(sprintID string, states []string) Result[[]Issue] {
	parsed := protocol.ParseRef(sprintID)
	if parsed.Value == "" {
		return result.Err[[]Issue]("INVALID_REF", "invalid sprint reference")
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
		if item.SprintHash.Valid && item.SprintHash.String == parsed.Value {
			issues = append(issues, PMItemToIssue(item))
		}
	}
	return result.Ok(issues)
}

func buildSprintContent(title, body string, state SprintState, start, end time.Time, editsRef string, origin *protocol.Origin) string {
	content := title
	if body != "" {
		content += "\n\n" + body
	}

	fields := map[string]string{
		"type":  string(ItemTypeSprint),
		"state": string(state),
		"start": start.Format("2006-01-02"),
		"end":   end.Format("2006-01-02"),
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	protocol.ApplyOrigin(fields, origin)

	header := protocol.Header{
		Ext:        "pm",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: sprintFieldOrder,
	}
	return protocol.FormatMessage(content, header, nil)
}

func cacheSprintFromCommit(workdir, repoURL, hash, branch string) error {
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
	if itemType != string(ItemTypeSprint) {
		return nil
	}

	state := msg.Header.Fields["state"]
	if state == "" {
		state = string(SprintStatePlanned)
	}

	editsRef := msg.Header.Fields["edits"]
	isRetracted := msg.Header.Fields["retracted"] == "true"
	if editsRef != "" {
		canonical := protocol.ResolveRefWithDefaults(editsRef, repoURL, branch)
		if canonical.Hash != "" {
			if err := cache.InsertVersion(repoURL, hash, branch, canonical.RepoURL, canonical.Hash, canonical.Branch, isRetracted); err != nil {
				log.Warn("insert sprint version failed", "hash", hash, "error", err)
			}
		}
	}

	// Store pm_items with commit's own coordinates (view resolves to latest edit)
	item := PMItem{
		RepoURL:   repoURL,
		Hash:      hash,
		Branch:    branch,
		Type:      itemType,
		State:     state,
		StartDate: cache.ToNullString(msg.Header.Fields["start"]),
		EndDate:   cache.ToNullString(msg.Header.Fields["end"]),
	}

	return InsertPMItem(item)
}
