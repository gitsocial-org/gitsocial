// golden_test.go - Visual regression tests via golden file comparison
package test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

var updateGolden = flag.Bool("update", false, "update golden files")

// Patterns that change between runs and must be normalized for comparison
var (
	relTimeRe = regexp.MustCompile(`\b(just now|\d+[mhd] ago)\s*`)
	tmpDirRe  = regexp.MustCompile(`(?:\.\.\./|[\w./-]+/)tui-test-\d+\s*`)
	cacheSzRe = regexp.MustCompile(`Cache\s+·+\s+[\d.]+[KMGT]?B`)
)

// normalizeGolden replaces dynamic parts of rendered output with stable placeholders.
func normalizeGolden(s string) string {
	s = relTimeRe.ReplaceAllStringFunc(s, func(m string) string {
		repl := "TIME"
		mLen := len(m)
		if mLen > len(repl) {
			repl += strings.Repeat(" ", mLen-len(repl))
		}
		return repl[:mLen]
	})
	s = tmpDirRe.ReplaceAllStringFunc(s, func(m string) string {
		// Replace with fixed-length placeholder, preserving total width
		repl := ".../tui-test-FIXTURE"
		if len(repl) < len(m) {
			repl += strings.Repeat(" ", len(m)-len(repl))
		}
		return repl[:len(m)]
	})
	s = cacheSzRe.ReplaceAllStringFunc(s, func(m string) string {
		// Preserve total display width (rune count, not byte count — · is multi-byte)
		mWidth := utf8.RuneCountInString(m)
		repl := "Cache  ········ XXXKB"
		rWidth := utf8.RuneCountInString(repl)
		if rWidth < mWidth {
			repl += strings.Repeat(" ", mWidth-rWidth)
		} else if rWidth > mWidth {
			repl = string([]rune(repl)[:mWidth])
		}
		return repl
	})
	// Trim trailing whitespace from each line (views may pad beyond terminal width)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// The terminal size every golden route is rendered at.
const (
	goldenCols  = 120
	goldenLines = 40
)

func TestGolden(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)
	h.SetSize(goldenCols, goldenLines)

	views := []struct {
		name string
		loc  tuicore.Location
	}{
		{"timeline_120x40", tuicore.Location{Path: "/social/timeline"}},
		{"board_120x40", tuicore.Location{Path: "/pm/board"}},
		{"issues_120x40", tuicore.Location{Path: "/pm/issues"}},
		{"pr_list_120x40", tuicore.Location{Path: "/review/prs"}},
		{"releases_120x40", tuicore.Location{Path: "/release/list"}},
		{"settings_120x40", tuicore.Location{Path: "/settings"}},
		{"help_120x40", tuicore.Location{Path: "/help"}},
		{"memo_personal_120x40", tuicore.Location{Path: "/memo/personal"}},
		{"memo_session_120x40", tuicore.Location{Path: "/memo/session"}},
		{"memo_session_items_120x40", tuicore.LocMemoSessionItems(fixtureSessionID)},
		{"memo_inherited_120x40", tuicore.Location{Path: "/memo/inherited"}},
		{"memo_inherits_120x40", tuicore.Location{Path: "/memo/inherits"}},
	}

	for _, v := range views {
		t.Run(v.name, func(t *testing.T) {
			h.NavigateTo(v.loc)
			frame := h.Rendered()
			assertFrameFits(t, frame, goldenCols, goldenLines)
			got := normalizeGolden(stripANSI(frame))
			golden := filepath.Join("testdata", v.name+".golden")

			if *updateGolden {
				if err := os.MkdirAll("testdata", 0755); err != nil {
					t.Fatalf("mkdir testdata: %v", err)
				}
				if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated %s", golden)
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Skipf("golden file %s not found — run with -update to create", golden)
				return
			}

			wantNorm := normalizeGolden(string(want))
			if wantNorm != got {
				wantLines := strings.Split(wantNorm, "\n")
				gotLines := strings.Split(got, "\n")
				diff := diffLines(wantLines, gotLines)
				t.Errorf("golden file mismatch for %s:\n%s\nRun with -update to regenerate", golden, diff)
			}
		})
	}

	t.Run("LayoutProperties", func(t *testing.T) {
		fullTierOnly(t)
		sizes := []struct {
			name   string
			width  int
			height int
		}{
			{"120x40", 120, 40},
			{"80x24", 80, 24},
			{"200x60", 200, 60},
		}
		for _, size := range sizes {
			t.Run(size.name, func(t *testing.T) {
				h.SetSize(size.width, size.height)
				for _, meta := range tuicore.AllViewMetas() {
					t.Run(meta.Path, func(t *testing.T) {
						h.Navigate(meta.Path)
						out := h.Rendered()
						assertNotEmpty(t, out)
						assertFrameFits(t, out, size.width, size.height)
					})
				}
			})
		}
	})
}

// The overflow that shipped: a styled nav panel one column past a 120-column terminal.
func TestGoldenWidth_rejectsNavOneColumnTooWide(t *testing.T) {
	frame := func(navCols int) string {
		lines := make([]string, goldenLines)
		for i := range lines {
			lines[i] = strings.Repeat(" ", goldenCols)
		}
		lines[1] = "\x1b[34m│\x1b[0m" + strings.Repeat("─", navCols-2) + "\x1b[34m┐\x1b[0m"
		return strings.Join(lines, "\n")
	}
	if over := frameOverflow(frame(goldenCols+1), goldenCols); len(over) != 1 {
		t.Errorf("a nav row of %d columns: got %d overflow reports, want 1: %v", goldenCols+1, len(over), over)
	}
	if over := frameOverflow(frame(goldenCols), goldenCols); len(over) != 0 {
		t.Errorf("a nav row of %d columns: got %d overflow reports, want 0: %v", goldenCols, len(over), over)
	}
}

// A fixture commit keeps its relative-time label however long a run takes.
func TestGoldenTime_labelHoldsForALongRun(t *testing.T) {
	now := time.Now()
	want := tuicore.FormatTime(now.Add(-fixtureCommitAge))
	for _, elapsed := range []time.Duration{time.Minute, time.Hour, 6 * time.Hour} {
		if got := tuicore.FormatTime(now.Add(-fixtureCommitAge - elapsed)); got != want {
			t.Errorf("a fixture commit renders %q after %s, want %q", got, elapsed, want)
		}
	}
}

// diffLines produces a simple diff between two line slices, showing up to 20 differences.
func diffLines(want, got []string) string {
	var b strings.Builder
	maxLen := len(want)
	if len(got) > maxLen {
		maxLen = len(got)
	}
	shown := 0
	for i := 0; i < maxLen && shown < 20; i++ {
		var w, g string
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			fmt.Fprintf(&b, "  line %d:\n", i+1)
			b.WriteString("    -" + truncate(w, 100) + "\n")
			b.WriteString("    +" + truncate(g, 100) + "\n")
			shown++
		}
	}
	if shown == 0 {
		b.WriteString("  (files differ in length only)\n")
	}
	return b.String()
}
