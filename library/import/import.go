// import.go - Import pipeline: plan, execute, report
package importpkg

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	releasepkg "github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

// ProgressEvent is sent to OnProgress to report import pipeline status.
type ProgressEvent struct {
	Extension string        // "pm", "release", "review", "social"
	Phase     ProgressPhase // "count", "fetch", "commit", "done"
	Stats     Stats         // cumulative stats so far
	ItemCount int           // items fetched so far in current phase
	ItemTotal int           // estimated total (-1 if unknown)
	Detail    string        // e.g. "Found: 1,247 issues, 892 PRs, 47 releases"
}

// Options configures an import run.
type Options struct {
	WorkDir    string
	RepoURL    string
	CacheDir   string
	Extensions []string // "pm", "release", "review", "social"
	MapFile    string
	LabelMode  string // "auto", "raw", "skip"
	DryRun     bool
	Update     bool
	Verbose    bool
	FetchOpts  FetchOptions
	Counts     *ItemCounts         // pre-fetched counts (skip counting if set)
	Mapping    *MappingFile        // pre-loaded mapping (skip ReadMapping if set)
	OnProgress func(ProgressEvent) // optional progress callback
}

// flushMapping writes the mapping to disk if not a dry run.
func flushMapping(opts Options, mapping *MappingFile) {
	if !opts.DryRun {
		if err := WriteMapping(opts.CacheDir, opts.RepoURL, opts.MapFile, mapping); err != nil {
			log.Warn("failed to flush mapping file", "error", err)
		}
	}
}

// Run executes the full import pipeline for the requested extensions.
func Run(adapter SourceAdapter, opts Options) (Stats, error) {
	progress := func(ext string, phase ProgressPhase, stats Stats, itemCount, itemTotal int, detail string) {
		if opts.OnProgress != nil {
			opts.OnProgress(ProgressEvent{
				Extension: ext, Phase: phase, Stats: stats,
				ItemCount: itemCount, ItemTotal: itemTotal, Detail: detail,
			})
		}
	}
	// Use pre-fetched counts or count now
	var counts ItemCounts
	if opts.Counts != nil {
		counts = *opts.Counts
	} else {
		var countErr error
		counts, countErr = adapter.CountItems(opts.FetchOpts)
		if countErr != nil {
			log.Warn("failed to count items", "error", countErr)
		}
		detail := FormatItemCounts(counts)
		if detail != "" {
			detail = "Found: " + detail
		}
		progress("", PhaseCount, Stats{}, 0, 0, detail)
	}
	var mapping *MappingFile
	if opts.Mapping != nil {
		mapping = opts.Mapping
	} else {
		mapping = ReadMapping(opts.CacheDir, opts.RepoURL, opts.MapFile)
		if len(mapping.Items) == 0 {
			RebuildMapping(opts.WorkDir, mapping)
		}
	}
	mapping.Source = adapter.Platform()
	mapping.RepoURL = opts.RepoURL
	// Store platform-specific metadata (e.g. GitLab project ID for upload URL resolution)
	if provider, ok := adapter.(PlatformMetaProvider); ok {
		if meta := provider.PlatformMeta(); len(meta) > 0 {
			normalized := protocol.NormalizeURL(opts.RepoURL)
			for k, v := range meta {
				if err := cache.SetRepositoryMeta(normalized, k, v); err != nil {
					log.Debug("failed to set repository meta", "key", k, "error", err)
				}
			}
		}
	}
	if !opts.Update {
		opts.FetchOpts.SkipExternalIDs = MappedExternalIDs(mapping, adapter.Platform())
	}
	stats := Stats{}
	exts := opts.Extensions
	if len(exts) == 0 || (len(exts) == 1 && exts[0] == "all") {
		exts = []string{"pm", "release", "review", "social"}
	}
	extSet := map[string]bool{}
	for _, e := range exts {
		extSet[e] = true
	}
	if extSet["pm"] {
		fetchOpts := opts.FetchOpts
		fetchOpts.OnFetchProgress = func(fetched int) {
			progress("pm", PhaseFetch, stats, fetched, counts.Issues, "")
		}
		progress("pm", PhaseFetch, stats, 0, counts.Issues, "")
		plan, err := adapter.FetchPM(fetchOpts)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "pm", Message: err.Error()})
		} else if plan != nil {
			stats.FilteredIssues += plan.Filtered
			progress("pm", PhaseCommit, stats, 0, 0, "")
			s := executePM(opts, plan, mapping)
			stats.Milestones += s.Milestones
			stats.Issues += s.Issues
			stats.Skipped += s.Skipped
			stats.Errors = append(stats.Errors, s.Errors...)
			if opts.Update {
				u := updatePM(opts, plan, mapping)
				stats.UpdatedIssues += u.UpdatedIssues
				stats.UpdatedMilestones += u.UpdatedMilestones
				stats.Errors = append(stats.Errors, u.Errors...)
			}
		}
		flushMapping(opts, mapping)
		progress("pm", PhaseDone, stats, 0, 0, "")
	}
	if extSet["release"] {
		fetchOpts := opts.FetchOpts
		fetchOpts.OnFetchProgress = func(fetched int) {
			progress("release", PhaseFetch, stats, fetched, counts.Releases, "")
		}
		progress("release", PhaseFetch, stats, 0, counts.Releases, "")
		plan, err := adapter.FetchReleases(fetchOpts)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "release", Message: err.Error()})
		} else if plan != nil {
			stats.FilteredReleases += plan.Filtered
			progress("release", PhaseCommit, stats, 0, 0, "")
			s := executeRelease(opts, plan, mapping)
			stats.Releases += s.Releases
			stats.Skipped += s.Skipped
			stats.Errors = append(stats.Errors, s.Errors...)
			if opts.Update {
				u := updateRelease(opts, plan, mapping)
				stats.UpdatedReleases += u.UpdatedReleases
				stats.Errors = append(stats.Errors, u.Errors...)
			}
		}
		flushMapping(opts, mapping)
		progress("release", PhaseDone, stats, 0, 0, "")
	}
	if extSet["review"] {
		fetchOpts := opts.FetchOpts
		fetchOpts.OnFetchProgress = func(fetched int) {
			progress("review", PhaseFetch, stats, fetched, counts.PRs, "")
		}
		progress("review", PhaseFetch, stats, 0, counts.PRs, "")
		plan, err := adapter.FetchReview(fetchOpts)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "review", Message: err.Error()})
		} else if plan != nil {
			stats.FilteredPRs += plan.Filtered
			progress("review", PhaseCommit, stats, 0, 0, "")
			s := executeReview(opts, plan, mapping)
			stats.Forks += s.Forks
			stats.PRs += s.PRs
			stats.Skipped += s.Skipped
			stats.Errors = append(stats.Errors, s.Errors...)
			if opts.Update {
				u := updateReview(opts, plan, mapping)
				stats.UpdatedPRs += u.UpdatedPRs
				stats.Errors = append(stats.Errors, u.Errors...)
			}
			// Stack detection runs after both new imports and updates so newly stacked
			// relationships (e.g., when a child PR is added to an existing parent on the
			// platform between runs) are picked up incrementally. Idempotent.
			if !opts.DryRun {
				stackErrs := applyStackRelationships(opts, plan, mapping)
				stats.Errors = append(stats.Errors, stackErrs...)
			}
		}
		flushMapping(opts, mapping)
		progress("review", PhaseDone, stats, 0, 0, "")
	}
	if extSet["social"] {
		fetchOpts := opts.FetchOpts
		fetchOpts.OnFetchProgress = func(fetched int) {
			progress("social", PhaseFetch, stats, fetched, counts.Discussions, "")
		}
		progress("social", PhaseFetch, stats, 0, counts.Discussions, "")
		plan, err := adapter.FetchSocial(fetchOpts)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "social", Message: err.Error()})
		} else if plan != nil {
			stats.FilteredDiscussions += plan.Filtered
			progress("social", PhaseCommit, stats, 0, 0, "")
			s := executeSocial(opts, plan, mapping)
			stats.Posts += s.Posts
			stats.Comments += s.Comments
			stats.Skipped += s.Skipped
			stats.Errors = append(stats.Errors, s.Errors...)
			if opts.Update {
				u := updateSocial(opts, plan, mapping)
				stats.UpdatedPosts += u.UpdatedPosts
				stats.Errors = append(stats.Errors, u.Errors...)
			}
		}
		flushMapping(opts, mapping)
		progress("social", PhaseDone, stats, 0, 0, "")
	}
	if !opts.DryRun {
		if err := WriteMapping(opts.CacheDir, opts.RepoURL, opts.MapFile, mapping); err != nil {
			return stats, fmt.Errorf("write mapping: %w", err)
		}
	}
	return stats, nil
}

func executePM(opts Options, plan *PMPlan, mapping *MappingFile) Stats {
	stats := Stats{}
	platform := mapping.Source
	if opts.DryRun {
		for _, m := range plan.Milestones {
			if mapping.IsMapped(MappingKey(platform, "milestone", m.ExternalID)) {
				stats.Skipped++
			} else {
				stats.Milestones++
			}
		}
		for _, issue := range plan.Issues {
			if mapping.IsMapped(MappingKey(platform, "issue", issue.ExternalID)) {
				stats.Skipped++
			} else {
				stats.Issues++
			}
		}
		return stats
	}
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	branch := gitmsg.GetExtBranch(opts.WorkDir, "pm")
	authorName, authorEmail, err := git.GetAuthorIdentity(opts.WorkDir)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "pm", Message: "get author: " + err.Error()})
		return stats
	}
	now := time.Now()
	// Phase 1: prepare milestone create messages
	type msEntry struct {
		item    ImportMilestone
		message string
	}
	var msEntries []msEntry
	var msMessages []string
	for _, m := range plan.Milestones {
		if mapping.IsMapped(MappingKey(platform, "milestone", m.ExternalID)) {
			stats.Skipped++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  pm  milestone: %s\n", m.Title)
		}
		origin := buildOrigin(m.AuthorName, m.AuthorEmail, m.CreatedAt, platform, opts.RepoURL, platformPath(platform, "milestone", fmt.Sprintf("%d", m.Number)))
		msg := buildMilestoneMessage(m.Title, m.Body, "open", m.DueDate, "", origin)
		msEntries = append(msEntries, msEntry{item: m, message: msg})
		msMessages = append(msMessages, msg)
	}
	// Phase 2: batch create milestone commits
	if len(msMessages) > 0 {
		msHashes, err := git.FastImportCommits(opts.WorkDir, branch, msMessages)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "milestone", Message: "fast-import: " + err.Error()})
			return stats
		}
		// Phase 3: cache + mapping
		for i, entry := range msEntries {
			hash := msHashes[i]
			if err := cache.InsertCommits([]cache.Commit{{
				Hash: hash, RepoURL: repoURL, Branch: branch,
				AuthorName: authorName, AuthorEmail: authorEmail,
				Message: entry.message, Timestamp: importTime(entry.item.CreatedAt, now),
			}}); err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "milestone", Message: "cache: " + err.Error()})
				continue
			}
			dueStr := ""
			if entry.item.DueDate != nil {
				dueStr = entry.item.DueDate.Format("2006-01-02")
			}
			if err := pm.InsertPMItem(pm.PMItem{
				RepoURL: repoURL, Hash: hash, Branch: branch,
				Type: "milestone", State: "open",
				Due: cache.ToNullString(dueStr),
			}); err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "milestone", Message: "cache: " + err.Error()})
				continue
			}
			key := MappingKey(platform, "milestone", entry.item.ExternalID)
			mapping.Record(key, hash, "gitmsg/pm", "milestone")
			stats.Milestones++
		}
		// Phase 4: prepare milestone close messages
		var closeMessages []string
		var closeIndices []int
		for i, entry := range msEntries {
			if entry.item.State != "closed" {
				continue
			}
			hash := msHashes[i]
			editsRef := protocol.CreateRef(protocol.RefTypeCommit, hash, "", branch)
			closeOrigin := buildStateChangeOrigin("", "", time.Time{}, platform, opts.RepoURL, platformPath(platform, "milestone", fmt.Sprintf("%d", entry.item.Number)))
			msg := buildMilestoneMessage(entry.item.Title, entry.item.Body, "closed", entry.item.DueDate, editsRef, closeOrigin)
			closeMessages = append(closeMessages, msg)
			closeIndices = append(closeIndices, i)
		}
		// Phase 5: batch create milestone close commits
		if len(closeMessages) > 0 {
			closeHashes, err := git.FastImportCommits(opts.WorkDir, branch, closeMessages)
			if err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "milestone-close", Message: "fast-import: " + err.Error()})
			} else {
				for j, closeHash := range closeHashes {
					entry := msEntries[closeIndices[j]]
					dueStr := ""
					if entry.item.DueDate != nil {
						dueStr = entry.item.DueDate.Format("2006-01-02")
					}
					if err := cache.InsertCommits([]cache.Commit{{
						Hash: closeHash, RepoURL: repoURL, Branch: branch,
						AuthorName: authorName, AuthorEmail: authorEmail,
						Message: closeMessages[j], Timestamp: now,
					}}); err != nil {
						stats.Errors = append(stats.Errors, ImportError{Type: "milestone-close", Message: "cache: " + err.Error()})
						continue
					}
					if err := pm.InsertPMItem(pm.PMItem{
						RepoURL: repoURL, Hash: closeHash, Branch: branch,
						Type: "milestone", State: "closed",
						Due: cache.ToNullString(dueStr),
					}); err != nil {
						stats.Errors = append(stats.Errors, ImportError{Type: "milestone-close", Message: "cache: " + err.Error()})
						continue
					}
				}
			}
		}
	}
	// Phase 6: prepare issue create messages
	type issueEntry struct {
		item      ImportIssue
		message   string
		milestone string
		labels    string
	}
	var issueEntries []issueEntry
	var issueMessages []string
	for _, issue := range plan.Issues {
		if mapping.IsMapped(MappingKey(platform, "issue", issue.ExternalID)) {
			stats.Skipped++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  pm  issue #%d: %s\n", issue.Number, issue.Title)
		}
		labels := MapLabels(issue.Labels, opts.LabelMode)
		labelStr := strings.Join(labels, ",")
		milestoneRef := ""
		if issue.MilestoneID != "" {
			msKey := MappingKey(platform, "milestone", issue.MilestoneID)
			if hash := mapping.GetHash(msKey); hash != "" && len(hash) >= 12 {
				milestoneRef = "#commit:" + hash[:12]
			}
		}
		origin := buildOrigin(issue.AuthorName, issue.AuthorEmail, issue.CreatedAt, platform, opts.RepoURL, platformPath(platform, "issue", issue.ExternalID))
		msg := buildIssueMessage(issue.Title, issue.Body, "open", issue.Assignees, nil, milestoneRef, labelStr, "", origin)
		issueEntries = append(issueEntries, issueEntry{item: issue, message: msg, milestone: milestoneRef, labels: labelStr})
		issueMessages = append(issueMessages, msg)
	}
	// Phase 7: batch create issue commits
	if len(issueMessages) > 0 {
		issueHashes, err := git.FastImportCommits(opts.WorkDir, branch, issueMessages)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "issue", Message: "fast-import: " + err.Error()})
			return stats
		}
		// Phase 8: cache + mapping
		for i, entry := range issueEntries {
			hash := issueHashes[i]
			if err := cache.InsertCommits([]cache.Commit{{
				Hash: hash, RepoURL: repoURL, Branch: branch,
				AuthorName: authorName, AuthorEmail: authorEmail,
				Message: entry.message, Timestamp: importTime(entry.item.CreatedAt, now),
			}}); err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "issue", Message: "cache: " + err.Error()})
				continue
			}
			msRepoURL, msHash, msBranch := parseRefComponents(entry.milestone, repoURL, branch)
			if err := pm.InsertPMItem(pm.PMItem{
				RepoURL: repoURL, Hash: hash, Branch: branch,
				Type: "issue", State: "open",
				Assignees:        cache.ToNullString(strings.Join(entry.item.Assignees, ",")),
				Labels:           cache.ToNullString(entry.labels),
				MilestoneRepoURL: cache.ToNullString(msRepoURL),
				MilestoneHash:    cache.ToNullString(msHash),
				MilestoneBranch:  cache.ToNullString(msBranch),
			}); err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "issue", Message: "cache: " + err.Error()})
				continue
			}
			key := MappingKey(platform, "issue", entry.item.ExternalID)
			mapping.Record(key, hash, "gitmsg/pm", "issue")
			stats.Issues++
		}
		flushMapping(opts, mapping)
		// Phase 9: prepare issue close messages
		var closeMessages []string
		var closeIndices []int
		for i, entry := range issueEntries {
			if entry.item.State != "closed" {
				continue
			}
			hash := issueHashes[i]
			editsRef := protocol.CreateRef(protocol.RefTypeCommit, hash, "", branch)
			closeOrigin := buildStateChangeOrigin(entry.item.ClosedByName, entry.item.ClosedByEmail, entry.item.ClosedAt, platform, opts.RepoURL, platformPath(platform, "issue", entry.item.ExternalID))
			msg := buildIssueMessage(entry.item.Title, entry.item.Body, "closed", entry.item.Assignees, nil, entry.milestone, entry.labels, editsRef, closeOrigin)
			closeMessages = append(closeMessages, msg)
			closeIndices = append(closeIndices, i)
		}
		// Phase 10: batch create issue close commits
		if len(closeMessages) > 0 {
			closeHashes, err := git.FastImportCommits(opts.WorkDir, branch, closeMessages)
			if err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "issue-close", Message: "fast-import: " + err.Error()})
			} else {
				for j, closeHash := range closeHashes {
					entry := issueEntries[closeIndices[j]]
					msRepoURL, msHash, msBranch := parseRefComponents(entry.milestone, repoURL, branch)
					if err := cache.InsertCommits([]cache.Commit{{
						Hash: closeHash, RepoURL: repoURL, Branch: branch,
						AuthorName: authorName, AuthorEmail: authorEmail,
						Message: closeMessages[j], Timestamp: importTime(entry.item.ClosedAt, now),
					}}); err != nil {
						stats.Errors = append(stats.Errors, ImportError{Type: "issue-close", Message: "cache: " + err.Error()})
						continue
					}
					if err := pm.InsertPMItem(pm.PMItem{
						RepoURL: repoURL, Hash: closeHash, Branch: branch,
						Type: "issue", State: "closed",
						Assignees:        cache.ToNullString(strings.Join(entry.item.Assignees, ",")),
						Labels:           cache.ToNullString(entry.labels),
						MilestoneRepoURL: cache.ToNullString(msRepoURL),
						MilestoneHash:    cache.ToNullString(msHash),
						MilestoneBranch:  cache.ToNullString(msBranch),
					}); err != nil {
						stats.Errors = append(stats.Errors, ImportError{Type: "issue-close", Message: "cache: " + err.Error()})
						continue
					}
				}
			}
		}
		// Phase 11: issue links (still sequential — small count, requires all mappings)
		for i, entry := range issueEntries {
			issue := entry.item
			if len(issue.BlocksIDs) == 0 && len(issue.BlockedByIDs) == 0 && len(issue.RelatedIDs) == 0 {
				continue
			}
			blocks := resolveIssueLinks(issue.BlocksIDs, platform, mapping)
			blockedBy := resolveIssueLinks(issue.BlockedByIDs, platform, mapping)
			related := resolveIssueLinks(issue.RelatedIDs, platform, mapping)
			if len(blocks) == 0 && len(blockedBy) == 0 && len(related) == 0 {
				continue
			}
			ref := "#commit:" + issueHashes[i][:12]
			result := pm.UpdateIssue(opts.WorkDir, ref, pm.UpdateIssueOptions{
				Blocks:    &blocks,
				BlockedBy: &blockedBy,
				Related:   &related,
			})
			if !result.Success {
				log.Warn("failed to update issue links", "ref", ref, "error", result.Error.Message)
			}
		}
	}
	return stats
}

func executeRelease(opts Options, plan *ReleasePlan, mapping *MappingFile) Stats {
	stats := Stats{}
	platform := mapping.Source
	if opts.DryRun {
		for _, r := range plan.Releases {
			if mapping.IsMapped(MappingKey(platform, "release", r.ExternalID)) {
				stats.Skipped++
			} else {
				stats.Releases++
			}
		}
		return stats
	}
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	branch := gitmsg.GetExtBranch(opts.WorkDir, "release")
	authorName, authorEmail, err := git.GetAuthorIdentity(opts.WorkDir)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "release", Message: "get author: " + err.Error()})
		return stats
	}
	now := time.Now()
	type relEntry struct {
		item    ImportRelease
		message string
	}
	var entries []relEntry
	var messages []string
	for _, r := range plan.Releases {
		if mapping.IsMapped(MappingKey(platform, "release", r.ExternalID)) {
			stats.Skipped++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  release  %s (%s)\n", r.Name, r.Tag)
		}
		origin := buildOrigin(r.AuthorName, r.AuthorEmail, r.CreatedAt, platform, opts.RepoURL, platformPath(platform, "release", r.Tag))
		msg := buildReleaseMessage(r, "", origin)
		entries = append(entries, relEntry{item: r, message: msg})
		messages = append(messages, msg)
	}
	if len(messages) == 0 {
		return stats
	}
	hashes, err := git.FastImportCommits(opts.WorkDir, branch, messages)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "release", Message: "fast-import: " + err.Error()})
		return stats
	}
	for i, entry := range entries {
		hash := hashes[i]
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: branch,
			AuthorName: authorName, AuthorEmail: authorEmail,
			Message: entry.message, Timestamp: importTime(entry.item.CreatedAt, now),
		}}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "release", Message: "cache: " + err.Error()})
			continue
		}
		if err := releasepkg.InsertReleaseItem(releasepkg.ReleaseItem{
			RepoURL:     repoURL,
			Hash:        hash,
			Branch:      branch,
			Tag:         cache.ToNullString(entry.item.Tag),
			Version:     cache.ToNullString(entry.item.Version),
			Prerelease:  entry.item.Prerelease,
			Artifacts:   cache.ToNullString(strings.Join(entry.item.Artifacts, ",")),
			ArtifactURL: cache.ToNullString(entry.item.ArtifactURL),
			Checksums:   cache.ToNullString(entry.item.Checksums),
			SignedBy:    cache.ToNullString(entry.item.SignedBy),
			SBOM:        cache.ToNullString(entry.item.SBOM),
		}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "release", Message: "cache: " + err.Error()})
			continue
		}
		key := MappingKey(platform, "release", entry.item.ExternalID)
		mapping.Record(key, hash, "gitmsg/release", "release")
		stats.Releases++
	}
	return stats
}

func executeReview(opts Options, plan *ReviewPlan, mapping *MappingFile) Stats {
	stats := Stats{}
	if len(plan.Forks) > 0 {
		if opts.DryRun {
			stats.Forks = len(plan.Forks)
		} else {
			if opts.Verbose {
				for _, forkURL := range plan.Forks {
					fmt.Printf("  review  fork: %s\n", forkURL)
				}
			}
			added, err := review.AddForks(opts.WorkDir, plan.Forks)
			if err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "fork", Message: err.Error()})
			} else {
				stats.Forks = added
			}
		}
	}
	platform := mapping.Source
	if opts.DryRun {
		for _, pr := range plan.PRs {
			if mapping.IsMapped(MappingKey(platform, "pr", pr.ExternalID)) {
				stats.Skipped++
			} else {
				stats.PRs++
			}
		}
		return stats
	}
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	branch := gitmsg.GetExtBranch(opts.WorkDir, "review")
	authorName, authorEmail, err := git.GetAuthorIdentity(opts.WorkDir)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "review", Message: "get author: " + err.Error()})
		return stats
	}
	now := time.Now()
	type prEntry struct {
		item                         ImportPR
		message                      string
		base, baseTip, head, headTip string
		labels                       []string
		mergeBase, mergeHead         string
	}
	var prEntries []prEntry
	var prMessages []string
	for _, pr := range plan.PRs {
		if mapping.IsMapped(MappingKey(platform, "pr", pr.ExternalID)) {
			stats.Skipped++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  review  PR #%d: %s\n", pr.Number, pr.Title)
		}
		head := "#branch:" + pr.HeadBranch
		if pr.HeadRepo != "" {
			head = pr.HeadRepo + "#branch:" + pr.HeadBranch
		}
		base := "#branch:" + pr.BaseBranch
		labels := MapLabels(pr.Labels, opts.LabelMode)
		var baseTip, headTip string
		if tip, err := git.ReadRef(opts.WorkDir, pr.BaseBranch); err == nil && len(tip) >= 12 {
			baseTip = tip[:12]
		} else if tip, err := git.ReadRef(opts.WorkDir, "origin/"+pr.BaseBranch); err == nil && len(tip) >= 12 {
			baseTip = tip[:12]
		}
		if pr.HeadRepo == "" {
			if tip, err := git.ReadRef(opts.WorkDir, pr.HeadBranch); err == nil && len(tip) >= 12 {
				headTip = tip[:12]
			} else if tip, err := git.ReadRef(opts.WorkDir, "origin/"+pr.HeadBranch); err == nil && len(tip) >= 12 {
				headTip = tip[:12]
			}
		}
		if pr.HeadRepo != "" && pr.HeadSHA != "" && headTip == "" {
			sha := pr.HeadSHA
			if len(sha) > 12 {
				sha = sha[:12]
			}
			headTip = sha
		}
		if pr.HeadRepo != "" && pr.DiffBaseSHA != "" {
			sha := pr.DiffBaseSHA
			if len(sha) > 12 {
				sha = sha[:12]
			}
			baseTip = sha
		}
		// Resolve merge-base/merge-head for merged PRs during prepare
		var mBase, mHead string
		if pr.State == "merged" && pr.MergeCommit != "" {
			if b, err := git.ReadRef(opts.WorkDir, pr.MergeCommit+"^1"); err == nil {
				mBase = b
			}
			if h, err := git.ReadRef(opts.WorkDir, pr.MergeCommit); err == nil {
				mHead = h
			}
			// Fallback: if merge commit isn't locally available, store the raw SHA
			// and use base-tip as an approximation for merge-base
			if mHead == "" {
				sha := pr.MergeCommit
				if len(sha) > 12 {
					sha = sha[:12]
				}
				mHead = sha
			}
			if mBase == "" && baseTip != "" {
				mBase = baseTip
			}
		}
		origin := buildOrigin(pr.AuthorName, pr.AuthorEmail, pr.CreatedAt, platform, opts.RepoURL, platformPath(platform, "pr", pr.ExternalID))
		msg := buildPRMessage(pr.Title, pr.Body, "open", pr.IsDraft, base, baseTip, head, headTip, pr.Reviewers, labels, "", "", "", origin)
		prEntries = append(prEntries, prEntry{
			item: pr, message: msg,
			base: base, baseTip: baseTip, head: head, headTip: headTip,
			labels: labels, mergeBase: mBase, mergeHead: mHead,
		})
		prMessages = append(prMessages, msg)
	}
	if len(prMessages) == 0 {
		return stats
	}
	prHashes, err := git.FastImportCommits(opts.WorkDir, branch, prMessages)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "pr", Message: "fast-import: " + err.Error()})
		return stats
	}
	for i, entry := range prEntries {
		hash := prHashes[i]
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: branch,
			AuthorName: authorName, AuthorEmail: authorEmail,
			Message: entry.message, Timestamp: importTime(entry.item.CreatedAt, now),
		}}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "pr", Message: "cache: " + err.Error()})
			continue
		}
		draft := 0
		if entry.item.IsDraft {
			draft = 1
		}
		if err := review.InsertReviewItem(review.ReviewItem{
			RepoURL:   repoURL,
			Hash:      hash,
			Branch:    branch,
			Type:      "pull-request",
			State:     cache.ToNullString("open"),
			Draft:     draft,
			Base:      cache.ToNullString(entry.base),
			BaseTip:   cache.ToNullString(entry.baseTip),
			Head:      cache.ToNullString(entry.head),
			HeadTip:   cache.ToNullString(entry.headTip),
			Reviewers: cache.ToNullString(strings.Join(entry.item.Reviewers, ",")),
			Labels:    cache.ToNullString(strings.Join(entry.labels, ",")),
		}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "pr", Message: "cache: " + err.Error()})
			continue
		}
		key := MappingKey(platform, "pr", entry.item.ExternalID)
		mapping.Record(key, hash, "gitmsg/review", "pull-request")
		stats.PRs++
	}
	flushMapping(opts, mapping)
	// State-change phase: prepare close/merge messages
	var stateMessages []string
	var stateData []prEntry
	for i, entry := range prEntries {
		if entry.item.State != "closed" && entry.item.State != "merged" {
			continue
		}
		hash := prHashes[i]
		editsRef := protocol.CreateRef(protocol.RefTypeCommit, hash, "", branch)
		state := entry.item.State
		var stateOrigin *protocol.Origin
		mBase, mHead := "", ""
		if state == "merged" {
			stateOrigin = buildStateChangeOrigin(entry.item.MergedByName, entry.item.MergedByEmail, entry.item.MergedAt, platform, opts.RepoURL, platformPath(platform, "pr", entry.item.ExternalID))
			mBase = entry.mergeBase
			mHead = entry.mergeHead
			// GITREVIEW.md §1.5: state="merged" edits MUST include both
			// merge-base and merge-head. The prepare phase falls back to
			// pr.MergeCommit / baseTip; if both fallbacks failed, leave
			// the PR open rather than write a non-conformant edit.
			if mBase == "" || mHead == "" {
				stats.Errors = append(stats.Errors, ImportError{
					Type:    "pr-state",
					Message: fmt.Sprintf("PR %s: cannot record merged state (missing merge-base/merge-head); imported as open", entry.item.ExternalID),
				})
				continue
			}
		} else {
			stateOrigin = buildStateChangeOrigin(entry.item.ClosedByName, entry.item.ClosedByEmail, entry.item.ClosedAt, platform, opts.RepoURL, platformPath(platform, "pr", entry.item.ExternalID))
		}
		msg := buildPRMessage(entry.item.Title, entry.item.Body, state, false, entry.base, entry.baseTip, entry.head, entry.headTip, entry.item.Reviewers, entry.labels, mBase, mHead, editsRef, stateOrigin)
		stateMessages = append(stateMessages, msg)
		stateData = append(stateData, entry)
	}
	if len(stateMessages) > 0 {
		stateHashes, err := git.FastImportCommits(opts.WorkDir, branch, stateMessages)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "pr-state", Message: "fast-import: " + err.Error()})
		} else {
			for j, stateHash := range stateHashes {
				entry := stateData[j]
				state := entry.item.State
				stateTime := entry.item.ClosedAt
				if state == "merged" {
					stateTime = entry.item.MergedAt
				}
				if err := cache.InsertCommits([]cache.Commit{{
					Hash: stateHash, RepoURL: repoURL, Branch: branch,
					AuthorName: authorName, AuthorEmail: authorEmail,
					Message: stateMessages[j], Timestamp: importTime(stateTime, now),
				}}); err != nil {
					stats.Errors = append(stats.Errors, ImportError{Type: "pr-state", Message: "cache: " + err.Error()})
					continue
				}
				if err := review.InsertReviewItem(review.ReviewItem{
					RepoURL:   repoURL,
					Hash:      stateHash,
					Branch:    branch,
					Type:      "pull-request",
					State:     cache.ToNullString(state),
					Base:      cache.ToNullString(entry.base),
					BaseTip:   cache.ToNullString(entry.baseTip),
					Head:      cache.ToNullString(entry.head),
					HeadTip:   cache.ToNullString(entry.headTip),
					Reviewers: cache.ToNullString(strings.Join(entry.item.Reviewers, ",")),
					Labels:    cache.ToNullString(strings.Join(entry.labels, ",")),
				}); err != nil {
					stats.Errors = append(stats.Errors, ImportError{Type: "pr-state", Message: "cache: " + err.Error()})
					continue
				}
			}
		}
	}
	return stats
}

// stackEdge represents a detected parent/child stack relationship between two PRs.
type stackEdge struct {
	ChildExternalID  string
	ParentExternalID string
}

// detectStackEdges finds same-repo PRs whose base branch matches another PR's head branch.
// Returns parent/child pairs (ChildExternalID depends on ParentExternalID).
// Only considers open PRs — stack operations don't apply to merged/closed.
// Platform-agnostic: operates purely on ImportPR fields populated by any adapter.
func detectStackEdges(prs []ImportPR) []stackEdge {
	if len(prs) == 0 {
		return nil
	}
	byHead := make(map[string]ImportPR)
	for _, pr := range prs {
		if pr.HeadRepo != "" {
			continue
		}
		if _, exists := byHead[pr.HeadBranch]; !exists {
			byHead[pr.HeadBranch] = pr
		}
	}
	var edges []stackEdge
	for _, pr := range prs {
		if pr.State != "open" || pr.HeadRepo != "" {
			continue
		}
		parent, ok := byHead[pr.BaseBranch]
		if !ok || parent.Number == pr.Number {
			continue
		}
		edges = append(edges, stackEdge{
			ChildExternalID:  pr.ExternalID,
			ParentExternalID: parent.ExternalID,
		})
	}
	return edges
}

// applyStackRelationships detects stack dependencies and creates edit commits
// adding depends-on to each child PR. Platform-agnostic — works for GitHub,
// GitLab, and any other adapter that populates ImportPR uniformly. Idempotent:
// skips PRs that already have the correct depends-on, enabling safe re-runs
// during --update mode.
func applyStackRelationships(opts Options, plan *ReviewPlan, mapping *MappingFile) []ImportError {
	edges := detectStackEdges(plan.PRs)
	if len(edges) == 0 {
		return nil
	}
	platform := mapping.Source
	branch := gitmsg.GetExtBranch(opts.WorkDir, "review")
	var errors []ImportError
	var applied int
	for _, edge := range edges {
		childHash := mapping.GetHash(MappingKey(platform, "pr", edge.ChildExternalID))
		parentHash := mapping.GetHash(MappingKey(platform, "pr", edge.ParentExternalID))
		if childHash == "" || parentHash == "" {
			continue
		}
		childRef := protocol.CreateRef(protocol.RefTypeCommit, childHash, "", branch)
		parentRef := protocol.CreateRef(protocol.RefTypeCommit, parentHash, "", branch)
		// Idempotency: skip if child already has exactly this depends-on.
		if existing := review.GetPR(childRef); existing.Success {
			if len(existing.Data.DependsOn) == 1 && existing.Data.DependsOn[0] == parentRef {
				continue
			}
		}
		deps := []string{parentRef}
		res := review.UpdatePR(opts.WorkDir, childRef, review.UpdatePROptions{DependsOn: &deps})
		if !res.Success {
			errors = append(errors, ImportError{ExternalID: edge.ChildExternalID, Type: "pr-stack", Message: res.Error.Message})
			continue
		}
		applied++
	}
	if opts.Verbose && applied > 0 {
		fmt.Printf("  review  linked %d stacked PR(s) via depends-on\n", applied)
	}
	return errors
}

func executeSocial(opts Options, plan *SocialPlan, mapping *MappingFile) Stats {
	stats := Stats{}
	platform := mapping.Source
	if opts.DryRun {
		for _, post := range plan.Posts {
			if mapping.IsMapped(MappingKey(platform, "post", post.ExternalID)) {
				stats.Skipped++
			} else {
				stats.Posts++
			}
		}
		for _, comment := range plan.Comments {
			if mapping.IsMapped(MappingKey(platform, "comment", comment.ExternalID)) {
				stats.Skipped++
			} else if mapping.GetHash(MappingKey(platform, "post", comment.PostID)) == "" {
				stats.Skipped++
			} else {
				stats.Comments++
			}
		}
		return stats
	}
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	branch := gitmsg.GetExtBranch(opts.WorkDir, "social")
	authorName, authorEmail, err := git.GetAuthorIdentity(opts.WorkDir)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "social", Message: "get author: " + err.Error()})
		return stats
	}
	now := time.Now()
	// Phase 1: prepare post messages
	type postEntry struct {
		item    ImportPost
		message string
	}
	var postEntries []postEntry
	var postMessages []string
	for _, post := range plan.Posts {
		if mapping.IsMapped(MappingKey(platform, "post", post.ExternalID)) {
			stats.Skipped++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  social  post: %s\n", truncate(post.Content, 60))
		}
		origin := buildOrigin(post.AuthorName, post.AuthorEmail, post.CreatedAt, platform, opts.RepoURL, platformPath(platform, "post", post.ExternalID))
		msg := buildPostMessage(post.Content, "", origin)
		postEntries = append(postEntries, postEntry{item: post, message: msg})
		postMessages = append(postMessages, msg)
	}
	// Phase 2: batch create post commits
	if len(postMessages) > 0 {
		postHashes, err := git.FastImportCommits(opts.WorkDir, branch, postMessages)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "post", Message: "fast-import: " + err.Error()})
			return stats
		}
		// Phase 3: cache + mapping
		for i, entry := range postEntries {
			hash := postHashes[i]
			if err := cache.InsertCommits([]cache.Commit{{
				Hash: hash, RepoURL: repoURL, Branch: branch,
				AuthorName: authorName, AuthorEmail: authorEmail,
				Message: entry.message, Timestamp: importTime(entry.item.CreatedAt, now),
			}}); err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "post", Message: "cache: " + err.Error()})
				continue
			}
			if err := social.InsertSocialItem(social.SocialItem{
				RepoURL: repoURL, Hash: hash, Branch: branch,
				Type: "post",
			}); err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "post", Message: "cache: " + err.Error()})
				continue
			}
			key := MappingKey(platform, "post", entry.item.ExternalID)
			mapping.Record(key, hash, "gitmsg/social", "post")
			stats.Posts++
		}
		flushMapping(opts, mapping)
	}
	// Phase 4: prepare comment messages
	// Build a lookup from post ExternalID to (hash, content) for ref sections
	postLookup := map[string]struct{ hash, content string }{}
	for i, entry := range postEntries {
		if i < len(postMessages) {
			// postHashes may not exist if fast-import failed above, but we returned early in that case
			h := mapping.GetHash(MappingKey(platform, "post", entry.item.ExternalID))
			postLookup[entry.item.ExternalID] = struct{ hash, content string }{h, entry.item.Content}
		}
	}
	type commentEntry struct {
		item    ImportComment
		message string
	}
	var commentEntries []commentEntry
	var commentMessages []string
	var commentOriginals []struct{ repoURL, hash, branch string }
	for _, comment := range plan.Comments {
		if mapping.IsMapped(MappingKey(platform, "comment", comment.ExternalID)) {
			stats.Skipped++
			continue
		}
		postHash := mapping.GetHash(MappingKey(platform, "post", comment.PostID))
		if postHash == "" {
			stats.Skipped++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  social  comment: %s\n", truncate(comment.Content, 60))
		}
		originalRef := protocol.CreateRef(protocol.RefTypeCommit, postHash, "", branch)
		// Build GitMsg-Ref section for the original post
		var ref *protocol.Ref
		if info, ok := postLookup[comment.PostID]; ok {
			r := protocol.Ref{
				Ext: "social", Author: authorName, Email: authorEmail,
				Time: now.Format(time.RFC3339), Ref: originalRef, V: "0.1.0",
				Fields:   map[string]string{"type": "post"},
				Metadata: protocol.QuoteContent(info.content),
			}
			ref = &r
		}
		commentPath := ""
		if comment.PostID != "" {
			commentPath = platformPath(platform, "post", comment.PostID) + "#discussioncomment-" + comment.ExternalID
		}
		origin := buildOrigin(comment.AuthorName, comment.AuthorEmail, comment.CreatedAt, platform, opts.RepoURL, commentPath)
		msg := buildCommentMessage(comment.Content, originalRef, ref, origin)
		commentEntries = append(commentEntries, commentEntry{item: comment, message: msg})
		commentMessages = append(commentMessages, msg)
		commentOriginals = append(commentOriginals, struct{ repoURL, hash, branch string }{repoURL, postHash, branch})
	}
	// Phase 5: batch create comment commits
	if len(commentMessages) > 0 {
		commentHashes, err := git.FastImportCommits(opts.WorkDir, branch, commentMessages)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "comment", Message: "fast-import: " + err.Error()})
			return stats
		}
		for i, entry := range commentEntries {
			hash := commentHashes[i]
			orig := commentOriginals[i]
			if err := cache.InsertCommits([]cache.Commit{{
				Hash: hash, RepoURL: repoURL, Branch: branch,
				AuthorName: authorName, AuthorEmail: authorEmail,
				Message: entry.message, Timestamp: importTime(entry.item.CreatedAt, now),
			}}); err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "comment", Message: "cache: " + err.Error()})
				continue
			}
			if err := social.InsertSocialItem(social.SocialItem{
				RepoURL: repoURL, Hash: hash, Branch: branch,
				Type:            "comment",
				OriginalRepoURL: sql.NullString{String: orig.repoURL, Valid: orig.repoURL != ""},
				OriginalHash:    sql.NullString{String: orig.hash, Valid: orig.hash != ""},
				OriginalBranch:  sql.NullString{String: orig.branch, Valid: orig.branch != ""},
			}); err != nil {
				stats.Errors = append(stats.Errors, ImportError{Type: "comment", Message: "cache: " + err.Error()})
				continue
			}
			key := MappingKey(platform, "comment", entry.item.ExternalID)
			mapping.Record(key, hash, "gitmsg/social", "comment")
			stats.Comments++
		}
	}
	return stats
}

// sortedCSV normalizes a comma-separated string by sorting its elements.
func sortedCSV(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ",")
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// nullStr extracts the string value from a sql.NullString, returning "" if invalid.
func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// updatePM compares platform fields with GitSocial state for already-imported items and creates edit commits for changes.
func updatePM(opts Options, plan *PMPlan, mapping *MappingFile) Stats {
	stats := Stats{}
	platform := mapping.Source
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	branch := gitmsg.GetExtBranch(opts.WorkDir, "pm")
	var authorName, authorEmail string
	if !opts.DryRun {
		var err error
		authorName, authorEmail, err = git.GetAuthorIdentity(opts.WorkDir)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "pm-update", Message: "get author: " + err.Error()})
			return stats
		}
	}
	now := time.Now()
	type updateEntry struct {
		message    string
		timestamp  time.Time
		itemType   string
		mappingKey string
		updatedAt  time.Time
	}
	var updates []updateEntry
	for _, m := range plan.Milestones {
		key := MappingKey(platform, "milestone", m.ExternalID)
		if !mapping.IsMapped(key) {
			continue
		}
		if skipByUpdatedAt(mapping, key, m.UpdatedAt) {
			continue
		}
		canonicalHash := mapping.GetHash(key)
		item, err := pm.GetPMItem(repoURL, canonicalHash, branch)
		if err != nil {
			continue
		}
		curTitle, curBody := protocol.SplitSubjectBody(item.Content)
		dueStr := ""
		if m.DueDate != nil {
			dueStr = m.DueDate.Format("2006-01-02")
		}
		if curTitle == m.Title && curBody == m.Body && item.State == m.State && nullStr(item.Due) == dueStr {
			if !m.UpdatedAt.IsZero() {
				mapping.SetUpdatedAt(key, m.UpdatedAt)
			}
			continue
		}
		if opts.DryRun {
			stats.UpdatedMilestones++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  pm  update milestone: %s\n", m.Title)
		}
		editsRef := protocol.CreateRef(protocol.RefTypeCommit, canonicalHash, "", branch)
		origin := buildStateChangeOrigin("", "", time.Time{}, platform, opts.RepoURL, platformPath(platform, "milestone", fmt.Sprintf("%d", m.Number)))
		msg := buildMilestoneMessage(m.Title, m.Body, m.State, m.DueDate, editsRef, origin)
		updates = append(updates, updateEntry{message: msg, timestamp: now, itemType: "milestone", mappingKey: key, updatedAt: m.UpdatedAt})
	}
	for _, issue := range plan.Issues {
		key := MappingKey(platform, "issue", issue.ExternalID)
		if !mapping.IsMapped(key) {
			continue
		}
		if skipByUpdatedAt(mapping, key, issue.UpdatedAt) {
			continue
		}
		canonicalHash := mapping.GetHash(key)
		item, err := pm.GetPMItem(repoURL, canonicalHash, branch)
		if err != nil {
			continue
		}
		labels := MapLabels(issue.Labels, opts.LabelMode)
		labelStr := strings.Join(labels, ",")
		milestoneRef := ""
		if issue.MilestoneID != "" {
			msKey := MappingKey(platform, "milestone", issue.MilestoneID)
			if hash := mapping.GetHash(msKey); hash != "" && len(hash) >= 12 {
				milestoneRef = "#commit:" + hash[:12]
			}
		}
		curTitle, curBody := protocol.SplitSubjectBody(item.Content)
		curMsHash := nullStr(item.MilestoneHash)
		platMsHash := ""
		if milestoneRef != "" {
			_, platMsHash, _ = parseRefComponents(milestoneRef, repoURL, branch)
		}
		if curTitle == issue.Title &&
			curBody == issue.Body &&
			item.State == issue.State &&
			sortedCSV(nullStr(item.Labels)) == sortedCSV(labelStr) &&
			sortedCSV(nullStr(item.Assignees)) == sortedCSV(strings.Join(issue.Assignees, ",")) &&
			curMsHash == platMsHash {
			if !issue.UpdatedAt.IsZero() {
				mapping.SetUpdatedAt(key, issue.UpdatedAt)
			}
			continue
		}
		if opts.DryRun {
			stats.UpdatedIssues++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  pm  update issue #%d: %s\n", issue.Number, issue.Title)
		}
		editsRef := protocol.CreateRef(protocol.RefTypeCommit, canonicalHash, "", branch)
		var origin *protocol.Origin
		if issue.State == "closed" && item.State != "closed" {
			origin = buildStateChangeOrigin(issue.ClosedByName, issue.ClosedByEmail, issue.ClosedAt, platform, opts.RepoURL, platformPath(platform, "issue", issue.ExternalID))
		} else {
			origin = buildStateChangeOrigin("", "", time.Time{}, platform, opts.RepoURL, platformPath(platform, "issue", issue.ExternalID))
		}
		msg := buildIssueMessage(issue.Title, issue.Body, issue.State, issue.Assignees, nil, milestoneRef, labelStr, editsRef, origin)
		ts := now
		if issue.State == "closed" && !issue.ClosedAt.IsZero() {
			ts = issue.ClosedAt
		}
		updates = append(updates, updateEntry{message: msg, timestamp: ts, itemType: "issue", mappingKey: key, updatedAt: issue.UpdatedAt})
	}
	if opts.DryRun || len(updates) == 0 {
		return stats
	}
	var messages []string
	for _, u := range updates {
		messages = append(messages, u.message)
	}
	hashes, err := git.FastImportCommits(opts.WorkDir, branch, messages)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "pm-update", Message: "fast-import: " + err.Error()})
		return stats
	}
	for i, u := range updates {
		hash := hashes[i]
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: branch,
			AuthorName: authorName, AuthorEmail: authorEmail,
			Message: u.message, Timestamp: u.timestamp,
		}}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "pm-update", Message: "cache: " + err.Error()})
			continue
		}
		msg := protocol.ParseMessage(u.message)
		state := "open"
		if msg != nil {
			if s := msg.Header.Fields["state"]; s != "" {
				state = s
			}
		}
		pmItem := pm.PMItem{
			RepoURL: repoURL, Hash: hash, Branch: branch,
			Type: u.itemType, State: state,
		}
		if msg != nil {
			if due := msg.Header.Fields["due"]; due != "" {
				pmItem.Due = cache.ToNullString(due)
			}
			if assignees := msg.Header.Fields["assignees"]; assignees != "" {
				pmItem.Assignees = cache.ToNullString(assignees)
			}
			if labels := msg.Header.Fields["labels"]; labels != "" {
				pmItem.Labels = cache.ToNullString(labels)
			}
			if ms := msg.Header.Fields["milestone"]; ms != "" {
				msRepoURL, msHash, msBranch := parseRefComponents(ms, repoURL, branch)
				pmItem.MilestoneRepoURL = cache.ToNullString(msRepoURL)
				pmItem.MilestoneHash = cache.ToNullString(msHash)
				pmItem.MilestoneBranch = cache.ToNullString(msBranch)
			}
		}
		if err := pm.InsertPMItem(pmItem); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "pm-update", Message: "cache: " + err.Error()})
			continue
		}
		if !u.updatedAt.IsZero() {
			mapping.SetUpdatedAt(u.mappingKey, u.updatedAt)
		}
		if u.itemType == "issue" {
			stats.UpdatedIssues++
		} else {
			stats.UpdatedMilestones++
		}
	}
	return stats
}

// updateReview compares platform fields with GitSocial state for already-imported PRs and creates edit commits for changes.
func updateReview(opts Options, plan *ReviewPlan, mapping *MappingFile) Stats {
	stats := Stats{}
	platform := mapping.Source
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	branch := gitmsg.GetExtBranch(opts.WorkDir, "review")
	var authorName, authorEmail string
	if !opts.DryRun {
		var err error
		authorName, authorEmail, err = git.GetAuthorIdentity(opts.WorkDir)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "review-update", Message: "get author: " + err.Error()})
			return stats
		}
	}
	now := time.Now()
	type updateEntry struct {
		pr         ImportPR
		message    string
		base       string
		baseTip    string
		head       string
		headTip    string
		labels     []string
		mappingKey string
	}
	var updates []updateEntry
	for _, pr := range plan.PRs {
		key := MappingKey(platform, "pr", pr.ExternalID)
		if !mapping.IsMapped(key) {
			continue
		}
		if skipByUpdatedAt(mapping, key, pr.UpdatedAt) {
			continue
		}
		canonicalHash := mapping.GetHash(key)
		item, err := review.GetReviewItem(repoURL, canonicalHash, branch)
		if err != nil {
			continue
		}
		currentState := nullStr(item.State)
		labels := MapLabels(pr.Labels, opts.LabelMode)
		curTitle, curBody := protocol.SplitSubjectBody(item.Content)
		if currentState == pr.State &&
			curTitle == pr.Title &&
			curBody == pr.Body &&
			sortedCSV(nullStr(item.Labels)) == sortedCSV(strings.Join(labels, ",")) &&
			sortedCSV(nullStr(item.Reviewers)) == sortedCSV(strings.Join(pr.Reviewers, ",")) {
			if !pr.UpdatedAt.IsZero() {
				mapping.SetUpdatedAt(key, pr.UpdatedAt)
			}
			continue
		}
		if opts.DryRun {
			stats.UpdatedPRs++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  review  update PR #%d: %s\n", pr.Number, pr.Title)
		}
		editsRef := protocol.CreateRef(protocol.RefTypeCommit, canonicalHash, "", branch)
		head := "#branch:" + pr.HeadBranch
		if pr.HeadRepo != "" {
			head = pr.HeadRepo + "#branch:" + pr.HeadBranch
		}
		base := "#branch:" + pr.BaseBranch
		baseTip := nullStr(item.BaseTip)
		headTip := nullStr(item.HeadTip)
		var stateOrigin *protocol.Origin
		mBase, mHead := "", ""
		switch pr.State {
		case "merged":
			stateOrigin = buildStateChangeOrigin(pr.MergedByName, pr.MergedByEmail, pr.MergedAt, platform, opts.RepoURL, platformPath(platform, "pr", pr.ExternalID))
			if pr.MergeCommit != "" {
				if b, err := git.ReadRef(opts.WorkDir, pr.MergeCommit+"^1"); err == nil {
					mBase = b
				}
				if h, err := git.ReadRef(opts.WorkDir, pr.MergeCommit); err == nil {
					mHead = h
				}
				if mHead == "" {
					sha := pr.MergeCommit
					if len(sha) > 12 {
						sha = sha[:12]
					}
					mHead = sha
				}
				if mBase == "" && baseTip != "" {
					mBase = baseTip
				}
			}
		case "closed":
			stateOrigin = buildStateChangeOrigin(pr.ClosedByName, pr.ClosedByEmail, pr.ClosedAt, platform, opts.RepoURL, platformPath(platform, "pr", pr.ExternalID))
		default:
			stateOrigin = buildStateChangeOrigin("", "", time.Time{}, platform, opts.RepoURL, platformPath(platform, "pr", pr.ExternalID))
		}
		msg := buildPRMessage(pr.Title, pr.Body, pr.State, pr.IsDraft, base, baseTip, head, headTip, pr.Reviewers, labels, mBase, mHead, editsRef, stateOrigin)
		updates = append(updates, updateEntry{pr: pr, message: msg, base: base, baseTip: baseTip, head: head, headTip: headTip, labels: labels, mappingKey: key})
	}
	if opts.DryRun || len(updates) == 0 {
		return stats
	}
	var messages []string
	for _, u := range updates {
		messages = append(messages, u.message)
	}
	hashes, err := git.FastImportCommits(opts.WorkDir, branch, messages)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "review-update", Message: "fast-import: " + err.Error()})
		return stats
	}
	for i, u := range updates {
		hash := hashes[i]
		stateTime := now
		if u.pr.State == "merged" && !u.pr.MergedAt.IsZero() {
			stateTime = u.pr.MergedAt
		} else if u.pr.State == "closed" && !u.pr.ClosedAt.IsZero() {
			stateTime = u.pr.ClosedAt
		}
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: branch,
			AuthorName: authorName, AuthorEmail: authorEmail,
			Message: u.message, Timestamp: stateTime,
		}}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "review-update", Message: "cache: " + err.Error()})
			continue
		}
		if err := review.InsertReviewItem(review.ReviewItem{
			RepoURL:   repoURL,
			Hash:      hash,
			Branch:    branch,
			Type:      "pull-request",
			State:     cache.ToNullString(u.pr.State),
			Base:      cache.ToNullString(u.base),
			BaseTip:   cache.ToNullString(u.baseTip),
			Head:      cache.ToNullString(u.head),
			HeadTip:   cache.ToNullString(u.headTip),
			Reviewers: cache.ToNullString(strings.Join(u.pr.Reviewers, ",")),
			Labels:    cache.ToNullString(strings.Join(u.labels, ",")),
		}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "review-update", Message: "cache: " + err.Error()})
			continue
		}
		if !u.pr.UpdatedAt.IsZero() {
			mapping.SetUpdatedAt(u.mappingKey, u.pr.UpdatedAt)
		}
		stats.UpdatedPRs++
	}
	return stats
}

// updateRelease compares platform fields with GitSocial state for already-imported releases and creates edit commits for changes.
func updateRelease(opts Options, plan *ReleasePlan, mapping *MappingFile) Stats {
	stats := Stats{}
	platform := mapping.Source
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	branch := gitmsg.GetExtBranch(opts.WorkDir, "release")
	var authorName, authorEmail string
	if !opts.DryRun {
		var err error
		authorName, authorEmail, err = git.GetAuthorIdentity(opts.WorkDir)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "release-update", Message: "get author: " + err.Error()})
			return stats
		}
	}
	now := time.Now()
	type updateEntry struct {
		rel        ImportRelease
		message    string
		mappingKey string
	}
	var updates []updateEntry
	for _, r := range plan.Releases {
		key := MappingKey(platform, "release", r.ExternalID)
		if !mapping.IsMapped(key) {
			continue
		}
		if skipByUpdatedAt(mapping, key, r.UpdatedAt) {
			continue
		}
		canonicalHash := mapping.GetHash(key)
		item, err := releasepkg.GetReleaseItem(repoURL, canonicalHash, branch)
		if err != nil {
			continue
		}
		curName, curBody := protocol.SplitSubjectBody(item.Content)
		platArtifacts := strings.Join(r.Artifacts, ",")
		if curName == r.Name &&
			curBody == r.Body &&
			item.Prerelease == r.Prerelease &&
			sortedCSV(nullStr(item.Artifacts)) == sortedCSV(platArtifacts) &&
			nullStr(item.ArtifactURL) == r.ArtifactURL {
			if !r.UpdatedAt.IsZero() {
				mapping.SetUpdatedAt(key, r.UpdatedAt)
			}
			continue
		}
		if opts.DryRun {
			stats.UpdatedReleases++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  release  update: %s\n", r.Name)
		}
		editsRef := protocol.CreateRef(protocol.RefTypeCommit, canonicalHash, "", branch)
		origin := buildStateChangeOrigin("", "", time.Time{}, platform, opts.RepoURL, platformPath(platform, "release", r.Tag))
		msg := buildReleaseMessage(r, editsRef, origin)
		updates = append(updates, updateEntry{rel: r, message: msg, mappingKey: key})
	}
	if opts.DryRun || len(updates) == 0 {
		return stats
	}
	var messages []string
	for _, u := range updates {
		messages = append(messages, u.message)
	}
	hashes, err := git.FastImportCommits(opts.WorkDir, branch, messages)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "release-update", Message: "fast-import: " + err.Error()})
		return stats
	}
	for i, u := range updates {
		hash := hashes[i]
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: branch,
			AuthorName: authorName, AuthorEmail: authorEmail,
			Message: u.message, Timestamp: now,
		}}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "release-update", Message: "cache: " + err.Error()})
			continue
		}
		if err := releasepkg.InsertReleaseItem(releasepkg.ReleaseItem{
			RepoURL:     repoURL,
			Hash:        hash,
			Branch:      branch,
			Tag:         cache.ToNullString(u.rel.Tag),
			Version:     cache.ToNullString(u.rel.Version),
			Prerelease:  u.rel.Prerelease,
			Artifacts:   cache.ToNullString(strings.Join(u.rel.Artifacts, ",")),
			ArtifactURL: cache.ToNullString(u.rel.ArtifactURL),
			Checksums:   cache.ToNullString(u.rel.Checksums),
			SignedBy:    cache.ToNullString(u.rel.SignedBy),
			SBOM:        cache.ToNullString(u.rel.SBOM),
		}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "release-update", Message: "cache: " + err.Error()})
			continue
		}
		if !u.rel.UpdatedAt.IsZero() {
			mapping.SetUpdatedAt(u.mappingKey, u.rel.UpdatedAt)
		}
		stats.UpdatedReleases++
	}
	return stats
}

// updateSocial compares platform content with GitSocial state for already-imported posts and creates edit commits for changes.
func updateSocial(opts Options, plan *SocialPlan, mapping *MappingFile) Stats {
	stats := Stats{}
	platform := mapping.Source
	repoURL := gitmsg.ResolveRepoURL(opts.WorkDir)
	branch := gitmsg.GetExtBranch(opts.WorkDir, "social")
	var authorName, authorEmail string
	if !opts.DryRun {
		var err error
		authorName, authorEmail, err = git.GetAuthorIdentity(opts.WorkDir)
		if err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "social-update", Message: "get author: " + err.Error()})
			return stats
		}
	}
	now := time.Now()
	type updateEntry struct {
		post       ImportPost
		message    string
		mappingKey string
	}
	var updates []updateEntry
	for _, post := range plan.Posts {
		key := MappingKey(platform, "post", post.ExternalID)
		if !mapping.IsMapped(key) {
			continue
		}
		if skipByUpdatedAt(mapping, key, post.UpdatedAt) {
			continue
		}
		canonicalHash := mapping.GetHash(key)
		item, err := social.GetSocialItem(repoURL, canonicalHash, branch, repoURL)
		if err != nil {
			continue
		}
		if strings.TrimSpace(item.Content) == strings.TrimSpace(post.Content) {
			if !post.UpdatedAt.IsZero() {
				mapping.SetUpdatedAt(key, post.UpdatedAt)
			}
			continue
		}
		if opts.DryRun {
			stats.UpdatedPosts++
			continue
		}
		if opts.Verbose {
			fmt.Printf("  social  update post: %s\n", truncate(post.Content, 60))
		}
		editsRef := protocol.CreateRef(protocol.RefTypeCommit, canonicalHash, "", branch)
		origin := buildStateChangeOrigin("", "", time.Time{}, platform, opts.RepoURL, platformPath(platform, "post", post.ExternalID))
		msg := buildPostMessage(post.Content, editsRef, origin)
		updates = append(updates, updateEntry{post: post, message: msg, mappingKey: key})
	}
	if opts.DryRun || len(updates) == 0 {
		return stats
	}
	var messages []string
	for _, u := range updates {
		messages = append(messages, u.message)
	}
	hashes, err := git.FastImportCommits(opts.WorkDir, branch, messages)
	if err != nil {
		stats.Errors = append(stats.Errors, ImportError{Type: "social-update", Message: "fast-import: " + err.Error()})
		return stats
	}
	for i, u := range updates {
		hash := hashes[i]
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: branch,
			AuthorName: authorName, AuthorEmail: authorEmail,
			Message: u.message, Timestamp: now,
		}}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "social-update", Message: "cache: " + err.Error()})
			continue
		}
		if err := social.InsertSocialItem(social.SocialItem{
			RepoURL: repoURL, Hash: hash, Branch: branch,
			Type: "post",
		}); err != nil {
			stats.Errors = append(stats.Errors, ImportError{Type: "social-update", Message: "cache: " + err.Error()})
			continue
		}
		if !u.post.UpdatedAt.IsZero() {
			mapping.SetUpdatedAt(u.mappingKey, u.post.UpdatedAt)
		}
		stats.UpdatedPosts++
	}
	return stats
}

// skipByUpdatedAt returns true if the item's platform UpdatedAt matches the stored mapping timestamp,
// meaning the item hasn't changed since we last synced.
func skipByUpdatedAt(mapping *MappingFile, key string, platformUpdatedAt time.Time) bool {
	if platformUpdatedAt.IsZero() {
		return false
	}
	stored := mapping.GetUpdatedAt(key)
	if stored == "" {
		return false
	}
	return stored == platformUpdatedAt.Format(time.RFC3339)
}

// parseRefComponents extracts repo_url, hash, branch from a commit ref string.
func parseRefComponents(ref, defaultRepoURL, defaultBranch string) (repoURL, hash, branch string) {
	if ref == "" {
		return "", "", ""
	}
	parsed := protocol.ParseRef(ref)
	if parsed.Value == "" {
		return "", "", ""
	}
	repoURL = parsed.Repository
	if repoURL == "" {
		repoURL = defaultRepoURL
	}
	hash = parsed.Value
	branch = parsed.Branch
	if branch == "" {
		branch = defaultBranch
	}
	return repoURL, hash, branch
}

// platformPath returns the platform-specific URL path for an imported item.
func platformPath(platform, itemType, id string) string {
	if platform == "gitlab" {
		switch itemType {
		case "milestone":
			return "-/milestones/" + id
		case "issue":
			return "-/issues/" + id
		case "release":
			return "-/releases/" + id
		case "pr":
			return "-/merge_requests/" + id
		}
	}
	switch itemType {
	case "milestone":
		return "milestone/" + id
	case "issue":
		return "issues/" + id
	case "release":
		return "releases/tag/" + id
	case "pr":
		return "pull/" + id
	case "post":
		return "discussions/" + id
	case "comment":
		return "discussions/" + id
	}
	return id
}

// buildOrigin constructs provenance metadata for imported content.
func buildOrigin(authorName, authorEmail string, createdAt time.Time, platform, repoURL, path string) *protocol.Origin {
	origin := &protocol.Origin{
		AuthorName:  authorName,
		AuthorEmail: authorEmail,
		Platform:    platform,
	}
	if !createdAt.IsZero() {
		origin.Time = createdAt.Format(time.RFC3339)
	}
	if path != "" {
		origin.URL = protocol.NormalizeURL(repoURL) + "/" + path
	}
	if origin.AuthorName == "" && origin.AuthorEmail == "" && origin.Time == "" && origin.URL == "" {
		return nil
	}
	return origin
}

// buildStateChangeOrigin constructs provenance metadata for state-change commits (merge, close).
func buildStateChangeOrigin(authorName, authorEmail string, timestamp time.Time, platform, repoURL, path string) *protocol.Origin {
	origin := &protocol.Origin{
		AuthorName:  authorName,
		AuthorEmail: authorEmail,
		Platform:    platform,
	}
	if !timestamp.IsZero() {
		origin.Time = timestamp.Format(time.RFC3339)
	}
	if path != "" {
		origin.URL = protocol.NormalizeURL(repoURL) + "/" + path
	}
	if origin.AuthorName == "" && origin.AuthorEmail == "" && origin.Time == "" && origin.URL == "" {
		return nil
	}
	return origin
}

// resolveIssueLinks converts external issue IDs to commit refs via the mapping file.
func resolveIssueLinks(ids []string, platform string, mapping *MappingFile) []string {
	var refs []string
	for _, id := range ids {
		key := MappingKey(platform, "issue", id)
		if hash := mapping.GetHash(key); hash != "" && len(hash) >= 12 {
			refs = append(refs, "#commit:"+hash[:12])
		}
	}
	return refs
}

func toPMLabels(labels []string) []pm.Label {
	out := make([]pm.Label, 0, len(labels))
	for _, l := range labels {
		parts := strings.SplitN(l, "/", 2)
		if len(parts) == 2 {
			out = append(out, pm.Label{Scope: parts[0], Value: parts[1]})
		} else {
			out = append(out, pm.Label{Value: l})
		}
	}
	return out
}

// importTime returns createdAt if non-zero, otherwise fallback.
func importTime(createdAt, fallback time.Time) time.Time {
	if !createdAt.IsZero() {
		return createdAt
	}
	return fallback
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
