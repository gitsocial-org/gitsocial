// mapping_test.go - Tests for mapping file logic
package importpkg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMappingKey(t *testing.T) {
	cases := []struct {
		platform, itemType, id, want string
	}{
		{"github", "issue", "42", "github:issue:42"},
		{"gitlab", "pr", "100", "gitlab:pr:100"},
		{"github", "milestone", "v1.0", "github:milestone:v1.0"},
	}
	for _, c := range cases {
		got := MappingKey(c.platform, c.itemType, c.id)
		if got != c.want {
			t.Errorf("MappingKey(%q, %q, %q) = %q, want %q", c.platform, c.itemType, c.id, got, c.want)
		}
	}
}

func TestMappingFile_IsMapped(t *testing.T) {
	m := &MappingFile{Items: map[string]MappedItem{
		"github:issue:1": {Hash: "abc123", Branch: "gitmsg/pm", Type: "issue"},
	}}
	if !m.IsMapped("github:issue:1") {
		t.Error("IsMapped should return true for existing key")
	}
	if m.IsMapped("github:issue:2") {
		t.Error("IsMapped should return false for missing key")
	}
}

func TestMappingFile_Record(t *testing.T) {
	m := &MappingFile{Items: make(map[string]MappedItem)}
	m.Record("github:issue:1", "abc123", "gitmsg/pm", "issue")
	if !m.IsMapped("github:issue:1") {
		t.Error("Record did not add item")
	}
	item := m.Items["github:issue:1"]
	if item.Hash != "abc123" || item.Branch != "gitmsg/pm" || item.Type != "issue" {
		t.Errorf("Record stored %+v", item)
	}
}

func TestMappingFile_GetHash(t *testing.T) {
	m := &MappingFile{Items: map[string]MappedItem{
		"github:issue:1": {Hash: "abc123"},
	}}
	if got := m.GetHash("github:issue:1"); got != "abc123" {
		t.Errorf("GetHash(existing) = %q, want abc123", got)
	}
	if got := m.GetHash("github:issue:99"); got != "" {
		t.Errorf("GetHash(missing) = %q, want empty", got)
	}
}

func TestURLToSlug(t *testing.T) {
	cases := []struct{ input, want string }{
		{"https://github.com/user/repo", "github.com-user-repo"},
		{"http://github.com/user/repo", "github.com-user-repo"},
		{"https://github.com/user/repo.git", "github.com-user-repo"},
		{"https://gitlab.example.com:8080/group/project", "gitlab.example.com-8080-group-project"},
	}
	for _, c := range cases {
		got := urlToSlug(c.input)
		if got != c.want {
			t.Errorf("urlToSlug(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestURLToSlug_Truncation(t *testing.T) {
	long := "https://github.com/very-long-organization-name/very-long-repository-name-that-goes-on-and-on"
	got := urlToSlug(long)
	if len(got) > 50 {
		t.Errorf("urlToSlug truncation failed: len=%d, want <=50", len(got))
	}
}

func TestReadWriteMapping(t *testing.T) {
	dir := t.TempDir()
	repoURL := "https://github.com/test/repo"
	m := ReadMapping(dir, repoURL, "")
	if m == nil || m.Items == nil {
		t.Fatal("ReadMapping returned nil or nil Items for nonexistent file")
	}
	m.Source = "github"
	m.RepoURL = repoURL
	m.Record("github:issue:1", "abc123", "gitmsg/pm", "issue")
	if err := WriteMapping(dir, repoURL, "", m); err != nil {
		t.Fatalf("WriteMapping: %v", err)
	}
	loaded := ReadMapping(dir, repoURL, "")
	if !loaded.IsMapped("github:issue:1") {
		t.Error("written mapping not found after reload")
	}
	if loaded.Source != "github" || loaded.RepoURL != repoURL {
		t.Errorf("metadata mismatch: source=%q, repo=%q", loaded.Source, loaded.RepoURL)
	}
}

func TestReadMapping_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "imports", "test.json")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte("not json"), 0644)
	m := ReadMapping(dir, "", path)
	if m == nil || m.Items == nil {
		t.Fatal("ReadMapping should return empty MappingFile for invalid JSON")
	}
}

func TestResolveMappingPath_AbsoluteMapFile(t *testing.T) {
	got := ResolveMappingPath("/cache", "https://example.com/repo", "/tmp/custom.json")
	if got != "/tmp/custom.json" {
		t.Errorf("ResolveMappingPath(abs) = %q, want /tmp/custom.json", got)
	}
}

func TestResolveMappingPath_Default(t *testing.T) {
	got := ResolveMappingPath("/cache", "https://github.com/user/repo", "")
	want := filepath.Join("/cache", "imports", "github.com-user-repo.json")
	if got != want {
		t.Errorf("ResolveMappingPath(default) = %q, want %q", got, want)
	}
}
