// review_test.go - Tests for CLI review helpers
package main

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/extensions/review"
)

func TestFormatTipStaleMarker(t *testing.T) {
	tests := []struct {
		name      string
		side      string
		storedTip string
		obs       *review.PRObservation
		want      string
	}{
		{
			name: "no observation → no marker",
			side: "head", storedTip: "abc123def456", obs: nil,
			want: "",
		},
		{
			name: "observation matches stored → no marker",
			side: "head", storedTip: "abc123def456",
			obs:  &review.PRObservation{HeadTip: "abc123def456", HeadExists: true, BaseExists: true},
			want: "",
		},
		{
			name: "observation diverges from stored → updated marker",
			side: "head", storedTip: "abc123def456",
			obs:  &review.PRObservation{HeadTip: "def456789012", HeadExists: true, BaseExists: true},
			want: "⚠ updated to #def456789012",
		},
		{
			name: "head deleted → deletion marker",
			side: "head", storedTip: "abc123def456",
			obs:  &review.PRObservation{HeadTip: "", HeadExists: false, BaseExists: true},
			want: "⚠ deleted on origin",
		},
		{
			name: "base side independent of head state",
			side: "base", storedTip: "111222333444",
			obs:  &review.PRObservation{BaseTip: "555666777888", BaseExists: true, HeadExists: true},
			want: "⚠ updated to #555666777888",
		},
		{
			name: "unknown side → no marker",
			side: "feet", storedTip: "abc",
			obs:  &review.PRObservation{HeadExists: true, BaseExists: true},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTipStaleMarker(tt.side, tt.storedTip, tt.obs)
			if tt.want == "" {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("got %q, want contains %q", got, tt.want)
			}
		})
	}
}
