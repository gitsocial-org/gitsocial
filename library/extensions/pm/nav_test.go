// nav_test.go - Tests for TUI navigation registration
package pm

import (
	"testing"

	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

func TestNavRegistration(t *testing.T) {
	t.Parallel()

	t.Run("RegisterNavItems", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		r := tuicore.NewNavRegistry()
		RegisterNavItems(r, workdir)

		pm := r.Get("pm")
		if pm == nil {
			t.Fatal("pm nav item should be registered")
		}
		if pm.Label != "PM" {
			t.Errorf("pm.Label = %q, want PM", pm.Label)
		}
		if !pm.IsTopLevel() {
			t.Error("pm should be top-level")
		}

		board := r.Get("pm.board")
		if board == nil {
			t.Fatal("pm.board should be registered")
		}
		if board.Parent != "pm" {
			t.Errorf("pm.board.Parent = %q, want pm", board.Parent)
		}

		issues := r.Get("pm.issues")
		if issues == nil {
			t.Fatal("pm.issues should be registered")
		}

		milestones := r.Get("pm.milestones")
		if milestones == nil {
			t.Fatal("pm.milestones should be registered (kanban default)")
		}
		sprints := r.Get("pm.sprints")
		if sprints != nil {
			t.Error("pm.sprints should not be registered (kanban default)")
		}
	})

	t.Run("UpdatePMNavItems_scrum", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		SavePMConfig(workdir, PMConfig{Framework: "scrum"})

		r := tuicore.NewNavRegistry()
		r.Register(tuicore.NavItem{ID: "pm", Label: "PM", Order: 1, Enabled: true})
		UpdatePMNavItems(r, workdir)

		milestones := r.Get("pm.milestones")
		if milestones == nil {
			t.Fatal("scrum should register milestones")
		}
		sprints := r.Get("pm.sprints")
		if sprints == nil {
			t.Fatal("scrum should register sprints")
		}
	})

	t.Run("UpdatePMNavItems_minimal", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		SavePMConfig(workdir, PMConfig{Framework: "minimal"})

		r := tuicore.NewNavRegistry()
		r.Register(tuicore.NavItem{ID: "pm", Label: "PM", Order: 1, Enabled: true})
		UpdatePMNavItems(r, workdir)

		milestones := r.Get("pm.milestones")
		if milestones != nil {
			t.Error("minimal should not register milestones")
		}
		sprints := r.Get("pm.sprints")
		if sprints != nil {
			t.Error("minimal should not register sprints")
		}
	})
}
