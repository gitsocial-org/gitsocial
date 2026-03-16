// status_test.go - Tests for time formatting utilities
package social

import (
	"testing"
	"time"
)

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		input time.Time
		want  string
	}{
		{"just now", now.Add(-10 * time.Second), "just now"},
		{"1 minute ago", now.Add(-90 * time.Second), "1 minute ago"},
		{"5 minutes ago", now.Add(-5 * time.Minute), "5 minutes ago"},
		{"1 hour ago", now.Add(-90 * time.Minute), "1 hour ago"},
		{"3 hours ago", now.Add(-3 * time.Hour), "3 hours ago"},
		{"1 day ago", now.Add(-36 * time.Hour), "1 day ago"},
		{"5 days ago", now.Add(-5 * 24 * time.Hour), "5 days ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRelativeTime(tt.input)
			if got != tt.want {
				t.Errorf("FormatRelativeTime() = %q, want %q", got, tt.want)
			}
		})
	}
}
