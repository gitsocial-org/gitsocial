// fuzz_test.go - Fuzz targets for the GitMsg header, message and ref parsers
package protocol

import (
	"maps"
	"strings"
	"testing"
)

const (
	fuzzSeedHeaderPost    = `GitMsg: ext="social"; type="post"; v="0.1.0"`
	fuzzSeedHeaderEdit    = `GitMsg: ext="social"; edits="#commit:abc123456789@main"; retracted="true"; v="0.1.0"`
	fuzzSeedHeaderAccepts = `GitMsg: ext="pm"; type="issue"; edits="#commit:abc123456789@gitmsg/pm"; accepts="https://github.com/alice/fork#commit:def456789abc@gitmsg/pm"; v="0.1.0"`
	fuzzSeedHeaderOrigin  = `GitMsg: ext="pm"; type="issue"; origin-author-email="alice@example.com"; origin-author-name="Alice Smith"; origin-platform="github"; origin-time="2025-01-06T10:30:00Z"; origin-url="https://github.com/user/repo/issues/42"; state="open"; v="0.1.0"`
	fuzzSeedHeaderExtV    = `GitMsg: ext="review"; ext-v="0.2.0"; type="pr"; state="open"; labels="kind/feature,priority/high"; v="0.1.0"`
	fuzzSeedRefSection    = `GitMsg-Ref: ext="social"; type="comment"; author="Alice"; email="alice@example.com"; time="2025-10-21T12:00:00Z"; ref="#commit:abc123456789@main"; v="0.1.0"`
)

// isWrittenOriginField reports whether CreateHeader emits this origin field.
func isWrittenOriginField(key string) bool {
	switch key {
	case "origin-author-email", "origin-author-name", "origin-platform", "origin-time", "origin-url":
		return true
	}
	return false
}

// headerFieldsAfterWrite returns the fields CreateHeader carries into its output.
func headerFieldsAfterWrite(fields map[string]string) map[string]string {
	kept := make(map[string]string, len(fields))
	for key, value := range fields {
		// The writer emits retracted only for the value "true" (GITMSG.md 1.5).
		if key == "retracted" && value != "true" {
			continue
		}
		// The writer emits the five origin fields of GITMSG.md 1.9 and no other.
		if strings.HasPrefix(key, "origin-") && !isWrittenOriginField(key) {
			continue
		}
		kept[key] = value
	}
	return kept
}

// checkHeaderRoundTrip asserts that CreateHeader and ParseHeader agree on a parsed header.
func checkHeaderRoundTrip(t *testing.T, header *Header) {
	t.Helper()
	// GITMSG.md 1.2 requires ext and v on every accepted header.
	if header.Ext == "" || header.V == "" {
		t.Fatalf("ParseHeader() accepted a header with ext=%q v=%q", header.Ext, header.V)
	}
	again := ParseHeader(CreateHeader(*header))
	if again == nil {
		t.Fatalf("CreateHeader() output does not parse: %q", CreateHeader(*header))
	}
	if again.Ext != header.Ext || again.V != header.V {
		t.Errorf("round trip ext/v = %q/%q, want %q/%q", again.Ext, again.V, header.Ext, header.V)
	}
	// A re-formatted header parses back to the fields the writer emits.
	want := headerFieldsAfterWrite(header.Fields)
	if !maps.Equal(again.Fields, want) {
		t.Errorf("round trip fields = %v, want %v", again.Fields, want)
	}
}

// FuzzParseHeader checks the GitMsg trailer parser against CreateHeader.
func FuzzParseHeader(f *testing.F) {
	seeds := []string{
		fuzzSeedHeaderPost,
		fuzzSeedHeaderEdit,
		fuzzSeedHeaderAccepts,
		fuzzSeedHeaderOrigin,
		fuzzSeedHeaderExtV,
		fuzzSeedRefSection,
		`GitMsg: ext="social"; v=""`,
		`GitMsg: `,
		`GitMsg: ext="social"; unterminated="; v="0.1.0"`,
		`GitMsg: ext="a"; 9bad="x"; a:b="c"; _u="d"; v="1"`,
		"GitMsg: ext=\"a\"; v=\"1\"\n",
		"plain subject line",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		header := ParseHeader(line)
		if header == nil {
			return
		}
		checkHeaderRoundTrip(t, header)
	})
}

// FuzzParseMessage checks the commit message parser against FormatMessage.
func FuzzParseMessage(f *testing.F) {
	seeds := []string{
		"Subject line\n\n" + fuzzSeedHeaderPost,
		"Subject line\n\nBody paragraph.\n\n" + fuzzSeedHeaderPost + "\n" + fuzzSeedRefSection + "\n > quoted line\n >\n > second quoted line",
		fuzzSeedHeaderEdit,
		"Imported issue\n\n" + fuzzSeedHeaderOrigin,
		"Subject\n\n" + fuzzSeedHeaderExtV + "\n" + fuzzSeedRefSection + "\n" + fuzzSeedRefSection,
		"Subject\n\nGitMsg: no fields here\n" + fuzzSeedHeaderPost,
		"Subject\n\n" + fuzzSeedHeaderPost + "\nGitMsg-Ref: ext=\"social\"; v=\"0.1.0\"",
		"Subject\n\n" + fuzzSeedHeaderPost + "\nSigned-off-by: Alice <alice@example.com>",
		"a regular commit\n\nCloses: #commit:abc123456789@main",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, message string) {
		parsed := ParseMessage(message)
		if parsed == nil {
			return
		}
		checkHeaderRoundTrip(t, &parsed.Header)
		// GITMSG.md 1.3 requires ext, ref, v, author, email and time on every reference.
		for _, ref := range parsed.References {
			if ref.Ext == "" || ref.Ref == "" || ref.V == "" || ref.Author == "" || ref.Email == "" || ref.Time == "" {
				t.Fatalf("ParseMessage() accepted an incomplete reference: %+v", ref)
			}
		}
		// A re-formatted message parses back to the same content and references.
		again := ParseMessage(FormatMessage(parsed.Content, parsed.Header, parsed.References))
		if again == nil {
			t.Fatalf("FormatMessage() output does not parse: content %q, ext %q", parsed.Content, parsed.Header.Ext)
		}
		if again.Content != parsed.Content {
			t.Errorf("round trip content = %q, want %q", again.Content, parsed.Content)
		}
		if len(again.References) != len(parsed.References) {
			t.Fatalf("round trip references = %d, want %d", len(again.References), len(parsed.References))
		}
		for i, ref := range parsed.References {
			got := again.References[i]
			if got.Ext != ref.Ext || got.Ref != ref.Ref || got.V != ref.V {
				t.Errorf("round trip reference %d = %q %q %q, want %q %q %q", i, got.Ext, got.Ref, got.V, ref.Ext, ref.Ref, ref.V)
			}
			if got.Author != ref.Author || got.Email != ref.Email || got.Time != ref.Time {
				t.Errorf("round trip reference %d identity = %q %q %q, want %q %q %q", i, got.Author, got.Email, got.Time, ref.Author, ref.Email, ref.Time)
			}
			if got.Metadata != ref.Metadata {
				t.Errorf("round trip reference %d metadata = %q, want %q", i, got.Metadata, ref.Metadata)
			}
			if !maps.Equal(got.Fields, ref.Fields) {
				t.Errorf("round trip reference %d fields = %v, want %v", i, got.Fields, ref.Fields)
			}
		}
	})
}

// FuzzParseRef checks the reference parser against CreateRef.
func FuzzParseRef(f *testing.F) {
	seeds := []string{
		"https://github.com/user/repo#commit:abc123456789@main",
		"#commit:abc123456789@main",
		"git@github.com:user/repo#commit:ABC123DEF4567890@feature/x",
		"s3://s3.example.com/bucket/repo#commit:abc123456789@main",
		"https://github.com/user/repo#branch:main",
		"https://github.com/user/repo#tag:v1.0.0",
		"https://github.com/user/repo#list:reading",
		"https://github.com/user/repo#file:path/to/file.go@main",
		"https://github.com/user/repo#file:path/to/file.go@main:L10-20",
		"https://github.com/user/repo#file:path/to/file.go@main:v1.0.0",
		"#file:a@main:L5",
		"https://github.com/user/repo.git.git#branch:main",
		"#commit:short@main",
		"#branch:",
		"PROJ-123",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, ref string) {
		parsed := ParseRef(ref)
		// GITMSG.md 1.3 caps a commit value at 12 characters, and the parser lowercases it.
		if parsed.Type == RefTypeCommit {
			if len(parsed.Value) > 12 {
				t.Errorf("commit value = %q, want at most 12 characters", parsed.Value)
			}
			if parsed.Value != strings.ToLower(parsed.Value) {
				t.Errorf("commit value = %q, want lower case", parsed.Value)
			}
		}
		// An unrecognized ref returns the input as its value and reads no components.
		if parsed.Type == RefTypeUnknown {
			if parsed.Value != ref {
				t.Errorf("unknown ref value = %q, want the input %q", parsed.Value, ref)
			}
			if parsed.Repository != "" || parsed.Branch != "" || parsed.FilePath != "" {
				t.Errorf("unknown ref reported components: %+v", parsed)
			}
		}
		if parsed.Type == RefTypeUnknown || parsed.Type == RefTypeFile {
			return
		}
		// NormalizeURL strips one .git suffix per pass, so repo.git.git has no fixed point.
		if parsed.Repository != NormalizeURL(parsed.Repository) {
			return
		}
		// A re-created ref parses back to the same type, repository, value and branch.
		again := ParseRef(CreateRef(parsed.Type, parsed.Value, parsed.Repository, parsed.Branch))
		if again.Type != parsed.Type || again.Repository != parsed.Repository {
			t.Errorf("round trip type/repository = %q/%q, want %q/%q", again.Type, again.Repository, parsed.Type, parsed.Repository)
		}
		if again.Value != parsed.Value || again.Branch != parsed.Branch {
			t.Errorf("round trip value/branch = %q/%q, want %q/%q", again.Value, again.Branch, parsed.Value, parsed.Branch)
		}
	})
}
