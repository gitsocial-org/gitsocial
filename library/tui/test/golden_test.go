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
	"unicode/utf8"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
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

func TestGolden(t *testing.T) {
	f := getFixture(t)
	h := New(t, f.Workdir, f.CacheDir)
	h.SetSize(120, 40)

	views := []struct {
		name string
		path string
	}{
		{"timeline_120x40", "/social/timeline"},
		{"board_120x40", "/pm/board"},
		{"issues_120x40", "/pm/issues"},
		{"pr_list_120x40", "/review/prs"},
		{"releases_120x40", "/release/list"},
		{"settings_120x40", "/settings"},
		{"help_120x40", "/help"},
	}

	for _, v := range views {
		t.Run(v.name, func(t *testing.T) {
			h.Navigate(v.path)
			got := normalizeGolden(stripANSI(h.Rendered()))
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
						assertLineCount(t, out, size.height)
					})
				}
			})
		}
	})
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
