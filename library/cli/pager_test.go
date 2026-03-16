// pager_test.go - Tests for pager utility functions
package main

import (
	"testing"
)

func TestCountLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"hello", 1},
		{"hello\nworld", 2},
		{"a\nb\nc", 3},
		{"a\nb\nc\n", 4},
		{"\n", 2},
		{"\n\n", 3},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := countLines(tt.input)
			if got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
