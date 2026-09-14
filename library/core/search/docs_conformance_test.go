// docs_conformance_test.go - Asserts documentation/CLI.md names every inline
// search prefix the parser reads, in the parser's order.
package search

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const cliDocPath = "../../../documentation/CLI.md"

var docPrefixCell = regexp.MustCompile("^\\| `([^`]+)`")

// documentedPrefixes reads the inline prefix table of CLI.md's search section.
func documentedPrefixes(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(cliDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", cliDocPath, err)
	}
	var prefixes []string
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "#") {
			inSection = strings.TrimSpace(line) == "### gitsocial search"
			continue
		}
		if !inSection {
			continue
		}
		match := docPrefixCell.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if key, _, found := strings.Cut(match[1], ":"); found {
			prefixes = append(prefixes, key)
		}
	}
	if len(prefixes) == 0 {
		t.Fatalf("%s: no inline prefixes parsed from the search section, or the layout changed", cliDocPath)
	}
	return prefixes
}

// TestSearchPrefixesMatchDocumentation pins the parser's prefix table to CLI.md, in both directions.
func TestSearchPrefixesMatchDocumentation(t *testing.T) {
	if documented := documentedPrefixes(t); !slices.Equal(documented, filterPrefixes) {
		t.Errorf("%s documents %v, the parser reads %v", cliDocPath, documented, filterPrefixes)
	}
}
