// diff.go - Git diff operations and unified diff parser
package git

import (
	"fmt"
	"strconv"
	"strings"
)

// GetDiff returns parsed file diffs between two refs.
func GetDiff(workdir, base, head string) ([]FileDiff, error) {
	output, err := execGitSimple(workdir, []string{"diff", "--no-color", "--unified=3", base + ".." + head})
	if err != nil {
		return nil, fmt.Errorf("get diff: %w", err)
	}
	if output == "" {
		return nil, nil
	}
	return parseDiff(output), nil
}

// GetFileDiff returns a parsed diff for a single file between two refs.
func GetFileDiff(workdir, base, head, file string) (*FileDiff, error) {
	output, err := execGitSimple(workdir, []string{"diff", "--no-color", "--unified=3", base + ".." + head, "--", file})
	if err != nil {
		return nil, fmt.Errorf("get file diff: %w", err)
	}
	if output == "" {
		return nil, nil
	}
	diffs := parseDiff(output)
	if len(diffs) == 0 {
		return nil, nil
	}
	return &diffs[0], nil
}

// GetFileContent returns file content at a specific ref.
func GetFileContent(workdir, ref, file string) (string, error) {
	output, err := execGitSimple(workdir, []string{"show", ref + ":" + file})
	if err != nil {
		return "", fmt.Errorf("get file content: %w", err)
	}
	return output, nil
}

// GetDiffStats returns aggregate diff statistics between two refs.
func GetDiffStats(workdir, base, head string) (DiffStats, error) {
	output, err := execGitSimple(workdir, []string{"diff", "--numstat", base + ".." + head})
	if err != nil {
		return DiffStats{}, fmt.Errorf("get diff stats: %w", err)
	}
	if output == "" {
		return DiffStats{}, nil
	}
	var stats DiffStats
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		stats.Files++
		if parts[0] != "-" {
			if n, err := strconv.Atoi(parts[0]); err == nil {
				stats.Added += n
			}
		}
		if parts[1] != "-" {
			if n, err := strconv.Atoi(parts[1]); err == nil {
				stats.Removed += n
			}
		}
	}
	return stats, nil
}

// parseDiff parses unified diff output into structured FileDiff slices.
func parseDiff(output string) []FileDiff {
	var diffs []FileDiff
	lines := strings.Split(output, "\n")
	i := 0
	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "diff --git ") {
			i++
			continue
		}
		diff, nextIdx := parseFileDiff(lines, i)
		diffs = append(diffs, diff)
		i = nextIdx
	}
	return diffs
}

// parseFileDiff parses a single file diff starting at the given line index.
func parseFileDiff(lines []string, start int) (FileDiff, int) {
	var fd FileDiff
	i := start
	// Parse "diff --git a/path b/path"
	header := lines[i]
	parts := strings.SplitN(header, " ", 4)
	if len(parts) >= 4 {
		fd.OldPath = strings.TrimPrefix(parts[2], "a/")
		fd.NewPath = strings.TrimPrefix(parts[3], "b/")
	}
	fd.Status = DiffStatusModified
	i++
	// Parse header lines until we hit @@, another diff, or end
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git ") {
			break
		}
		if strings.HasPrefix(line, "@@") {
			break
		}
		if strings.HasPrefix(line, "new file") {
			fd.Status = DiffStatusAdded
		} else if strings.HasPrefix(line, "deleted file") {
			fd.Status = DiffStatusDeleted
		} else if strings.HasPrefix(line, "rename from ") {
			fd.Status = DiffStatusRenamed
			fd.OldPath = strings.TrimPrefix(line, "rename from ")
		} else if strings.HasPrefix(line, "rename to ") {
			fd.NewPath = strings.TrimPrefix(line, "rename to ")
		} else if strings.HasPrefix(line, "Binary files") {
			fd.Binary = true
		}
		i++
	}
	// Parse hunks
	for i < len(lines) && !strings.HasPrefix(lines[i], "diff --git ") {
		if strings.HasPrefix(lines[i], "@@") {
			hunk, nextIdx := parseHunk(lines, i)
			fd.Hunks = append(fd.Hunks, hunk)
			i = nextIdx
		} else {
			i++
		}
	}
	return fd, i
}

// parseHunk parses a single hunk starting at the @@ line.
func parseHunk(lines []string, start int) (Hunk, int) {
	var h Hunk
	h.Header = lines[start]
	// Parse @@ -oldStart,oldCount +newStart,newCount @@
	h.OldStart, h.OldCount, h.NewStart, h.NewCount = parseHunkHeader(lines[start])
	oldLine := h.OldStart
	newLine := h.NewStart
	i := start + 1
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "@@") {
			break
		}
		if len(line) == 0 {
			// Empty line in diff = context line with empty content
			h.Lines = append(h.Lines, DiffLine{Type: LineContext, Content: "", OldNum: oldLine, NewNum: newLine})
			oldLine++
			newLine++
		} else {
			switch line[0] {
			case '+':
				h.Lines = append(h.Lines, DiffLine{Type: LineAdded, Content: line[1:], NewNum: newLine})
				newLine++
			case '-':
				h.Lines = append(h.Lines, DiffLine{Type: LineRemoved, Content: line[1:], OldNum: oldLine})
				oldLine++
			case '\\':
				// "\ No newline at end of file" - skip
			default:
				// Context line (space prefix or unexpected)
				content := line
				if len(content) > 0 && content[0] == ' ' {
					content = content[1:]
				}
				h.Lines = append(h.Lines, DiffLine{Type: LineContext, Content: content, OldNum: oldLine, NewNum: newLine})
				oldLine++
				newLine++
			}
		}
		i++
	}
	return h, i
}

// parseHunkHeader parses "@@ -10,7 +10,8 @@ optional header" into components.
func parseHunkHeader(line string) (oldStart, oldCount, newStart, newCount int) {
	// Find the range info between @@ markers
	atIdx := strings.Index(line, "@@")
	if atIdx == -1 {
		return
	}
	rest := line[atIdx+2:]
	endIdx := strings.Index(rest, "@@")
	if endIdx == -1 {
		return
	}
	rangeStr := strings.TrimSpace(rest[:endIdx])
	parts := strings.Fields(rangeStr)
	for _, p := range parts {
		if strings.HasPrefix(p, "-") {
			oldStart, oldCount = parseRange(p[1:])
		} else if strings.HasPrefix(p, "+") {
			newStart, newCount = parseRange(p[1:])
		}
	}
	return
}

// parseRange parses "10,7" or "10" into start and count.
func parseRange(s string) (int, int) {
	if idx := strings.Index(s, ","); idx >= 0 {
		start, _ := strconv.Atoi(s[:idx])
		count, _ := strconv.Atoi(s[idx+1:])
		return start, count
	}
	start, _ := strconv.Atoi(s)
	return start, 1
}
