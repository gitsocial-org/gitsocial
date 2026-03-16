// ref.go - GitMsg ref parsing and formatting (repo#type:value@branch)
package protocol

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reRepoPrefix = regexp.MustCompile(`^([^#]+)#`)
	reCommitRef  = regexp.MustCompile(`#commit:([a-f0-9]{12,})(?:@([a-zA-Z0-9/_.-]+))?$`)
	reBranchRef  = regexp.MustCompile(`#branch:([^#\s]+)$`)
	reTagRef     = regexp.MustCompile(`#tag:([^#\s]+)$`)
	reFileRef    = regexp.MustCompile(`#file:([^@\s]+)@([a-zA-Z0-9/_.-]+)(?::(?:L(\d+)(?:-(\d+))?|([a-zA-Z0-9/_.-]+)))?$`)
	reListRef    = regexp.MustCompile(`#list:([^#\s]+)$`)
)

type RefType string

const (
	RefTypeCommit  RefType = "commit"
	RefTypeBranch  RefType = "branch"
	RefTypeTag     RefType = "tag"
	RefTypeFile    RefType = "file"
	RefTypeList    RefType = "list"
	RefTypeUnknown RefType = "unknown"
)

type ParsedRef struct {
	Type       RefType
	Repository string
	Value      string
	Branch     string
	// File ref specific fields
	FilePath  string // For file refs: the file path
	FileRef   string // For file refs: specific commit/tag (optional)
	LineStart int    // For file refs: start line number (0 if not specified)
	LineEnd   int    // For file refs: end line number (0 if not specified, or same as LineStart for single line)
}

// CreateRef formats a ref string from its components.
func CreateRef(refType RefType, value string, repository string, branch string) string {
	normalizedValue := value
	if refType == RefTypeCommit {
		normalizedValue = strings.ToLower(value)
		if len(normalizedValue) > 12 {
			normalizedValue = normalizedValue[:12]
		}
	}

	var ref string
	if repository != "" {
		ref = repository + "#" + string(refType) + ":" + normalizedValue
	} else {
		ref = "#" + string(refType) + ":" + normalizedValue
	}

	// Add branch suffix for commit refs (branch/tag refs don't need it)
	if refType == RefTypeCommit && branch != "" {
		ref = ref + "@" + branch
	}

	return ref
}

// ParseRef parses a ref string into its components.
func ParseRef(ref string) ParsedRef {
	if strings.Contains(ref, "#commit:") {
		repoMatch := reRepoPrefix.FindStringSubmatch(ref)
		var repository string
		if len(repoMatch) > 1 {
			repository = NormalizeURL(repoMatch[1])
		}
		commitMatch := reCommitRef.FindStringSubmatch(ref)
		hash := ""
		branch := ""
		if len(commitMatch) > 1 {
			hash = strings.ToLower(commitMatch[1])
			if len(hash) > 12 {
				hash = hash[:12]
			}
		}
		if len(commitMatch) > 2 {
			branch = commitMatch[2]
		}
		return ParsedRef{
			Type:       RefTypeCommit,
			Repository: repository,
			Value:      hash,
			Branch:     branch,
		}
	}

	if strings.Contains(ref, "#branch:") {
		repoMatch := reRepoPrefix.FindStringSubmatch(ref)
		var repository string
		if len(repoMatch) > 1 {
			repository = NormalizeURL(repoMatch[1])
		}
		branchMatch := reBranchRef.FindStringSubmatch(ref)
		branchValue := ""
		if len(branchMatch) > 1 {
			branchValue = branchMatch[1]
		}
		return ParsedRef{
			Type:       RefTypeBranch,
			Repository: repository,
			Value:      branchValue,
		}
	}

	if strings.Contains(ref, "#tag:") {
		repoMatch := reRepoPrefix.FindStringSubmatch(ref)
		var repository string
		if len(repoMatch) > 1 {
			repository = NormalizeURL(repoMatch[1])
		}
		tagMatch := reTagRef.FindStringSubmatch(ref)
		tagValue := ""
		if len(tagMatch) > 1 {
			tagValue = tagMatch[1]
		}
		return ParsedRef{
			Type:       RefTypeTag,
			Repository: repository,
			Value:      tagValue,
		}
	}

	if strings.Contains(ref, "#file:") {
		repoMatch := reRepoPrefix.FindStringSubmatch(ref)
		var repository string
		if len(repoMatch) > 1 {
			repository = NormalizeURL(repoMatch[1])
		}
		fileMatch := reFileRef.FindStringSubmatch(ref)
		if len(fileMatch) > 2 {
			parsed := ParsedRef{
				Type:       RefTypeFile,
				Repository: repository,
				FilePath:   fileMatch[1],
				Branch:     fileMatch[2],
				Value:      fileMatch[1],
			}
			if len(fileMatch) > 3 && fileMatch[3] != "" {
				if n, err := parseInt(fileMatch[3]); err == nil {
					parsed.LineStart = n
					parsed.LineEnd = n
				}
			}
			if len(fileMatch) > 4 && fileMatch[4] != "" {
				if m, err := parseInt(fileMatch[4]); err == nil {
					parsed.LineEnd = m
				}
			}
			if len(fileMatch) > 5 && fileMatch[5] != "" {
				parsed.FileRef = fileMatch[5]
			}
			return parsed
		}
	}

	if strings.Contains(ref, "#list:") {
		repoMatch := reRepoPrefix.FindStringSubmatch(ref)
		var repository string
		if len(repoMatch) > 1 {
			repository = NormalizeURL(repoMatch[1])
		}
		listMatch := reListRef.FindStringSubmatch(ref)
		listID := ""
		if len(listMatch) > 1 {
			listID = listMatch[1]
		}
		return ParsedRef{
			Type:       RefTypeList,
			Repository: repository,
			Value:      listID,
		}
	}

	return ParsedRef{
		Type:  RefTypeUnknown,
		Value: ref,
	}
}

// parseInt parses a string to int for line number extraction.
func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

// ResolvedRef holds the resolved components of a ref after applying defaults.
type ResolvedRef struct {
	RepoURL string
	Hash    string
	Branch  string
}

// ResolveRefWithDefaults parses a ref string and fills in missing repo/branch from defaults.
func ResolveRefWithDefaults(refStr, defaultRepoURL, defaultBranch string) ResolvedRef {
	parsed := ParseRef(refStr)
	if parsed.Value == "" {
		return ResolvedRef{}
	}
	repoURL := parsed.Repository
	if repoURL == "" {
		repoURL = defaultRepoURL
	}
	branch := parsed.Branch
	if branch == "" {
		branch = defaultBranch
	}
	return ResolvedRef{RepoURL: repoURL, Hash: parsed.Value, Branch: branch}
}

// NormalizeRef normalizes a ref string to canonical format.
func NormalizeRef(ref string) string {
	if ref == "" {
		return ref
	}
	parsed := ParseRef(ref)
	if parsed.Type == RefTypeCommit {
		return CreateRef(RefTypeCommit, parsed.Value, parsed.Repository, parsed.Branch)
	}
	return ref
}

type RepositoryID struct {
	Repository string
	Branch     string
}

// ParseRepositoryID parses a repository identifier into URL and branch.
func ParseRepositoryID(identifier string) RepositoryID {
	if strings.Contains(identifier, "#branch:") {
		parts := strings.SplitN(identifier, "#branch:", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return RepositoryID{
				Repository: NormalizeURL(parts[0]),
				Branch:     parts[1],
			}
		}
	}
	return RepositoryID{
		Repository: NormalizeURL(identifier),
		Branch:     "main",
	}
}

// ExtractBranchFromRemote extracts branch name from remote ref.
func ExtractBranchFromRemote(remoteBranch string) string {
	if strings.HasPrefix(remoteBranch, "remotes/") {
		parts := strings.Split(remoteBranch, "/")
		if len(parts) >= 3 {
			return strings.Join(parts[2:], "/")
		}
		return remoteBranch
	}

	if strings.Contains(remoteBranch, "/") {
		idx := strings.Index(remoteBranch, "/")
		if idx > 0 {
			return remoteBranch[idx+1:]
		}
	}

	return remoteBranch
}

// NormalizeRefWithContext normalizes a ref with repository context.
func NormalizeRefWithContext(ref string, currentRepository string, branch string) string {
	if ref == "" {
		return ref
	}
	parsed := ParseRef(ref)
	if parsed.Type == RefTypeCommit {
		// Use parsed branch if available, otherwise use provided branch
		effectiveBranch := parsed.Branch
		if effectiveBranch == "" {
			effectiveBranch = branch
		}
		// If local ref, add repository context
		if parsed.Repository == "" && currentRepository != "" {
			normalizedRepo := currentRepository
			if strings.HasPrefix(normalizedRepo, "http") {
				normalizedRepo = strings.TrimSuffix(normalizedRepo, ".git")
			}
			return CreateRef(RefTypeCommit, parsed.Value, normalizedRepo, effectiveBranch)
		}
		// If ref already has repo but missing branch, add branch
		if parsed.Branch == "" && branch != "" {
			return CreateRef(RefTypeCommit, parsed.Value, parsed.Repository, branch)
		}
	}
	return ref
}

// LocalizeRef strips the repository URL from a ref if it belongs to the workspace repo.
// Remote refs are returned unchanged.
func LocalizeRef(ref, workspaceRepoURL string) string {
	if ref == "" {
		return ref
	}
	parsed := ParseRef(ref)
	if parsed.Repository == "" || parsed.Repository == workspaceRepoURL {
		return StripRepoFromRef(ref)
	}
	return ref
}

// EnsureBranchRef normalizes a value to a proper #branch: ref if it isn't already a recognized ref type.
func EnsureBranchRef(value string) string {
	if value == "" {
		return value
	}
	parsed := ParseRef(value)
	if parsed.Type == RefTypeUnknown {
		return CreateRef(RefTypeBranch, value, "", "")
	}
	return value
}

// FormatShortRef formats a ref for compact display.
// Workspace items (matching workspaceURL or local) show "#hash@branch".
// External items show the full ref. Omits the type prefix for brevity.
func FormatShortRef(ref, workspaceURL string) string {
	parsed := ParseRef(ref)
	id := "#" + parsed.Value
	if parsed.Branch != "" {
		id += "@" + parsed.Branch
	}
	if parsed.Repository != "" && parsed.Repository != workspaceURL {
		id = parsed.Repository + id
	}
	return id
}

// StripRepoFromRef removes the repository prefix from a ref, returning just "#type:value@branch".
// This is used for workspace items where refs should be relative.
func StripRepoFromRef(ref string) string {
	if ref == "" {
		return ref
	}
	parsed := ParseRef(ref)
	if parsed.Type == "" {
		return ref
	}
	return CreateRef(parsed.Type, parsed.Value, "", parsed.Branch)
}
