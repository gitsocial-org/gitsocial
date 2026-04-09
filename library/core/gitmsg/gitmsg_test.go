// gitmsg_test.go - Tests for gitmsg protocol-level storage
package gitmsg

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
)

var baseRepoDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gitmsg-test-base-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	git.Init(dir, "main")
	git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	git.CreateCommit(dir, git.CommitOptions{Message: "Initial commit", AllowEmpty: true})
	baseRepoDir = dir
	os.Exit(m.Run())
}

func cloneFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	cmd := exec.Command("cp", "-a", baseRepoDir+"/.", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cloneFixture: %v: %s", err, out)
	}
	return dst
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	return cloneFixture(t)
}

func setupTestDB(t *testing.T) {
	t.Helper()
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() { cache.Reset() })
}

// --- Config tests ---

func TestWriteExtConfig_ReadExtConfig(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	config := map[string]interface{}{
		"branch": "gitmsg/social",
		"key1":   "value1",
	}
	err := WriteExtConfig(dir, "social", config)
	if err != nil {
		t.Fatalf("WriteExtConfig() error = %v", err)
	}

	result, err := ReadExtConfig(dir, "social")
	if err != nil {
		t.Fatalf("ReadExtConfig() error = %v", err)
	}
	if result == nil {
		t.Fatal("ReadExtConfig() returned nil")
	}
	if result["branch"] != "gitmsg/social" {
		t.Errorf("branch = %q, want gitmsg/social", result["branch"])
	}
	if result["key1"] != "value1" {
		t.Errorf("key1 = %q, want value1", result["key1"])
	}
}

func TestWriteExtConfig_addsVersion(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	config := map[string]interface{}{"branch": "gitmsg/social"}
	WriteExtConfig(dir, "social", config)

	result, _ := ReadExtConfig(dir, "social")
	if result["version"] == nil {
		t.Error("version should be auto-added")
	}
}

func TestWriteExtConfig_existingVersion(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	config := map[string]interface{}{
		"version": "1.0.0",
		"branch":  "gitmsg/social",
	}
	WriteExtConfig(dir, "social", config)

	result, _ := ReadExtConfig(dir, "social")
	if result["version"] != "1.0.0" {
		t.Errorf("version = %v, want 1.0.0 (should not override)", result["version"])
	}
}

func TestReadExtConfig_noRef(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	config, err := ReadExtConfig(dir, "nonexistent")
	if err != nil {
		t.Fatalf("ReadExtConfig() error = %v", err)
	}
	if config != nil {
		t.Errorf("ReadExtConfig() = %v, want nil", config)
	}
}

func TestGetExtConfigValue_string(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteExtConfig(dir, "social", map[string]interface{}{
		"branch": "gitmsg/social",
		"key":    "value",
	})

	val, ok := GetExtConfigValue(dir, "social", "key")
	if !ok {
		t.Error("ok should be true")
	}
	if val != "value" {
		t.Errorf("val = %q, want value", val)
	}
}

func TestGetExtConfigValue_float(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteExtConfig(dir, "social", map[string]interface{}{
		"count": 42.0,
	})

	val, ok := GetExtConfigValue(dir, "social", "count")
	if !ok {
		t.Error("ok should be true")
	}
	if val != "42" {
		t.Errorf("val = %q, want 42", val)
	}
}

func TestGetExtConfigValue_bool(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteExtConfig(dir, "social", map[string]interface{}{
		"enabled": true,
	})

	val, ok := GetExtConfigValue(dir, "social", "enabled")
	if !ok {
		t.Error("ok should be true")
	}
	if val != "true" {
		t.Errorf("val = %q, want true", val)
	}
}

func TestGetExtConfigValue_noConfig(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	val, ok := GetExtConfigValue(dir, "nonexistent", "key")
	if ok {
		t.Error("ok should be false")
	}
	if val != "" {
		t.Errorf("val = %q, want empty", val)
	}
}

func TestGetExtConfigValue_missingKey(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteExtConfig(dir, "social", map[string]interface{}{"branch": "gitmsg/social"})

	val, ok := GetExtConfigValue(dir, "social", "nonexistent")
	if ok {
		t.Error("ok should be false for missing key")
	}
	if val != "" {
		t.Errorf("val = %q, want empty", val)
	}
}

func TestSetExtConfigValue(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	err := SetExtConfigValue(dir, "social", "branch", "gitmsg/social")
	if err != nil {
		t.Fatalf("SetExtConfigValue() error = %v", err)
	}

	val, ok := GetExtConfigValue(dir, "social", "branch")
	if !ok {
		t.Error("ok should be true")
	}
	if val != "gitmsg/social" {
		t.Errorf("val = %q, want gitmsg/social", val)
	}
}

func TestSetExtConfigValue_existing(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	SetExtConfigValue(dir, "social", "branch", "gitmsg/social")
	SetExtConfigValue(dir, "social", "branch", "gitmsg/social2")

	val, _ := GetExtConfigValue(dir, "social", "branch")
	if val != "gitmsg/social2" {
		t.Errorf("val = %q, want gitmsg/social2", val)
	}
}

func TestDeleteExtConfigKey(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteExtConfig(dir, "social", map[string]interface{}{
		"branch": "gitmsg/social",
		"extra":  "data",
	})

	err := DeleteExtConfigKey(dir, "social", "extra")
	if err != nil {
		t.Fatalf("DeleteExtConfigKey() error = %v", err)
	}

	_, ok := GetExtConfigValue(dir, "social", "extra")
	if ok {
		t.Error("key should be deleted")
	}
}

func TestDeleteExtConfigKey_noConfig(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	err := DeleteExtConfigKey(dir, "nonexistent", "key")
	if err != nil {
		t.Errorf("DeleteExtConfigKey() should not error on missing config, got %v", err)
	}
}

func TestListExtConfig(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteExtConfig(dir, "social", map[string]interface{}{
		"branch": "gitmsg/social",
		"key1":   "val1",
	})

	result := ListExtConfig(dir, "social")
	if len(result) < 2 {
		t.Fatalf("len(result) = %d, want >= 2", len(result))
	}

	found := false
	for _, kv := range result {
		if kv.Key == "key1" && kv.Value == "val1" {
			found = true
		}
	}
	if !found {
		t.Error("ListExtConfig should include key1=val1")
	}
}

func TestListExtConfig_noConfig(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	result := ListExtConfig(dir, "nonexistent")
	if result != nil {
		t.Errorf("ListExtConfig() = %v, want nil", result)
	}
}

func TestGetExtBranch_default(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	branch := GetExtBranch(dir, "social")
	if branch != "gitmsg/social" {
		t.Errorf("GetExtBranch() = %q, want gitmsg/social", branch)
	}
}

func TestGetExtBranch_configured(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	SetExtConfigValue(dir, "social", "branch", "custom-branch")

	branch := GetExtBranch(dir, "social")
	if branch != "custom-branch" {
		t.Errorf("GetExtBranch() = %q, want custom-branch", branch)
	}
}

func TestIsExtInitialized(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	if IsExtInitialized(dir, "social") {
		t.Error("should be false before init")
	}

	SetExtConfigValue(dir, "social", "branch", "gitmsg/social")

	if !IsExtInitialized(dir, "social") {
		t.Error("should be true after setting branch")
	}
}

func TestGetExtBranches(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create gitmsg branches
	git.CreateCommitOnBranch(dir, "gitmsg/social", "social")
	git.CreateCommitOnBranch(dir, "gitmsg/pm", "pm")

	branches := GetExtBranches(dir)
	if len(branches) < 2 {
		t.Errorf("len(branches) = %d, want >= 2", len(branches))
	}
}

func TestGetExtBranches_none(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	branches := GetExtBranches(dir)
	if branches != nil {
		t.Errorf("GetExtBranches() = %v, want nil", branches)
	}
}

// --- List tests ---

func TestWriteList_ReadList(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	data := ListData{
		Version:      "0.1.0",
		ID:           "test-list",
		Name:         "Test List",
		Repositories: []string{"https://github.com/user/repo"},
	}
	err := WriteList(dir, "social", "following", data)
	if err != nil {
		t.Fatalf("WriteList() error = %v", err)
	}

	result, err := ReadList(dir, "social", "following")
	if err != nil {
		t.Fatalf("ReadList() error = %v", err)
	}
	if result == nil {
		t.Fatal("ReadList() returned nil")
	}
	if result.Name != "Test List" {
		t.Errorf("Name = %q, want Test List", result.Name)
	}
	if len(result.Repositories) != 1 {
		t.Errorf("len(Repositories) = %d, want 1", len(result.Repositories))
	}
}

func TestReadList_noRef(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	result, err := ReadList(dir, "social", "nonexistent")
	if err != nil {
		t.Fatalf("ReadList() error = %v", err)
	}
	if result != nil {
		t.Errorf("ReadList() = %v, want nil", result)
	}
}

func TestWriteList_update(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	data1 := ListData{Version: "0.1.0", ID: "list1", Name: "List 1", Repositories: []string{"repo1"}}
	WriteList(dir, "social", "following", data1)

	data2 := ListData{Version: "0.1.0", ID: "list1", Name: "List 1 Updated", Repositories: []string{"repo1", "repo2"}}
	WriteList(dir, "social", "following", data2)

	result, _ := ReadList(dir, "social", "following")
	if result.Name != "List 1 Updated" {
		t.Errorf("Name = %q, want List 1 Updated", result.Name)
	}
	if len(result.Repositories) != 2 {
		t.Errorf("len(Repositories) = %d, want 2", len(result.Repositories))
	}
}

func TestEnumerateLists(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteList(dir, "social", "following", ListData{Version: "0.1.0", ID: "1", Name: "Following"})
	WriteList(dir, "social", "muted", ListData{Version: "0.1.0", ID: "2", Name: "Muted"})

	names, err := EnumerateLists(dir, "social")
	if err != nil {
		t.Fatalf("EnumerateLists() error = %v", err)
	}
	if len(names) != 2 {
		t.Errorf("len(names) = %d, want 2", len(names))
	}
}

func TestEnumerateLists_empty(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	names, err := EnumerateLists(dir, "social")
	if err != nil {
		t.Fatalf("EnumerateLists() error = %v", err)
	}
	if len(names) != 0 {
		t.Errorf("len(names) = %d, want 0", len(names))
	}
}

func TestDeleteList(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteList(dir, "social", "following", ListData{Version: "0.1.0", ID: "1", Name: "Following"})

	err := DeleteList(dir, "social", "following")
	if err != nil {
		t.Fatalf("DeleteList() error = %v", err)
	}

	result, _ := ReadList(dir, "social", "following")
	if result != nil {
		t.Error("ReadList() should return nil after delete")
	}
}

func TestFindListAdditionTime(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	data := ListData{
		Version:      "0.1.0",
		ID:           "list1",
		Name:         "Following",
		Repositories: []string{"https://github.com/user/repo"},
	}
	WriteList(dir, "social", "following", data)

	ts, hash, found := FindListAdditionTime(dir, "social", "following", "https://github.com/user/repo")
	if !found {
		t.Error("should find the repo in list")
	}
	if ts.IsZero() {
		t.Error("timestamp should not be zero")
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
}

func TestFindListAdditionTime_notFound(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteList(dir, "social", "following", ListData{
		Version:      "0.1.0",
		ID:           "list1",
		Name:         "Following",
		Repositories: []string{"https://github.com/user/repo"},
	})

	_, _, found := FindListAdditionTime(dir, "social", "following", "https://github.com/other/repo")
	if found {
		t.Error("should not find a non-existent repo")
	}
}

func TestFindListAdditionTime_noList(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	_, _, found := FindListAdditionTime(dir, "social", "nonexistent", "https://github.com/user/repo")
	if found {
		t.Error("should not find in nonexistent list")
	}
}

// --- Refs tests ---

func TestParseRefOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single", "refs/gitmsg/social/config abc123", 1},
		{"multi", "refs/gitmsg/social/config abc123\nrefs/gitmsg/social/lists/following def456", 2},
		{"with empty lines", "refs/gitmsg/config abc\n\nrefs/gitmsg/lists def", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRefOutput(tt.input)
			if len(result) != tt.want {
				t.Errorf("len(result) = %d, want %d", len(result), tt.want)
			}
		})
	}
}

func TestParseRefOutput_values(t *testing.T) {
	t.Parallel()
	result := parseRefOutput("refs/gitmsg/config hashvalue")
	if result["refs/gitmsg/config"] != "hashvalue" {
		t.Errorf("ref value = %q, want hashvalue", result["refs/gitmsg/config"])
	}
}

func TestParseRemoteOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single", "abc123\trefs/gitmsg/social/config", 1},
		{"multi", "abc123\trefs/gitmsg/config\ndef456\trefs/gitmsg/lists", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRemoteOutput(tt.input)
			if len(result) != tt.want {
				t.Errorf("len(result) = %d, want %d", len(result), tt.want)
			}
		})
	}
}

func TestParseRemoteOutput_values(t *testing.T) {
	t.Parallel()
	result := parseRemoteOutput("abc123\trefs/gitmsg/config")
	if result["refs/gitmsg/config"] != "abc123" {
		t.Errorf("ref hash = %q, want abc123", result["refs/gitmsg/config"])
	}
}

func TestGetUnpushedCounts_noBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	counts, err := GetUnpushedCounts(dir, "")
	if err != nil {
		t.Fatalf("GetUnpushedCounts() error = %v", err)
	}
	if counts.Posts != 0 {
		t.Errorf("Posts = %d, want 0 for empty branch", counts.Posts)
	}
}

func TestGetUnpushedCounts_noRemote(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create the gitmsg branch so there are commits to count
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 1")

	counts, err := GetUnpushedCounts(dir, "gitmsg/social")
	if err != nil {
		t.Fatalf("GetUnpushedCounts() error = %v", err)
	}
	// No remote tracking, should count all local commits as unpushed
	if counts.Posts == 0 {
		t.Error("Posts should be > 0 when no remote tracking")
	}
}

// --- URL tests ---

func TestResolveRepoURL_withOrigin(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	git.ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/User/Repo.git"})

	url := ResolveRepoURL(dir)
	// NormalizeURL lowercases host, strips .git, but preserves path case
	if url != "https://github.com/User/Repo" {
		t.Errorf("ResolveRepoURL() = %q, want https://github.com/User/Repo", url)
	}
}

func TestResolveRepoURL_noOrigin(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	url := ResolveRepoURL(dir)
	expected := "local:" + dir
	if url != expected {
		t.Errorf("ResolveRepoURL() = %q, want %q", url, expected)
	}
}

// --- History tests ---

func TestFormatHistory_empty(t *testing.T) {
	t.Parallel()
	result := FormatHistory(nil)
	if result != "No versions found." {
		t.Errorf("FormatHistory(nil) = %q, want 'No versions found.'", result)
	}
}

func TestFormatHistory_single(t *testing.T) {
	t.Parallel()
	versions := []MessageVersion{
		{
			CommitHash: "aabbccdd1234",
			Branch:     "main",
			Timestamp:  time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
			Content:    "Hello world",
		},
	}

	result := FormatHistory(versions)
	if result == "" {
		t.Error("result should not be empty")
	}
	if result == "No versions found." {
		t.Error("should format single version")
	}
}

func TestFormatHistory_multiple(t *testing.T) {
	t.Parallel()
	versions := []MessageVersion{
		{
			CommitHash: "edit12345678",
			Branch:     "main",
			Timestamp:  time.Date(2025, 10, 21, 13, 0, 0, 0, time.UTC),
			Content:    "Edited content",
			EditOf:     "#commit:aabbccdd1234@main",
		},
		{
			CommitHash: "aabbccdd1234",
			Branch:     "main",
			Timestamp:  time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
			Content:    "Original content",
		},
	}

	result := FormatHistory(versions)
	if result == "" {
		t.Error("result should not be empty")
	}
}

func TestFormatHistory_retracted(t *testing.T) {
	t.Parallel()
	versions := []MessageVersion{
		{
			CommitHash:  "aabbccdd1234",
			Branch:      "main",
			Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
			Content:     "Deleted content",
			IsRetracted: true,
		},
	}

	result := FormatHistory(versions)
	if result == "" {
		t.Error("result should not be empty")
	}
}

func TestFormatHistory_longHash(t *testing.T) {
	t.Parallel()
	versions := []MessageVersion{
		{
			CommitHash: "aabbccdd1234567890",
			Branch:     "main",
			Timestamp:  time.Now(),
			Content:    "Content",
		},
	}

	result := FormatHistory(versions)
	if result == "" {
		t.Error("result should not be empty")
	}
}

func TestFormatHistory_threeVersions(t *testing.T) {
	t.Parallel()
	versions := []MessageVersion{
		{CommitHash: "cccccccccccc", Branch: "main", Timestamp: time.Now(), Content: "V3", EditOf: "ref"},
		{CommitHash: "bbbbbbbbbbbb", Branch: "main", Timestamp: time.Now().Add(-time.Hour), Content: "V2", EditOf: "ref"},
		{CommitHash: "aaaaaaaaaaaa", Branch: "main", Timestamp: time.Now().Add(-2 * time.Hour), Content: "V1"},
	}

	result := FormatHistory(versions)
	if result == "" {
		t.Error("result should not be empty")
	}
}

func TestGetHistory_noRef(t *testing.T) {
	setupTestDB(t)

	result, err := GetHistory("", "https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if result != nil {
		t.Errorf("GetHistory() = %v, want nil for empty ref", result)
	}
}

func TestGetHistory_withData(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)
	_ = dir

	// Insert a canonical commit
	cache.InsertCommits([]cache.Commit{
		{
			Hash:      "aabbccdd1234",
			RepoURL:   "https://github.com/user/repo",
			Branch:    "main",
			Message:   "Hello world\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"",
			Timestamp: time.Now().UTC(),
		},
	})

	result, err := GetHistory("https://github.com/user/repo#commit:aabbccdd1234@main", "https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(result) == 0 {
		t.Error("should find at least 1 version")
	}
}

func TestGetHistory_withWorkspaceURL(t *testing.T) {
	setupTestDB(t)

	cache.InsertCommits([]cache.Commit{
		{
			Hash:      "aabbccdd1234",
			RepoURL:   "https://github.com/user/repo",
			Branch:    "main",
			Message:   "Post content",
			Timestamp: time.Now().UTC(),
		},
	})

	// Ref without repository, should use workspaceURL
	result, err := GetHistory("#commit:aabbccdd1234@main", "https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(result) == 0 {
		t.Error("should find version using workspace URL")
	}
}

func TestGetHistory_noBranch(t *testing.T) {
	setupTestDB(t)

	cache.InsertCommits([]cache.Commit{
		{
			Hash:      "aabbccdd1234",
			RepoURL:   "https://github.com/user/repo",
			Branch:    "main",
			Message:   "Post content",
			Timestamp: time.Now().UTC(),
		},
	})

	// Ref without branch should default to "main"
	result, err := GetHistory("https://github.com/user/repo#commit:aabbccdd1234", "https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(result) == 0 {
		t.Error("should find version with default branch")
	}
}

// --- Push tests ---

func TestPush_noBranches(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	result, err := Push(dir, true)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if result.Commits != 0 {
		t.Errorf("Commits = %d, want 0", result.Commits)
	}
	if result.Refs != 0 {
		t.Errorf("Refs = %d, want 0", result.Refs)
	}
}

func TestPush_noRemote(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create a gitmsg branch but no remote
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post")

	_, err := Push(dir, true)
	if err == nil {
		t.Error("Push() should error when no remote configured")
	}
}

func TestPush_dryRun(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	git.ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(dir, []string{"push", "origin", "main"})

	// Create a gitmsg branch with commits
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 1")
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 2")

	// Push the branch to remote first so ValidatePushPreconditions passes
	git.ExecGit(dir, []string{"push", "origin", "gitmsg/social"})

	// Add more commits locally
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 3")

	result, err := Push(dir, true)
	if err != nil {
		t.Fatalf("Push() dry run error = %v", err)
	}
	if result.Commits == 0 {
		t.Error("Commits should be > 0 in dry run")
	}
}

func TestPush_dryRunWithUnpushedRefs(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	git.ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(dir, []string{"push", "origin", "main"})

	// Create gitmsg branch and push it
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 1")
	git.ExecGit(dir, []string{"push", "origin", "gitmsg/social"})

	// Fetch so we have origin/gitmsg/social
	git.ExecGit(dir, []string{"fetch", "origin"})

	// Create more local commits (unpushed)
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 2")
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 3")

	result, err := Push(dir, true)
	if err != nil {
		t.Fatalf("Push() dry run error = %v", err)
	}
	if result.Commits < 2 {
		t.Errorf("Commits = %d, want >= 2", result.Commits)
	}
}

func TestGetUnpushedCounts_withRemote(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	git.ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(dir, []string{"push", "origin", "main"})

	// Create gitmsg branch, push, then add more commits
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 1")
	git.ExecGit(dir, []string{"push", "origin", "gitmsg/social"})
	git.ExecGit(dir, []string{"fetch", "origin"})

	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 2")

	counts, err := GetUnpushedCounts(dir, "gitmsg/social")
	if err != nil {
		t.Fatalf("GetUnpushedCounts() error = %v", err)
	}
	if counts.Posts != 1 {
		t.Errorf("Posts = %d, want 1", counts.Posts)
	}
}

func TestGetUnpushedCounts_withLocalRefs(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	git.ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(dir, []string{"push", "origin", "main"})

	// Create gitmsg refs (config + list)
	head, _ := git.ReadRef(dir, "HEAD")
	fullResult, _ := git.ExecGit(dir, []string{"rev-parse", head})
	git.WriteRef(dir, "refs/gitmsg/social/config", fullResult.Stdout)
	git.WriteRef(dir, "refs/gitmsg/social/lists/following", fullResult.Stdout)

	counts, err := GetUnpushedCounts(dir, "")
	if err != nil {
		t.Fatalf("GetUnpushedCounts() error = %v", err)
	}
	if counts.Lists == 0 {
		t.Error("Lists should be > 0 for unpushed list refs")
	}
}

func TestPush_withRefs(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	git.ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(dir, []string{"push", "origin", "main"})

	// Create gitmsg refs
	head, _ := git.ReadRef(dir, "HEAD")
	fullResult, _ := git.ExecGit(dir, []string{"rev-parse", head})
	git.WriteRef(dir, "refs/gitmsg/social/config", fullResult.Stdout)

	result, err := Push(dir, true)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if result.Refs == 0 {
		t.Error("Refs should be > 0")
	}
}

func TestPush_actualPush(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	bareDir := t.TempDir()
	git.ExecGit(bareDir, []string{"init", "--bare"})

	dir := initTestRepo(t)
	git.ExecGit(dir, []string{"remote", "add", "origin", bareDir})
	git.ExecGit(dir, []string{"push", "origin", "main"})

	// Create gitmsg branch, push initial, then add more
	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 1")
	git.ExecGit(dir, []string{"push", "origin", "gitmsg/social"})
	git.ExecGit(dir, []string{"fetch", "origin"})

	git.CreateCommitOnBranch(dir, "gitmsg/social", "post 2")

	// Create unpushed gitmsg refs
	head, _ := git.ReadRef(dir, "HEAD")
	fullResult, _ := git.ExecGit(dir, []string{"rev-parse", head})
	git.WriteRef(dir, "refs/gitmsg/social/config", fullResult.Stdout)

	// Actual push (not dry run)
	result, err := Push(dir, false)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if result.Commits == 0 {
		t.Error("Commits should be > 0")
	}
	if result.Refs == 0 {
		t.Error("Refs should be > 0")
	}
}

func TestGetExtConfigValue_emptyString(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteExtConfig(dir, "social", map[string]interface{}{
		"empty": "",
	})

	val, ok := GetExtConfigValue(dir, "social", "empty")
	if ok {
		t.Error("ok should be false for empty string value")
	}
	if val != "" {
		t.Errorf("val = %q, want empty", val)
	}
}

func TestListExtConfig_withNonStringValues(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	WriteExtConfig(dir, "social", map[string]interface{}{
		"branch": "gitmsg/social",
	})

	result := ListExtConfig(dir, "social")
	if result == nil {
		t.Fatal("ListExtConfig() returned nil")
	}
	// version is auto-added as string, branch is string
	for _, kv := range result {
		if kv.Value == "" {
			t.Errorf("key %q has empty value", kv.Key)
		}
	}
}

func TestEnumerateLists_filterEmpty(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Create a list and verify enumeration works
	WriteList(dir, "social", "test", ListData{Version: "0.1.0", ID: "1", Name: "Test"})

	names, err := EnumerateLists(dir, "social")
	if err != nil {
		t.Fatalf("EnumerateLists() error = %v", err)
	}
	for _, n := range names {
		if n == "" {
			t.Error("should not have empty name")
		}
	}
}

func TestGetHistory_withEditChain(t *testing.T) {
	setupTestDB(t)

	now := time.Now().UTC()

	// Insert canonical commit
	cache.InsertCommits([]cache.Commit{
		{
			Hash:      "aabbccdd1234",
			RepoURL:   "https://github.com/user/repo",
			Branch:    "main",
			Message:   "Original\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"",
			Timestamp: now.Add(-time.Hour),
		},
	})
	// Insert edit commit
	cache.InsertCommits([]cache.Commit{
		{
			Hash:      "eeff00112233",
			RepoURL:   "https://github.com/user/repo",
			Branch:    "main",
			Message:   "Edited\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"https://github.com/user/repo#commit:aabbccdd1234@main\"; v=\"0.1.0\"",
			Timestamp: now,
		},
	})
	cache.InsertVersion("https://github.com/user/repo", "eeff00112233", "main",
		"https://github.com/user/repo", "aabbccdd1234", "main", false)

	result, err := GetHistory("https://github.com/user/repo#commit:eeff00112233@main", "https://github.com/user/repo")
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(result) < 2 {
		t.Errorf("len(result) = %d, want >= 2 (canonical + edit)", len(result))
	}
}
