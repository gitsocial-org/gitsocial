// log.go - Activity log retrieval and formatting
package social

import (
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/log"
	"github.com/gitsocial-org/gitsocial/core/protocol"
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
		return Failure[[]LogEntry]("INVALID_SCOPE", "list scope not supported for logs; use search command instead")
	case strings.HasPrefix(scope, "repository:"):
		return Failure[[]LogEntry]("INVALID_SCOPE", "external repository scope not supported for logs; use search command instead")
	default:
		return Failure[[]LogEntry]("INVALID_SCOPE", "unknown scope: "+scope)
	}

	commits, err := git.GetCommits(workdir, gitOpts)
	if err != nil {
		return FailureWithDetails[[]LogEntry]("GIT_ERROR", "Failed to get commits", err)
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

	return Success(entries)
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

	return LogEntry{
		Hash:      commit.Hash,
		Timestamp: commit.Timestamp,
		Author: Author{
			Name:  commit.Author,
			Email: commit.Email,
		},
		Type:       entryType,
		Details:    details,
		Repository: extractRepoFromRefname(commit.Refname),
		PostID:     commit.Hash,
	}
}

// detectLogEntryType determines the type of activity from a commit.
func detectLogEntryType(commit git.Commit, msg *protocol.Message, refMap map[string]string) LogEntryType {
	if ref, ok := refMap[commit.Hash]; ok {
		if strings.HasPrefix(ref, "social/list/") {
			if strings.Contains(commit.Message, "deleted") || strings.Contains(commit.Message, "remove") {
				return LogTypeListDelete
			}
			return LogTypeListCreate
		}
		if strings.HasPrefix(ref, "config") {
			return LogTypeConfig
		}
		return LogTypeMetadata
	}

	if msg != nil {
		if interactionType, ok := msg.Header.Fields["interaction"]; ok {
			switch interactionType {
			case "comment":
				return LogTypeComment
			case "repost":
				return LogTypeRepost
			case "quote":
				return LogTypeQuote
			}
		}

		if len(msg.References) > 0 {
			for _, ref := range msg.References {
				if refType, ok := ref.Fields["type"]; ok {
					switch refType {
					case "comment":
						return LogTypeComment
					case "repost":
						return LogTypeRepost
					case "quote":
						return LogTypeQuote
					}
				}
			}
		}
	}

	return LogTypePost
}

// formatLogDetails creates a summary string for a log entry.
func formatLogDetails(commit git.Commit, _ *protocol.Message, entryType LogEntryType) string {
	content := protocol.ExtractCleanContent(commit.Message)
	if len(content) > 80 {
		content = content[:77] + "..."
	}
	content = strings.ReplaceAll(content, "\n", " ")

	switch entryType {
	case LogTypeComment:
		return "Re: " + content
	case LogTypeRepost:
		return "Repost: " + content
	case LogTypeQuote:
		return "Quote: " + content
	case LogTypeListCreate:
		return "Created list"
	case LogTypeListDelete:
		return "Deleted list"
	case LogTypeConfig:
		return "Updated config"
	case LogTypeMetadata:
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
