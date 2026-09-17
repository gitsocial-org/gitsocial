// site_docs_conformance_test.go - Design gate: every chip variant, route, page kind and page version the site emits has its documented row.

package site

import (
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	siteDesignDocPath = "../../../documentation/STATIC-SITE-DESIGN.md"
	siteStaticDocPath = "../../../documentation/STATIC-SITE.md"
)

// siteDocsRead reads one documentation file.
func siteDocsRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// siteDocsAsset reads one embedded shell asset.
func siteDocsAsset(t *testing.T, name string) string {
	t.Helper()
	data, err := siteFiles.ReadFile("assets/" + name)
	if err != nil {
		t.Fatalf("read embedded assets/%s: %v", name, err)
	}
	return string(data)
}

// siteDocsGoSources concatenates the package's own non-test Go sources.
func siteDocsGoSources(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		b.Write(data)
	}
	return b.String()
}

// siteDocsSection returns the lines of the section opened by the given heading.
func siteDocsSection(t *testing.T, doc, heading string) []string {
	t.Helper()
	var section []string
	inSection := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "#") {
			if inSection {
				break
			}
			inSection = strings.TrimSpace(line) == heading
			continue
		}
		if inSection {
			section = append(section, line)
		}
	}
	if len(section) == 0 {
		t.Fatalf("no %q section, or its heading changed", heading)
	}
	return section
}

// siteDocsRowsWithPrefix returns the table rows whose first cell starts with the given text.
func siteDocsRowsWithPrefix(t *testing.T, doc, prefix string) []string {
	t.Helper()
	var rows []string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "| "+prefix) {
			rows = append(rows, line)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("no table row starting with %q, or the table changed", prefix)
	}
	return rows
}

// siteDocsTableRows returns the rows of the table opened by the given header line.
func siteDocsTableRows(t *testing.T, lines []string, header string) []string {
	t.Helper()
	var rows []string
	found := false
	for _, line := range lines {
		if !found {
			found = strings.TrimSpace(line) == header
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		rows = append(rows, line)
	}
	if len(rows) == 0 {
		t.Fatalf("no table under the header %q, or the header changed", header)
	}
	return rows
}

// siteDocsClassRe matches a class mention in the design doc's prose.
var siteDocsClassRe = regexp.MustCompile(`\.([a-z][\w-]*)`)

// siteDocsWordRe matches one lowercase word.
var siteDocsWordRe = regexp.MustCompile(`[a-z][a-z-]*`)

// siteDocsBacktickRe matches one backticked span.
var siteDocsBacktickRe = regexp.MustCompile("`([^`]+)`")

// siteDocsClassStringRe matches a class-list-shaped string literal.
var siteDocsClassStringRe = regexp.MustCompile("[\"'`]([a-z][a-z0-9 -]*)[\"'`]")

// siteDocsChipTokenRe matches a chip variant class token wherever it is spelled.
var siteDocsChipTokenRe = regexp.MustCompile(`\b(chip-[a-z][a-z0-9-]*|[a-z][a-z0-9-]*-chip)\b`)

// siteDocsGoChipRe matches the class of a chip the page layer builds.
var siteDocsGoChipRe = regexp.MustCompile(`sitePageChip\{Class: "([^"]*)"`)

// siteDocsStateClassesRe matches the app's solid-fill chip state table.
var siteDocsStateClassesRe = regexp.MustCompile(`const CHIP_STATE_CLASSES = \{([^}]*)\}`)

// siteDocsKeyRe matches one key of a JS object literal.
var siteDocsKeyRe = regexp.MustCompile(`([a-z][\w-]*)\s*:`)

// siteDocsQuotedKeyRe matches one quoted key of a JS object literal.
var siteDocsQuotedKeyRe = regexp.MustCompile(`"([^"]+)"\s*:`)

// siteDocsStateLiteralRe matches a bare state literal.
var siteDocsStateLiteralRe = regexp.MustCompile(`"([a-z-]+)"`)

// siteDocsRouteTypeRe matches a route descriptor's type.
var siteDocsRouteTypeRe = regexp.MustCompile(`type: "([a-z]+)"`)

// siteDocsDefineRe matches one page template definition.
var siteDocsDefineRe = regexp.MustCompile(`\{\{define "([a-z]+)"\}\}`)

// siteDocsVersionRowRe matches a page version row.
var siteDocsVersionRowRe = regexp.MustCompile(`^\| (\d+) \|`)

// siteDocsEmittedChipClasses collects every chip variant class the app and the page layer emit.
func siteDocsEmittedChipClasses(t *testing.T) []string {
	t.Helper()
	found := map[string]bool{}
	add := func(classes string) {
		fields := strings.Fields(classes)
		if !slices.Contains(fields, "chip") {
			return
		}
		for _, f := range fields {
			if f != "chip" {
				found[f] = true
			}
		}
	}
	for _, name := range []string{"gs-render.js", "gs-core.js"} {
		src := siteDocsAsset(t, name)
		for _, m := range siteDocsClassStringRe.FindAllStringSubmatch(src, -1) {
			add(m[1])
		}
		for _, m := range siteDocsChipTokenRe.FindAllStringSubmatch(src, -1) {
			found[m[1]] = true
		}
	}
	for _, m := range siteDocsGoChipRe.FindAllStringSubmatch(siteDocsGoSources(t), -1) {
		for _, f := range strings.Fields(m[1]) {
			found[f] = true
		}
	}
	if len(found) < 10 {
		t.Fatalf("found %d chip variant classes, too few to be reading the sources", len(found))
	}
	return slices.Sorted(maps.Keys(found))
}

// siteDocsChipStates collects the workflow states the app gives a chip class of its own.
func siteDocsChipStates(t *testing.T) []string {
	t.Helper()
	src := siteDocsAsset(t, "gs-core.js")
	table := siteDocsStateClassesRe.FindStringSubmatch(src)
	if table == nil {
		t.Fatal("gs-core.js: no CHIP_STATE_CLASSES table, or it was renamed")
	}
	matches := siteDocsKeyRe.FindAllStringSubmatch(table[1], -1)
	states := make([]string, 0, len(matches))
	for _, m := range matches {
		states = append(states, m[1])
	}
	body := siteDocsSlice(t, src, "function chipStateClass(", "\n  }")
	for _, m := range siteDocsStateLiteralRe.FindAllStringSubmatch(body, -1) {
		states = append(states, m[1])
	}
	return states
}

// siteDocsSlice returns the source between an opening marker and the first closing marker after it.
func siteDocsSlice(t *testing.T, src, open, closeMark string) string {
	t.Helper()
	start := strings.Index(src, open)
	if start < 0 {
		t.Fatalf("no %q in the source, or it was renamed", open)
	}
	rest := src[start:]
	end := strings.Index(rest, closeMark)
	if end < 0 {
		t.Fatalf("no %q after %q", closeMark, open)
	}
	return rest[:end]
}

// TestSiteDocsChipVariantsDocumented asserts every chip variant class both renderers emit has its row in the design doc.
func TestSiteDocsChipVariantsDocumented(t *testing.T) {
	rows := strings.Join(siteDocsRowsWithPrefix(t, siteDocsRead(t, siteDesignDocPath), "Chip"), "\n")
	documented := map[string]bool{}
	for _, m := range siteDocsClassRe.FindAllStringSubmatch(rows, -1) {
		documented[m[1]] = true
	}
	named := map[string]bool{}
	for _, w := range siteDocsWordRe.FindAllString(rows, -1) {
		named[w] = true
	}
	states := siteDocsChipStates(t)
	for _, state := range states {
		if !named[state] {
			t.Errorf("%s: chip state %q is not named in the chip rows", siteDesignDocPath, state)
		}
	}
	for _, class := range siteDocsEmittedChipClasses(t) {
		if documented[class] || slices.Contains(states, class) {
			continue
		}
		t.Errorf("%s: chip variant .%s is emitted but has no chip row", siteDesignDocPath, class)
	}
}

// TestSiteDocsRoutesAndPageKindsDocumented asserts every app route and page kind has its row in the design doc's route table.
func TestSiteDocsRoutesAndPageKindsDocumented(t *testing.T) {
	rows := siteDocsTableRows(t, siteDocsSection(t, siteDocsRead(t, siteDesignDocPath), "## Layout"), "| Route | Fragment | Page |")
	documented := map[string]bool{}
	for _, m := range siteDocsBacktickRe.FindAllStringSubmatch(strings.Join(rows, "\n"), -1) {
		for _, token := range strings.FieldsFunc(m[1], func(r rune) bool {
			return r == '/' || r == ':' || r == '@' || r == '<' || r == '#' || r == '.'
		}) {
			documented[token] = true
		}
	}
	for _, route := range siteDocsAppRoutes(t) {
		if !documented[route] {
			t.Errorf("%s: route %q is dispatched but has no route row", siteDesignDocPath, route)
		}
	}
	for _, kind := range siteDocsPageKinds(t) {
		if !documented[kind] {
			t.Errorf("%s: page kind %q is written but has no route row", siteDesignDocPath, kind)
		}
	}
}

// siteDocsAppRoutes collects every route parseRoute dispatches, index tabs included.
func siteDocsAppRoutes(t *testing.T) []string {
	t.Helper()
	src := siteDocsAsset(t, "gs-core.js")
	body := siteDocsSlice(t, src, "function parseRoute(", "\n  }\n")
	routes := map[string]bool{}
	for _, m := range siteDocsRouteTypeRe.FindAllStringSubmatch(body, -1) {
		routes[m[1]] = true
	}
	tabs := siteDocsSlice(t, src, "const INDEX_TABS = {", "}")
	for _, m := range siteDocsKeyRe.FindAllStringSubmatch(tabs, -1) {
		routes[m[1]] = true
	}
	for _, m := range siteDocsQuotedKeyRe.FindAllStringSubmatch(tabs, -1) {
		routes[m[1]] = true
	}
	if len(routes) < 15 {
		t.Fatalf("parsed %d routes from parseRoute, too few to be reading the dispatcher", len(routes))
	}
	return slices.Sorted(maps.Keys(routes))
}

// siteDocsPageKinds collects every page template the page layer renders whole pages from.
func siteDocsPageKinds(t *testing.T) []string {
	t.Helper()
	locs := siteDocsDefineRe.FindAllStringSubmatchIndex(sitePageTemplateText, -1)
	var kinds []string
	for i, loc := range locs {
		end := len(sitePageTemplateText)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if strings.Contains(sitePageTemplateText[loc[1]:end], `{{template "head"`) {
			kinds = append(kinds, sitePageTemplateText[loc[2]:loc[3]])
		}
	}
	if len(kinds) == 0 {
		t.Fatal("no page template renders the shared head, or the template set changed")
	}
	return kinds
}

// TestSiteDocsPageVersionHasEntry asserts the current sitePagesVersion has its entry in the page-keys section.
func TestSiteDocsPageVersionHasEntry(t *testing.T) {
	section := siteDocsSection(t, siteDocsRead(t, siteStaticDocPath), "### Page keys")
	versions := map[string]bool{}
	for _, line := range section {
		if m := siteDocsVersionRowRe.FindStringSubmatch(line); m != nil {
			versions[m[1]] = true
		}
	}
	if len(versions) == 0 {
		t.Fatalf("%s: no page version entries in the page-keys section", siteStaticDocPath)
	}
	if current := strconv.Itoa(sitePagesVersion); !versions[current] {
		t.Errorf("%s: page version %s has no entry in the page-keys section", siteStaticDocPath, current)
	}
}
