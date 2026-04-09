// parser.go - GitMsg protocol message parsing
package protocol

import (
	"regexp"
	"strings"
)

var (
	headerLinePattern = regexp.MustCompile(`^GitMsg: (.*)$`)
	refLinePattern    = regexp.MustCompile(`^GitMsg-Ref: (.*)$`)
	fieldPattern      = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_:-]*)="([^"]*)"`)
)

// ParseHeader parses a GitMsg trailer line into a Header struct.
func ParseHeader(headerLine string) *Header {
	match := headerLinePattern.FindStringSubmatch(headerLine)
	if len(match) < 2 {
		return nil
	}

	fields := make(map[string]string)
	var ext, v string

	headerContent := match[1]
	fieldMatches := fieldPattern.FindAllStringSubmatch(headerContent, -1)

	for _, fm := range fieldMatches {
		if len(fm) < 3 {
			continue
		}
		key, value := fm[1], fm[2]
		switch key {
		case "ext":
			ext = value
		case "v":
			v = value
		default:
			fields[key] = value
		}
	}

	if ext == "" || v == "" {
		return nil
	}

	return &Header{
		Ext:    ext,
		V:      v,
		Fields: fields,
	}
}

// ParseRefSection parses a GitMsg-Ref trailer with continuation lines into a Ref struct.
func ParseRefSection(refSection string) *Ref {
	lines := strings.Split(refSection, "\n")
	if len(lines) == 0 {
		return nil
	}

	headerLine := lines[0]
	match := refLinePattern.FindStringSubmatch(headerLine)
	if len(match) < 2 {
		return nil
	}

	fields := make(map[string]string)
	var ext, ref, v, author, email, time string

	refContent := match[1]
	fieldMatches := fieldPattern.FindAllStringSubmatch(refContent, -1)

	for _, fm := range fieldMatches {
		if len(fm) < 3 {
			continue
		}
		key, value := fm[1], fm[2]
		switch key {
		case "ext":
			ext = value
		case "ref":
			ref = value
		case "v":
			v = value
		case "author":
			author = value
		case "email":
			email = value
		case "time":
			time = value
		default:
			fields[key] = value
		}
	}

	if ext == "" || ref == "" || v == "" || author == "" || email == "" || time == "" {
		return nil
	}

	metadata := ""
	if len(lines) > 1 {
		metaLines := make([]string, 0, len(lines)-1)
		for _, line := range lines[1:] {
			// Strip leading space (trailer continuation marker)
			if strings.HasPrefix(line, " ") {
				metaLines = append(metaLines, line[1:])
			} else {
				metaLines = append(metaLines, line)
			}
		}
		metadata = strings.TrimSpace(strings.Join(metaLines, "\n"))
	}

	return &Ref{
		Ext:      ext,
		Ref:      ref,
		V:        v,
		Author:   author,
		Email:    email,
		Time:     time,
		Fields:   fields,
		Metadata: metadata,
	}
}

// ParseMessage parses a complete GitMsg message with header and refs.
func ParseMessage(message string) *Message {
	// Find the GitMsg trailer (preceded by newline or at start of message)
	headerIdx := -1
	if strings.HasPrefix(message, "GitMsg: ") {
		headerIdx = 0
	} else {
		idx := strings.Index(message, "\nGitMsg: ")
		if idx != -1 {
			headerIdx = idx + 1
		}
	}
	if headerIdx == -1 {
		return nil
	}

	content := ""
	if headerIdx > 0 {
		content = strings.TrimSpace(message[:headerIdx])
	}

	// Split trailer block into lines
	trailerBlock := message[headerIdx:]
	lines := strings.Split(trailerBlock, "\n")

	// First line is the GitMsg header
	header := ParseHeader(lines[0])
	if header == nil {
		return nil
	}

	// Parse remaining lines for GitMsg-Ref trailers with continuation lines
	var references []Ref
	var currentRefLines []string

	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "GitMsg-Ref: ") {
			// Flush previous ref if any
			if len(currentRefLines) > 0 {
				ref := ParseRefSection(strings.Join(currentRefLines, "\n"))
				if ref != nil {
					references = append(references, *ref)
				}
			}
			currentRefLines = []string{line}
		} else if strings.HasPrefix(line, " ") && len(currentRefLines) > 0 {
			// Continuation line for current ref
			currentRefLines = append(currentRefLines, line)
		}
	}
	// Flush last ref
	if len(currentRefLines) > 0 {
		ref := ParseRefSection(strings.Join(currentRefLines, "\n"))
		if ref != nil {
			references = append(references, *ref)
		}
	}

	return &Message{
		Content:    content,
		Header:     *header,
		References: references,
	}
}

// ExtractCleanContent extracts user content without protocol metadata.
func ExtractCleanContent(message string) string {
	headerIdx := -1
	if strings.HasPrefix(message, "GitMsg: ") {
		headerIdx = 0
	} else {
		idx := strings.Index(message, "\nGitMsg: ")
		if idx != -1 {
			headerIdx = idx
		}
	}
	content := message
	if headerIdx != -1 {
		content = message[:headerIdx]
	}
	content = strings.ReplaceAll(content, "\r", "")
	return strings.TrimSpace(content)
}
