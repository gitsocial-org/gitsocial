// writer_test.go - Tests for GitMsg protocol message formatting
package protocol

import (
	"strings"
	"testing"
)

func TestCreateHeader(t *testing.T) {
	tests := []struct {
		name   string
		header Header
		check  func(t *testing.T, result string)
	}{
		{
			name: "basic post",
			header: Header{
				Ext: "social",
				V:   "0.1.0",
				Fields: map[string]string{
					"type": "post",
				},
			},
			check: func(t *testing.T, result string) {
				if !strings.HasPrefix(result, "GitMsg: ") {
					t.Errorf("missing prefix: %q", result)
				}
				if !strings.Contains(result, `ext="social"`) {
					t.Error("missing ext")
				}
				if !strings.Contains(result, `type="post"`) {
					t.Error("missing type")
				}
				if !strings.Contains(result, `v="0.1.0"`) {
					t.Error("missing v")
				}
			},
		},
		{
			name: "field ordering: ext first, v last",
			header: Header{
				Ext: "social",
				V:   "0.1.0",
				Fields: map[string]string{
					"type": "post",
				},
			},
			check: func(t *testing.T, result string) {
				extIdx := strings.Index(result, `ext="social"`)
				typeIdx := strings.Index(result, `type="post"`)
				vIdx := strings.Index(result, `v="0.1.0"`)
				if extIdx > typeIdx || typeIdx > vIdx {
					t.Errorf("wrong field order: ext=%d, type=%d, v=%d in %q", extIdx, typeIdx, vIdx, result)
				}
			},
		},
		{
			name: "edits before extension fields",
			header: Header{
				Ext: "social",
				V:   "0.1.0",
				Fields: map[string]string{
					"type":  "post",
					"edits": "#commit:abc123456789@main",
				},
			},
			check: func(t *testing.T, result string) {
				editsIdx := strings.Index(result, `edits=`)
				vIdx := strings.Index(result, `v="0.1.0"`)
				if editsIdx > vIdx {
					t.Errorf("edits should come before v in %q", result)
				}
			},
		},
		{
			name: "retracted field",
			header: Header{
				Ext: "social",
				V:   "0.1.0",
				Fields: map[string]string{
					"type":      "post",
					"retracted": "true",
				},
			},
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, `retracted="true"`) {
					t.Errorf("missing retracted in %q", result)
				}
			},
		},
		{
			name: "social-specific reply-to and original",
			header: Header{
				Ext: "social",
				V:   "0.1.0",
				Fields: map[string]string{
					"type":     "comment",
					"reply-to": "#commit:abc123456789@main",
					"original": "#commit:def456789abc@main",
				},
			},
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, `reply-to="#commit:abc123456789@main"`) {
					t.Errorf("missing reply-to in %q", result)
				}
				if !strings.Contains(result, `original="#commit:def456789abc@main"`) {
					t.Errorf("missing original in %q", result)
				}
			},
		},
		{
			name: "non-social extension ignores reply-to/original ordering",
			header: Header{
				Ext: "pm",
				V:   "0.1.0",
				Fields: map[string]string{
					"type":  "issue",
					"state": "open",
				},
			},
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, `ext="pm"`) {
					t.Error("missing ext")
				}
				if !strings.Contains(result, `state="open"`) {
					t.Error("missing state")
				}
			},
		},
		{
			name: "no extension fields",
			header: Header{
				Ext:    "social",
				V:      "0.1.0",
				Fields: map[string]string{},
			},
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, `ext="social"`) || !strings.Contains(result, `v="0.1.0"`) {
					t.Errorf("missing required fields in %q", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CreateHeader(tt.header)
			tt.check(t, result)
		})
	}
}

func TestCreateRefSection(t *testing.T) {
	tests := []struct {
		name  string
		ref   Ref
		check func(t *testing.T, result string)
	}{
		{
			name: "basic ref",
			ref: Ref{
				Ext:    "social",
				Author: "Alice",
				Email:  "alice@example.com",
				Time:   "2025-10-21T12:00:00Z",
				Ref:    "#commit:abc123456789",
				V:      "0.1.0",
				Fields: map[string]string{},
			},
			check: func(t *testing.T, result string) {
				if !strings.HasPrefix(result, "GitMsg-Ref: ") {
					t.Error("missing prefix")
				}
				if !strings.Contains(result, `author="Alice"`) {
					t.Error("missing author")
				}
				if !strings.Contains(result, `email="alice@example.com"`) {
					t.Error("missing email")
				}
				if !strings.Contains(result, `ref="#commit:abc123456789"`) {
					t.Error("missing ref")
				}
			},
		},
		{
			name: "ref with metadata",
			ref: Ref{
				Ext:      "social",
				Author:   "Bob",
				Email:    "bob@example.com",
				Time:     "2025-10-21T12:00:00Z",
				Ref:      "#commit:abc123456789",
				V:        "0.1.0",
				Fields:   map[string]string{},
				Metadata: "> Quoted content\n> Second line",
			},
			check: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				if len(lines) < 3 {
					t.Fatalf("expected at least 3 lines, got %d", len(lines))
				}
				if lines[1] != " > Quoted content" {
					t.Errorf("line 1 = %q, want %q", lines[1], " > Quoted content")
				}
				if lines[2] != " > Second line" {
					t.Errorf("line 2 = %q, want %q", lines[2], " > Second line")
				}
			},
		},
		{
			name: "ref with extension fields",
			ref: Ref{
				Ext:    "social",
				Author: "Charlie",
				Email:  "c@example.com",
				Time:   "2025-10-21T12:00:00Z",
				Ref:    "#commit:abc123456789",
				V:      "0.1.0",
				Fields: map[string]string{"type": "comment"},
			},
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, `type="comment"`) {
					t.Errorf("missing type field in %q", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CreateRefSection(tt.ref)
			tt.check(t, result)
		})
	}
}

func TestQuoteContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single line",
			input: "Hello world",
			want:  "> Hello world",
		},
		{
			name:  "multiple lines",
			input: "Line 1\nLine 2\nLine 3",
			want:  "> Line 1\n> Line 2\n> Line 3",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "trailing newline stripped",
			input: "Content\n",
			want:  "> Content",
		},
		{
			name:  "empty lines quoted",
			input: "Before\n\nAfter",
			want:  "> Before\n> \n> After",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuoteContent(tt.input)
			if got != tt.want {
				t.Errorf("QuoteContent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		header     Header
		references []Ref
		check      func(t *testing.T, result string)
	}{
		{
			name:    "content and header",
			content: "Hello world!",
			header:  Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}},
			check: func(t *testing.T, result string) {
				if !strings.HasPrefix(result, "Hello world!") {
					t.Error("should start with content")
				}
				if !strings.Contains(result, "GitMsg:") {
					t.Error("missing header")
				}
			},
		},
		{
			name:    "content, header, and refs",
			content: "A comment",
			header:  Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "comment"}},
			references: []Ref{
				{
					Ext:    "social",
					Author: "Alice",
					Email:  "alice@example.com",
					Time:   "2025-10-21T12:00:00Z",
					Ref:    "#commit:abc123456789",
					V:      "0.1.0",
					Fields: map[string]string{},
				},
			},
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, "GitMsg-Ref:") {
					t.Error("missing ref section")
				}
			},
		},
		{
			name:    "empty content",
			content: "",
			header:  Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}},
			check: func(t *testing.T, result string) {
				if !strings.Contains(result, "GitMsg:") {
					t.Error("missing header")
				}
			},
		},
		{
			name:    "content is trimmed",
			content: "  spaces  ",
			header:  Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}},
			check: func(t *testing.T, result string) {
				if !strings.HasPrefix(result, "spaces") {
					t.Errorf("content not trimmed: %q", result)
				}
			},
		},
		{
			name:    "no blank lines between trailers",
			content: "A comment",
			header:  Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "comment"}},
			references: []Ref{
				{
					Ext: "social", Author: "Alice", Email: "a@b.com",
					Time: "2025-01-01T00:00:00Z", Ref: "#commit:abc123456789",
					V: "0.1.0", Fields: map[string]string{},
				},
				{
					Ext: "social", Author: "Bob", Email: "b@b.com",
					Time: "2025-01-02T00:00:00Z", Ref: "#commit:def456789abc",
					V: "0.1.0", Fields: map[string]string{},
				},
			},
			check: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				// Find the GitMsg: line
				for i, line := range lines {
					if strings.HasPrefix(line, "GitMsg: ") {
						// Next line should be GitMsg-Ref, not blank
						if i+1 < len(lines) && lines[i+1] == "" {
							t.Error("blank line between GitMsg and GitMsg-Ref")
						}
					}
					if strings.HasPrefix(line, "GitMsg-Ref: ") {
						// Check next non-continuation line isn't blank
						j := i + 1
						for j < len(lines) && strings.HasPrefix(lines[j], " ") {
							j++
						}
						if j < len(lines) && lines[j] == "" {
							t.Error("blank line between GitMsg-Ref trailers")
						}
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatMessage(tt.content, tt.header, tt.references)
			tt.check(t, result)
		})
	}
}

func TestFormatMessage_Roundtrip(t *testing.T) {
	header := Header{
		Ext: "social",
		V:   "0.1.0",
		Fields: map[string]string{
			"type": "comment",
		},
	}
	refs := []Ref{
		{
			Ext:    "social",
			Author: "Alice",
			Email:  "alice@example.com",
			Time:   "2025-10-21T12:00:00Z",
			Ref:    "#commit:abc123456789",
			V:      "0.1.0",
			Fields: map[string]string{},
		},
	}

	formatted := FormatMessage("Test content", header, refs)
	parsed := ParseMessage(formatted)

	if parsed == nil {
		t.Fatal("roundtrip: ParseMessage returned nil")
	}
	if parsed.Content != "Test content" {
		t.Errorf("roundtrip: Content = %q", parsed.Content)
	}
	if parsed.Header.Ext != "social" {
		t.Errorf("roundtrip: Ext = %q", parsed.Header.Ext)
	}
	if parsed.Header.Fields["type"] != "comment" {
		t.Errorf("roundtrip: type = %q", parsed.Header.Fields["type"])
	}
	if len(parsed.References) != 1 {
		t.Fatalf("roundtrip: References = %d, want 1", len(parsed.References))
	}
	if parsed.References[0].Author != "Alice" {
		t.Errorf("roundtrip: Author = %q", parsed.References[0].Author)
	}
}

func TestCreateRefSection_withMetadata(t *testing.T) {
	ref := Ref{
		Ext:      "social",
		Author:   "Alice",
		Email:    "alice@example.com",
		Time:     "2025-10-21T12:00:00Z",
		Ref:      "#commit:abc123456789",
		V:        "0.1.0",
		Fields:   map[string]string{"type": "quote"},
		Metadata: "> Some quoted content",
	}
	result := CreateRefSection(ref)
	if !strings.HasPrefix(result, "GitMsg-Ref: ") {
		t.Error("missing prefix")
	}
	if !strings.Contains(result, " > Some quoted content") {
		t.Error("metadata not included")
	}
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}
}

func TestCreateRefSection_socialFieldOrdering(t *testing.T) {
	ref := Ref{
		Ext:    "social",
		Author: "Bob",
		Email:  "bob@example.com",
		Time:   "2025-10-21T12:00:00Z",
		Ref:    "#commit:abc123456789",
		V:      "0.1.0",
		Fields: map[string]string{
			"type":     "comment",
			"reply-to": "#commit:def456789abc@main",
			"original": "#commit:ghi789abc123@main",
		},
	}
	result := CreateRefSection(ref)
	if !strings.Contains(result, `reply-to=`) {
		t.Error("missing reply-to")
	}
	if !strings.Contains(result, `original=`) {
		t.Error("missing original")
	}
	refIdx := strings.Index(result, `ref="#commit:abc123456789"`)
	vIdx := strings.Index(result, `v="0.1.0"`)
	replyIdx := strings.Index(result, `reply-to=`)
	if replyIdx > refIdx {
		t.Errorf("reply-to should come before ref: reply-to=%d, ref=%d", replyIdx, refIdx)
	}
	if refIdx > vIdx {
		t.Errorf("ref should come before v: ref=%d, v=%d", refIdx, vIdx)
	}
}
