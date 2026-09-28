// view_help_test.go - the embedded help copies match their documentation sources.
package tuiviews

import (
	"os"
	"testing"
)

// TestHelpCopiesMatchSource fails when an embedded copy drifts from documentation/; run go generate ./library/tui/tuiviews to refresh.
func TestHelpCopiesMatchSource(t *testing.T) {
	for _, c := range []struct{ src, name, embedded string }{
		{"../../../documentation/TUI-HELP.md", "help.md", helpContent},
		{"../../../documentation/TUI-KEYS.md", "help_keys.md", helpKeysContent},
	} {
		src, err := os.ReadFile(c.src)
		if err != nil {
			t.Fatalf("read %s: %v", c.src, err)
		}
		if string(src) != c.embedded {
			t.Errorf("%s drifted from %s; run go generate ./library/tui/tuiviews", c.name, c.src)
		}
	}
}
