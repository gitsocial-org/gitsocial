// parser.go - GitMsg protocol message parsing
package protocol

import (
	"regexp"
	"strings"
)

var (
	headerLinePattern = regexp.MustCompile(`^--- GitMsg: (.*) ---$`)
	refLinePattern    = regexp.MustCompile(`^--- GitMsg-Ref: (.*) ---$`)
	fieldPattern      = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_:-]*)="([^"]*)"`)
)

// ParseHeader parses a GitMsg header line into a Header struct.
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

// ParseRefSection parses a GitMsg-Ref section into a Ref struct.
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
		metadata = strings.TrimSpace(strings.Join(lines[1:], "\n"))
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
	headerMarker := "--- GitMsg: "
	headerIdx := strings.Index(message, headerMarker)
	if headerIdx == -1 {
		return nil
	}

	content := strings.TrimSpace(message[:headerIdx])

	headerEndIdx := strings.Index(message[headerIdx:], " ---")
	if headerEndIdx == -1 {
		return nil
	}
	headerEndIdx = headerIdx + headerEndIdx + 4

	headerLine := message[headerIdx:headerEndIdx]
	header := ParseHeader(headerLine)
	if header == nil {
		return nil
	}

	var references []Ref
	remainingMessage := message[headerEndIdx:]

	refMarker := "--- GitMsg-Ref: "
	refSections := strings.Split(remainingMessage, refMarker)

	for i, section := range refSections {
		if i == 0 {
			continue
		}
		endIdx := strings.Index(section, " ---")
		if endIdx == -1 {
			continue
		}
		fullSection := refMarker + section
		nextRefIdx := strings.Index(section[endIdx:], refMarker)
		if nextRefIdx != -1 {
			fullSection = refMarker + section[:endIdx+nextRefIdx]
		}
		ref := ParseRefSection(strings.TrimSpace(fullSection))
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
	headerMarker := "--- GitMsg: "
	headerIdx := strings.Index(message, headerMarker)
	content := message
	if headerIdx != -1 {
		content = message[:headerIdx]
	}
	content = strings.ReplaceAll(content, "\r", "")
	return strings.TrimSpace(content)
}
