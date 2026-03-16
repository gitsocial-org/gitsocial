// types.go - Common types for the import pipeline
package importpkg

import (
	"fmt"
	"strings"
	"time"
)

// SourceAdapter fetches items from an external platform.
type SourceAdapter interface {
	CountItems(opts FetchOptions) (ItemCounts, error)
	FetchPM(opts FetchOptions) (*PMPlan, error)
	FetchReleases(opts FetchOptions) (*ReleasePlan, error)
	FetchReview(opts FetchOptions) (*ReviewPlan, error)
	FetchSocial(opts FetchOptions) (*SocialPlan, error)
	Platform() string
}

// PlatformMetaProvider is optionally implemented by adapters that provide platform-specific metadata.
type PlatformMetaProvider interface {
	PlatformMeta() map[string]string
}

// ItemCounts holds estimated totals for each importable type. -1 means unknown.
type ItemCounts struct {
	Issues      int
	PRs         int
	Releases    int
	Discussions int
}

type FetchOptions struct {
	RepoURL         string
	Owner           string
	Repo            string
	Limit           int
	Since           *time.Time
	SkipBots        bool
	APIBaseURL      string
	Token           string
	State           string          // "open", "closed", "merged", "all"
	Categories      []string        // discussion category slugs to import (social only)
	SkipExternalIDs map[string]bool // "type:externalID" keys to skip (already imported)
	OnFetchProgress func(fetched int)
}

// FormatItemCounts returns a human-readable summary of item counts.
func FormatItemCounts(c ItemCounts) string {
	var parts []string
	if c.Issues >= 0 {
		parts = append(parts, formatCountWithLabel(c.Issues, "issue", "issues"))
	}
	if c.PRs >= 0 {
		parts = append(parts, formatCountWithLabel(c.PRs, "PR", "PRs"))
	}
	if c.Releases >= 0 {
		parts = append(parts, formatCountWithLabel(c.Releases, "release", "releases"))
	}
	if c.Discussions >= 0 {
		parts = append(parts, formatCountWithLabel(c.Discussions, "discussion", "discussions"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func formatCountWithLabel(n int, singular, plural string) string {
	s := fmt.Sprintf("%d", n)
	if len(s) > 3 {
		var result []byte
		for i, c := range s {
			if i > 0 && (len(s)-i)%3 == 0 {
				result = append(result, ',')
			}
			result = append(result, byte(c))
		}
		s = string(result)
	}
	if n == 1 {
		return s + " " + singular
	}
	return s + " " + plural
}

type PMPlan struct {
	Milestones []ImportMilestone
	Issues     []ImportIssue
	Filtered   int // issues filtered by --since/--skip-bots
}

type ReleasePlan struct {
	Releases []ImportRelease
	Filtered int
}

type ReviewPlan struct {
	Forks    []string
	PRs      []ImportPR
	Filtered int
}

type SocialPlan struct {
	Posts    []ImportPost
	Comments []ImportComment
	Filtered int // discussions filtered by category/--since/--skip-bots
}

type ImportMilestone struct {
	ExternalID  string
	Number      int // platform number/ID for URL construction
	Title       string
	Body        string
	State       string
	DueDate     *time.Time
	AuthorName  string
	AuthorEmail string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ImportIssue struct {
	ExternalID    string
	Number        int
	Title         string
	Body          string
	State         string
	Labels        []string
	Assignees     []string
	MilestoneID   string   // external milestone ID for linking
	BlocksIDs     []string // external issue IDs this issue blocks
	BlockedByIDs  []string // external issue IDs blocking this issue
	RelatedIDs    []string // external issue IDs related to this issue
	AuthorName    string
	AuthorEmail   string
	CreatedAt     time.Time
	ClosedAt      time.Time
	ClosedByName  string
	ClosedByEmail string
	UpdatedAt     time.Time
}

type ImportRelease struct {
	ExternalID  string
	Name        string
	Body        string
	Tag         string
	Version     string
	Prerelease  bool
	Artifacts   []string
	ArtifactURL string
	Checksums   string
	SignedBy    string
	SBOM        string
	AuthorName  string
	AuthorEmail string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ImportPR struct {
	ExternalID    string
	Number        int
	Title         string
	Body          string
	State         string // "open", "closed", "merged"
	IsDraft       bool
	BaseBranch    string
	HeadBranch    string
	HeadRepo      string // full fork URL if fork PR, empty if same-repo
	HeadSHA       string // head commit SHA from platform API (for accurate fork diffs)
	DiffBaseSHA   string // merge-base SHA from platform diff_refs (fork PRs only)
	MergeCommit   string // merge commit SHA (merged PRs only)
	Labels        []string
	Reviewers     []string
	AuthorName    string
	AuthorEmail   string
	CreatedAt     time.Time
	MergedByName  string
	MergedByEmail string
	MergedAt      time.Time
	ClosedAt      time.Time
	ClosedByName  string
	ClosedByEmail string
	UpdatedAt     time.Time
}

type ImportPost struct {
	ExternalID  string
	Content     string
	AuthorName  string
	AuthorEmail string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ImportComment struct {
	ExternalID  string
	PostID      string // external post ID this references
	Content     string
	AuthorName  string
	AuthorEmail string
	CreatedAt   time.Time
}

// Stats holds the result of an import run.
type Stats struct {
	Milestones          int
	Issues              int
	Releases            int
	Forks               int
	PRs                 int
	Posts               int
	Comments            int
	Skipped             int
	FilteredIssues      int
	FilteredReleases    int
	FilteredPRs         int
	FilteredDiscussions int
	UpdatedIssues       int
	UpdatedPRs          int
	UpdatedMilestones   int
	UpdatedReleases     int
	UpdatedPosts        int
	Errors              []ImportError
}

type ImportError struct {
	ExternalID string
	Type       string
	Message    string
}

// Total returns the total number of imported items.
func (s Stats) Total() int {
	return s.Milestones + s.Issues + s.Releases + s.Forks + s.PRs + s.Posts + s.Comments
}

// ProgressPhase identifies what the import pipeline is currently doing.
type ProgressPhase string

const (
	PhaseCount  ProgressPhase = "count"
	PhaseFetch  ProgressPhase = "fetch"
	PhaseCommit ProgressPhase = "commit"
	PhaseDone   ProgressPhase = "done"
)
