// site_progress.go - per-extension progress context for the site item-index build.
package site

import "github.com/gitsocial-org/gitsocial/library/core/objstore"

// siteProgress carries the per-extension pass context threaded through the site
// item-index build: the progress hook, the extension being maintained (so both
// phases — the bounded commit walk and the shard uploads — report under a stable
// "<phase> <ext>" label), and an optional local commit source (the walk reads
// commits from the local odb when the pusher has the repo, falling back to the
// bucket per missing object; nil = bucket-only). A nil *siteProgress is silent
// and bucket-only; a nil hook or nil src inside one is fine.
type siteProgress struct {
	progress objstore.Progress
	ext      string
	src      *objstore.LocalCommitSource
}

// commitSource returns the pass's local commit source (nil-safe: a nil
// *siteProgress or an unset source yields nil, which getCommit treats as
// bucket-only).
func (sp *siteProgress) commitSource() *objstore.LocalCommitSource {
	if sp == nil {
		return nil
	}
	return sp.src
}

// walk reports commit-walk progress ("site index <ext>: <done>[/<total> (NN%)]").
// total is a ceiling only when the remaining size is known; every caller passes 0, so the walk budget reports a plain count.
func (sp *siteProgress) walk(done, total int) {
	if sp != nil {
		sp.progress.Call("site index "+sp.ext, done, total)
	}
}

// shards reports shard-upload progress ("site <corpus> shards <ext>: <done>/
// <total>"). corpus ("items" / "bodies") disambiguates the two corpora, which
// seal the same shard count in one pass: without it a single push printed the
// identical "site shards <ext>: N/N" twice, reading like duplicate work when it
// is just the two corpora advancing in lockstep.
func (sp *siteProgress) shards(corpus string, done, total int) {
	if sp != nil {
		sp.progress.Call("site "+corpus+" shards "+sp.ext, done, total)
	}
}
