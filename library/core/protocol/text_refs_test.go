// text_refs_test.go - Tests for the free-text ref and mention tokenizer
package protocol

import (
	"strings"
	"testing"
)

// refTexts joins the refs FindRefs finds, in order.
func refTexts(text string) string {
	refs := FindRefs(text)
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Ref)
	}
	return strings.Join(out, " ")
}

// TestFindRefs_list: a list ref tokenizes beside the other four types.
func TestFindRefs_list(t *testing.T) {
	text := "See #list:reading, #commit:abc123def456@gitmsg/pm, #branch:feature/x, #tag:v1.2.0 and #file:docs/A.md@main:L3-9."
	want := "#list:reading #commit:abc123def456@gitmsg/pm #branch:feature/x #tag:v1.2.0 #file:docs/A.md@main:L3-9"
	if got := refTexts(text); got != want {
		t.Errorf("refs = %q, want %q", got, want)
	}
}

// TestFindRefs_trailingPunctuation: a period or comma after a ref stays outside it, a ref of periods alone is none, and the span ends with the ref.
func TestFindRefs_trailingPunctuation(t *testing.T) {
	text := "Done in #tag:v1.2.0., then #commit:abc123def456@main., not #tag:. or #branch:.."
	spans := FindRefs(text)
	if len(spans) != 2 || spans[0].Ref != "#tag:v1.2.0" || spans[1].Ref != "#commit:abc123def456@main" {
		t.Fatalf("spans = %+v", spans)
	}
	for _, s := range spans {
		if text[s.Start:s.End] != s.Ref {
			t.Errorf("span %d:%d reads %q, want %q", s.Start, s.End, text[s.Start:s.End], s.Ref)
		}
	}
}

// TestFindRefs_boundaries: a ref needs a boundary before it, and one inside a URL or a markdown link is not a local ref.
func TestFindRefs_boundaries(t *testing.T) {
	cases := map[string]string{
		"word#commit:abc1234 ok":                                           "",
		"see https://forge.example/x#commit:abc1234 now":                   "",
		"[fix #commit:abc1234](https://example.com) and (#commit:abc1234)": "#commit:abc1234",
		"![#tag:v1](img.png) #tag:v2":                                      "#tag:v2",
		"#commit:abc123 #commit:ABC1234 #file:README.md":                   "",
		"fix\u00a0#tag:v1 and\t#tag:v2":                                    "#tag:v2",
	}
	for text, want := range cases {
		if got := refTexts(text); got != want {
			t.Errorf("%q: refs = %q, want %q", text, got, want)
		}
	}
}

// TestFindMentions: a mention is @email at a boundary, as written.
func TestFindMentions(t *testing.T) {
	text := "Ping @alice@example.com and @Bob@Example.ORG, not alice@example.com nor @@x@y.io."
	spans := FindMentions(text)
	if len(spans) != 2 || spans[0].Email != "alice@example.com" || spans[1].Email != "Bob@Example.ORG" {
		t.Fatalf("spans = %+v", spans)
	}
	if text[spans[0].Start:spans[0].End] != "@alice@example.com" {
		t.Errorf("span reads %q, want the mention with its @", text[spans[0].Start:spans[0].End])
	}
}
