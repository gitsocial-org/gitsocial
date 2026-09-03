// lfs_test.go - Tests for Git LFS pointer formatting and parsing
package git

import (
	"strings"
	"testing"
)

// TestFormatLFSPointer pins the exact bytes written, which are the three lines
// the LFS v1 pointer format requires, each newline-terminated.
func TestFormatLFSPointer(t *testing.T) {
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	got := string(FormatLFSPointer(oid, 12345))
	want := "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize 12345\n"
	if got != want {
		t.Errorf("FormatLFSPointer() =\n%q\nwant\n%q", got, want)
	}
	if !IsLFSPointer([]byte(got)) {
		t.Error("IsLFSPointer() = false for freshly formatted pointer")
	}
}

// TestLFSPointerRoundTrip checks that formatting then parsing returns the same
// oid and size, across the sizes a real artifact takes.
func TestLFSPointerRoundTrip(t *testing.T) {
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	for _, size := range []int64{1, 42, 1024, 1 << 30, 1<<62 - 1} {
		gotOID, gotSize, ok := ParseLFSPointer(FormatLFSPointer(oid, size))
		if !ok {
			t.Errorf("size %d: ParseLFSPointer() ok = false", size)
			continue
		}
		if gotOID != oid {
			t.Errorf("size %d: oid = %q, want %q", size, gotOID, oid)
		}
		if gotSize != size {
			t.Errorf("size %d: size = %d", size, gotSize)
		}
	}
}

// TestParseLFSPointerRejects covers the inputs that must not be read as a
// pointer, so unrelated file content is never mistaken for one.
func TestParseLFSPointerRejects(t *testing.T) {
	const oidLine = "oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\n"
	tests := []struct {
		name string
		data string
	}{
		{"empty", ""},
		{"plain text", "just a regular file\n"},
		{"version line missing", oidLine + "size 10\n"},
		{"version line not first", "\n" + lfsPointerPrefix + oidLine + "size 10\n"},
		{"different spec version", "version https://git-lfs.github.com/spec/v2\n" + oidLine + "size 10\n"},
		{"version line without newline", strings.TrimSuffix(lfsPointerPrefix, "\n")},
		{"no oid", lfsPointerPrefix + "size 10\n"},
		{"empty oid", lfsPointerPrefix + "oid sha256:\nsize 10\n"},
		{"no size", lfsPointerPrefix + oidLine},
		{"non-numeric size", lfsPointerPrefix + oidLine + "size abc\n"},
		{"negative size", lfsPointerPrefix + oidLine + "size -5\n"},
		{"unsupported hash algorithm", lfsPointerPrefix + "oid sha1:abc123\nsize 10\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oid, size, ok := ParseLFSPointer([]byte(tt.data))
			if ok {
				t.Errorf("ParseLFSPointer() ok = true (oid %q, size %d), want false", oid, size)
			}
		})
	}
}

// TestParseLFSPointerTolerates covers well-formed pointers that carry more than
// the two fields we read, which the LFS spec permits.
func TestParseLFSPointerTolerates(t *testing.T) {
	const oid = "4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393"
	tests := []struct {
		name string
		data string
	}{
		{"extra key", lfsPointerPrefix + "ext-0-shalink sha256:deadbeef\noid sha256:" + oid + "\nsize 7\n"},
		{"no trailing newline", lfsPointerPrefix + "oid sha256:" + oid + "\nsize 7"},
		{"trailing blank lines", lfsPointerPrefix + "oid sha256:" + oid + "\nsize 7\n\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOID, gotSize, ok := ParseLFSPointer([]byte(tt.data))
			if !ok || gotOID != oid || gotSize != 7 {
				t.Errorf("ParseLFSPointer() = %q, %d, %v; want %q, 7, true", gotOID, gotSize, ok, oid)
			}
		})
	}
}

// TestParseLFSPointerZeroSize documents that an empty file does not round-trip:
// FormatLFSPointer writes a valid `size 0` pointer, but ParseLFSPointer treats
// any non-positive size as "not a pointer", so callers fall back to raw content.
func TestParseLFSPointerZeroSize(t *testing.T) {
	const oid = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	data := FormatLFSPointer(oid, 0)
	if !IsLFSPointer(data) {
		t.Fatal("IsLFSPointer() = false for a size-0 pointer")
	}
	if _, _, ok := ParseLFSPointer(data); ok {
		t.Error("ParseLFSPointer() now accepts size 0; update this test and the callers that rely on the fallback")
	}
}

// TestIsLFSPointer checks the prefix test on its own, since it gates every read
// path that decides whether a file is content or a pointer.
func TestIsLFSPointer(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"valid prefix", lfsPointerPrefix + "oid sha256:abc\nsize 1\n", true},
		{"prefix only", lfsPointerPrefix, true},
		{"empty", "", false},
		{"prefix without newline", strings.TrimSuffix(lfsPointerPrefix, "\n"), false},
		{"prefix later in the file", "# notes\n" + lfsPointerPrefix, false},
		{"leading whitespace", " " + lfsPointerPrefix, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLFSPointer([]byte(tt.data)); got != tt.want {
				t.Errorf("IsLFSPointer() = %v, want %v", got, tt.want)
			}
		})
	}
}
