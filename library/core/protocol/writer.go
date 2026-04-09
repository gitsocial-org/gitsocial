// writer.go - GitMsg protocol message formatting and header generation
package protocol

import (
	"fmt"
	"sort"
	"strings"
)

// CreateHeader formats a Header struct into a GitMsg trailer line.
func CreateHeader(header Header) string {
	fields := make([]string, 0, len(header.Fields)+4)
	fields = append(fields, fmt.Sprintf(`ext="%s"`, header.Ext))
	if ev, ok := header.Fields["ext-v"]; ok {
		fields = append(fields, fmt.Sprintf(`ext-v="%s"`, ev))
	}

	// Field order per GITMSG.md: ext, ext-v, type, edits, retracted, origin, extension-specific, v
	if t, ok := header.Fields["type"]; ok {
		fields = append(fields, fmt.Sprintf(`type="%s"`, t))
	}
	if e, ok := header.Fields["edits"]; ok {
		fields = append(fields, fmt.Sprintf(`edits="%s"`, e))
	}
	if r, ok := header.Fields["retracted"]; ok && r == "true" {
		fields = append(fields, `retracted="true"`)
	}

	// Origin fields (core, ordered alphabetically within group)
	for _, originKey := range []string{"origin-author-email", "origin-author-name", "origin-platform", "origin-time", "origin-url"} {
		if v, ok := header.Fields[originKey]; ok {
			fields = append(fields, fmt.Sprintf(`%s="%s"`, originKey, v))
		}
	}

	// Extension-declared priority fields (before alphabetical remainder)
	placed := map[string]bool{"type": true, "edits": true, "retracted": true, "ext-v": true}
	for _, k := range header.FieldOrder {
		if v, ok := header.Fields[k]; ok {
			fields = append(fields, fmt.Sprintf(`%s="%s"`, k, v))
			placed[k] = true
		}
	}
	for k := range header.Fields {
		if strings.HasPrefix(k, "origin-") {
			placed[k] = true
		}
	}
	// Remaining fields alphabetically
	keys := make([]string, 0, len(header.Fields))
	for k := range header.Fields {
		if !placed[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fields = append(fields, fmt.Sprintf(`%s="%s"`, k, header.Fields[k]))
	}

	fields = append(fields, fmt.Sprintf(`v="%s"`, header.V))

	return fmt.Sprintf("GitMsg: %s", strings.Join(fields, "; "))
}

// CreateRefSection formats a Ref struct into a GitMsg-Ref trailer with continuation lines.
func CreateRefSection(ref Ref) string {
	fields := make([]string, 0, len(ref.Fields)+6)
	fields = append(fields, fmt.Sprintf(`ext="%s"`, ref.Ext))

	if t, ok := ref.Fields["type"]; ok {
		fields = append(fields, fmt.Sprintf(`type="%s"`, t))
	}

	fields = append(fields, fmt.Sprintf(`author="%s"`, ref.Author))
	fields = append(fields, fmt.Sprintf(`email="%s"`, ref.Email))
	fields = append(fields, fmt.Sprintf(`time="%s"`, ref.Time))

	// Extension-declared priority fields (before alphabetical remainder)
	placed := map[string]bool{"type": true}
	for _, k := range ref.FieldOrder {
		if v, ok := ref.Fields[k]; ok {
			fields = append(fields, fmt.Sprintf(`%s="%s"`, k, v))
			placed[k] = true
		}
	}
	// Remaining fields alphabetically
	keys := make([]string, 0, len(ref.Fields))
	for k := range ref.Fields {
		if !placed[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fields = append(fields, fmt.Sprintf(`%s="%s"`, k, ref.Fields[k]))
	}

	fields = append(fields, fmt.Sprintf(`ref="%s"`, ref.Ref))
	fields = append(fields, fmt.Sprintf(`v="%s"`, ref.V))

	headerLine := fmt.Sprintf("GitMsg-Ref: %s", strings.Join(fields, "; "))

	if ref.Metadata != "" {
		// Prefix each line with space for trailer continuation
		metaLines := strings.Split(ref.Metadata, "\n")
		for i, line := range metaLines {
			metaLines[i] = " " + line
		}
		return headerLine + "\n" + strings.Join(metaLines, "\n")
	}

	return headerLine
}

// QuoteContent prefixes each line with "> " for blockquote formatting in GitMsg-Ref metadata.
func QuoteContent(content string) string {
	if content == "" {
		return ""
	}
	content = strings.TrimSuffix(content, "\n")
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

// FormatMessage assembles content, header, and refs into a commit message.
func FormatMessage(content string, header Header, references []Ref) string {
	parts := make([]string, 0, 2+len(references)+1)
	parts = append(parts, strings.TrimSpace(content))
	parts = append(parts, "") // blank line separating body from trailer block
	parts = append(parts, CreateHeader(header))

	for _, ref := range references {
		parts = append(parts, CreateRefSection(ref))
	}

	return strings.Join(parts, "\n")
}
