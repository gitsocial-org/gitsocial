// labels_test.go - Tests for label mapping logic
package importpkg

import "testing"

func TestMapLabel_Raw(t *testing.T) {
	cases := []struct{ input, want string }{
		{"bug", "bug"},
		{"My Custom Label", "My Custom Label"},
		{"", ""},
	}
	for _, c := range cases {
		got := MapLabel(c.input, "raw")
		if got != c.want {
			t.Errorf("MapLabel(%q, raw) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestMapLabel_Skip(t *testing.T) {
	if got := MapLabel("bug", "skip"); got != "" {
		t.Errorf("MapLabel(bug, skip) = %q, want empty", got)
	}
}

func TestMapLabel_Auto(t *testing.T) {
	cases := []struct{ input, want string }{
		{"bug", "kind/bug"},
		{"crash", "kind/crash"},
		{"defect", "kind/defect"},
		{"regression", "kind/regression"},
		{"feature", "kind/feature"},
		{"enhancement", "kind/enhancement"},
		{"documentation", "kind/docs"},
		{"docs", "kind/docs"},
		{"good-first-issue", "priority/good-first-issue"},
		{"contributor-friendly", "priority/contributor-friendly"},
		{"help-wanted", "priority/help-wanted"},
		{"networking", "area/networking"},
		{"ui", "area/ui"},
	}
	for _, c := range cases {
		got := MapLabel(c.input, "auto")
		if got != c.want {
			t.Errorf("MapLabel(%q, auto) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestAutoMapLabel_Normalization(t *testing.T) {
	cases := []struct{ input, want string }{
		{"  Bug  ", "kind/bug"},
		{"FEATURE", "kind/feature"},
		{"Good First Issue", "priority/good-first-issue"},
		{"Help Wanted", "priority/help-wanted"},
	}
	for _, c := range cases {
		got := MapLabel(c.input, "auto")
		if got != c.want {
			t.Errorf("MapLabel(%q, auto) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestAutoMapLabel_AlreadyScoped(t *testing.T) {
	cases := []struct{ input, want string }{
		{"kind/bug", "kind/bug"},
		{"area/networking", "area/networking"},
		{"priority/p0", "priority/p0"},
	}
	for _, c := range cases {
		got := MapLabel(c.input, "auto")
		if got != c.want {
			t.Errorf("MapLabel(%q, auto) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestMapLabels(t *testing.T) {
	input := []string{"bug", "feature", "networking"}
	got := MapLabels(input, "auto")
	want := []string{"kind/bug", "kind/feature", "area/networking"}
	if len(got) != len(want) {
		t.Fatalf("MapLabels len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("MapLabels[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMapLabels_Skip(t *testing.T) {
	got := MapLabels([]string{"bug", "feature"}, "skip")
	if got != nil {
		t.Errorf("MapLabels(skip) = %v, want nil", got)
	}
}

func TestMapLabels_Empty(t *testing.T) {
	got := MapLabels([]string{}, "auto")
	if len(got) != 0 {
		t.Errorf("MapLabels(empty) = %v, want empty", got)
	}
}
