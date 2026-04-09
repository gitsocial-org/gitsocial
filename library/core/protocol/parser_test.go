// parser_test.go - Tests for GitMsg protocol message parsing
package protocol

import (
	"testing"
)

func TestParseHeader(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Header
		wantNil bool
	}{
		{
			name:  "valid social post",
			input: `GitMsg: ext="social"; type="post"; v="0.1.0"`,
			want:  &Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}},
		},
		{
			name:  "multiple extension fields",
			input: `GitMsg: ext="social"; type="comment"; reply-to="#commit:abc123456789"; v="0.1.0"`,
			want:  &Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "comment", "reply-to": "#commit:abc123456789"}},
		},
		{
			name:  "pm extension",
			input: `GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0"`,
			want:  &Header{Ext: "pm", V: "0.1.0", Fields: map[string]string{"type": "issue", "state": "open"}},
		},
		{
			name:  "edits field",
			input: `GitMsg: ext="social"; type="post"; edits="#commit:abc123456789@main"; v="0.1.0"`,
			want:  &Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post", "edits": "#commit:abc123456789@main"}},
		},
		{
			name:  "retracted field",
			input: `GitMsg: ext="social"; type="post"; retracted="true"; v="0.1.0"`,
			want:  &Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post", "retracted": "true"}},
		},
		{
			name:  "empty field value",
			input: `GitMsg: ext="social"; type=""; v="0.1.0"`,
			want:  &Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": ""}},
		},
		{
			name:  "special chars in value",
			input: `GitMsg: ext="social"; type="post"; content="Hello, World!"; v="0.1.0"`,
			want:  &Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post", "content": "Hello, World!"}},
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "not a header",
			input:   "Not a valid header",
			wantNil: true,
		},
		{
			name:    "wrong prefix",
			input:   `GitMsg-Ref: ext="social"; v="0.1.0"`,
			wantNil: true,
		},
		{
			name:    "missing ext",
			input:   `GitMsg: type="post"; v="0.1.0"`,
			wantNil: true,
		},
		{
			name:    "missing v",
			input:   `GitMsg: ext="social"; type="post"`,
			wantNil: true,
		},
		{
			name:    "old format with dashes",
			input:   `--- GitMsg: ext="social"; v="0.1.0" ---`,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseHeader(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseHeader() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseHeader() = nil, want non-nil")
			}
			if got.Ext != tt.want.Ext {
				t.Errorf("Ext = %q, want %q", got.Ext, tt.want.Ext)
			}
			if got.V != tt.want.V {
				t.Errorf("V = %q, want %q", got.V, tt.want.V)
			}
			for k, wantV := range tt.want.Fields {
				if gotV, ok := got.Fields[k]; !ok || gotV != wantV {
					t.Errorf("Fields[%q] = %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}

func TestParseRefSection(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
		check   func(t *testing.T, ref *Ref)
	}{
		{
			name:  "valid ref with all required fields",
			input: `GitMsg-Ref: ext="social"; author="Alice"; email="alice@example.com"; time="2025-10-21T12:00:00Z"; ref="#commit:abc123456789"; v="0.1.0"`,
			check: func(t *testing.T, ref *Ref) {
				if ref.Ext != "social" {
					t.Errorf("Ext = %q, want social", ref.Ext)
				}
				if ref.Author != "Alice" {
					t.Errorf("Author = %q, want Alice", ref.Author)
				}
				if ref.Email != "alice@example.com" {
					t.Errorf("Email = %q", ref.Email)
				}
				if ref.Time != "2025-10-21T12:00:00Z" {
					t.Errorf("Time = %q", ref.Time)
				}
				if ref.Ref != "#commit:abc123456789" {
					t.Errorf("Ref = %q", ref.Ref)
				}
				if ref.V != "0.1.0" {
					t.Errorf("V = %q", ref.V)
				}
				if ref.Metadata != "" {
					t.Errorf("Metadata = %q, want empty", ref.Metadata)
				}
			},
		},
		{
			name: "ref with metadata",
			input: "GitMsg-Ref: ext=\"social\"; author=\"Bob\"; email=\"bob@example.com\"; time=\"2025-10-21T12:00:00Z\"; ref=\"#commit:abc123456789\"; v=\"0.1.0\"\n" +
				" Original content\n Second line",
			check: func(t *testing.T, ref *Ref) {
				if ref.Metadata != "Original content\nSecond line" {
					t.Errorf("Metadata = %q, want multiline content", ref.Metadata)
				}
			},
		},
		{
			name:  "ref with extension fields",
			input: `GitMsg-Ref: ext="social"; type="comment"; author="Bob"; email="bob@example.com"; time="2025-10-21T12:00:00Z"; ref="#commit:abc123456789"; v="0.1.0"`,
			check: func(t *testing.T, ref *Ref) {
				if ref.Fields["type"] != "comment" {
					t.Errorf("Fields[type] = %q, want comment", ref.Fields["type"])
				}
			},
		},
		{
			name:  "absolute repo ref",
			input: `GitMsg-Ref: ext="social"; author="Charlie"; email="c@example.com"; time="2025-10-21T12:00:00Z"; ref="https://github.com/user/repo#commit:abc123456789"; v="0.1.0"`,
			check: func(t *testing.T, ref *Ref) {
				if ref.Ref != "https://github.com/user/repo#commit:abc123456789" {
					t.Errorf("Ref = %q", ref.Ref)
				}
			},
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "wrong prefix",
			input:   `GitMsg: ext="social"; v="0.1.0"`,
			wantNil: true,
		},
		{
			name:    "missing author",
			input:   `GitMsg-Ref: ext="social"; email="a@b.com"; time="2025-01-01T00:00:00Z"; ref="#commit:abc123456789"; v="0.1.0"`,
			wantNil: true,
		},
		{
			name:    "missing email",
			input:   `GitMsg-Ref: ext="social"; author="Alice"; time="2025-01-01T00:00:00Z"; ref="#commit:abc123456789"; v="0.1.0"`,
			wantNil: true,
		},
		{
			name:    "missing time",
			input:   `GitMsg-Ref: ext="social"; author="Alice"; email="a@b.com"; ref="#commit:abc123456789"; v="0.1.0"`,
			wantNil: true,
		},
		{
			name:    "missing ref",
			input:   `GitMsg-Ref: ext="social"; author="Alice"; email="a@b.com"; time="2025-01-01T00:00:00Z"; v="0.1.0"`,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRefSection(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseRefSection() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseRefSection() = nil, want non-nil")
			}
			tt.check(t, got)
		})
	}
}

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
		check   func(t *testing.T, msg *Message)
	}{
		{
			name: "simple post",
			input: "Hello world!\n\n" +
				`GitMsg: ext="social"; type="post"; v="0.1.0"`,
			check: func(t *testing.T, msg *Message) {
				if msg.Content != "Hello world!" {
					t.Errorf("Content = %q", msg.Content)
				}
				if msg.Header.Ext != "social" {
					t.Errorf("Ext = %q", msg.Header.Ext)
				}
				if msg.Header.Fields["type"] != "post" {
					t.Errorf("type = %q", msg.Header.Fields["type"])
				}
				if len(msg.References) != 0 {
					t.Errorf("References = %d, want 0", len(msg.References))
				}
			},
		},
		{
			name: "multiline content",
			input: "First line\nSecond line\nThird line\n\n" +
				`GitMsg: ext="social"; type="post"; v="0.1.0"`,
			check: func(t *testing.T, msg *Message) {
				if msg.Content != "First line\nSecond line\nThird line" {
					t.Errorf("Content = %q", msg.Content)
				}
			},
		},
		{
			name: "message with one reference",
			input: "Replying to a post\n\n" +
				"GitMsg: ext=\"social\"; type=\"comment\"; v=\"0.1.0\"\n" +
				"GitMsg-Ref: ext=\"social\"; author=\"Alice\"; email=\"alice@example.com\"; " +
				"time=\"2025-10-21T12:00:00Z\"; ref=\"#commit:abc123456789\"; v=\"0.1.0\"",
			check: func(t *testing.T, msg *Message) {
				if msg.Content != "Replying to a post" {
					t.Errorf("Content = %q", msg.Content)
				}
				if msg.Header.Fields["type"] != "comment" {
					t.Errorf("type = %q", msg.Header.Fields["type"])
				}
				if len(msg.References) != 1 {
					t.Fatalf("References = %d, want 1", len(msg.References))
				}
				if msg.References[0].Ref != "#commit:abc123456789" {
					t.Errorf("Ref = %q", msg.References[0].Ref)
				}
			},
		},
		{
			name: "message with two references",
			input: "Quote with refs\n\n" +
				"GitMsg: ext=\"social\"; type=\"quote\"; v=\"0.1.0\"\n" +
				"GitMsg-Ref: ext=\"social\"; author=\"Bob\"; email=\"bob@example.com\"; " +
				"time=\"2025-10-21T10:00:00Z\"; ref=\"#commit:abc123456789\"; v=\"0.1.0\"\n" +
				"GitMsg-Ref: ext=\"social\"; author=\"Charlie\"; email=\"charlie@example.com\"; " +
				"time=\"2025-10-21T11:00:00Z\"; ref=\"#commit:def456789abc\"; v=\"0.1.0\"",
			check: func(t *testing.T, msg *Message) {
				if len(msg.References) != 2 {
					t.Fatalf("References = %d, want 2", len(msg.References))
				}
				if msg.References[0].Ref != "#commit:abc123456789" {
					t.Errorf("Ref[0] = %q", msg.References[0].Ref)
				}
				if msg.References[1].Ref != "#commit:def456789abc" {
					t.Errorf("Ref[1] = %q", msg.References[1].Ref)
				}
			},
		},
		{
			name: "reference with metadata",
			input: "Quote message\n\n" +
				"GitMsg: ext=\"social\"; type=\"quote\"; v=\"0.1.0\"\n" +
				"GitMsg-Ref: ext=\"social\"; author=\"Dave\"; email=\"dave@example.com\"; " +
				"time=\"2025-10-21T09:00:00Z\"; ref=\"#commit:abc123456789\"; v=\"0.1.0\"\n" +
				" Quoted content here",
			check: func(t *testing.T, msg *Message) {
				if len(msg.References) != 1 {
					t.Fatalf("References = %d, want 1", len(msg.References))
				}
				if msg.References[0].Metadata != "Quoted content here" {
					t.Errorf("Metadata = %q", msg.References[0].Metadata)
				}
			},
		},
		{
			name: "empty content",
			input: "\n" +
				`GitMsg: ext="social"; type="post"; v="0.1.0"`,
			check: func(t *testing.T, msg *Message) {
				if msg.Content != "" {
					t.Errorf("Content = %q, want empty", msg.Content)
				}
			},
		},
		{
			name:    "regular commit message",
			input:   "Just a regular commit message",
			wantNil: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "invalid header format",
			input:   "Content\n\nInvalid: format",
			wantNil: true,
		},
		{
			name:    "header missing required fields",
			input:   "Content\n\nGitMsg: ext=\"social\"",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMessage(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseMessage() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("ParseMessage() = nil, want non-nil")
			}
			tt.check(t, got)
		})
	}
}

func TestExtractCleanContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips header",
			input: "This is content\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"",
			want:  "This is content",
		},
		{
			name: "strips header and refs",
			input: "Content here\n\n" +
				"GitMsg: ext=\"social\"; type=\"comment\"; v=\"0.1.0\"\n" +
				"GitMsg-Ref: ext=\"social\"; ref=\"#commit:abc123456789\"; v=\"0.1.0\"",
			want: "Content here",
		},
		{
			name:  "no header",
			input: "Regular commit message",
			want:  "Regular commit message",
		},
		{
			name:  "empty content before header",
			input: "\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"",
			want:  "",
		},
		{
			name:  "multiline content",
			input: "Line 1\nLine 2\nLine 3\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"",
			want:  "Line 1\nLine 2\nLine 3",
		},
		{
			name:  "strips carriage returns",
			input: "Content\r\n\r\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"",
			want:  "Content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCleanContent(tt.input)
			if got != tt.want {
				t.Errorf("ExtractCleanContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseMessage_headerOnly(t *testing.T) {
	input := `GitMsg: ext="social"; type="post"; v="0.1.0"`
	got := ParseMessage(input)
	if got == nil {
		t.Fatal("ParseMessage() = nil, want non-nil")
	}
	if got.Content != "" {
		t.Errorf("Content = %q, want empty", got.Content)
	}
}

func TestParseRefSection_malformedFields(t *testing.T) {
	input := `GitMsg-Ref: ext social author Alice; v="0.1.0"`
	got := ParseRefSection(input)
	if got != nil {
		t.Errorf("ParseRefSection() should return nil for malformed fields")
	}
}
