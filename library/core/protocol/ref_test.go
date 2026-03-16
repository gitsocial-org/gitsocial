// ref_test.go - Tests for ref parsing, formatting, and normalization
package protocol

import (
	"testing"
)

func TestCreateRef(t *testing.T) {
	tests := []struct {
		name       string
		refType    RefType
		value      string
		repository string
		branch     string
		want       string
	}{
		{
			name:       "commit with repo and branch",
			refType:    RefTypeCommit,
			value:      "abc123456789def",
			repository: "https://github.com/user/repo",
			branch:     "main",
			want:       "https://github.com/user/repo#commit:abc123456789@main",
		},
		{
			name:    "commit without repo (workspace-relative)",
			refType: RefTypeCommit,
			value:   "abc123456789",
			branch:  "main",
			want:    "#commit:abc123456789@main",
		},
		{
			name:       "commit with feature branch",
			refType:    RefTypeCommit,
			value:      "abc123456789",
			repository: "https://github.com/user/repo",
			branch:     "feature/auth",
			want:       "https://github.com/user/repo#commit:abc123456789@feature/auth",
		},
		{
			name:       "commit hash truncated to 12",
			refType:    RefTypeCommit,
			value:      "abc123456789extra",
			repository: "https://github.com/user/repo",
			branch:     "main",
			want:       "https://github.com/user/repo#commit:abc123456789@main",
		},
		{
			name:       "commit hash lowercased",
			refType:    RefTypeCommit,
			value:      "ABC123DEF456",
			repository: "https://github.com/user/repo",
			branch:     "main",
			want:       "https://github.com/user/repo#commit:abc123def456@main",
		},
		{
			name:       "commit without branch (no @suffix)",
			refType:    RefTypeCommit,
			value:      "abc123456789",
			repository: "https://github.com/user/repo",
			want:       "https://github.com/user/repo#commit:abc123456789",
		},
		{
			name:       "branch ref ignores branch param",
			refType:    RefTypeBranch,
			value:      "main",
			repository: "https://github.com/user/repo",
			branch:     "ignored",
			want:       "https://github.com/user/repo#branch:main",
		},
		{
			name:       "tag ref",
			refType:    RefTypeTag,
			value:      "v1.0.0",
			repository: "https://github.com/user/repo",
			want:       "https://github.com/user/repo#tag:v1.0.0",
		},
		{
			name:       "file ref",
			refType:    RefTypeFile,
			value:      "README.md",
			repository: "https://github.com/user/repo",
			want:       "https://github.com/user/repo#file:README.md",
		},
		{
			name:       "list ref",
			refType:    RefTypeList,
			value:      "reading",
			repository: "https://github.com/user/repo",
			want:       "https://github.com/user/repo#list:reading",
		},
		{
			name:    "branch ref without repo",
			refType: RefTypeBranch,
			value:   "develop",
			want:    "#branch:develop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateRef(tt.refType, tt.value, tt.repository, tt.branch)
			if got != tt.want {
				t.Errorf("CreateRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want ParsedRef
	}{
		{
			name: "commit with repo and branch",
			ref:  "https://github.com/user/repo#commit:abc123456789@main",
			want: ParsedRef{Type: RefTypeCommit, Repository: "https://github.com/user/repo", Value: "abc123456789", Branch: "main"},
		},
		{
			name: "commit without repo",
			ref:  "#commit:abc123456789@main",
			want: ParsedRef{Type: RefTypeCommit, Value: "abc123456789", Branch: "main"},
		},
		{
			name: "commit with feature branch",
			ref:  "https://github.com/user/repo#commit:abc123456789@feature/auth",
			want: ParsedRef{Type: RefTypeCommit, Repository: "https://github.com/user/repo", Value: "abc123456789", Branch: "feature/auth"},
		},
		{
			name: "commit without branch",
			ref:  "https://github.com/user/repo#commit:abc123456789",
			want: ParsedRef{Type: RefTypeCommit, Repository: "https://github.com/user/repo", Value: "abc123456789"},
		},
		{
			name: "commit hash truncated to 12",
			ref:  "https://github.com/user/repo#commit:abc123456789def0@main",
			want: ParsedRef{Type: RefTypeCommit, Repository: "https://github.com/user/repo", Value: "abc123456789", Branch: "main"},
		},
		{
			name: "branch ref",
			ref:  "https://github.com/user/repo#branch:main",
			want: ParsedRef{Type: RefTypeBranch, Repository: "https://github.com/user/repo", Value: "main"},
		},
		{
			name: "branch ref without repo",
			ref:  "#branch:develop",
			want: ParsedRef{Type: RefTypeBranch, Value: "develop"},
		},
		{
			name: "tag ref",
			ref:  "https://github.com/user/repo#tag:v1.0.0",
			want: ParsedRef{Type: RefTypeTag, Repository: "https://github.com/user/repo", Value: "v1.0.0"},
		},
		{
			name: "list ref",
			ref:  "https://github.com/user/repo#list:reading",
			want: ParsedRef{Type: RefTypeList, Repository: "https://github.com/user/repo", Value: "reading"},
		},
		{
			name: "file ref with branch",
			ref:  "https://github.com/user/repo#file:src/main.go@main",
			want: ParsedRef{Type: RefTypeFile, Repository: "https://github.com/user/repo", Value: "src/main.go", FilePath: "src/main.go", Branch: "main"},
		},
		{
			name: "file ref with line number",
			ref:  "https://github.com/user/repo#file:src/main.go@main:L42",
			want: ParsedRef{Type: RefTypeFile, Repository: "https://github.com/user/repo", Value: "src/main.go", FilePath: "src/main.go", Branch: "main", LineStart: 42, LineEnd: 42},
		},
		{
			name: "file ref with line range",
			ref:  "https://github.com/user/repo#file:src/main.go@main:L10-20",
			want: ParsedRef{Type: RefTypeFile, Repository: "https://github.com/user/repo", Value: "src/main.go", FilePath: "src/main.go", Branch: "main", LineStart: 10, LineEnd: 20},
		},
		{
			name: "ssh url normalized in ref",
			ref:  "git@github.com:user/repo.git#commit:abc123456789@main",
			want: ParsedRef{Type: RefTypeCommit, Repository: "https://github.com/user/repo", Value: "abc123456789", Branch: "main"},
		},
		{
			name: "unknown ref type",
			ref:  "just-a-string",
			want: ParsedRef{Type: RefTypeUnknown, Value: "just-a-string"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRef(tt.ref)
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.Repository != tt.want.Repository {
				t.Errorf("Repository = %q, want %q", got.Repository, tt.want.Repository)
			}
			if got.Value != tt.want.Value {
				t.Errorf("Value = %q, want %q", got.Value, tt.want.Value)
			}
			if got.Branch != tt.want.Branch {
				t.Errorf("Branch = %q, want %q", got.Branch, tt.want.Branch)
			}
			if got.FilePath != tt.want.FilePath {
				t.Errorf("FilePath = %q, want %q", got.FilePath, tt.want.FilePath)
			}
			if got.LineStart != tt.want.LineStart {
				t.Errorf("LineStart = %d, want %d", got.LineStart, tt.want.LineStart)
			}
			if got.LineEnd != tt.want.LineEnd {
				t.Errorf("LineEnd = %d, want %d", got.LineEnd, tt.want.LineEnd)
			}
		})
	}
}

func TestCreateRef_ParseRef_Roundtrip(t *testing.T) {
	tests := []struct {
		name       string
		refType    RefType
		value      string
		repository string
		branch     string
	}{
		{"commit with repo", RefTypeCommit, "abc123456789", "https://github.com/user/repo", "main"},
		{"commit without repo", RefTypeCommit, "abc123456789", "", "main"},
		{"branch ref", RefTypeBranch, "develop", "https://github.com/user/repo", ""},
		{"tag ref", RefTypeTag, "v1.0.0", "https://github.com/user/repo", ""},
		{"list ref", RefTypeList, "reading", "https://github.com/user/repo", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created := CreateRef(tt.refType, tt.value, tt.repository, tt.branch)
			parsed := ParseRef(created)
			if parsed.Type != tt.refType {
				t.Errorf("roundtrip Type = %q, want %q", parsed.Type, tt.refType)
			}
			if parsed.Repository != tt.repository {
				t.Errorf("roundtrip Repository = %q, want %q", parsed.Repository, tt.repository)
			}
			if parsed.Value != tt.value {
				t.Errorf("roundtrip Value = %q, want %q", parsed.Value, tt.value)
			}
			if tt.refType == RefTypeCommit && parsed.Branch != tt.branch {
				t.Errorf("roundtrip Branch = %q, want %q", parsed.Branch, tt.branch)
			}
		})
	}
}

func TestNormalizeRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "commit ref normalized (long hash truncated)",
			input: "https://github.com/user/repo#commit:abc123def456789@main",
			want:  "https://github.com/user/repo#commit:abc123def456@main",
		},
		{
			name:  "non-commit ref unchanged",
			input: "https://github.com/user/repo#branch:main",
			want:  "https://github.com/user/repo#branch:main",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRef(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRepositoryID(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantRepo   string
		wantBranch string
	}{
		{
			name:       "url with branch ref",
			input:      "https://github.com/user/repo#branch:develop",
			wantRepo:   "https://github.com/user/repo",
			wantBranch: "develop",
		},
		{
			name:       "plain url defaults to main",
			input:      "https://github.com/user/repo",
			wantRepo:   "https://github.com/user/repo",
			wantBranch: "main",
		},
		{
			name:       "ssh url normalized",
			input:      "git@github.com:user/repo.git",
			wantRepo:   "https://github.com/user/repo",
			wantBranch: "main",
		},
		{
			name:       "url with custom branch",
			input:      "https://github.com/user/repo#branch:feature/auth",
			wantRepo:   "https://github.com/user/repo",
			wantBranch: "feature/auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRepositoryID(tt.input)
			if got.Repository != tt.wantRepo {
				t.Errorf("Repository = %q, want %q", got.Repository, tt.wantRepo)
			}
			if got.Branch != tt.wantBranch {
				t.Errorf("Branch = %q, want %q", got.Branch, tt.wantBranch)
			}
		})
	}
}

func TestExtractBranchFromRemote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"remotes/origin/main", "remotes/origin/main", "main"},
		{"remotes/origin/feature/auth", "remotes/origin/feature/auth", "feature/auth"},
		{"origin/main shorthand", "origin/main", "main"},
		{"origin/feature/auth", "origin/feature/auth", "feature/auth"},
		{"plain branch", "main", "main"},
		{"remotes with only two parts", "remotes/origin", "remotes/origin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractBranchFromRemote(tt.input)
			if got != tt.want {
				t.Errorf("ExtractBranchFromRemote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeRefWithContext(t *testing.T) {
	tests := []struct {
		name        string
		ref         string
		currentRepo string
		branch      string
		want        string
	}{
		{
			name:        "local ref gets repo context",
			ref:         "#commit:abc123456789@main",
			currentRepo: "https://github.com/user/repo",
			branch:      "main",
			want:        "https://github.com/user/repo#commit:abc123456789@main",
		},
		{
			name:        "local ref without branch uses provided branch",
			ref:         "#commit:abc123456789",
			currentRepo: "https://github.com/user/repo",
			branch:      "develop",
			want:        "https://github.com/user/repo#commit:abc123456789@develop",
		},
		{
			name:        "full ref missing branch gets branch added",
			ref:         "https://github.com/user/repo#commit:abc123456789",
			currentRepo: "https://github.com/other/repo",
			branch:      "main",
			want:        "https://github.com/user/repo#commit:abc123456789@main",
		},
		{
			name:        "already complete ref unchanged",
			ref:         "https://github.com/user/repo#commit:abc123456789@develop",
			currentRepo: "https://github.com/other/repo",
			branch:      "main",
			want:        "https://github.com/user/repo#commit:abc123456789@develop",
		},
		{
			name:        "non-commit ref unchanged",
			ref:         "https://github.com/user/repo#branch:main",
			currentRepo: "https://github.com/user/repo",
			branch:      "main",
			want:        "https://github.com/user/repo#branch:main",
		},
		{
			name: "empty ref",
			ref:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRefWithContext(tt.ref, tt.currentRepo, tt.branch)
			if got != tt.want {
				t.Errorf("NormalizeRefWithContext(%q, %q, %q) = %q, want %q", tt.ref, tt.currentRepo, tt.branch, got, tt.want)
			}
		})
	}
}

func TestLocalizeRef(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		workspaceURL string
		want         string
	}{
		{
			name:         "workspace ref gets stripped",
			ref:          "https://github.com/user/repo#commit:abc123456789@main",
			workspaceURL: "https://github.com/user/repo",
			want:         "#commit:abc123456789@main",
		},
		{
			name:         "external ref unchanged",
			ref:          "https://github.com/other/repo#commit:abc123456789@main",
			workspaceURL: "https://github.com/user/repo",
			want:         "https://github.com/other/repo#commit:abc123456789@main",
		},
		{
			name:         "already local ref",
			ref:          "#commit:abc123456789@main",
			workspaceURL: "https://github.com/user/repo",
			want:         "#commit:abc123456789@main",
		},
		{
			name: "empty ref",
			ref:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LocalizeRef(tt.ref, tt.workspaceURL)
			if got != tt.want {
				t.Errorf("LocalizeRef(%q, %q) = %q, want %q", tt.ref, tt.workspaceURL, got, tt.want)
			}
		})
	}
}

func TestEnsureBranchRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain branch name becomes ref",
			input: "main",
			want:  "#branch:main",
		},
		{
			name:  "already a branch ref",
			input: "#branch:main",
			want:  "#branch:main",
		},
		{
			name:  "commit ref unchanged",
			input: "#commit:abc123456789@main",
			want:  "#commit:abc123456789@main",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnsureBranchRef(tt.input)
			if got != tt.want {
				t.Errorf("EnsureBranchRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatShortRef(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		workspaceURL string
		want         string
	}{
		{
			name:         "workspace ref shows short form",
			ref:          "https://github.com/user/repo#commit:abc123456789@main",
			workspaceURL: "https://github.com/user/repo",
			want:         "#abc123456789@main",
		},
		{
			name:         "external ref shows full",
			ref:          "https://github.com/other/repo#commit:abc123456789@main",
			workspaceURL: "https://github.com/user/repo",
			want:         "https://github.com/other/repo#abc123456789@main",
		},
		{
			name:         "local ref (no repo)",
			ref:          "#commit:abc123456789@main",
			workspaceURL: "https://github.com/user/repo",
			want:         "#abc123456789@main",
		},
		{
			name:         "ref without branch",
			ref:          "#commit:abc123456789",
			workspaceURL: "",
			want:         "#abc123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatShortRef(tt.ref, tt.workspaceURL)
			if got != tt.want {
				t.Errorf("FormatShortRef(%q, %q) = %q, want %q", tt.ref, tt.workspaceURL, got, tt.want)
			}
		})
	}
}

func TestStripRepoFromRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips repo from commit ref",
			input: "https://github.com/user/repo#commit:abc123456789@main",
			want:  "#commit:abc123456789@main",
		},
		{
			name:  "strips repo from branch ref",
			input: "https://github.com/user/repo#branch:develop",
			want:  "#branch:develop",
		},
		{
			name:  "already local",
			input: "#commit:abc123456789@main",
			want:  "#commit:abc123456789@main",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripRepoFromRef(tt.input)
			if got != tt.want {
				t.Errorf("StripRepoFromRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripRepoFromRef_unknownInput(t *testing.T) {
	// Input without # separator - parsed as unknown type, reconstructed as #unknown:value
	got := StripRepoFromRef("just-a-string")
	if got != "#unknown:just-a-string" {
		t.Errorf("StripRepoFromRef() = %q, want %q", got, "#unknown:just-a-string")
	}
}

func TestStripRepoFromRef_tagRef(t *testing.T) {
	got := StripRepoFromRef("https://github.com/user/repo#tag:v1.0@main")
	if got != "#tag:v1.0@main" {
		t.Errorf("StripRepoFromRef() = %q, want %q", got, "#tag:v1.0@main")
	}
}
