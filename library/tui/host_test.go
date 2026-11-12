// host_test.go - Tests for layout calculation
package tui

import (
	"testing"
	"time"
)

func TestNewLayout_wide(t *testing.T) {
	l := NewLayout(120, 40)
	if l.Width != 120 {
		t.Errorf("Width = %d, want 120", l.Width)
	}
	if l.Height != 40 {
		t.Errorf("Height = %d, want 40", l.Height)
	}
	if l.NavWidth != 32 {
		t.Errorf("NavWidth = %d, want 32", l.NavWidth)
	}
	if l.ContentWidth != 88 {
		t.Errorf("ContentWidth = %d, want 88", l.ContentWidth)
	}
}

func TestNewLayout_narrow(t *testing.T) {
	l := NewLayout(79, 30)
	if l.NavWidth != 0 {
		t.Errorf("NavWidth = %d, want 0 for narrow terminal", l.NavWidth)
	}
	if l.ContentWidth != 79 {
		t.Errorf("ContentWidth = %d, want 79", l.ContentWidth)
	}
}

func TestNewLayout_exactBreakpoint(t *testing.T) {
	l := NewLayout(80, 30)
	if l.NavWidth != 32 {
		t.Errorf("NavWidth = %d, want 32 at width=80", l.NavWidth)
	}
	if l.ContentWidth != 48 {
		t.Errorf("ContentWidth = %d, want 48", l.ContentWidth)
	}
}

func TestLayout_ShowNav(t *testing.T) {
	wide := NewLayout(100, 30)
	if !wide.ShowNav() {
		t.Error("ShowNav() should be true for wide layout")
	}

	narrow := NewLayout(60, 30)
	if narrow.ShowNav() {
		t.Error("ShowNav() should be false for narrow layout")
	}
}

func TestNewLayout_contentWidthConsistency(t *testing.T) {
	for _, w := range []int{60, 79, 80, 100, 120, 200} {
		l := NewLayout(w, 30)
		if l.NavWidth+l.ContentWidth != w {
			t.Errorf("NavWidth(%d) + ContentWidth(%d) = %d, want %d", l.NavWidth, l.ContentWidth, l.NavWidth+l.ContentWidth, w)
		}
	}
}

func TestBackoffInterval_growth(t *testing.T) {
	for _, c := range []struct {
		base, streak int
		want         time.Duration
	}{
		{5, 0, 5 * time.Second},
		{5, 1, 6250 * time.Millisecond},
		{5, 4, 12207031250 * time.Nanosecond},
		{5, 27, autoFetchCeiling},
		{300, 1, 375 * time.Second},
		{300, 40, autoFetchCeiling},
		{0, 0, autoFetchCeiling},
	} {
		if got := backoffInterval(c.base, c.streak); got != c.want {
			t.Errorf("backoffInterval(%d, %d) = %v, want %v", c.base, c.streak, got, c.want)
		}
	}
	if got := backoffInterval(5, 26); got >= autoFetchCeiling || got < 25*time.Minute {
		t.Errorf("backoffInterval(5, 26) = %v, want the last step under the ceiling", got)
	}
}
