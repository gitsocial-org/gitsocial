// hash_test.go - Tests for hash normalization and validation
package protocol

import (
	"testing"
)

func TestNormalizeHash(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "full 40-char hash truncated to 12",
			input: "abc123def456789012345678901234567890abcd",
			want:  "abc123def456",
		},
		{
			name:  "already 12 chars",
			input: "abc123def456",
			want:  "abc123def456",
		},
		{
			name:  "uppercase normalized to lowercase",
			input: "ABC123DEF456",
			want:  "abc123def456",
		},
		{
			name:  "mixed case",
			input: "AbC123DeF456789",
			want:  "abc123def456",
		},
		{
			name:  "short hash (less than 12)",
			input: "abc123",
			want:  "abc123",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "non-hex characters",
			input:   "ghijklmnopqr",
			wantErr: true,
		},
		{
			name:    "hex with non-hex mixed",
			input:   "abc123xyz456",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeHash(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NormalizeHash(%q) error = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeHash(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeHash(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateHash(t *testing.T) {
	tests := []struct {
		hash string
		want bool
	}{
		{"abc123456789", true},
		{"ABC123456789", true},   // uppercase accepted
		{"AbC123dEf456", true},   // mixed case
		{"000000000000", true},   // all zeros
		{"ffffffffffff", true},   // all f's
		{"abc12345678", false},   // 11 chars - too short
		{"abc1234567890", false}, // 13 chars - too long
		{"ghijklmnopqr", false},  // non-hex
		{"abc123g56789", false},  // one non-hex char
		{"", false},              // empty
	}

	for _, tt := range tests {
		t.Run(tt.hash, func(t *testing.T) {
			got := ValidateHash(tt.hash)
			if got != tt.want {
				t.Errorf("ValidateHash(%q) = %v, want %v", tt.hash, got, tt.want)
			}
		})
	}
}
