// suggestion_test.go - Tests for suggestion code parsing and application
package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSuggestionCode(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			"suggestion fence",
			"```suggestion\nfmt.Println(\"hello\")\n```",
			"fmt.Println(\"hello\")",
		},
		{
			"multiline suggestion",
			"```suggestion\nline1\nline2\nline3\n```",
			"line1\nline2\nline3",
		},
		{
			"with surrounding text",
			"Consider using:\n```suggestion\nfmt.Println(\"hello\")\n```\nThis is better.",
			"fmt.Println(\"hello\")",
		},
		{
			"a language fence before the suggestion fence",
			"Today it reads:\n```go\ncurrent\n```\nMake it:\n```suggestion\nreplacement\n```",
			"replacement",
		},
		{
			"a language fence alone is not a suggestion",
			"```go\nfmt.Println(\"hello\")\n```",
			"",
		},
		{
			"a fence with no language is not a suggestion",
			"```\nsome code\n```",
			"",
		},
		{
			"no fence",
			"Just regular text without fences",
			"",
		},
		{
			"empty suggestion fence",
			"```suggestion\n\n```",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSuggestionCode(tt.content)
			if got != tt.want {
				t.Errorf("ParseSuggestionCode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplySuggestion(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	content := "line1\nline2\nline3\nline4\nline5\n"
	os.WriteFile(filePath, []byte(content), 0644)

	fb := Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    2,
		NewLineEnd: 3,
		Content:    "Replace lines 2-3:\n```suggestion\nnewLine2\nnewLine3\n```",
	}

	res := ApplySuggestion(dir, fb)
	if !res.Success {
		t.Fatalf("ApplySuggestion() failed: %s", res.Error.Message)
	}
	if res.Data != "main.go" {
		t.Errorf("Data = %q, want main.go", res.Data)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "newLine2") {
		t.Error("file should contain suggested code")
	}
	if strings.Contains(string(data), "line2\n") {
		t.Error("file should not contain original line2")
	}
}

// TestApplySuggestion_afterLanguageFence applies the suggestion, not the code it quotes.
func TestApplySuggestion_afterLanguageFence(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	os.WriteFile(filePath, []byte("line1\ncurrent\nline3\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    2,
		Content:    "Today it reads:\n```go\ncurrent\n```\nMake it:\n```suggestion\nreplacement\n```",
	})
	if !res.Success {
		t.Fatalf("ApplySuggestion() failed: %s", res.Error.Message)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "replacement") {
		t.Errorf("file should hold the suggestion, got:\n%s", data)
	}
	if strings.Contains(string(data), "current") {
		t.Errorf("the quoted current code was written back:\n%s", data)
	}
}

func TestApplySuggestion_singleLine(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	os.WriteFile(filePath, []byte("line1\nline2\nline3\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    2,
		Content:    "```suggestion\nreplacement\n```",
	})
	if !res.Success {
		t.Fatalf("ApplySuggestion() failed: %s", res.Error.Message)
	}
}

func TestApplySuggestion_usesOldLine(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	os.WriteFile(filePath, []byte("line1\nline2\nline3\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		OldLine:    2,
		Content:    "```suggestion\nreplacement\n```",
	})
	if !res.Success {
		t.Fatalf("ApplySuggestion() failed: %s", res.Error.Message)
	}
}

func TestApplySuggestion_notSuggestion(t *testing.T) {
	res := ApplySuggestion(t.TempDir(), Feedback{Suggestion: false})
	if res.Success || res.Error.Code != "NOT_SUGGESTION" {
		t.Errorf("ApplySuggestion() on plain feedback = %+v, want NOT_SUGGESTION", res)
	}
}

func TestApplySuggestion_noFile(t *testing.T) {
	res := ApplySuggestion(t.TempDir(), Feedback{Suggestion: true})
	if res.Success || res.Error.Code != "NOT_SUGGESTION" {
		t.Errorf("ApplySuggestion() with no file = %+v, want NOT_SUGGESTION", res)
	}
}

// TestApplySuggestion_noSuggestionFence refuses a stored suggestion carrying another fence.
func TestApplySuggestion_noSuggestionFence(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("line1\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    1,
		Content:    "```go\ncode\n```",
	})
	if res.Success || res.Error.Code != "PARSE_ERROR" {
		t.Errorf("ApplySuggestion() with no suggestion fence = %+v, want PARSE_ERROR", res)
	}
}

func TestApplySuggestion_noCodeBlock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("line1\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    1,
		Content:    "No code block here",
	})
	if res.Success || res.Error.Code != "PARSE_ERROR" {
		t.Errorf("ApplySuggestion() with no fence = %+v, want PARSE_ERROR", res)
	}
}

func TestApplySuggestion_noLine(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("line1\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		Content:    "```suggestion\ncode\n```",
	})
	if res.Success || res.Error.Code != "NOT_SUGGESTION" {
		t.Errorf("ApplySuggestion() with no line = %+v, want NOT_SUGGESTION", res)
	}
}

// TestApplySuggestion_pathOutsideRepository refuses a file path that leaves the working tree.
func TestApplySuggestion_pathOutsideRepository(t *testing.T) {
	res := ApplySuggestion(t.TempDir(), Feedback{
		Suggestion: true,
		File:       "../outside.go",
		NewLine:    1,
		Content:    "```suggestion\ncode\n```",
	})
	if res.Success || res.Error.Code != "INVALID_PATH" {
		t.Errorf("ApplySuggestion() with a path outside the repository = %+v, want INVALID_PATH", res)
	}
}

func TestApplySuggestion_fileNotFound(t *testing.T) {
	res := ApplySuggestion(t.TempDir(), Feedback{
		Suggestion: true,
		File:       "nonexistent.go",
		NewLine:    1,
		Content:    "```suggestion\ncode\n```",
	})
	if res.Success || res.Error.Code != "FILE_ERROR" {
		t.Errorf("ApplySuggestion() on a missing file = %+v, want FILE_ERROR", res)
	}
}

func TestApplySuggestion_lineOutOfRange(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("line1\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    999,
		Content:    "```suggestion\ncode\n```",
	})
	if res.Success || res.Error.Code != "RANGE_ERROR" {
		t.Errorf("ApplySuggestion() past the end of the file = %+v, want RANGE_ERROR", res)
	}
}

func TestApplySuggestion_endLineBeyondFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("line1\nline2\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    1,
		NewLineEnd: 999,
		Content:    "```suggestion\nreplaced\n```",
	})
	if !res.Success {
		t.Fatalf("should clamp end line: %s", res.Error.Message)
	}
}

// TestCreateFeedback_suggestionNeedsFence refuses a suggestion with no suggestion fence.
func TestCreateFeedback_suggestionNeedsFence(t *testing.T) {
	setupTestDB(t)
	dir := initTestRepo(t)

	opts := CreateFeedbackOptions{
		PullRequest: "#commit:ab0c12345678@gitmsg/review",
		Commit:      "abc123456789",
		File:        "main.go",
		NewLine:     2,
		Suggestion:  true,
	}
	res := CreateFeedback(dir, "Use this instead:\n```go\nreplacement\n```", opts)
	if res.Success || res.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("CreateFeedback with no suggestion fence: want VALIDATION_ERROR, got success=%v code=%q", res.Success, res.Error.Code)
	}

	ok := CreateFeedback(dir, "Use this instead:\n```suggestion\nreplacement\n```", opts)
	if !ok.Success {
		t.Fatalf("CreateFeedback with a suggestion fence: %s", ok.Error.Message)
	}
	if !ok.Data.Suggestion {
		t.Error("the stored feedback should be a suggestion")
	}
}
