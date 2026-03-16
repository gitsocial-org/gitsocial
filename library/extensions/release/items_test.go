// items_test.go - Tests for release item conversion functions
package release

import (
	"database/sql"
	"testing"
	"time"
)

func TestReleaseItemToRelease(t *testing.T) {
	item := ReleaseItem{
		RepoURL:     "https://github.com/user/repo",
		Hash:        "abc123def456",
		Branch:      "gitmsg/release",
		Tag:         sql.NullString{String: "v1.0.0", Valid: true},
		Version:     sql.NullString{String: "1.0.0", Valid: true},
		Prerelease:  false,
		Artifacts:   sql.NullString{String: "binary-linux,binary-darwin,binary-windows", Valid: true},
		ArtifactURL: sql.NullString{String: "https://releases.example.com/v1.0.0", Valid: true},
		Checksums:   sql.NullString{String: "sha256:abc123", Valid: true},
		SignedBy:    sql.NullString{String: "alice@example.com", Valid: true},
		Content:     "Release v1.0.0\n\nFirst stable release",
		AuthorName:  "Alice",
		AuthorEmail: "alice@example.com",
		Timestamp:   time.Date(2025, 10, 15, 12, 0, 0, 0, time.UTC),
		IsEdited:    true,
		Comments:    2,
	}

	rel := ReleaseItemToRelease(item)
	if rel.Subject != "Release v1.0.0" {
		t.Errorf("Subject = %q", rel.Subject)
	}
	if rel.Body != "First stable release" {
		t.Errorf("Body = %q", rel.Body)
	}
	if rel.Tag != "v1.0.0" {
		t.Errorf("Tag = %q", rel.Tag)
	}
	if rel.Version != "1.0.0" {
		t.Errorf("Version = %q", rel.Version)
	}
	if rel.Prerelease {
		t.Error("Prerelease should be false")
	}
	if len(rel.Artifacts) != 3 {
		t.Errorf("len(Artifacts) = %d, want 3", len(rel.Artifacts))
	}
	if rel.ArtifactURL != "https://releases.example.com/v1.0.0" {
		t.Errorf("ArtifactURL = %q", rel.ArtifactURL)
	}
	if rel.Checksums != "sha256:abc123" {
		t.Errorf("Checksums = %q", rel.Checksums)
	}
	if rel.SignedBy != "alice@example.com" {
		t.Errorf("SignedBy = %q", rel.SignedBy)
	}
	if !rel.IsEdited {
		t.Error("IsEdited should be true")
	}
	if rel.Comments != 2 {
		t.Errorf("Comments = %d, want 2", rel.Comments)
	}
	if rel.Author.Name != "Alice" {
		t.Errorf("Author.Name = %q", rel.Author.Name)
	}
}

func TestReleaseItemToRelease_emptyArtifacts(t *testing.T) {
	item := ReleaseItem{
		RepoURL: "https://github.com/user/repo",
		Hash:    "abc123",
		Branch:  "gitmsg/release",
		Content: "Minimal release",
	}

	rel := ReleaseItemToRelease(item)
	if len(rel.Artifacts) != 0 {
		t.Errorf("len(Artifacts) = %d, want 0", len(rel.Artifacts))
	}
	if rel.Tag != "" {
		t.Errorf("Tag = %q, want empty", rel.Tag)
	}
}

func TestReleaseItemToRelease_prerelease(t *testing.T) {
	item := ReleaseItem{
		RepoURL:    "https://github.com/user/repo",
		Hash:       "abc123",
		Branch:     "gitmsg/release",
		Prerelease: true,
		Content:    "Beta release",
	}

	rel := ReleaseItemToRelease(item)
	if !rel.Prerelease {
		t.Error("Prerelease should be true")
	}
}

func TestGetArtifactURL(t *testing.T) {
	tests := []struct {
		name     string
		rel      Release
		filename string
		want     string
	}{
		{
			"normal URL",
			Release{ArtifactURL: "https://releases.example.com/v1.0.0"},
			"binary-linux",
			"https://releases.example.com/v1.0.0/binary-linux",
		},
		{
			"URL with trailing slash",
			Release{ArtifactURL: "https://releases.example.com/v1.0.0/"},
			"binary-linux",
			"https://releases.example.com/v1.0.0/binary-linux",
		},
		{
			"empty artifact URL",
			Release{ArtifactURL: ""},
			"binary-linux",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetArtifactURL(tt.rel, tt.filename)
			if got != tt.want {
				t.Errorf("GetArtifactURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
