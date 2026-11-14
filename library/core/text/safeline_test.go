// safeline_test.go - Tests for the safe line of a notification
package text

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSafeLine(t *testing.T) {
	data, err := os.ReadFile("../site/sitetest/parity_fixtures.json")
	if err != nil {
		t.Fatalf("read the parity fixture: %v", err)
	}
	var fixture struct {
		SafeLines []struct {
			In    string `json:"in"`
			Limit int    `json:"limit"`
			Out   string `json:"out"`
		} `json:"safeLines"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse the parity fixture: %v", err)
	}
	if len(fixture.SafeLines) == 0 {
		t.Fatal("no safe line cases in the parity fixture")
	}
	for _, c := range fixture.SafeLines {
		if got := SafeLine(c.In, c.Limit); got != c.Out {
			t.Errorf("SafeLine(%q, %d) = %q, want %q", c.In, c.Limit, got, c.Out)
		}
	}
	if got := SafeLine("a\xffb\xc3", 0); got != "a\uFFFDb\uFFFD" {
		t.Errorf("invalid bytes gave %q, want each replaced", got)
	}
}
