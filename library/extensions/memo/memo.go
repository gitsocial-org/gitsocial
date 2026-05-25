// memo.go - Memo public API: create, edit, retract
package memo

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// memoFieldOrder declares the spec-defined header field order: labels.
// (Project and expires are now plain labels — no dedicated header fields.)
var memoFieldOrder = []string{"labels"}

// CreateMemoOptions captures user-provided memo metadata.
type CreateMemoOptions struct {
	Tier   Tier             // empty defaults to TierSession
	Labels []string         // protocol-level labels (kind/*, priority/*, expires/<date>, ...)
	Origin *protocol.Origin // optional, for imports
}

// EditMemoOptions configures EditMemo. Pointer fields participate only when set.
type EditMemoOptions struct {
	Subject *string
	Body    *string
	Labels  *[]string
}

// CreateMemo creates a new memo on the requested tier (default: session).
func CreateMemo(workdir, subject, body string, opts CreateMemoOptions) Result[Memo] {
	if strings.TrimSpace(subject) == "" {
		return result.Err[Memo]("INVALID_ARGS", "memo subject cannot be empty")
	}
	if err := validateLabels(opts.Labels); err != nil {
		return result.Err[Memo]("INVALID_LABELS", err.Error())
	}
	tier := opts.Tier
	if tier == "" {
		tier = TierSession
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	repoPath, repoURL, branch, err := ResolveTierTarget(tier, workdir, workspaceURL)
	if err != nil {
		return result.Err[Memo]("TIER_INIT_FAILED", err.Error())
	}

	content := buildMemoContent(subject, body, opts, "")
	hash, err := git.CreateCommitOnBranch(repoPath, branch, content)
	if err != nil {
		return result.Err[Memo]("COMMIT_FAILED", err.Error())
	}
	if err := cacheMemoFromCommit(repoPath, repoURL, hash, branch); err != nil {
		return result.Err[Memo]("CACHE_FAILED", err.Error())
	}
	item, err := GetMemoItem(repoURL, hash, branch)
	if err != nil {
		return result.Err[Memo]("GET_FAILED", err.Error())
	}
	return result.Ok(MemoItemToMemo(*item, workspaceURL, ListInherits(workdir)))
}

// EditMemo updates an existing memo on its source tier via the core edit chain.
// Inherited and external memos (memos in repos owned by other parties) are read-only.
func EditMemo(workdir, memoRef string, opts EditMemoOptions) Result[Memo] {
	if opts.Labels != nil {
		if err := validateLabels(*opts.Labels); err != nil {
			return result.Err[Memo]("INVALID_LABELS", err.Error())
		}
	}
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	inheritedURLs := ListInherits(workdir)
	existing, err := GetMemoItemByRef(memoRef, workspaceURL)
	if err != nil {
		return result.Err[Memo]("NOT_FOUND", "memo not found")
	}

	sourceTier := TierForRepoURL(existing.RepoURL, workspaceURL, inheritedURLs)
	if sourceTier == TierExternal || sourceTier == TierInherited {
		return result.Err[Memo]("READONLY_TIER", string(sourceTier)+"-tier memos are read-only")
	}
	repoPath, repoURL, _, err := ResolveTierTarget(sourceTier, workdir, workspaceURL)
	if err != nil {
		return result.Err[Memo]("TIER_INIT_FAILED", err.Error())
	}
	branch := existing.Branch
	if branch == "" {
		branch = MemoBranch
	}

	memo := MemoItemToMemo(*existing, workspaceURL, inheritedURLs)
	subject := memo.Subject
	body := memo.Body
	if opts.Subject != nil {
		subject = *opts.Subject
	}
	if opts.Body != nil {
		body = *opts.Body
	}
	createOpts := CreateMemoOptions{
		Tier:   sourceTier,
		Labels: memo.Labels,
		Origin: existing.Origin,
	}
	if opts.Labels != nil {
		createOpts.Labels = *opts.Labels
	}

	// Short-circuit no-op edits — a call where every applied field matches the
	// existing memo would still write a fresh commit and flip is_edited, just
	// to record nothing. Compare after applying opts so an explicit "set to
	// current value" also no-ops.
	if subject == memo.Subject && body == memo.Body && labelsEqual(createOpts.Labels, memo.Labels) {
		return result.Err[Memo]("NO_CHANGES", "no changes to apply")
	}

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	content := buildMemoContent(subject, body, createOpts, canonicalRef)
	hash, err := git.CreateCommitOnBranch(repoPath, branch, content)
	if err != nil {
		return result.Err[Memo]("COMMIT_FAILED", err.Error())
	}
	if err := cacheMemoFromCommit(repoPath, repoURL, hash, branch); err != nil {
		return result.Err[Memo]("CACHE_FAILED", err.Error())
	}
	item, err := GetMemoItem(existing.RepoURL, existing.Hash, existing.Branch)
	if err != nil {
		return result.Err[Memo]("GET_FAILED", err.Error())
	}
	return result.Ok(MemoItemToMemo(*item, workspaceURL, inheritedURLs))
}

// RetractMemo marks a memo as retracted via the core edit chain.
// Inherited and external memos are read-only.
func RetractMemo(workdir, memoRef string) Result[bool] {
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	inheritedURLs := ListInherits(workdir)
	existing, err := GetMemoItemByRef(memoRef, workspaceURL)
	if err != nil {
		return result.Err[bool]("NOT_FOUND", "memo not found")
	}
	sourceTier := TierForRepoURL(existing.RepoURL, workspaceURL, inheritedURLs)
	if sourceTier == TierExternal || sourceTier == TierInherited {
		return result.Err[bool]("READONLY_TIER", string(sourceTier)+"-tier memos are read-only")
	}
	repoPath, repoURL, _, err := ResolveTierTarget(sourceTier, workdir, workspaceURL)
	if err != nil {
		return result.Err[bool]("TIER_INIT_FAILED", err.Error())
	}
	branch := existing.Branch
	if branch == "" {
		branch = MemoBranch
	}

	canonicalRef := protocol.LocalizeRef(
		protocol.CreateRef(protocol.RefTypeCommit, existing.Hash, existing.RepoURL, existing.Branch),
		repoURL,
	)
	header := protocol.Header{
		Ext: "memo",
		V:   "0.1.0",
		Fields: map[string]string{
			"edits":     canonicalRef,
			"retracted": "true",
		},
	}
	content := protocol.FormatMessage("", header, nil)
	hash, err := git.CreateCommitOnBranch(repoPath, branch, content)
	if err != nil {
		return result.Err[bool]("COMMIT_FAILED", err.Error())
	}
	if err := cacheMemoFromCommit(repoPath, repoURL, hash, branch); err != nil {
		return result.Err[bool]("CACHE_FAILED", err.Error())
	}
	return result.Ok(true)
}

// buildMemoContent assembles a memo commit body and GitMsg trailer.
func buildMemoContent(subject, body string, opts CreateMemoOptions, editsRef string) string {
	content := subject
	if body != "" {
		content += "\n\n" + body
	}
	fields := map[string]string{
		"type": "memo",
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if labelStr := joinLabels(opts.Labels); labelStr != "" {
		fields["labels"] = labelStr
	}
	protocol.ApplyOrigin(fields, opts.Origin)

	header := protocol.Header{
		Ext:        "memo",
		V:          "0.1.0",
		Fields:     fields,
		FieldOrder: memoFieldOrder,
	}
	return protocol.FormatMessage(content, header, nil)
}

// validateLabels rejects malformed special-form labels.
// `expires/<date>` must be `YYYY-MM-DD` or RFC3339; otherwise the label
// silently passes the SQL date filter and the memo never expires.
func validateLabels(labels []string) error {
	for _, l := range labels {
		l = strings.TrimSpace(l)
		v, ok := strings.CutPrefix(l, "expires/")
		if !ok {
			continue
		}
		if _, err := time.Parse("2006-01-02", v); err == nil {
			continue
		}
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			continue
		}
		return fmt.Errorf("label %q: expected expires/YYYY-MM-DD or expires/<RFC3339>", l)
	}
	return nil
}

// labelsEqual returns true when two label slices contain the same set of
// labels after trimming and ignoring duplicates and order. Memo.Labels is
// already normalized (sorted, deduped, trimmed via joinLabels); the user-
// supplied slice in opts.Labels might not be, so we normalize both sides.
func labelsEqual(a, b []string) bool {
	return joinLabels(a) == joinLabels(b)
}

// joinLabels produces a deterministic comma-separated label string.
func joinLabels(labels []string) string {
	cleaned := make([]string, 0, len(labels))
	seen := make(map[string]bool, len(labels))
	for _, l := range labels {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		cleaned = append(cleaned, l)
	}
	sort.Strings(cleaned)
	return strings.Join(cleaned, ",")
}

// cacheMemoFromCommit reads a freshly-written commit and inserts it into cache.
func cacheMemoFromCommit(workdir, repoURL, hash, branch string) error {
	commit, err := git.GetCommit(workdir, hash)
	if err != nil {
		return fmt.Errorf("get commit: %w", err)
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
	if msg == nil || msg.Header.Ext != "memo" {
		return nil
	}
	cache.ProcessVersionFromHeader(msg, hash, repoURL, branch)
	if err := InsertMemoItem(MemoItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "memo"}); err != nil {
		return fmt.Errorf("insert memo item: %w", err)
	}
	cache.SyncEditExtensionFields([]cache.EditKey{{RepoURL: repoURL, Hash: hash, Branch: branch}})
	return nil
}
