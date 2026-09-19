// split_test.go - Tests for the comma-separated list helpers
package text

import (
	"reflect"
	"testing"
)

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"   ", nil},
		{"alice@example.com", []string{"alice@example.com"}},
		{" alice@example.com , bob@example.com ", []string{"alice@example.com", "bob@example.com"}},
		{"alice@example.com,,", []string{"alice@example.com"}},
	}
	for _, c := range cases {
		if got := SplitCSV(c.input); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitCSV(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestInCSV(t *testing.T) {
	cases := []struct {
		list  string
		value string
		want  bool
	}{
		{"alice@example.com,bob@example.com", "bob@example.com", true},
		{" alice@example.com , bob@example.com ", "alice@example.com", true},
		{"alice@example.com", "bob@example.com", false},
		{"", "alice@example.com", false},
		{"alice@example.com", "alice", false},
	}
	for _, c := range cases {
		if got := InCSV(c.list, c.value); got != c.want {
			t.Errorf("InCSV(%q, %q) = %v, want %v", c.list, c.value, got, c.want)
		}
	}
}
