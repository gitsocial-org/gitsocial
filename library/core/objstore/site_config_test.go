// site_config_test.go - resolveSiteBoard mirrors pm.ResolveBoardConfig: a custom
// board wins, else the framework columns, else the kanban default.

package objstore

import "testing"

func TestResolveSiteBoard(t *testing.T) {
	colNames := func(b siteResolvedBoard) []string {
		out := make([]string, len(b.Columns))
		for i, c := range b.Columns {
			out[i] = c.Name
		}
		return out
	}
	eq := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	t.Run("empty config falls back to kanban default", func(t *testing.T) {
		b := resolveSiteBoard(sitePMConfig{})
		if !eq(colNames(b), []string{"Backlog", "In Progress", "Review", "Done"}) {
			t.Fatalf("empty config columns = %v, want kanban default", colNames(b))
		}
	})

	t.Run("minimal framework -> Open/Closed", func(t *testing.T) {
		b := resolveSiteBoard(sitePMConfig{Framework: "minimal"})
		if !eq(colNames(b), []string{"Open", "Closed"}) {
			t.Fatalf("minimal columns = %v, want Open/Closed", colNames(b))
		}
	})

	t.Run("scrum framework -> five columns with Sprint", func(t *testing.T) {
		b := resolveSiteBoard(sitePMConfig{Framework: "scrum"})
		if !eq(colNames(b), []string{"Backlog", "Sprint", "In Progress", "Review", "Done"}) {
			t.Fatalf("scrum columns = %v", colNames(b))
		}
	})

	t.Run("unknown framework falls back to kanban", func(t *testing.T) {
		b := resolveSiteBoard(sitePMConfig{Framework: "waterfall"})
		if !eq(colNames(b), []string{"Backlog", "In Progress", "Review", "Done"}) {
			t.Fatalf("unknown framework columns = %v, want kanban default", colNames(b))
		}
	})

	t.Run("custom board wins over framework", func(t *testing.T) {
		cfg := sitePMConfig{
			Framework: "kanban",
			Boards: []sitePMBoard{{
				ID:   "custom",
				Name: "My Flow",
				Columns: []sitePMColumn{
					{Name: "Todo", Filter: "state:open"},
					{Name: "Doing", Filter: "status:wip"},
					{Name: "Done", Filter: "state:closed"},
				},
			}},
		}
		b := resolveSiteBoard(cfg)
		if b.Name != "My Flow" {
			t.Fatalf("custom board name = %q, want My Flow", b.Name)
		}
		if !eq(colNames(b), []string{"Todo", "Doing", "Done"}) {
			t.Fatalf("custom columns = %v", colNames(b))
		}
	})

	t.Run("kanban WIP limits carried", func(t *testing.T) {
		b := resolveSiteBoard(sitePMConfig{Framework: "kanban"})
		if b.Columns[1].WIP == nil || *b.Columns[1].WIP != 3 {
			t.Fatalf("In Progress WIP = %v, want 3", b.Columns[1].WIP)
		}
	})
}
