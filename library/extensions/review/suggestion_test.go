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
			"simple code block",
			"```go\nfmt.Println(\"hello\")\n```",
			"fmt.Println(\"hello\")",
		},
		{
			"no language specifier",
			"```\nsome code\n```",
			"some code",
		},
		{
			"multiline code",
			"```go\nline1\nline2\nline3\n```",
			"line1\nline2\nline3",
		},
		{
			"with surrounding text",
			"Consider using:\n```go\nfmt.Println(\"hello\")\n```\nThis is better.",
			"fmt.Println(\"hello\")",
		},
		{
			"no code block",
			"Just regular text without fences",
			"",
		},
		{
			"empty code block",
			"```\n\n```",
			"",
		},
		{
			"multiple code blocks - first wins",
			"```go\nfirst\n```\n\n```go\nsecond\n```",
			"first",
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
		Content:    "Replace lines 2-3:\n```go\nnewLine2\nnewLine3\n```",
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

func TestApplySuggestion_singleLine(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	content := "line1\nline2\nline3\n"
	os.WriteFile(filePath, []byte(content), 0644)

	fb := Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    2,
		Content:    "```go\nreplacement\n```",
	}

	res := ApplySuggestion(dir, fb)
	if !res.Success {
		t.Fatalf("ApplySuggestion() failed: %s", res.Error.Message)
	}
}

func TestApplySuggestion_usesOldLine(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	content := "line1\nline2\nline3\n"
	os.WriteFile(filePath, []byte(content), 0644)

	fb := Feedback{
		Suggestion: true,
		File:       "main.go",
		OldLine:    2,
		Content:    "```go\nreplacement\n```",
	}

	res := ApplySuggestion(dir, fb)
	if !res.Success {
		t.Fatalf("ApplySuggestion() failed: %s", res.Error.Message)
	}
}

func TestApplySuggestion_notSuggestion(t *testing.T) {
	res := ApplySuggestion(t.TempDir(), Feedback{Suggestion: false})
	if res.Success {
		t.Error("should fail when not a suggestion")
	}
}

func TestApplySuggestion_noFile(t *testing.T) {
	res := ApplySuggestion(t.TempDir(), Feedback{Suggestion: true})
	if res.Success {
		t.Error("should fail when no file reference")
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
	if res.Success {
		t.Error("should fail when no code block found")
	}
}

func TestApplySuggestion_noLine(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("line1\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		Content:    "```go\ncode\n```",
	})
	if res.Success {
		t.Error("should fail when no line reference")
	}
}

func TestApplySuggestion_fileNotFound(t *testing.T) {
	res := ApplySuggestion(t.TempDir(), Feedback{
		Suggestion: true,
		File:       "nonexistent.go",
		NewLine:    1,
		Content:    "```go\ncode\n```",
	})
	if res.Success {
		t.Error("should fail when file not found")
	}
}

func TestApplySuggestion_lineOutOfRange(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("line1\n"), 0644)

	res := ApplySuggestion(dir, Feedback{
		Suggestion: true,
		File:       "main.go",
		NewLine:    999,
		Content:    "```go\ncode\n```",
	})
	if res.Success {
		t.Error("should fail when line out of range")
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
		Content:    "```go\nreplaced\n```",
	})
	if !res.Success {
		t.Fatalf("should clamp end line: %s", res.Error.Message)
	}
}
