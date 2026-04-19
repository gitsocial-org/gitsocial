// issue.go - Issue creation and management
package pm

import (
	"fmt"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

type CreateIssueOptions struct {
	State     State
	Assignees []string
	Due       *time.Time
	Milestone string
	Sprint    string
	Parent    string
	Root      string
	Blocks    []string
	BlockedBy []string
	Related   []string
	Labels    []Label
	Origin    *protocol.Origin
}

// CreateIssue creates a new issue on the PM branch.
func CreateIssue(workdir, subject, body string, opts CreateIssueOptions) Result[Issue] {
	branch := gitmsg.GetExtBranch(workdir, "pm")

	repoURL := gitmsg.ResolveRepoURL(workdir)
	opts.Milestone = protocol.LocalizeRef(opts.Milestone, repoURL)
	opts.Sprint = protocol.LocalizeRef(opts.Sprint, repoURL)
	opts.Parent = protocol.LocalizeRef(opts.Parent, repoURL)
	opts.Root = protocol.LocalizeRef(opts.Root, repoURL)
	for i, r := range opts.Blocks {
		opts.Blocks[i] = protocol.LocalizeRef(r, repoURL)
	}
	for i, r := range opts.BlockedBy {
		opts.BlockedBy[i] = protocol.LocalizeRef(r, repoURL)
	}
	for i, r := range opts.Related {
		opts.Related[i] = protocol.LocalizeRef(r, repoURL)
	}

	content := buildIssueContent(subject, body, opts)
	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[Issue]("COMMIT_FAILED", err.Error())
	}
	if err := cacheIssueFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Issue]("CACHE_FAILED", err.Error())
	}

	item, err := GetPMItem(repoURL, hash, branch)
	if err != nil {
		return result.Err[Issue]("GET_FAILED", err.Error())
	}

	return result.Ok(PMItemToIssue(*item))
}

// GetIssue retrieves a single issue by reference (hash or partial hash).
func GetIssue(issueRef string) Result[Issue] {
	parsed := protocol.ParseRef(issueRef)
	hash := parsed.Value
	if hash == "" {
		hash = issueRef
	}
	item, err := GetPMItemByHashPrefix(hash, string(ItemTypeIssue))
	if err != nil {
		return result.Err[Issue]("NOT_FOUND", "issue not found: "+issueRef)
	}
	return result.Ok(PMItemToIssue(*item))
}

type UpdateIssueOptions struct {
	State     *State
	Assignees *[]string
	Due       *time.Time
	Milestone *string
	Sprint    *string
	Parent    *string
	Root      *string
	Blocks    *[]string
	BlockedBy *[]string
	Related   *[]string
	Labels    *[]Label
	Subject   *string
	Body      *string
	Origin    *protocol.Origin
}

// UpdateIssue edits an existing issue using core versioning.
func UpdateIssue(workdir, issueRef string, opts UpdateIssueOptions) Result[Issue] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetPMItemByRef(issueRef, repoURL)
	if err != nil {
		return result.Err[Issue]("NOT_FOUND", "issue not found")
	}

	branch := gitmsg.GetExtBranch(workdir, "pm")

	issue := PMItemToIssue(*existing)
	createOpts := CreateIssueOptions{
		State:     issue.State,
		Assignees: issue.Assignees,
		Due:       issue.Due,
		Labels:    issue.Labels,
	}
	if opts.Origin != nil {
		createOpts.Origin = opts.Origin
	} else {
		createOpts.Origin = existing.Origin
	}
	if issue.Milestone != nil {
		createOpts.Milestone = protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, issue.Milestone.Hash, issue.Milestone.RepoURL, issue.Milestone.Branch), repoURL)
	}
	if issue.Sprint != nil {
		createOpts.Sprint = protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, issue.Sprint.Hash, issue.Sprint.RepoURL, issue.Sprint.Branch), repoURL)
	}
	if issue.Parent != nil {
		createOpts.Parent = protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, issue.Parent.Hash, issue.Parent.RepoURL, issue.Parent.Branch), repoURL)
	}
	if issue.Root != nil {
		createOpts.Root = protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, issue.Root.Hash, issue.Root.RepoURL, issue.Root.Branch), repoURL)
	}
	for _, ref := range issue.Blocks {
		createOpts.Blocks = append(createOpts.Blocks, protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch), repoURL))
	}
	for _, ref := range issue.BlockedBy {
		createOpts.BlockedBy = append(createOpts.BlockedBy, protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch), repoURL))
	}
	for _, ref := range issue.Related {
		createOpts.Related = append(createOpts.Related, protocol.LocalizeRef(protocol.CreateRef(protocol.RefTypeCommit, ref.Hash, ref.RepoURL, ref.Branch), repoURL))
	}

	subject := issue.Subject
	body := issue.Body

	if opts.State != nil {
		createOpts.State = *opts.State
	}
	if opts.Assignees != nil {
		createOpts.Assignees = *opts.Assignees
	}
	if opts.Due != nil {
		createOpts.Due = opts.Due
	}
	if opts.Milestone != nil {
		createOpts.Milestone = *opts.Milestone
	}
	if opts.Sprint != nil {
		createOpts.Sprint = *opts.Sprint
	}
	if opts.Parent != nil {
		createOpts.Parent = *opts.Parent
	}
	if opts.Root != nil {
		createOpts.Root = *opts.Root
	}
	if opts.Blocks != nil {
		createOpts.Blocks = *opts.Blocks
	}
	if opts.BlockedBy != nil {
		createOpts.BlockedBy = *opts.BlockedBy
	}
	if opts.Related != nil {
		createOpts.Related = *opts.Related
	}
	if opts.Labels != nil {
		createOpts.Labels = *opts.Labels
	}
	if opts.Subject != nil {
		subject = *opts.Subject
	}
	if opts.Body != nil {
		body = *opts.Body
	}

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	content := buildIssueContentWithEdits(subject, body, createOpts, canonicalRef)

	hash, err := git.CreateCommitOnBranch(workdir, branch, content)
	if err != nil {
		return result.Err[Issue]("COMMIT_FAILED", err.Error())
	}

	if err := cacheIssueFromCommit(workdir, repoURL, hash, branch); err != nil {
		return result.Err[Issue]("CACHE_FAILED", err.Error())
	}

	// Return the canonical issue with updated content
	item, err := GetPMItem(existing.RepoURL, existing.Hash, existing.Branch)
	if err != nil {
		return result.Err[Issue]("GET_FAILED", err.Error())
	}

	return result.Ok(PMItemToIssue(*item))
}

// CloseIssue changes an issue's state to closed.
func CloseIssue(workdir, issueRef string) Result[Issue] {
	closed := StateClosed
	return UpdateIssue(workdir, issueRef, UpdateIssueOptions{State: &closed})
}

// ReopenIssue changes an issue's state to open.
func ReopenIssue(workdir, issueRef string) Result[Issue] {
	open := StateOpen
	return UpdateIssue(workdir, issueRef, UpdateIssueOptions{State: &open})
}

// RetractIssue marks an issue as retracted (deleted).
func RetractIssue(workdir, issueRef string) Result[bool] {
	repoURL := gitmsg.ResolveRepoURL(workdir)
	existing, err := GetPMItemByRef(issueRef, repoURL)
	if err != nil {
		return result.Err[bool]("NOT_FOUND", "issue not found")
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

func buildIssueContent(subject, body string, opts CreateIssueOptions) string {
	return buildIssueContentWithEdits(subject, body, opts, "")
}

func buildIssueContentWithEdits(subject, body string, opts CreateIssueOptions, editsRef string) string {
	content := subject
	if body != "" {
		content += "\n\n" + body
	}

	state := opts.State
	if state == "" {
		state = StateOpen
	}

	fields := map[string]string{
		"type":  string(ItemTypeIssue),
		"state": string(state),
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if len(opts.Assignees) > 0 {
		fields["assignees"] = strings.Join(opts.Assignees, ",")
	}
	if opts.Due != nil {
		fields["due"] = opts.Due.Format("2006-01-02")
	}
	if opts.Milestone != "" {
		fields["milestone"] = opts.Milestone
	}
	if opts.Sprint != "" {
		fields["sprint"] = opts.Sprint
	}
	if opts.Parent != "" {
		fields["parent"] = opts.Parent
	}
	if opts.Root != "" {
		fields["root"] = opts.Root
	}
	if len(opts.Blocks) > 0 {
		fields["blocks"] = strings.Join(opts.Blocks, ",")
	}
	if len(opts.BlockedBy) > 0 {
		fields["blocked-by"] = strings.Join(opts.BlockedBy, ",")
	}
	if len(opts.Related) > 0 {
		fields["related"] = strings.Join(opts.Related, ",")
	}
	if len(opts.Labels) > 0 {
		labelStrs := make([]string, len(opts.Labels))
		for i, l := range opts.Labels {
			if l.Scope != "" {
				labelStrs[i] = l.Scope + "/" + l.Value
			} else {
				labelStrs[i] = l.Value
			}
		}
		fields["labels"] = strings.Join(labelStrs, ",")
	}
	protocol.ApplyOrigin(fields, opts.Origin)

	header := protocol.Header{
		Ext:        "pm",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: issueFieldOrder,
	}
	return protocol.FormatMessage(content, header, nil)
}

func cacheIssueFromCommit(workdir, repoURL, hash, branch string) error {
	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return err
	}

	// Insert into core_commits first (extracts edits field and populates version table)
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
		return nil // Not a PM item, skip caching
	}

	itemType := msg.Header.Fields["type"]
	if itemType == "" {
		itemType = string(ItemTypeIssue)
	}
	state := msg.Header.Fields["state"]
	if state == "" {
		state = string(StateOpen)
	}

	// Ensure version table is populated for edits
	editsRef := msg.Header.Fields["edits"]
	isRetracted := msg.Header.Fields["retracted"] == "true"
	if editsRef != "" {
		parsed := protocol.ParseRef(editsRef)
		if parsed.Value != "" {
			canonicalRepoURL := repoURL
			canonicalHash := parsed.Value
			canonicalBranch := branch
			if parsed.Repository != "" {
				canonicalRepoURL = parsed.Repository
			}
			if parsed.Branch != "" {
				canonicalBranch = parsed.Branch
			}
			if err := cache.InsertVersion(repoURL, hash, branch, canonicalRepoURL, canonicalHash, canonicalBranch, isRetracted); err != nil {
				log.Warn("insert issue version failed", "hash", hash, "error", err)
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
		Assignees: cache.ToNullString(msg.Header.Fields["assignees"]),
		Due:       cache.ToNullString(msg.Header.Fields["due"]),
		Labels:    cache.ToNullString(msg.Header.Fields["labels"]),
	}

	// Parse milestone ref
	if milestone := msg.Header.Fields["milestone"]; milestone != "" {
		parsed := protocol.ParseRef(milestone)
		if parsed.Value != "" {
			mRepoURL := parsed.Repository
			if mRepoURL == "" {
				mRepoURL = repoURL
			}
			mBranch := parsed.Branch
			if mBranch == "" {
				mBranch = branch
			}
			item.MilestoneRepoURL = cache.ToNullString(mRepoURL)
			item.MilestoneHash = cache.ToNullString(parsed.Value)
			item.MilestoneBranch = cache.ToNullString(mBranch)
		}
	}

	// Parse sprint ref
	if sprint := msg.Header.Fields["sprint"]; sprint != "" {
		parsed := protocol.ParseRef(sprint)
		if parsed.Value != "" {
			sRepoURL := parsed.Repository
			if sRepoURL == "" {
				sRepoURL = repoURL
			}
			sBranch := parsed.Branch
			if sBranch == "" {
				sBranch = branch
			}
			item.SprintRepoURL = cache.ToNullString(sRepoURL)
			item.SprintHash = cache.ToNullString(parsed.Value)
			item.SprintBranch = cache.ToNullString(sBranch)
		}
	}

	// Parse parent ref
	if parent := msg.Header.Fields["parent"]; parent != "" {
		parsed := protocol.ParseRef(parent)
		if parsed.Value != "" {
			pRepoURL := parsed.Repository
			if pRepoURL == "" {
				pRepoURL = repoURL
			}
			pBranch := parsed.Branch
			if pBranch == "" {
				pBranch = branch
			}
			item.ParentRepoURL = cache.ToNullString(pRepoURL)
			item.ParentHash = cache.ToNullString(parsed.Value)
			item.ParentBranch = cache.ToNullString(pBranch)
		}
	}

	// Parse root ref
	if root := msg.Header.Fields["root"]; root != "" {
		parsed := protocol.ParseRef(root)
		if parsed.Value != "" {
			rRepoURL := parsed.Repository
			if rRepoURL == "" {
				rRepoURL = repoURL
			}
			rBranch := parsed.Branch
			if rBranch == "" {
				rBranch = branch
			}
			item.RootRepoURL = cache.ToNullString(rRepoURL)
			item.RootHash = cache.ToNullString(parsed.Value)
			item.RootBranch = cache.ToNullString(rBranch)
		}
	}

	if err := InsertPMItem(item); err != nil {
		return err
	}

	// Propagate mutable fields from edit to canonical (extension row now exists)
	cache.SyncEditExtensionFields([]cache.EditKey{{RepoURL: repoURL, Hash: hash, Branch: branch}})

	// Parse and store links
	blocks := parseRefList(msg.Header.Fields["blocks"], repoURL, branch)
	blockedBy := parseRefList(msg.Header.Fields["blocked-by"], repoURL, branch)
	related := parseRefList(msg.Header.Fields["related"], repoURL, branch)
	if len(blocks) > 0 || len(blockedBy) > 0 || len(related) > 0 {
		return InsertLinks(repoURL, hash, branch, blocks, blockedBy, related)
	}
	return nil
}
