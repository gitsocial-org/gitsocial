// suggestion.go - Parse and apply code suggestions from review feedback
package review

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/result"
)

var fencePattern = regexp.MustCompile("(?s)```[^\n]*\n(.*?)\n```")

// ParseSuggestionCode extracts code from a markdown fenced code block in feedback content.
func ParseSuggestionCode(content string) string {
	match := fencePattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

// ApplySuggestion applies a suggestion to the working tree without committing.
func ApplySuggestion(workdir string, feedback Feedback) Result[string] {
	if !feedback.Suggestion {
		return result.Err[string]("invalid", "feedback is not a suggestion")
	}
	if feedback.File == "" {
		return result.Err[string]("invalid", "feedback has no file reference")
	}
	suggested := ParseSuggestionCode(feedback.Content)
	if suggested == "" {
		return result.Err[string]("parse_error", "could not parse suggestion code from feedback content")
	}
	startLine := feedback.NewLine
	if startLine <= 0 {
		startLine = feedback.OldLine
	}
	if startLine <= 0 {
		return result.Err[string]("invalid", "feedback has no line reference")
	}
	endLine := feedback.NewLineEnd
	if endLine <= 0 {
		endLine = startLine
	}
	clean := filepath.Clean(feedback.File)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return result.Err[string]("invalid", "file path must be relative and within the repository")
	}
	filePath := filepath.Join(workdir, clean)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return result.Err[string]("file_error", fmt.Sprintf("read file: %s", err))
	}
	lines := strings.Split(string(data), "\n")
	if startLine > len(lines) {
		return result.Err[string]("range_error", fmt.Sprintf("start line %d exceeds file length %d", startLine, len(lines)))
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	suggestedLines := strings.Split(suggested, "\n")
	// Replace lines[startLine-1:endLine] with suggestedLines
	newLines := make([]string, 0, len(lines)-endLine+startLine-1+len(suggestedLines))
	newLines = append(newLines, lines[:startLine-1]...)
	newLines = append(newLines, suggestedLines...)
	newLines = append(newLines, lines[endLine:]...)
	info, err := os.Stat(filePath)
	perm := os.FileMode(0644)
	if err == nil {
		perm = info.Mode()
	}
	if err := os.WriteFile(filePath, []byte(strings.Join(newLines, "\n")), perm); err != nil {
		return result.Err[string]("write_error", fmt.Sprintf("write file: %s", err))
	}
	return result.Ok(feedback.File)
}
