// ranges_test.go - Tests for monthly date range utilities
package social

import (
	"testing"
	"time"
)

func TestGetMonthRange(t *testing.T) {
	tests := []struct {
		year      int
		month     time.Month
		wantStart string
		wantEnd   string
	}{
		{2025, time.January, "2025-01-01", "2025-01-31"},
		{2025, time.February, "2025-02-01", "2025-02-28"},
		{2024, time.February, "2024-02-01", "2024-02-29"}, // leap year
		{2025, time.March, "2025-03-01", "2025-03-31"},
		{2025, time.April, "2025-04-01", "2025-04-30"},
		{2025, time.December, "2025-12-01", "2025-12-31"},
	}

	for _, tt := range tests {
		t.Run(tt.wantStart, func(t *testing.T) {
			got := GetMonthRange(tt.year, tt.month)
			if got.Start != tt.wantStart {
				t.Errorf("Start = %q, want %q", got.Start, tt.wantStart)
			}
			if got.End != tt.wantEnd {
				t.Errorf("End = %q, want %q", got.End, tt.wantEnd)
			}
		})
	}
}

func TestInitialFetchMonths(t *testing.T) {
	months := InitialFetchMonths()
	if len(months) == 0 {
		t.Fatal("InitialFetchMonths() returned empty")
	}
	// Always includes current month
	now := time.Now()
	current := GetMonthRange(now.Year(), now.Month())
	if months[0].Start != current.Start {
		t.Errorf("First month start = %q, want %q", months[0].Start, current.Start)
	}
	// If before 15th, should include previous month too
	if now.Day() < 15 && len(months) != 2 {
		t.Errorf("Before 15th: len(months) = %d, want 2", len(months))
	}
	if now.Day() >= 15 && len(months) != 1 {
		t.Errorf("On/after 15th: len(months) = %d, want 1", len(months))
	}
}

func TestPreviousMonthRange_invalidInput(t *testing.T) {
	got := PreviousMonthRange("not-a-date")
	if got.Start == "" || got.End == "" {
		t.Error("PreviousMonthRange(invalid) should fall back to current time, not return empty")
	}
}

func TestNextMonthRange_invalidInput(t *testing.T) {
	got := NextMonthRange("not-a-date")
	if got.Start == "" || got.End == "" {
		t.Error("NextMonthRange(invalid) should fall back to current time, not return empty")
	}
}

func TestPreviousMonthRange(t *testing.T) {
	tests := []struct {
		input     string
		wantStart string
		wantEnd   string
	}{
		{"2025-03", "2025-02-01", "2025-02-28"},
		{"2025-01", "2024-12-01", "2024-12-31"},
		{"2024-03", "2024-02-01", "2024-02-29"}, // leap year
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := PreviousMonthRange(tt.input)
			if got.Start != tt.wantStart {
				t.Errorf("Start = %q, want %q", got.Start, tt.wantStart)
			}
			if got.End != tt.wantEnd {
				t.Errorf("End = %q, want %q", got.End, tt.wantEnd)
			}
		})
	}
}

func TestNextMonthRange(t *testing.T) {
	tests := []struct {
		input     string
		wantStart string
		wantEnd   string
	}{
		{"2025-01", "2025-02-01", "2025-02-28"},
		{"2025-12", "2026-01-01", "2026-01-31"},
		{"2024-01", "2024-02-01", "2024-02-29"}, // leap year
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NextMonthRange(tt.input)
			if got.Start != tt.wantStart {
				t.Errorf("Start = %q, want %q", got.Start, tt.wantStart)
			}
			if got.End != tt.wantEnd {
				t.Errorf("End = %q, want %q", got.End, tt.wantEnd)
			}
		})
	}
}

func TestFormatMonthDisplay(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2025-01", "Jan 2025"},
		{"2025-12", "Dec 2025"},
		{"2024-06", "Jun 2024"},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := FormatMonthDisplay(tt.input)
			if got != tt.want {
				t.Errorf("FormatMonthDisplay(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatMonthRangeDisplay(t *testing.T) {
	tests := []struct {
		name   string
		months []string
		want   string
	}{
		{"empty", nil, ""},
		{"single", []string{"2025-01"}, "Jan 2025"},
		{"range", []string{"2025-03", "2025-01"}, "Jan 2025 – Mar 2025"},
		{"cross year", []string{"2026-01", "2025-11"}, "Nov 2025 – Jan 2026"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMonthRangeDisplay(tt.months)
			if got != tt.want {
				t.Errorf("FormatMonthRangeDisplay() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCurrentYearMonth(t *testing.T) {
	got := CurrentYearMonth()
	if len(got) != 7 {
		t.Errorf("CurrentYearMonth() = %q, want YYYY-MM format", got)
	}
	if got[4] != '-' {
		t.Errorf("CurrentYearMonth() = %q, missing dash at pos 4", got)
	}
}

func TestYearMonthFromRange(t *testing.T) {
	tests := []struct {
		r    MonthRange
		want string
	}{
		{MonthRange{Start: "2025-01-01", End: "2025-01-31"}, "2025-01"},
		{MonthRange{Start: "2025-12-01", End: "2025-12-31"}, "2025-12"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := YearMonthFromRange(tt.r)
			if got != tt.want {
				t.Errorf("YearMonthFromRange() = %q, want %q", got, tt.want)
			}
		})
	}
}
