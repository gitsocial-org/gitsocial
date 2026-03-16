// types_test.go - Tests for protocol type helpers
package protocol

import (
	"testing"
)

func TestIsMessageType(t *testing.T) {
	tests := []struct {
		name    string
		header  *Header
		ext     string
		msgType string
		want    bool
	}{
		{
			name:    "matching social post",
			header:  &Header{Ext: "social", Fields: map[string]string{"type": "post"}},
			ext:     "social",
			msgType: "post",
			want:    true,
		},
		{
			name:    "matching social comment",
			header:  &Header{Ext: "social", Fields: map[string]string{"type": "comment"}},
			ext:     "social",
			msgType: "comment",
			want:    true,
		},
		{
			name:    "wrong extension",
			header:  &Header{Ext: "pm", Fields: map[string]string{"type": "post"}},
			ext:     "social",
			msgType: "post",
			want:    false,
		},
		{
			name:    "wrong type",
			header:  &Header{Ext: "social", Fields: map[string]string{"type": "comment"}},
			ext:     "social",
			msgType: "post",
			want:    false,
		},
		{
			name:    "nil header",
			header:  nil,
			ext:     "social",
			msgType: "post",
			want:    false,
		},
		{
			name:    "missing type field",
			header:  &Header{Ext: "social", Fields: map[string]string{}},
			ext:     "social",
			msgType: "post",
			want:    false,
		},
		{
			name:    "pm extension match",
			header:  &Header{Ext: "pm", Fields: map[string]string{"type": "issue"}},
			ext:     "pm",
			msgType: "issue",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMessageType(tt.header, tt.ext, tt.msgType)
			if got != tt.want {
				t.Errorf("IsMessageType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitSubjectBody(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantSubject string
		wantBody    string
	}{
		{"subject only", "Fix login bug", "Fix login bug", ""},
		{"subject and body", "Fix login bug\n\nDetailed description", "Fix login bug", "Detailed description"},
		{"empty", "", "", ""},
		{"whitespace", "  Subject  \n  Body  ", "Subject", "Body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, body := SplitSubjectBody(tt.content)
			if subject != tt.wantSubject {
				t.Errorf("subject = %q, want %q", subject, tt.wantSubject)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}
