// truncate_test.go - Tests for rune-safe truncation
package text

import "testing"

func TestFit(t *testing.T) {
	cases := []struct {
		input string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"hello world this is long", 10, "hello w..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
		{"abcd", 2, "ab"},
		{"abcd", 0, ""},
		{"éééééé", 5, "éé..."},
	}
	for _, c := range cases {
		if got := Fit(c.input, c.width); got != c.want {
			t.Errorf("Fit(%q, %d) = %q, want %q", c.input, c.width, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"hello world", 5, "hello..."},
		{"abcd", 0, ""},
		{"éééé", 2, "éé..."},
	}
	for _, c := range cases {
		if got := Truncate(c.input, c.n); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.input, c.n, got, c.want)
		}
	}
}
