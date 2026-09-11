// site_progress.go - per-extension progress context for the site item-index build.
package objstore

// siteProgress carries the per-extension pass context threaded through the site
// item-index build: the progress hook, the extension being maintained (so both
// phases — the bounded commit walk and the shard uploads — report under a stable
// "<phase> <ext>" label), and an optional local commit source (the walk reads
// commits from the local odb when the pusher has the repo, falling back to the
// bucket per missing object; nil = bucket-only). A nil *siteProgress is silent
// and bucket-only; a nil hook or nil src inside one is fine.
type siteProgress struct {
	progress Progress
	ext      string
	src      *localCommitSource
}

// commitSource returns the pass's local commit source (nil-safe: a nil
// *siteProgress or an unset source yields nil, which getCommit treats as
// bucket-only).
func (sp *siteProgress) commitSource() *localCommitSource {
	if sp == nil {
		return nil
	}
	return sp.src
}

// walk reports commit-walk progress ("site index <ext>: <done>[/<total> (NN%)]").
// total is the honest ceiling on this walk only when the true remaining size is
// knowable; a percentage is NEVER shown against the walk budget, which is a
// per-push cap the walk usually won't reach (a 6k-commit branch against a 50k
// budget must not read "12%"). Every current caller passes 0 (plain count): the
// bootstrap/backfill budget is a cap, the append gap is unbounded, and neither
// the manifest nor the cursor tracks a real remaining count.
func (sp *siteProgress) walk(done, total int) {
	if sp != nil {
		sp.progress.call("site index "+sp.ext, done, total)
	}
}

// shards reports shard-upload progress ("site <corpus> shards <ext>: <done>/
// <total>"). corpus ("items" / "bodies") disambiguates the two corpora, which
// seal the same shard count in one pass: without it a single push printed the
// identical "site shards <ext>: N/N" twice, reading like duplicate work when it
// is just the two corpora advancing in lockstep.
func (sp *siteProgress) shards(corpus string, done, total int) {
	if sp != nil {
		sp.progress.call("site "+corpus+" shards "+sp.ext, done, total)
	}
}
