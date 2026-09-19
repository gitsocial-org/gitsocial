// resolved_test.go - the message aliases every extension's resolved view projects
package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolvedViews_projectRawMessage holds the five extension schemas to the raw_message alias ResolvedCommonColumns scans.
func TestResolvedViews_projectRawMessage(t *testing.T) {
	if !strings.Contains(ResolvedCommonColumns, "v.raw_message") {
		t.Fatalf("ResolvedCommonColumns selects no raw_message: %s", ResolvedCommonColumns)
	}
	for _, ext := range []string{"memo", "pm", "release", "review", "social"} {
		path := filepath.Join("..", "..", "extensions", ext, "schema.go")
		schema, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(schema), "AS raw_message") {
			t.Errorf("%s: the resolved view projects no raw_message", path)
		}
		if strings.Contains(string(schema), "original_message") {
			t.Errorf("%s: the resolved view still projects original_message", path)
		}
	}
}
