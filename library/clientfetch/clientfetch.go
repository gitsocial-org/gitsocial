// clientfetch.go - Shared fetch wiring (extension processor lists + fork
// backfill) for the thin clients (CLI, TUI, RPC). Centralized so the three
// can't drift: a prior drift here left the TUI's fork fetch without
// social.Processors(), so fork comments landed in core_commits with no
// social_items row and stayed invisible to threads until a CLI backfill.
package clientfetch

import (
	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

// ExtraProcessors returns the non-social processors layered onto social.Fetch
// for the workspace/subscribed pass. social.Fetch adds social.Processors()
// itself, so it is intentionally omitted here.
func ExtraProcessors() []fetch.CommitProcessor {
	procs := append(pm.Processors(), review.Processors()...)
	procs = append(procs, memo.Processors()...)
	procs = append(procs, release.Processors()...)
	return append(procs, notifications.MentionProcessor(), notifications.TrailerProcessor())
}

// ForkProcessors returns the full processor set for fork fetches. Fork commits
// do not flow through social.Fetch, so social.Processors() MUST be included
// here or fork posts/comments never get their social_items row.
func ForkProcessors() []fetch.CommitProcessor {
	return append(ExtraProcessors(), social.Processors()...)
}

// FetchForks fetches every registered fork with the full processor set, then
// backfills any extension items row missed by dedup. Fetch and backfill are
// paired in one call so a fork commit can't linger in core_commits without its
// extension row (which the dedup would then skip forever).
func FetchForks(workdir, cacheDir string) fetch.FetchForkStats {
	procs := ForkProcessors()
	stats := fetch.FetchForks(workdir, cacheDir, procs)
	fetch.BackfillExtensionItems(backfillRepos(workdir), backfillSpecs(), procs)
	// Link any just-fetched fork edits to their canonicals so proposals attach.
	_, _ = cache.ReconcileVersions()
	return stats
}

// backfillSpecs enumerates the extension items tables the post-fetch backfill scans.
func backfillSpecs() []fetch.ExtBackfillSpec {
	return []fetch.ExtBackfillSpec{
		social.BackfillSpec(),
		pm.BackfillSpec(),
		release.BackfillSpec(),
		review.BackfillSpec(),
		memo.BackfillSpec(),
	}
}

// backfillRepos returns the workspace URL plus all registered fork URLs — the
// set whose cached commits may carry an orphaned extension row.
func backfillRepos(workdir string) []string {
	repos := make([]string, 0, 8)
	if ws := gitmsg.ResolveRepoURL(workdir); ws != "" {
		repos = append(repos, ws)
	}
	return append(repos, gitmsg.GetForks(workdir)...)
}
