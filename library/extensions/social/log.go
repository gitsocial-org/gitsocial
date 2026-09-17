// log.go - Activity log retrieval and formatting
package social

import (
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/text"
)

type GetLogsOptions struct {
	Limit  int
	Types  []LogEntryType
	After  *time.Time
	Before *time.Time
	Author string
}

// GetLogs retrieves activity log entries with optional filtering.
func GetLogs(workdir, scope string, opts *GetLogsOptions) Result[[]LogEntry] {
	if opts == nil {
		opts = &GetLogsOptions{Limit: 20}
	}
	if opts.Limit == 0 {
		opts.Limit = 20
	}

	branch := gitmsg.GetExtBranch(workdir, "social")

	gitOpts := &git.GetCommitsOptions{
		Branch: branch,
		Limit:  opts.Limit * 2,
	}

	switch {
	case scope == "" || scope == "repository:my":
		// default: current workspace
	case scope == "timeline":
		gitOpts.All = true
	case strings.HasPrefix(scope, "list:"):
		return failure[[]LogEntry]("INVALID_SCOPE", "list scope is not supported for logs: use gitsocial search")
	case strings.HasPrefix(scope, "repository:"):
		return failure[[]LogEntry]("INVALID_SCOPE", "external repository scope is not supported for logs: use gitsocial search")
	default:
		return failure[[]LogEntry]("INVALID_SCOPE", "unknown scope: "+scope)
	}

	commits, err := git.GetCommits(workdir, gitOpts)
	if err != nil {
		return failureWithDetails[[]LogEntry]("GIT_ERROR", "read commits", err)
	}

	refs, err := git.ListRefs(workdir, "social/")
	if err != nil {
		log.Debug("list social refs failed", "error", err)
	}
	refMap := make(map[string]string)
	for _, ref := range refs {
		hash, err := git.ReadRef(workdir, "refs/gitmsg/"+ref)
		if err != nil {
			log.Debug("read social ref failed", "ref", ref, "error", err)
			continue
		}
		if hash != "" {
			refMap[strings.TrimSpace(hash)] = ref
		}
	}

	var entries []LogEntry
	for _, commit := range commits {
		entry := commitToLogEntry(commit, refMap)
		if !matchesLogFilters(entry, opts) {
			continue
		}
		entries = append(entries, entry)
		if len(entries) >= opts.Limit {
			break
		}
	}

	return success(entries)
}

// matchesLogFilters checks if a log entry matches the filter criteria.
func matchesLogFilters(entry LogEntry, opts *GetLogsOptions) bool {
	if len(opts.Types) > 0 {
		found := false
		for _, t := range opts.Types {
			if t == entry.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if opts.Author != "" && !strings.Contains(strings.ToLower(entry.Author.Email), strings.ToLower(opts.Author)) {
		return false
	}
	if opts.After != nil && entry.Timestamp.Before(*opts.After) {
		return false
	}
	if opts.Before != nil && entry.Timestamp.After(*opts.Before) {
		return false
	}
	return true
}

// commitToLogEntry converts a git commit to a LogEntry.
func commitToLogEntry(commit git.Commit, refMap map[string]string) LogEntry {
	msg := protocol.ParseMessage(commit.Message)
	entryType := detectLogEntryType(commit, msg, refMap)
	details := formatLogDetails(commit, msg, entryType)

	// An activity entry names who wrote the item: the origin author and time over the importer's.
	var header *protocol.Header
	if msg != nil {
		header = &msg.Header
	}
	authorName, authorEmail := protocol.EffectiveAuthor(header, commit.Author, commit.Email)
	return LogEntry{
		Hash:      commit.Hash,
		Timestamp: protocol.EffectiveTime(header, commit.Timestamp),
		Author: Author{
			Name:  authorName,
			Email: authorEmail,
		},
		Type:       entryType,
		Details:    details,
		Repository: extractRepoFromRefname(commit.Refname),
		PostID:     commit.Hash,
	}
}

// detectLogEntryType determines the type of activity from a commit.
func detectLogEntryType(commit git.Commit, msg *protocol.Message, refMap map[string]string) LogEntryType {
	// The ref shapes are the ones git.ListRefs returns, with refs/gitmsg/ trimmed.
	if ref, ok := refMap[commit.Hash]; ok {
		if strings.HasPrefix(ref, "social/lists/") {
			if strings.Contains(commit.Message, "deleted") || strings.Contains(commit.Message, "remove") {
				return logTypeListDelete
			}
			return logTypeListCreate
		}
		if strings.HasPrefix(ref, "social/config") {
			return logTypeConfig
		}
		return logTypeMetadata
	}

	// An entry names the item by its own header type, not by what it references.
	switch getPostType(msg) {
	case PostTypeComment:
		return logTypeComment
	case PostTypeRepost:
		return logTypeRepost
	case PostTypeQuote:
		return logTypeQuote
	}
	return logTypePost
}

// formatLogDetails creates a summary string for a log entry.
func formatLogDetails(commit git.Commit, _ *protocol.Message, entryType LogEntryType) string {
	content := protocol.ExtractCleanContent(commit.Message)
	content = text.Truncate(content, 77)
	content = strings.ReplaceAll(content, "\n", " ")

	switch entryType {
	case logTypeComment:
		return "Re: " + content
	case logTypeRepost:
		return "Repost: " + content
	case logTypeQuote:
		return "Quote: " + content
	case logTypeListCreate:
		return "Created list"
	case logTypeListDelete:
		return "Deleted list"
	case logTypeConfig:
		return "Updated config"
	case logTypeMetadata:
		return "Metadata update"
	default:
		return content
	}
}

// extractRepoFromRefname extracts the remote name from a git refname.
func extractRepoFromRefname(refname string) string {
	if refname == "" {
		return ""
	}
	if strings.HasPrefix(refname, "refs/remotes/") {
		parts := strings.SplitN(refname, "/", 4)
		if len(parts) >= 3 {
			return parts[2]
		}
	}
	return ""
}
