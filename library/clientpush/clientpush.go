// clientpush.go - Shared publish orchestration (data push + browser site) for
// the thin clients (CLI, TUI, RPC). Centralized so the three can't drift on how
// a `gitsocial push` publishes: they all resolve code branches the same way,
// push the gitmsg data, and — for s3 remotes with the site.publish guard on —
// publish the site.
//
// Why here and not in core/gitmsg: objstore already imports gitmsg (site
// customization / config readers), so gitmsg.Push MUST NOT call objstore (import
// cycle). This package sits a layer up, importing both gitmsg and objstore, and
// wires push-then-site as one user-visible operation. Site failure after a
// successful data push is a WARNING on the result, never an error: the data push
// stands (spec decision).
//
// Site gate: the `site.publish` config guard (default off) is the only enabler —
// objstore.PushSite reads it from the workspace and skips everything when it is
// not "true". `--no-site` and `git config gitsocial.pushSite false` remain
// per-push/per-machine force-offs on top. The git remote helper's OWN post-push
// maintenance (core/objstore helper_push.go) applies the same guard bucket-side,
// so a plain `git push` respects it too.
package clientpush

import (
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
)

// Options configures a publish. Zero value = default behavior (reason-based
// data push + site for s3 remotes).
type Options struct {
	Remote      string // explicit target; "" resolves via git.PushRemote
	DryRun      bool   // preview only, touch nothing
	NoCode      bool   // skip code branches (default branch + open-PR heads)
	NoSite      bool   // skip the site step (overrides config)
	AllBranches bool   // publish every local branch (refs/heads/*), not just reasoned
}

// SiteOutcome is the site-publication result of a publish. Published is true
// when the site step ran successfully; Skipped names why it didn't run (empty
// when it ran); Err holds a site failure that did NOT fail the data push (the
// data push still succeeded). Error mirrors Err as a string for JSON/RPC
// consumers (error itself doesn't serialize).
type SiteOutcome struct {
	Published bool   `json:"published"`
	Skipped   string `json:"skipped,omitempty"`
	Error     string `json:"error,omitempty"`
	Err       error  `json:"-"`
}

// Result combines the data-push result with the site outcome and whether the
// remote was empty before this push (first publish).
type Result struct {
	Push      *gitmsg.PushResult `json:"push"`
	Site      SiteOutcome        `json:"site"`
	EmptyBoot bool               `json:"emptyBoot"`
}

// ResolveRemote returns the remote a publish targets given the explicit choice
// (empty resolves via the config/heuristic order in git.PushRemote).
func ResolveRemote(workdir, remote string) string {
	if remote != "" {
		return remote
	}
	return git.PushRemote(workdir)
}

// ResolveSiteOverride reads a remote's per-remote site deployment overrides from
// git config (remote.<name>.gitsocial-site-{url,publish,pages}). Empty struct
// when the remote name is empty or no keys are set. Shared so the CLI, the
// `gitsocial push` site step, and `gitsocial site push` all resolve the same
// values the git remote helper reads bucket-side.
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

// Preview returns the offline push preview for the resolved remote, including
// --all extras when requested. Used by dry-run and the TUI prompt.
func Preview(workdir string, opts Options) (*gitmsg.PushPreview, string, error) {
	remote := ResolveRemote(workdir, opts.Remote)
	codeBranches := resolveCodeBranches(workdir, opts.NoCode, remote)
	preview, err := gitmsg.GetPushPreview(workdir, codeBranches, remote, opts.AllBranches)
	return preview, remote, err
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
func Publish(workdir string, opts Options, onBranch gitmsg.PushBranchProgress, siteProgress objstore.Progress) (*Result, error) {
	remote := ResolveRemote(workdir, opts.Remote)
	codeBranches := resolveCodeBranches(workdir, opts.NoCode, remote)

	res := &Result{EmptyBoot: gitmsg.RemoteIsEmpty(workdir, remote)}

	// Real push (not dry-run) against an s3 remote: reconcile the tracking refs
	// to the bucket's actual state before counting, so a recreated/drifted bucket
	// doesn't silently skip branches. Best-effort — a listing failure leaves the
	// existing (possibly stale) counting, self-healing on the next push.
	if !opts.DryRun {
		reconcileTrackingRefs(workdir, remote)
	}

	pushResult, err := gitmsg.PushWithProgress(workdir, opts.DryRun, codeBranches, remote, opts.AllBranches, onBranch)
	if err != nil {
		return nil, err
	}
	res.Push = pushResult

	res.Site = publishSite(workdir, remote, pushResult.RemoteURL, opts, siteProgress)
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
	published, err := PublishSite(workdir, remoteURL, ResolveSiteOverride(workdir, remote), progress)
	if err != nil {
		return SiteOutcome{Err: err, Error: err.Error()}
	}
	if !published {
		return SiteOutcome{Skipped: "site.publish not enabled"}
	}
	return SiteOutcome{Published: true}
}

// PublishSite uploads the browser static site to an s3 bucket and refreshes the
// bucket HEAD + push-time stats. override carries the target remote's per-remote
// deployment overrides (url/publish/pages) so the site stamps this bucket's own
// values. Shared by `gitsocial push` (via Publish) and the explicit
// `gitsocial site push` so their site wiring can't drift.
// published is false (no error) when the workspace's site.publish guard is not
// enabled — the only enabler for the static site. HEAD and stats are
// best-effort: a failure there does not fail the site push.
func PublishSite(workdir, remoteURL string, override objstore.SiteOverride, progress objstore.Progress) (published bool, err error) {
	published, err = objstore.PushSite(remoteURL, objstore.HelperEnvFromOS(), workdir, override, progress)
	if err != nil || !published {
		return published, err
	}
	// Point the bucket HEAD at the repo's real default branch (not an assumed
	// "main"), and publish push-time stats (the default branch's commit count +
	// times) the browser can't cheaply derive. Best-effort: never fails the push.
	branch, times, err := defaultBranchStats(workdir)
	if err != nil {
		return true, nil
	}
	_ = objstore.SetRemoteHead(remoteURL, objstore.HelperEnvFromOS(), branch)
	stats := map[string]any{"branch": branch, "commits": len(times), "commitTimes": times}
	_ = objstore.WriteSiteStats(remoteURL, objstore.HelperEnvFromOS(), stats)
	return true, nil
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
