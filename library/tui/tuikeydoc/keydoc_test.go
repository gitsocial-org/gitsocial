// keydoc_test.go - Tests for keydoc utility functions
package tuikeydoc

import "testing"

func TestCapitalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "Hello"},
		{"a", "A"},
		{"", ""},
		{"Hello", "Hello"},
		{"hello world", "Hello world"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := capitalize(tt.input)
			if got != tt.want {
				t.Errorf("capitalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDomainToExtensionName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"social", "Social"},
		{"pm", "PM"},
		{"review", "Review"},
		{"release", "Release"},
		{"cicd", "CI/CD"},
		{"infra", "Infrastructure"},
		{"ops", "Operations"},
		{"security", "Security"},
		{"dm", "DM"},
		{"portfolio", "Portfolio"},
		{"unknown", "unknown"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := domainToExtensionName(tt.input)
			if got != tt.want {
				t.Errorf("domainToExtensionName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestComponentKeys(t *testing.T) {
	tests := []struct {
		component string
		wantNil   bool
		wantLen   int
	}{
		{"CardList", false, len(CardListKeys)},
		{"SectionList", false, len(SectionListKeys)},
		{"VersionPicker", false, len(VersionPickerKeys)},
		{"Unknown", true, 0},
		{"", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.component, func(t *testing.T) {
			got := ComponentKeys(tt.component)
			if tt.wantNil && got != nil {
				t.Errorf("ComponentKeys(%q) = %v, want nil", tt.component, got)
			}
			if !tt.wantNil && len(got) != tt.wantLen {
				t.Errorf("ComponentKeys(%q) len = %d, want %d", tt.component, len(got), tt.wantLen)
			}
		})
	}
}

func TestComponentKeys_nonEmpty(t *testing.T) {
	for _, name := range []string{"CardList", "SectionList", "VersionPicker"} {
		keys := ComponentKeys(name)
		if len(keys) == 0 {
			t.Errorf("ComponentKeys(%q) returned empty slice", name)
		}
		for i, k := range keys {
			if k.Key == "" {
				t.Errorf("ComponentKeys(%q)[%d].Key is empty", name, i)
			}
			if k.Label == "" {
				t.Errorf("ComponentKeys(%q)[%d].Label is empty", name, i)
			}
		}
	}
}

func TestDomainOrder(t *testing.T) {
	expected := []string{"social", "pm", "review", "release", "core"}
	if len(domainOrder) != len(expected) {
		t.Fatalf("domainOrder len = %d, want %d", len(domainOrder), len(expected))
	}
	for i, d := range expected {
		if domainOrder[i] != d {
			t.Errorf("domainOrder[%d] = %q, want %q", i, domainOrder[i], d)
		}
	}
}

func TestDomainTitles(t *testing.T) {
	for _, d := range domainOrder {
		if _, ok := domainTitles[d]; !ok {
			t.Errorf("domainTitles missing entry for %q", d)
		}
	}
}
