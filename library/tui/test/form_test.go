// form_test.go - Form submit flows
package test

import (
	"strings"
	"testing"
)

// TestMilestoneFormCreates submits the new-milestone form and expects the milestone detail view.
func TestMilestoneFormCreates(t *testing.T) {
	f := SetupFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	h.Navigate("/pm/new-milestone")
	if h.CurrentPath() != "/pm/new-milestone" {
		t.Fatalf("expected /pm/new-milestone, got %q", h.CurrentPath())
	}
	h.SendKeys(strings.Split("Beta cut", "")...)
	submitForm(h)

	if h.CurrentPath() != "/pm/milestone" {
		t.Fatalf("after submit: path = %q, want /pm/milestone\n%s", h.CurrentPath(), rendered(h))
	}
	assertContains(t, h.Rendered(), "Beta cut")
}

// TestSprintFormCreates submits the new-sprint form and expects the sprint detail view.
func TestSprintFormCreates(t *testing.T) {
	f := SetupFixture(t)
	h := New(t, f.Workdir, f.CacheDir)

	h.Navigate("/pm/new-sprint")
	if h.CurrentPath() != "/pm/new-sprint" {
		t.Fatalf("expected /pm/new-sprint, got %q", h.CurrentPath())
	}
	h.SendKeys(strings.Split("Sprint 42", "")...)
	submitForm(h)

	if h.CurrentPath() != "/pm/sprint" {
		t.Fatalf("after submit: path = %q, want /pm/sprint\n%s", h.CurrentPath(), rendered(h))
	}
	assertContains(t, h.Rendered(), "Sprint 42")
}

// submitForm tabs past every field to the submit button and presses it; the button ignores the extra tabs.
func submitForm(h *Harness) {
	for i := 0; i < 8; i++ {
		h.SendKey("tab")
	}
	h.SendKey("enter")
}
