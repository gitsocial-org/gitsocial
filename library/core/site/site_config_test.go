// site_config_test.go - resolveSiteBoard mirrors pm.ResolveBoardConfig: a custom
// board wins, else the framework columns, else the kanban default.

package site

import (
	"path/filepath"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

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

// TestReadSiteConfigs_PackedOnlyAndLocal: the push-time config readers succeed
// against a bucket whose config commits exist ONLY inside packs (the pack-map
// fallback), and resolve purely from the local odb when a source is available
// (asserted against an empty bucket, where only the local path can answer).
func TestReadSiteConfigs_PackedOnlyAndLocal(t *testing.T) {
	dir := packTestRepo(t, 4)
	pmSha := gitRun(t, dir, "commit-tree", gitRun(t, dir, "mktree"), "-m", `{"framework":"scrum"}`)
	gitRun(t, dir, "update-ref", "refs/gitmsg/pm/config", pmSha)
	coreSha := gitRun(t, dir, "commit-tree", gitRun(t, dir, "mktree"), "-m", `{"version":1,"site":{"title":"Packed"}}`)
	gitRun(t, dir, "update-ref", "refs/gitmsg/core/config", coreSha)
	client := pushPackedBucket(t, dir, "refs/heads/main", "refs/gitmsg/pm/config", "refs/gitmsg/core/config")
	for _, sha := range []string{pmSha, coreSha} {
		if _, err := client.Get("objects/" + sha[:2] + "/" + sha[2:]); err == nil {
			t.Fatalf("fixture is not packed-only: %s has a loose key", sha)
		}
	}
	refs := map[string]string{"refs/gitmsg/pm/config": pmSha, "refs/gitmsg/core/config": coreSha}

	cfg, ok, err := readSitePMConfig(client, "", refs, nil)
	if err != nil || !ok || cfg.Framework != "scrum" {
		t.Errorf("readSitePMConfig over a packed-only bucket = %+v ok=%v err=%v", cfg, ok, err)
	}
	custom, ok, err := readSiteBaseCustomization(client, "", refs, nil)
	if err != nil || !ok || custom.Title != "Packed" {
		t.Errorf("readSiteBaseCustomization over a packed-only bucket = %+v ok=%v err=%v", custom, ok, err)
	}

	src := objstore.NewLocalCommitSource(filepath.Join(dir, ".git"), "")
	defer src.Close()
	emptyClient, _ := testClient(t)
	cfg, ok, err = readSitePMConfig(emptyClient, "", refs, src)
	if err != nil || !ok || cfg.Framework != "scrum" {
		t.Errorf("readSitePMConfig via the local odb = %+v ok=%v err=%v", cfg, ok, err)
	}
	custom, ok, err = readSiteBaseCustomization(emptyClient, "", refs, src)
	if err != nil || !ok || custom.Title != "Packed" {
		t.Errorf("readSiteBaseCustomization via the local odb = %+v ok=%v err=%v", custom, ok, err)
	}
}
