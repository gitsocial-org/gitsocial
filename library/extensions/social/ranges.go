// ranges.go - Monthly date range utilities for unfollowed repo fetching
package social

import (
	"fmt"
	"time"
)

type MonthRange struct {
	Start string // "2006-01-02"
	End   string // "2006-01-02"
}

// GetMonthRange returns the first and last day of a given month.
func GetMonthRange(year int, month time.Month) MonthRange {
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	return MonthRange{
		Start: first.Format("2006-01-02"),
		End:   last.Format("2006-01-02"),
	}
}

// InitialFetchMonths returns month ranges for initial unfollowed repo fetch.
// Returns current month, plus previous month if today is before the 15th.
func InitialFetchMonths() []MonthRange {
	now := time.Now()
	year, month, day := now.Year(), now.Month(), now.Day()
	var months []MonthRange
	months = append(months, GetMonthRange(year, month))
	if day < 15 {
		prevYear, prevMonth := year, month-1
		if prevMonth < 1 {
			prevMonth = 12
			prevYear--
		}
		months = append(months, GetMonthRange(prevYear, prevMonth))
	}
	return months
}

// PreviousMonthRange returns the month range before the given YYYY-MM string.
func PreviousMonthRange(yearMonth string) MonthRange {
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		t = time.Now()
	}
	prevYear, prevMonth := t.Year(), t.Month()-1
	if prevMonth < 1 {
		prevMonth = 12
		prevYear--
	}
	return GetMonthRange(prevYear, prevMonth)
}

// NextMonthRange returns the month range after the given YYYY-MM string.
func NextMonthRange(yearMonth string) MonthRange {
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		t = time.Now()
	}
	nextYear, nextMonth := t.Year(), t.Month()+1
	if nextMonth > 12 {
		nextMonth = 1
		nextYear++
	}
	return GetMonthRange(nextYear, nextMonth)
}

// FormatMonthDisplay formats "2006-01" as "Jan 2006".
func FormatMonthDisplay(yearMonth string) string {
	t, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return yearMonth
	}
	return t.Format("Jan 2006")
}

// FormatMonthRangeDisplay formats a slice of months for display.
// Single month: "Jan 2026"
// Multiple months: "Nov 2025 – Jan 2026"
func FormatMonthRangeDisplay(months []string) string {
	if len(months) == 0 {
		return ""
	}
	if len(months) == 1 {
		return FormatMonthDisplay(months[0])
	}
	oldest := months[len(months)-1]
	newest := months[0]
	return fmt.Sprintf("%s – %s", FormatMonthDisplay(oldest), FormatMonthDisplay(newest))
}

// CurrentYearMonth returns the current month formatted as "2006-01".
func CurrentYearMonth() string {
	return time.Now().Format("2006-01")
}

// YearMonthFromRange extracts the year-month string from a MonthRange start date.
func YearMonthFromRange(r MonthRange) string {
	return r.Start[:7]
}
