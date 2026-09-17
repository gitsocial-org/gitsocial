// push.go - Publish orchestration for the thin clients: the data push, then the site
package client

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"

	"github.com/gitsocial-org/gitsocial/library/core/site"
)

// Options configures a publish. Zero value = default behavior (reason-based
// data push + site for s3 remotes).
type Options struct {
	DryRun      bool // preview only, touch nothing
	NoCode      bool // skip code branches (default branch + open-PR heads)
	NoSite      bool // skip the site step (overrides config)
	SiteOnly    bool // publish only the site, no data push (explicit refresh; fails loudly)
	AllBranches bool // publish every local branch (refs/heads/*), not just reasoned
	Full        bool // detach a thin fork relationship: upload everything the bucket lacks
}

// SiteOutcome is the site-publication result of a publish. Published is true
// when the site step ran successfully; Skipped names why it didn't run (empty
// when it ran); Err holds a site failure that did NOT fail the data push (the
// data push still succeeded). Error mirrors Err as a string for JSON/RPC
// consumers (error itself doesn't serialize).
type SiteOutcome struct {
	Published bool `json:"published"`
	// Complete is false when the site published but a bootstrap still owes work
	// a later push must finish, so a partial mirror is reported rather than
	// reading as a finished publish.
	Complete bool   `json:"complete"`
	Skipped  string `json:"skipped,omitempty"`
	Error    string `json:"error,omitempty"`
	Err      error  `json:"-"`
}

// Result combines the data-push result with the site outcome and whether the
// remote was empty before this push (first publish).
type Result struct {
	Push      *gitmsg.PushResult `json:"push"`
	Site      SiteOutcome        `json:"site"`
	EmptyBoot bool               `json:"emptyBoot"`
}

// ResolveRemotes returns the remotes a publish targets: the named ones, else the defaults.
func ResolveRemotes(workdir string, args []string) ([]string, git.PushResolution) {
	if len(args) > 0 {
		return args, git.PushConfigured
	}
	return git.ResolvePushRemotes(workdir)
}

// ResolveSiteOverride reads a remote's per-remote site deployment overrides from
// git config (remote.<name>.gitsocial-site-{url,publish,pages}). Empty struct
// when the remote name is empty or no keys are set. Shared so the CLI, the
// `gitsocial push` site step, and `gitsocial push --site-only` all resolve the
// same values the git remote helper reads bucket-side.
func ResolveSiteOverride(workdir, remote string) objstore.SiteOverride {
	if remote == "" {
		return objstore.SiteOverride{}
	}
	get := func(suffix string) string {
		out, err := git.ExecGit(workdir, []string{"config", "--get", "remote." + remote + "." + suffix})
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out.Stdout)
	}
	return objstore.SiteOverride{
		URL:     get(objstore.SiteOverrideURLKey),
		Publish: get(objstore.SiteOverridePublishKey),
		Pages:   get(objstore.SiteOverridePagesKey),
	}
}

// Preview returns the offline push preview for one remote.
func Preview(workdir, remote string, opts Options) (*gitmsg.PushPreview, error) {
	codeBranches := resolveCodeBranches(workdir, opts.NoCode, remote)
	return gitmsg.GetPushPreview(workdir, codeBranches, remote, opts.AllBranches)
}

// resolveCodeBranches returns the reason-based code branches to publish against
// the resolved remote, or nil when code is opted out. Centralized so CLI/TUI/RPC agree.
func resolveCodeBranches(workdir string, noCode bool, remote string) map[string]int {
	if noCode {
		return nil
	}
	branches, _ := review.CodeBranchesToPush(workdir, remote)
	return branches
}

// Publish runs the data push, then (for s3 remotes not opted out) publishes the
// browser site. onBranch reports coarse per-branch push progress (nil = none);
// siteProgress reports site-upload progress (nil = none). The data push and the
// site step share one operation from the caller's view, but their failure modes
// differ: a data-push error is returned as err (nothing published); a site error
// after a good data push lands in Result.Site.Err (the push still succeeded).
func Publish(workdir, remote string, opts Options, onBranch gitmsg.PushBranchProgress, siteProgress objstore.Progress) (*Result, error) {
	if opts.SiteOnly {
		return publishSiteOnly(workdir, remote, opts, siteProgress)
	}
	res := &Result{EmptyBoot: gitmsg.RemoteIsEmpty(workdir, remote)}

	// --full detaches a thin fork relationship. Clearing the config FIRST makes
	// the push below upload its whole delta and rewrite the ref advertisement;
	// objstore.PushFull afterwards carries what no push moves (the objects earlier
	// thin pushes left to upstream, and the bucket's thin marker).
	if opts.Full && !opts.DryRun {
		clearThinRelationship(workdir, remote)
	}

	// Real push (not dry-run) against an s3 remote: reconcile the tracking refs
	// to the bucket's actual state before counting, so a recreated/drifted bucket
	// doesn't silently skip branches. Best-effort — a listing failure leaves the
	// existing (possibly stale) counting, self-healing on the next push.
	if !opts.DryRun {
		reconcileTrackingRefs(workdir, remote)
	}

	// Counted AFTER the reconcile above, which is the whole point of it: the
	// count reads the tracking refs, so a bucket emptied or rewound since the
	// last push must be counted against what the bucket actually holds now, not
	// against the tracking refs the last push wrote.
	codeBranches := resolveCodeBranches(workdir, opts.NoCode, remote)

	pushResult, err := gitmsg.PushWithProgress(workdir, opts.DryRun, codeBranches, remote, opts.AllBranches, onBranch)
	if err != nil {
		return nil, err
	}
	res.Push = pushResult

	if opts.Full && !opts.DryRun && strings.HasPrefix(pushResult.RemoteURL, "s3://") {
		if err := objstore.PushFull(pushResult.RemoteURL, objstore.HelperEnvFromOS(), workdir, siteProgress); err != nil {
			return nil, fmt.Errorf("detach thin fork %s: %w", pushResult.RemoteURL, err)
		}
	}

	res.Site = publishSite(workdir, remote, pushResult.RemoteURL, opts, siteProgress)
	return res, nil
}

// PublishAll publishes to every remote in order, continuing past a failure into one error.
func PublishAll(workdir string, remotes []string, opts Options, onRemote func(remote string), onBranch func(remote, branch string, done, total int), siteProgress objstore.Progress) ([]Result, error) {
	results := make([]Result, 0, len(remotes))
	var failures []string
	for _, remote := range remotes {
		if onRemote != nil {
			onRemote(remote)
		}
		var branchProgress gitmsg.PushBranchProgress
		if onBranch != nil {
			branchProgress = func(branch string, done, total int) { onBranch(remote, branch, done, total) }
		}
		result, err := Publish(workdir, remote, opts, branchProgress, siteProgress)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", remote, err))
			continue
		}
		results = append(results, *result)
	}
	if len(failures) > 0 {
		return results, fmt.Errorf("push failed for %s", strings.Join(failures, "; "))
	}
	return results, nil
}

// clearThinRelationship removes the thin flag from a remote, so every later push
// to it is a full one. The upstream URL is left recorded: it is history, not a
// switch.
func clearThinRelationship(workdir, remote string) {
	_, _ = git.ExecGit(workdir, []string{"config", "--unset", "remote." + remote + "." + objstore.ThinConfigKey})
}

// publishSiteOnly runs only the site step (the explicit site refresh, `gitsocial
// push --site-only`), pushing no data. Unlike a full publish — where the site is
// a best-effort tail on the data push — an explicit site request fails loudly: a
// missing or non-s3 remote, the site.publish guard being off, and any upload
// failure are all errors. A dry run stays offline and reports the site skipped.
func publishSiteOnly(workdir, remote string, opts Options, progress objstore.Progress) (*Result, error) {
	remoteURL := git.RemoteURL(workdir, remote)
	if remoteURL == "" {
		return nil, fmt.Errorf("remote %q is not configured", remote)
	}
	res := &Result{Push: &gitmsg.PushResult{Remote: remote, RemoteURL: remoteURL}}
	if opts.DryRun {
		res.Site = SiteOutcome{Skipped: "dry-run"}
		return res, nil
	}
	if !strings.HasPrefix(remoteURL, "s3://") {
		return nil, fmt.Errorf("remote %q is %s, not an s3 remote", remote, remoteURL)
	}
	published, complete, err := PublishSite(workdir, remoteURL, ResolveSiteOverride(workdir, remote), progress)
	if err != nil {
		return nil, fmt.Errorf("push site to %s: %w", remoteURL, err)
	}
	if !published {
		return nil, errors.New("site publishing is disabled: enable it with gitsocial config site set publish true")
	}
	res.Site = SiteOutcome{Published: true, Complete: complete}
	return res, nil
}

// publishSite runs the site step for the resolved remote URL, deciding whether
// it applies. Non-s3 remotes, opt-outs, and repos without the site.publish
// guard are skipped with a reason; a dry run never touches the bucket. A site
// error is captured (warning), not returned, so it can't undo a successful data
// push.
func publishSite(workdir, remote, remoteURL string, opts Options, progress objstore.Progress) SiteOutcome {
	if opts.NoSite {
		return SiteOutcome{Skipped: "--no-site"}
	}
	if !git.PushSiteEnabled(workdir) {
		return SiteOutcome{Skipped: "config gitsocial.pushSite=false"}
	}
	if !strings.HasPrefix(remoteURL, "s3://") {
		return SiteOutcome{Skipped: "non-s3 remote"}
	}
	if opts.DryRun {
		return SiteOutcome{Skipped: "dry-run"}
	}
	published, complete, err := PublishSite(workdir, remoteURL, ResolveSiteOverride(workdir, remote), progress)
	if errors.Is(err, objstore.ErrThinBucket) {
		// A thin fork bucket has no site by design; that is a skip, not a failure
		// (an explicit `--site-only` still fails loudly, see publishSiteOnly).
		return SiteOutcome{Skipped: "thin fork bucket"}
	}
	if err != nil {
		return SiteOutcome{Err: err, Error: err.Error()}
	}
	if !published {
		return SiteOutcome{Skipped: "site.publish not enabled"}
	}
	return SiteOutcome{Published: true, Complete: complete}
}

// PublishSite uploads the browser static site to an s3 bucket and refreshes the
// bucket HEAD + push-time stats. override carries the target remote's per-remote
// deployment overrides (url/publish/pages) so the site stamps this bucket's own
// values. Shared by `gitsocial push` (via Publish) and the explicit
// `gitsocial push --site-only` so their site wiring can't drift.
// published is false (no error) when the workspace's site.publish guard is not
// enabled — the only enabler for the static site. HEAD and stats are
// best-effort: a failure there does not fail the site push.
func PublishSite(workdir, remoteURL string, override objstore.SiteOverride, progress objstore.Progress) (published, complete bool, err error) {
	published, complete, err = site.Push(remoteURL, objstore.HelperEnvFromOS(), workdir, override, progress)
	if err != nil || !published {
		return published, complete, err
	}
	// Point the bucket HEAD at the repo's real default branch (not an assumed
	// "main"), and publish push-time stats (the default branch's commit count +
	// times) the browser can't cheaply derive. Best-effort: never fails the push.
	branch, times, err := defaultBranchStats(workdir)
	if err != nil {
		return true, complete, nil
	}
	_ = site.SetRemoteHead(remoteURL, objstore.HelperEnvFromOS(), branch)
	stats := map[string]any{"branch": branch, "commits": len(times), "commitTimes": times}
	_ = site.WriteSiteStats(remoteURL, objstore.HelperEnvFromOS(), stats)
	return true, complete, nil
}

// defaultBranchStats returns the current branch and every regular commit's
// author time (unix seconds) on it — the served default branch in the bucket.
// The browser buckets these into the analytics activity chart with the same
// period logic it uses for items, and the count is len().
func defaultBranchStats(workdir string) (string, []int, error) {
	br, err := git.ExecGit(workdir, []string{"rev-parse", "--abbrev-ref", "HEAD"})
	if err != nil {
		return "", nil, err
	}
	lr, err := git.ExecGit(workdir, []string{"log", "--format=%ct", "HEAD"})
	if err != nil {
		return "", nil, err
	}
	fields := strings.Fields(lr.Stdout)
	times := make([]int, 0, len(fields))
	for _, f := range fields {
		if n, e := strconv.Atoi(f); e == nil {
			times = append(times, n)
		}
	}
	return strings.TrimSpace(br.Stdout), times, nil
}
