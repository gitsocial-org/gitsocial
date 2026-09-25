// site_parity_test.go - writer/reader parity invariants: subject/header, the feedback card, the release head and the detail head.
// The Go writer's subjectOf / extractHeaderLine (site_items.go) and the JS
// reader's cleanContent / parseGitmsg (site/gs-core.js) must derive the same
// subject and GitMsg header line from a commit message. This test pins the Go
// half against the SAME shared fixtures (sitetest/parity_fixtures.json) the JS
// unit half (sitetest/unit_parity.js) asserts, so the two implementations are
// checked against one ground truth on the hard cases: a gpgsig-bearing commit,
// a CRLF-line-ending commit, and the empty-subject "body starts with GitMsg: "
// case.

package site

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// parityMessageCase pins the subject/header expected from a raw commit message.
type parityMessageCase struct {
	Name          string `json:"name"`
	Message       string `json:"message"`
	ExpectSubject string `json:"expectSubject"`
	ExpectHeader  string `json:"expectHeader"`
}

// parityRawObjectCase pins the subject/header expected from a full loose-object
// commit text (header/message split included).
type parityRawObjectCase struct {
	Name          string `json:"name"`
	CommitText    string `json:"commitText"`
	ExpectSubject string `json:"expectSubject"`
	ExpectHeader  string `json:"expectHeader"`
}

// parityFeedbackCase pins the verdict and anchor label a feedback card's header yields.
type parityFeedbackCase struct {
	Name          string            `json:"name"`
	Header        map[string]string `json:"header"`
	ExpectVerdict string            `json:"expectVerdict"`
	ExpectAnchor  string            `json:"expectAnchor"`
}

// parityReleaseHeadCase pins the subject and version chip a release head yields.
type parityReleaseHeadCase struct {
	Name              string            `json:"name"`
	Header            map[string]string `json:"header"`
	FirstLine         string            `json:"firstLine"`
	ExpectSubject     string            `json:"expectSubject"`
	ExpectVersionChip string            `json:"expectVersionChip"`
}

// parityChip pins one head chip's class and label.
type parityChip struct {
	Class string `json:"class"`
	Label string `json:"label"`
}

// parityDetailHeadCase pins the subject and chips one item type's detail head yields.
type parityDetailHeadCase struct {
	Name          string            `json:"name"`
	Ext           string            `json:"ext"`
	Header        map[string]string `json:"header"`
	Retracted     bool              `json:"retracted"`
	FirstLine     string            `json:"firstLine"`
	ExpectSubject string            `json:"expectSubject"`
	ExpectChips   []parityChip      `json:"expectChips"`
}

// parityAuthorCase pins the label and title an author bit yields.
type parityAuthorCase struct {
	Name        string `json:"name"`
	Author      string `json:"author"`
	Email       string `json:"email"`
	ExpectLabel string `json:"expectLabel"`
	ExpectTitle string `json:"expectTitle"`
}

// parityMetaRow pins the meta row skeleton: its bit classes in order, the edited row's, and its author cases.
type parityMetaRow struct {
	Bits       []string           `json:"bits"`
	EditedBits []string           `json:"editedBits"`
	Authors    []parityAuthorCase `json:"authors"`
}

// parityDefaultTitle pins the name a site with no configured title takes.
type parityDefaultTitle struct {
	Name        string `json:"name"`
	Base        string `json:"base"`
	ExpectTitle string `json:"expectTitle"`
}

// parityRowGlyphCase pins the type glyph class and title a row carries.
type parityRowGlyphCase struct {
	Name        string            `json:"name"`
	Ext         string            `json:"ext"`
	Header      map[string]string `json:"header"`
	ExpectClass string            `json:"expectClass"`
	ExpectTitle string            `json:"expectTitle"`
}

// parityReleaseRowCase pins the asset count a release row carries.
type parityReleaseRowCase struct {
	Name        string `json:"name"`
	Artifacts   string `json:"artifacts"`
	ExpectLabel string `json:"expectLabel"`
}

// parityCardSkeletonCase pins the head slots one item type's list row fills; an empty ExpectHead is a body-only row, which leads its meta row with ExpectLead.
type parityCardSkeletonCase struct {
	Name       string            `json:"name"`
	Ext        string            `json:"ext"`
	Header     map[string]string `json:"header"`
	FirstLine  string            `json:"firstLine"`
	ExpectHead []string          `json:"expectHead"`
	ExpectLead []string          `json:"expectLead"`
}

// parityCardSkeleton pins the one card shape: its part order, its head's slot order, the body-only part order, and a case per item type.
type parityCardSkeleton struct {
	Parts         []string                 `json:"parts"`
	Head          []string                 `json:"head"`
	BodyOnlyParts []string                 `json:"bodyOnlyParts"`
	Cases         []parityCardSkeletonCase `json:"cases"`
}

// parityItemCounts pins the sidebar count per list for a set of top-level items.
type parityItemCounts struct {
	Items []struct {
		Ext       string `json:"ext"`
		Type      string `json:"type"`
		State     string `json:"state"`
		Subject   string `json:"subject"`
		Origin    string `json:"origin"`
		TS        int64  `json:"ts"`
		Retracted bool   `json:"retracted"`
	} `json:"items"`
	Expect map[string]int `json:"expect"`
}

// parityHomeRows pins how many root entries the home section shows before its chevron.
type parityHomeRows struct {
	Limit int `json:"limit"`
}

// parityBodyBlock pins one block a release body splits into: prose lines, or commit rows.
type parityBodyBlock struct {
	Lines []string        `json:"lines"`
	Notes []parityNoteRow `json:"notes"`
}

// parityNoteRow pins one commit row a release body yields.
type parityNoteRow struct {
	Hash string `json:"hash"`
	Text string `json:"text"`
}

// parityReleaseNotesCase pins the blocks one release body renders as.
type parityReleaseNotesCase struct {
	Name   string            `json:"name"`
	Body   string            `json:"body"`
	Expect []parityBodyBlock `json:"expect"`
}

// parityAssetRow pins one asset row: its name, its link and its kind chip.
type parityAssetRow struct {
	Name string `json:"name"`
	Href string `json:"href"`
	Chip string `json:"chip"`
}

// parityReleaseAssetsCase pins the asset rows one release header yields.
type parityReleaseAssetsCase struct {
	Name            string            `json:"name"`
	Header          map[string]string `json:"header"`
	ExpectArtifacts []parityAssetRow  `json:"expectArtifacts"`
	ExpectExtra     []parityAssetRow  `json:"expectExtra"`
	ExpectSignedBy  string            `json:"expectSignedBy"`
}

// parityMarkdownPath pins whether a path renders as prose on both surfaces.
type parityMarkdownPath struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	ExpectProse bool   `json:"expectProse"`
}

// parityMDXStripCase pins the source a prose or MDX document renders from, once a strip has run over it.
type parityMDXStripCase struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Expect string `json:"expect"`
}

// parityFixtures is the shared fixture file shape.
type parityFixtures struct {
	MessageCases   []parityMessageCase       `json:"messageCases"`
	RawObjectCases []parityRawObjectCase     `json:"rawObjectCases"`
	FeedbackCards  []parityFeedbackCase      `json:"feedbackCards"`
	ReleaseHeads   []parityReleaseHeadCase   `json:"releaseHeads"`
	DetailHeads    []parityDetailHeadCase    `json:"detailHeads"`
	RowHeads       []parityDetailHeadCase    `json:"rowHeads"`
	MetaRow        parityMetaRow             `json:"metaRow"`
	DefaultTitles  []parityDefaultTitle      `json:"defaultTitles"`
	RowGlyphs      []parityRowGlyphCase      `json:"rowGlyphs"`
	ReleaseRows    []parityReleaseRowCase    `json:"releaseRows"`
	ReleaseNotes   []parityReleaseNotesCase  `json:"releaseNotes"`
	ReleaseAssets  []parityReleaseAssetsCase `json:"releaseAssets"`
	CardSkeleton   parityCardSkeleton        `json:"cardSkeleton"`
	HomeRows       parityHomeRows            `json:"homeRows"`
	ItemCounts     parityItemCounts          `json:"itemCounts"`
	MarkdownPaths  []parityMarkdownPath      `json:"markdownPaths"`
	MDXStrip       []parityMDXStripCase      `json:"mdxStrip"`
	FrontMatter    []parityMDXStripCase      `json:"frontMatter"`
	ListEmpty      map[string]string         `json:"listEmpty"`
	ListHeadings   map[string]string         `json:"listHeadings"`
}

// loadParityFixtures reads the shared JSON fixtures the JS half also consumes.
func loadParityFixtures(t *testing.T) parityFixtures {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("sitetest", "parity_fixtures.json"))
	if err != nil {
		t.Fatalf("read parity fixtures: %v", err)
	}
	var f parityFixtures
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse parity fixtures: %v", err)
	}
	return f
}

// TestParitySubjectHeader asserts subjectOf / extractHeaderLine match the pinned
// expected values from a raw commit message.
func TestParitySubjectHeader(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.MessageCases) == 0 {
		t.Fatal("no message cases in parity fixtures")
	}
	for _, c := range f.MessageCases {
		t.Run(c.Name, func(t *testing.T) {
			if got := subjectOf(c.Message); got != c.ExpectSubject {
				t.Errorf("subjectOf = %q, want %q", got, c.ExpectSubject)
			}
			if got := extractHeaderLine(c.Message); got != c.ExpectHeader {
				t.Errorf("extractHeaderLine = %q, want %q", got, c.ExpectHeader)
			}
		})
	}
}

// TestParityListHeadings pins every list page's heading to the fixture the app's
// LIST_HEADINGS is checked against (unit_parity.js); the app heads two more routes.
func TestParityListHeadings(t *testing.T) {
	f := loadParityFixtures(t)
	lists := append(append([]sitePageList(nil), sitePageLists...), siteCommitsList)
	for _, list := range lists {
		tab := strings.TrimPrefix(list.Route, "/")
		if want, ok := f.ListHeadings[tab]; !ok || want != list.NavLabel {
			t.Errorf("%s: heading %q, fixture %q", tab, list.NavLabel, want)
		}
	}
}

// parityBitClasses lists a meta row's leading bit classes, the form both halves compare.
func parityBitClasses(bits []sitePageBit, n int) []string {
	out := make([]string, 0, n)
	for _, b := range bits[:min(n, len(bits))] {
		out = append(out, b.Class)
	}
	return out
}

// TestParityMetaRow asserts the page layer's meta rows lead with the skeleton
// unit_parity.js and verify_styles.js pin on the app's own side.
func TestParityMetaRow(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.MetaRow.Bits) == 0 || len(f.MetaRow.Authors) == 0 {
		t.Fatal("no meta row case in parity fixtures")
	}
	for _, c := range f.MetaRow.Authors {
		t.Run(c.Name, func(t *testing.T) {
			bit := sitePageAuthorBit(&sitePageMsg{Author: c.Author, Email: c.Email})
			if bit.Class != "author" || bit.Text != c.ExpectLabel || bit.Title != c.ExpectTitle {
				t.Errorf("author bit = %+v, want class author, text %q, title %q", bit, c.ExpectLabel, c.ExpectTitle)
			}
		})
	}
	msg := &sitePageMsg{Ext: "pm", SHA: strings.Repeat("b", 40), Short: strings.Repeat("b", 12), Message: "An issue", Header: &protocol.Header{Ext: "pm", Fields: map[string]string{"type": "issue"}}}
	it := &sitePageItem{Msg: msg, Resolved: msg}
	want := strings.Join(f.MetaRow.Bits, ",")
	if got := strings.Join(parityBitClasses(siteItemPageMeta(it), len(f.MetaRow.Bits)), ","); got != want {
		t.Errorf("item page meta bits = %q, want %q", got, want)
	}
	row := buildSiteListEntry(it, "issue")
	if got := strings.Join(parityBitClasses(row.Meta, len(f.MetaRow.Bits)), ","); got != want {
		t.Errorf("list row meta bits = %q, want %q", got, want)
	}
	if row.Meta[2].Href != "../i/"+msg.Short+".html" {
		t.Errorf("list row hash href = %q, want the item's own page", row.Meta[2].Href)
	}
}

// TestParityEditedMetaRow asserts an edited item carries the marker as a bit
// after the hash, on its item page and on its list row alike.
func TestParityEditedMetaRow(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.MetaRow.EditedBits) == 0 {
		t.Fatal("no edited meta row case in parity fixtures")
	}
	header := &protocol.Header{Ext: "pm", Fields: map[string]string{"type": "issue"}}
	msg := &sitePageMsg{Ext: "pm", SHA: strings.Repeat("c", 40), Short: strings.Repeat("c", 12), Message: "An issue", TS: 1700000000, Header: header}
	edit := &sitePageMsg{Ext: "pm", SHA: strings.Repeat("d", 40), Short: strings.Repeat("d", 12), Message: "An issue, edited", TS: 1700003600, Author: "Bob", Email: "bob@example.com", Header: header}
	it := &sitePageItem{Msg: msg, Resolved: edit, Edited: true}
	want := strings.Join(f.MetaRow.EditedBits, ",")
	for name, bits := range map[string][]sitePageBit{"item page": siteItemPageMeta(it), "list row": buildSiteListEntry(it, "issue").Meta} {
		if got := strings.Join(parityBitClasses(bits, len(f.MetaRow.EditedBits)), ","); got != want {
			t.Errorf("%s meta bits = %q, want %q", name, got, want)
		}
	}
	bit := sitePageEditedBit(it)
	if bit.Text != "edited by Bob" || bit.Title != sitePagePreciseTime(edit.TS) {
		t.Errorf("edited bit = %+v, want the editor's name and the edit's precise time", bit)
	}
}

// TestParityDefaultTitle asserts an unconfigured site takes the bucket name the
// app's own repoTitle derives, against the fixture unit_parity.js also asserts.
func TestParityDefaultTitle(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.DefaultTitles) == 0 {
		t.Fatal("no default title cases in parity fixtures")
	}
	for _, c := range f.DefaultTitles {
		t.Run(c.Name, func(t *testing.T) {
			if got := sitePageDefaultTitle(c.Base); got != c.ExpectTitle {
				t.Errorf("default title = %q, want %q", got, c.ExpectTitle)
			}
			if got := sitePageSiteFor(SiteCustomization{}, c.Base).Title; got != c.ExpectTitle {
				t.Errorf("stamped site title = %q, want %q", got, c.ExpectTitle)
			}
		})
	}
	if got := sitePageSiteFor(SiteCustomization{Title: "Thread Demo"}, "https://example.com/thread-demo/").Title; got != "Thread Demo" {
		t.Errorf("configured site title = %q, want the configured value", got)
	}
}

// TestParityListEmpty pins every list page's empty sentence to the fixture the
// app's LIST_EMPTY is checked against (unit_parity.js), and renders one.
func TestParityListEmpty(t *testing.T) {
	f := loadParityFixtures(t)
	lists := append(append([]sitePageList(nil), sitePageLists...), siteCommitsList)
	for _, list := range lists {
		tab := strings.TrimPrefix(list.Route, "/")
		if want, ok := f.ListEmpty[tab]; !ok || want != sitePageEmptyText(list) {
			t.Errorf("%s: empty %q, fixture %q", tab, sitePageEmptyText(list), want)
		}
	}
	d := siteChainedListPage(sitePageLists[0], nil, nil, 0, 0)
	d.Chrome = sitePageChrome{Title: "t", Base: "../"}
	page, err := renderSitePage("list", d)
	if err != nil {
		t.Fatalf("render list page: %v", err)
	}
	want := `<p class="empty">No issues in this repository.</p>`
	if !strings.Contains(string(page), want) {
		t.Errorf("an empty list page carries %s", want)
	}
}

// TestParityRowGlyph asserts a row's type glyph takes its class and title from
// the item's type, the extension default standing in for a header that names
// none, against the fixture unit_render_cards.js also asserts.
func TestParityRowGlyph(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.RowGlyphs) == 0 {
		t.Fatal("no row glyph cases in parity fixtures")
	}
	for _, c := range f.RowGlyphs {
		t.Run(c.Name, func(t *testing.T) {
			msg := &sitePageMsg{Ext: c.Ext, SHA: strings.Repeat("f", 40), Short: strings.Repeat("f", 12), Message: "A row", Header: &protocol.Header{Ext: c.Ext, Fields: c.Header}}
			it := &sitePageItem{Msg: msg, Resolved: msg}
			itemType := pageItemType(it)
			state := pageItemField(it, "state")
			if _, class := sitePageGlyph(itemType, state); class != c.ExpectClass {
				t.Errorf("glyph class = %q, want %q", class, c.ExpectClass)
			}
			if got := sitePageGlyphTitle(itemType, state); got != c.ExpectTitle {
				t.Errorf("glyph title = %q, want %q", got, c.ExpectTitle)
			}
		})
	}
}

// TestParityReleaseRow asserts a release row counts its assets where every
// other row links its hash, against the fixture unit_parity.js also asserts.
func TestParityReleaseRow(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.ReleaseRows) == 0 {
		t.Fatal("no release row cases in parity fixtures")
	}
	for _, c := range f.ReleaseRows {
		t.Run(c.Name, func(t *testing.T) {
			if got := siteReleaseAssetLabel(c.Artifacts); got != c.ExpectLabel {
				t.Errorf("asset label = %q, want %q", got, c.ExpectLabel)
			}
			fields := map[string]string{"type": "release", "tag": "v1.0", "artifacts": c.Artifacts}
			msg := &sitePageMsg{Ext: "release", SHA: strings.Repeat("e", 40), Short: strings.Repeat("e", 12), Message: "v1.0", TS: 1700000000, Header: &protocol.Header{Ext: "release", Fields: fields}}
			row := buildSiteListEntry(&sitePageItem{Msg: msg, Resolved: msg}, "release")
			var bits []string
			for _, b := range row.Meta {
				bits = append(bits, b.Class+"|"+b.Text)
			}
			want := []string{"author|unknown", "reltime|" + sitePageDate(msg.TS)}
			if c.ExpectLabel != "" {
				want = append(want, "|"+c.ExpectLabel)
			}
			if strings.Join(bits, ",") != strings.Join(want, ",") {
				t.Errorf("release row meta = %v, want %v", bits, want)
			}
		})
	}
}

// paritySlotRe matches a rendered card slot, which is a span or an anchor opening with a class.
var paritySlotRe = regexp.MustCompile(`<(?:span|a) class="([a-z-]+)`)

// parityCardSlots names a rendered card head's slots in order, the form both halves compare.
func parityCardSlots(head string) []string {
	found := paritySlotRe.FindAllStringSubmatch(head, -1)
	out := make([]string, 0, len(found))
	for _, m := range found {
		out = append(out, m[1])
	}
	return out
}

// parityHeadedCard asserts a headed row fills the head's slots in order, then the meta row.
func parityHeadedCard(t *testing.T, card string, c parityCardSkeletonCase, parts []string) {
	t.Helper()
	open, end := strings.Index(card, `<div class="card-head">`), strings.Index(card, "</div>")
	if open < 0 || end < open {
		t.Fatalf("no card head in %s", card)
	}
	if got := parityCardSlots(card[open:end]); strings.Join(got, ",") != strings.Join(c.ExpectHead, ",") {
		t.Errorf("head slots = %v, want %v", got, c.ExpectHead)
	}
	for i, part := range parts[:2] {
		at := strings.Index(card, `class="`+part+`"`)
		if at < 0 {
			t.Fatalf("no %s in %s", part, card)
		}
		if i > 0 && at < strings.Index(card, `class="`+parts[i-1]+`"`) {
			t.Errorf("%s comes before %s", part, parts[i-1])
		}
	}
}

// parityBodyOnlyCard asserts a body-only row takes no head, leads its meta row with the glyph and the marker, and stands on its first line.
func parityBodyOnlyCard(t *testing.T, card string, c parityCardSkeletonCase, parts []string) {
	t.Helper()
	if strings.Contains(card, `class="card-head"`) {
		t.Errorf("a body-only row carries a card head: %s", card)
	}
	want := append([]string{parts[0]}, c.ExpectLead...)
	got := parityCardSlots(card)
	if len(got) < len(want) || strings.Join(got[:len(want)], ",") != strings.Join(want, ",") {
		t.Errorf("row slots = %v, want them to lead with %v", got, want)
	}
	meta := strings.Index(card, `<span class="meta meta-lead">`)
	body := strings.Index(card, `<div class="`+parts[1]+`">`+c.FirstLine+`</div>`)
	if meta < 0 || body < meta {
		t.Errorf("want the meta row then the %s carrying %q: %s", parts[1], c.FirstLine, card)
	}
}

// TestParityCardSkeleton asserts a list row's markup is the one card shape
// unit_parity.js asserts on the app's own card: the head's slots in order and
// then the meta row, or no head at all on a body-only type.
func TestParityCardSkeleton(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.CardSkeleton.Cases) == 0 {
		t.Fatal("no card skeleton cases in parity fixtures")
	}
	for _, c := range f.CardSkeleton.Cases {
		t.Run(c.Name, func(t *testing.T) {
			msg := &sitePageMsg{
				Ext: c.Ext, SHA: strings.Repeat("a", 40), Short: strings.Repeat("a", 12), Message: c.FirstLine,
				Header: &protocol.Header{Ext: c.Ext, Fields: c.Header},
			}
			it := &sitePageItem{Msg: msg, Resolved: msg, Retracted: c.Header["retracted"] == "true"}
			entry := buildSiteListEntry(it, sitePageDefaultTypes[c.Ext])
			page, err := renderSitePage("entries", []sitePageListEntry{entry})
			if err != nil {
				t.Fatalf("render entries: %v", err)
			}
			if len(c.ExpectHead) == 0 {
				parityBodyOnlyCard(t, string(page), c, f.CardSkeleton.BodyOnlyParts)
				return
			}
			parityHeadedCard(t, string(page), c, f.CardSkeleton.Parts)
		})
	}
}

// TestParityHomeRows asserts the front page's row count against the fixture unit_parity.js also asserts.
func TestParityHomeRows(t *testing.T) {
	if f := loadParityFixtures(t); f.HomeRows.Limit != sitePagesHomeRows {
		t.Errorf("home rows = %d, fixture %d", sitePagesHomeRows, f.HomeRows.Limit)
	}
}

// TestParityItemCounts asserts the manifest's open counts against the fixture unit_parity.js renders through the app's own list views.
func TestParityItemCounts(t *testing.T) {
	f := loadParityFixtures(t)
	roots := map[string][]*sitePageItem{}
	for i, c := range f.ItemCounts.Items {
		fields := map[string]string{}
		for k, v := range map[string]string{"type": c.Type, "state": c.State, "origin-url": c.Origin} {
			if v != "" {
				fields[k] = v
			}
		}
		sha := fmt.Sprintf("%040d", i)
		msg := &sitePageMsg{Ext: c.Ext, SHA: sha, Short: sha[:12], Message: c.Subject, TS: c.TS, Header: &protocol.Header{Ext: c.Ext, Fields: fields}}
		roots[c.Ext] = append(roots[c.Ext], &sitePageItem{Msg: msg, Resolved: msg, Retracted: c.Retracted})
	}
	if got := siteItemCounts(roots); !maps.Equal(got, f.ItemCounts.Expect) {
		t.Errorf("item counts = %v, want %v", got, f.ItemCounts.Expect)
	}
}

// TestParityReleaseNotes asserts the page layer splits a release body into the
// blocks the app's own itemBodyBlocks yields (unit_parity.js).
func TestParityReleaseNotes(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.ReleaseNotes) == 0 {
		t.Fatal("no release note cases in parity fixtures")
	}
	for _, c := range f.ReleaseNotes {
		t.Run(c.Name, func(t *testing.T) {
			blocks := sitePageBodyBlocks(c.Body, true)
			if len(blocks) != len(c.Expect) {
				t.Fatalf("%d blocks, want %d", len(blocks), len(c.Expect))
			}
			for i, want := range c.Expect {
				if got := strings.Join(blocks[i].Lines, "\n"); got != strings.Join(want.Lines, "\n") {
					t.Errorf("block %d lines = %q, want %q", i, got, strings.Join(want.Lines, "\n"))
				}
				if len(blocks[i].Notes) != len(want.Notes) {
					t.Fatalf("block %d has %d rows, want %d", i, len(blocks[i].Notes), len(want.Notes))
				}
				for j, row := range want.Notes {
					if blocks[i].Notes[j].Hash != row.Hash || blocks[i].Notes[j].Text != row.Text {
						t.Errorf("block %d row %d = %q %q, want %q %q", i, j, blocks[i].Notes[j].Hash, blocks[i].Notes[j].Text, row.Hash, row.Text)
					}
				}
			}
		})
	}
}

// TestParityReleaseAssets asserts the page layer builds the asset rows the
// app's own releaseAssets yields, link for link (unit_parity.js).
func TestParityReleaseAssets(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.ReleaseAssets) == 0 {
		t.Fatal("no release asset cases in parity fixtures")
	}
	for _, c := range f.ReleaseAssets {
		t.Run(c.Name, func(t *testing.T) {
			fields := map[string]string{"type": "release"}
			for k, v := range c.Header {
				fields[k] = v
			}
			msg := &sitePageMsg{Ext: "release", Header: &protocol.Header{Ext: "release", Fields: fields}}
			assets := buildSiteReleaseAssets(&sitePageItem{Msg: msg, Resolved: msg})
			if assets == nil {
				t.Fatal("no asset block")
			}
			assertParityAssetRows(t, "artifact", assets.Artifacts, c.ExpectArtifacts)
			assertParityAssetRows(t, "extra", assets.Extra, c.ExpectExtra)
			if assets.SignedBy != c.ExpectSignedBy {
				t.Errorf("signed-by = %q, want %q", assets.SignedBy, c.ExpectSignedBy)
			}
		})
	}
}

// assertParityAssetRows compares one asset list against its fixture rows.
func assertParityAssetRows(t *testing.T, kind string, got []sitePageAsset, want []parityAssetRow) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d %s rows, want %d", len(got), kind, len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Href != want[i].Href || got[i].Chip != want[i].Chip {
			t.Errorf("%s row %d = %+v, want %+v", kind, i, got[i], want[i])
		}
	}
}

// TestParityMarkdownPaths asserts the page layer renders as prose the same
// paths the app's own isMarkdownPath accepts (unit_parity.js).
func TestParityMarkdownPaths(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.MarkdownPaths) == 0 {
		t.Fatal("no markdown path cases in parity fixtures")
	}
	for _, c := range f.MarkdownPaths {
		t.Run(c.Name, func(t *testing.T) {
			if markdown, _ := siteFileIsDocument(c.Path); markdown != c.ExpectProse {
				t.Errorf("%s renders as prose = %v, want %v", c.Path, markdown, c.ExpectProse)
			}
		})
	}
}

// TestParityMDXStrip asserts siteFileStripMDX drops the lines the app's own
// stripMDX drops, against the fixture unit_parity.js also asserts.
func TestParityMDXStrip(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.MDXStrip) == 0 {
		t.Fatal("no mdx strip cases in parity fixtures")
	}
	for _, c := range f.MDXStrip {
		t.Run(c.Name, func(t *testing.T) {
			if got := siteFileStripMDX(c.Source); got != c.Expect {
				t.Errorf("siteFileStripMDX = %q, want %q", got, c.Expect)
			}
		})
	}
}

// TestParityFrontMatter asserts siteStripFrontMatter drops the YAML block the
// app's own stripFrontMatter drops, against the fixture unit_parity.js asserts.
func TestParityFrontMatter(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.FrontMatter) == 0 {
		t.Fatal("no front matter cases in parity fixtures")
	}
	for _, c := range f.FrontMatter {
		t.Run(c.Name, func(t *testing.T) {
			if got := siteStripFrontMatter(c.Source); got != c.Expect {
				t.Errorf("siteStripFrontMatter = %q, want %q", got, c.Expect)
			}
		})
	}
}

// TestParityFeedbackCard asserts the feedback card's verdict class and anchor
// chip label match the fixture the app's own half (unit_parity.js) asserts.
func TestParityFeedbackCard(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.FeedbackCards) == 0 {
		t.Fatal("no feedback cases in parity fixtures")
	}
	for _, c := range f.FeedbackCards {
		t.Run(c.Name, func(t *testing.T) {
			msg := &sitePageMsg{Ext: "review", Header: &protocol.Header{Ext: "review", Fields: c.Header}}
			item := &sitePageItem{Msg: msg, Resolved: msg}
			if got := sitePageFeedbackVerdict(item); got != c.ExpectVerdict {
				t.Errorf("verdict = %q, want %q", got, c.ExpectVerdict)
			}
			if got := sitePageFeedbackAnchor(item); got != c.ExpectAnchor {
				t.Errorf("anchor = %q, want %q", got, c.ExpectAnchor)
			}
		})
	}
}

// TestParityFeedbackCardVariant asserts a feedback reply renders as the card
// variant both renderers build, with the verdict on the card and on a chip.
func TestParityFeedbackCardVariant(t *testing.T) {
	fields := map[string]string{"type": "feedback", "review-state": "changes-requested", "file": "notes.txt", "new-line": "2"}
	msg := &sitePageMsg{Ext: "review", Header: &protocol.Header{Ext: "review", Fields: fields}}
	reply := buildSiteReply(&sitePageItem{Msg: msg, Resolved: msg})
	if reply.Variant != "feedback verdict-changes-requested" {
		t.Errorf("variant = %q, want %q", reply.Variant, "feedback verdict-changes-requested")
	}
	want := []sitePageChip{{Class: "verdict-changes-requested", Label: "changes requested"}, {Label: "notes.txt:2"}}
	if len(reply.Chips) != len(want) {
		t.Fatalf("chips = %v, want %v", reply.Chips, want)
	}
	for i := range want {
		if reply.Chips[i] != want[i] {
			t.Errorf("chip %d = %v, want %v", i, reply.Chips[i], want[i])
		}
	}
}

// TestParityReleaseHead asserts the release head's subject and version chip against the fixture unit_parity.js also asserts.
func TestParityReleaseHead(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.ReleaseHeads) == 0 {
		t.Fatal("no release head cases in parity fixtures")
	}
	for _, c := range f.ReleaseHeads {
		t.Run(c.Name, func(t *testing.T) {
			msg := &sitePageMsg{Ext: "release", Header: &protocol.Header{Ext: "release", Fields: c.Header}}
			it := &sitePageItem{Msg: msg, Resolved: msg}
			subject := siteHeadSubject(pageItemType(it), pageItemField(it, "tag"), pageItemField(it, "version"), c.FirstLine)
			if subject != c.ExpectSubject {
				t.Errorf("subject = %q, want %q", subject, c.ExpectSubject)
			}
			chips := siteReleaseVersionChips(it, subject)
			var got string
			if len(chips) == 1 {
				got = chips[0].Label
			} else if len(chips) > 1 {
				t.Fatalf("chips = %v, want at most one", chips)
			}
			if got != c.ExpectVersionChip {
				t.Errorf("version chip = %q, want %q", got, c.ExpectVersionChip)
			}
			if got != "" && strings.Contains(subject, c.Header["version"]) {
				t.Errorf("subject %q already names version %q, so the chip %q repeats it", subject, c.Header["version"], got)
			}
		})
	}
}

// parityChipList formats a head's chips as "class|label" bits, the form both halves compare.
func parityChipList(chips []sitePageChip) []string {
	out := make([]string, 0, len(chips))
	for _, c := range chips {
		out = append(out, c.Class+"|"+c.Label)
	}
	return out
}

// TestParityDetailHead asserts every detail type's head subject and chips against the fixture unit_parity.js also asserts.
func TestParityDetailHead(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.DetailHeads) == 0 {
		t.Fatal("no detail head cases in parity fixtures")
	}
	for _, c := range f.DetailHeads {
		t.Run(c.Name, func(t *testing.T) {
			msg := &sitePageMsg{Ext: c.Ext, Header: &protocol.Header{Ext: c.Ext, Fields: c.Header}}
			it := &sitePageItem{Msg: msg, Resolved: msg, Retracted: c.Retracted}
			subject := siteHeadSubject(pageItemType(it), pageItemField(it, "tag"), pageItemField(it, "version"), c.FirstLine)
			if subject != c.ExpectSubject {
				t.Errorf("subject = %q, want %q", subject, c.ExpectSubject)
			}
			want := make([]string, 0, len(c.ExpectChips))
			for _, chip := range c.ExpectChips {
				want = append(want, chip.Class+"|"+chip.Label)
			}
			got := parityChipList(siteHeadChips(it, subject))
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("chips = %v, want %v", got, want)
			}
		})
	}
}

// TestParityRowHead asserts every row type's head subject and chips against the fixture unit_parity.js also asserts.
func TestParityRowHead(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.RowHeads) == 0 {
		t.Fatal("no row head cases in parity fixtures")
	}
	for _, c := range f.RowHeads {
		t.Run(c.Name, func(t *testing.T) {
			msg := &sitePageMsg{Ext: c.Ext, Header: &protocol.Header{Ext: c.Ext, Fields: c.Header}}
			it := &sitePageItem{Msg: msg, Resolved: msg, Retracted: c.Retracted}
			subject := siteHeadSubject(pageItemType(it), pageItemField(it, "tag"), pageItemField(it, "version"), c.FirstLine)
			if subject != c.ExpectSubject {
				t.Errorf("subject = %q, want %q", subject, c.ExpectSubject)
			}
			want := make([]string, 0, len(c.ExpectChips))
			for _, chip := range c.ExpectChips {
				want = append(want, chip.Class+"|"+chip.Label)
			}
			got := parityChipList(siteRowChips(it, subject))
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("chips = %v, want %v", got, want)
			}
		})
	}
}

// TestParityDetailHeadMarkup asserts the rendered detail head is one card head: an h1 subject, then the chip slot.
func TestParityDetailHeadMarkup(t *testing.T) {
	fields := map[string]string{"type": "pull-request", "state": "merged"}
	msg := &sitePageMsg{Ext: "review", SHA: strings.Repeat("a", 40), Short: strings.Repeat("a", 12), Message: "Expand notes with more lines", Header: &protocol.Header{Ext: "review", Fields: fields}}
	d := buildSiteItemPage(&sitePageItem{Msg: msg, Resolved: msg}, sitePageLists[1], sitePageSite{URL: "https://example.com/"}, "Expand notes with more lines")
	page, err := renderSitePage("item", d)
	if err != nil {
		t.Fatalf("render item page: %v", err)
	}
	want := `<div class="card-head"><h1 class="subject">Expand notes with more lines</h1> <span class="chip state merged">merged</span></div>`
	if !strings.Contains(string(page), want) {
		t.Errorf("detail head markup missing:\nwant %s\ngot %.900s", want, page)
	}
	if strings.Count(string(page), "<h1") != 1 {
		t.Errorf("an item page carries one h1, got %d", strings.Count(string(page), "<h1"))
	}
}

// TestParityReleasePageMarkup asserts a release page renders its commit rows as
// linked definition rows and its artifacts as the app's own asset list.
func TestParityReleasePageMarkup(t *testing.T) {
	fields := map[string]string{
		"type": "release", "tag": "v1.2.0", "artifacts": "gs-linux.tar.gz",
		"checksums": "SHA256SUMS", "artifact-url": "https://dl.example.com/v1.2.0",
	}
	body := "Ship the rewrite\n\nabc1234 Fix the walk\ndef5678 Add a retry budget"
	msg := &sitePageMsg{Ext: "release", SHA: strings.Repeat("a", 40), Short: strings.Repeat("a", 12), Message: body, Header: &protocol.Header{Ext: "release", Fields: fields}}
	d := buildSiteItemPage(&sitePageItem{Msg: msg, Resolved: msg}, sitePageLists[3], sitePageSite{URL: "https://example.com/"}, "v1.2.0")
	page, err := renderSitePage("item", d)
	if err != nil {
		t.Fatalf("render item page: %v", err)
	}
	for _, want := range []string{
		`<dl class="release-notes"><dt><a class="hash" href="https://example.com/index.html#commit:abc1234@">abc1234</a></dt><dd>Fix the walk</dd>`,
		`<dt><a class="hash" href="https://example.com/index.html#commit:def5678@">def5678</a></dt><dd>Add a retry budget</dd></dl>`,
		`<div class="assets"><div class="assets-head">Assets</div>`,
		`<a class="asset-row" href="https://dl.example.com/v1.2.0/gs-linux.tar.gz" rel="noopener"><span class="mono selectable">gs-linux.tar.gz</span></a>`,
		`<span class="mono selectable">SHA256SUMS</span><span class="chip">checksums</span>`,
	} {
		if !strings.Contains(string(page), want) {
			t.Errorf("release page markup missing:\nwant %s\ngot %s", want, page)
		}
	}
	if strings.Contains(string(page), "<pre>") {
		t.Error("a release page renders no preformatted artifact block")
	}
}

// TestParityRawObjectSplit asserts parseBucketCommit's header/message split
// (gpgsig blocks, CRLF) yields the same pinned subject and header line the JS
// reader's parseCommit derives.
func TestParityRawObjectSplit(t *testing.T) {
	f := loadParityFixtures(t)
	if len(f.RawObjectCases) == 0 {
		t.Fatal("no raw-object cases in parity fixtures")
	}
	for _, c := range f.RawObjectCases {
		t.Run(c.Name, func(t *testing.T) {
			// parseBucketCommit takes the object body (everything after the
			// "commit <size>\0" loose-object header); the fixture stores that
			// body verbatim as commitText.
			bc, err := parseBucketCommit("0000000000000000000000000000000000000000", []byte(c.CommitText))
			if err != nil {
				t.Fatalf("parseBucketCommit: %v", err)
			}
			if got := subjectOf(bc.item.Message); got != c.ExpectSubject {
				t.Errorf("subjectOf(parsed message) = %q, want %q", got, c.ExpectSubject)
			}
			if bc.item.Header != c.ExpectHeader {
				t.Errorf("parsed header line = %q, want %q", bc.item.Header, c.ExpectHeader)
			}
			if got := extractHeaderLine(bc.item.Message); got != c.ExpectHeader {
				t.Errorf("extractHeaderLine(parsed message) = %q, want %q", got, c.ExpectHeader)
			}
		})
	}
}
