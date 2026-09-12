// site_progress_test.go - the site index walk and shard uploads report labeled progress.

package site

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// TestNilSiteProgress_NilSafe: the nil-safe site progress helpers never panic on a nil pass context.
func TestNilSiteProgress_NilSafe(t *testing.T) {
	var p objstore.Progress
	p.Call("phase", 1, 2) // must not panic
	var sp *siteProgress
	sp.walk(5, 0) // must not panic
	sp.shards("items", 1, 2)
}

// TestUpdateSiteItemsIndex_ProgressPhases: a bootstrap over a seeded commit
// chain fires both site-maintenance phases with sane, labeled counts — the
// bounded walk ("site index <ext>") and the per-corpus shard uploads ("site
// items shards <ext>" / "site bodies shards <ext>").
func TestUpdateSiteItemsIndex_ProgressPhases(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		const n = 13
		shas := seedChain(t, client, "", "", n)
		tip := shas[n-1]

		var walkMax, shardsSeen int
		corpora := map[string]bool{}
		hook := func(phase string, done, total int) {
			switch {
			case strings.HasPrefix(phase, "site index social"):
				if done > walkMax {
					walkMax = done
				}
			case strings.HasPrefix(phase, "site items shards social"):
				corpora["items"] = true
				shardsSeen++
				if done < 1 || (total > 0 && done > total) {
					t.Errorf("shard progress out of range: %d/%d", done, total)
				}
			case strings.HasPrefix(phase, "site bodies shards social"):
				corpora["bodies"] = true
				shardsSeen++
				if done < 1 || (total > 0 && done > total) {
					t.Errorf("shard progress out of range: %d/%d", done, total)
				}
			default:
				t.Errorf("unexpected phase %q", phase)
			}
		}
		sp := &siteProgress{progress: hook, ext: "social"}
		if err := updateSiteItemsIndex(client, "", "social", tip, sp); err != nil {
			t.Fatalf("updateSiteItemsIndex: %v", err)
		}
		if walkMax != n {
			t.Errorf("walk progress reached %d, want %d (every commit walked once)", walkMax, n)
		}
		if shardsSeen == 0 {
			t.Error("shard-upload progress never fired")
		}
		if !corpora["items"] || !corpora["bodies"] {
			t.Errorf("both corpora must report shard progress under distinct labels, saw %v", corpora)
		}
	})
}
