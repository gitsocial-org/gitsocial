// reconcile.go - push-path tracking-ref reconcile against the s3 bucket.
//
// The offline push preview counts unpushed work against the remote's tracking
// refs, a cache of the bucket. When that cache drifts from the bucket (a
// recreated bucket, or pushes by a pre-tracking-fix binary), branches are
// silently skipped and the bucket quietly stays stale. Before a real push this
// syncs the tracking refs to the bucket's actual refs/ listing (the observable
// truth), so the existing counting is correct by construction.
package clientpush

import (
	"log/slog"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// reconcileTrackingRefs syncs the remote's tracking refs to the s3 bucket's
// actual state before a real push. Best-effort and s3-only: a non-s3 remote or
// a bucket-listing error is a no-op, leaving the existing tracking refs (the
// stale-tracking-ref failure then self-heals on the next successful listing).
func reconcileTrackingRefs(workdir, remote string) {
	remoteURL := git.RemoteURL(workdir, remote)
	if !strings.HasPrefix(remoteURL, "s3://") {
		return
	}
	bucketRefs, err := objstore.ListRemoteRefs(remoteURL, objstore.HelperEnvFromOS())
	if err != nil {
		slog.Debug("reconcile tracking refs: list bucket", "remote", remote, "error", err)
		return
	}
	applyTrackingReconcile(workdir, remote, bucketRefs)
}

// applyTrackingReconcile is the git-side of reconcileTrackingRefs: given the
// bucket's refs, it updates every tracking ref that differs and deletes every
// tracking ref (in the reconciled namespaces) whose bucket ref is gone. Split
// out so it is testable without a bucket. Overwriting a tracking ref is
// definitionally safe: the bucket is the truth, the tracking refs are its cache.
func applyTrackingReconcile(workdir, remote string, bucketRefs map[string]string) {
	// Desired tracking state: map each bucket ref into the namespace the offline
	// counting reads. Branches (refs/heads/*, including gitmsg/* ext branches) →
	// refs/remotes/<remote>/*; gitmsg state refs (refs/gitmsg/*) → the tracking
	// prefix. Tags are uncountable offline (git keeps no remote tag state) and
	// HEAD isn't a tracking ref, so both are skipped.
	desired := map[string]string{}
	for ref, sha := range bucketRefs {
		if tip, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			desired["refs/remotes/"+remote+"/"+tip] = sha
		} else if strings.HasPrefix(ref, "refs/gitmsg/") {
			desired[gitmsg.TrackingRef(remote, ref)] = sha
		}
	}
	existing := trackingRefsIn(workdir, "refs/remotes/"+remote+"/", gitmsg.TrackingRefPrefix(remote))
	for ref, sha := range desired {
		if existing[ref] == sha {
			continue
		}
		if err := git.WriteRef(workdir, ref, sha); err != nil {
			// The object may not be present locally (bucket is ahead of us); the
			// push's fast-forward check handles that, so leave the tracking ref.
			slog.Debug("reconcile tracking refs: write", "ref", ref, "error", err)
		}
	}
	for ref := range existing {
		if _, keep := desired[ref]; keep {
			continue
		}
		if err := git.DeleteRef(workdir, ref); err != nil {
			slog.Debug("reconcile tracking refs: delete", "ref", ref, "error", err)
		}
	}
}

// trackingRefsIn returns refname → sha for every ref under the given prefixes.
func trackingRefsIn(workdir string, prefixes ...string) map[string]string {
	out := map[string]string{}
	for _, prefix := range prefixes {
		res, err := git.ExecGit(workdir, []string{"for-each-ref", "--format=%(refname) %(objectname)", prefix})
		if err != nil {
			continue
		}
		for _, line := range strings.Split(res.Stdout, "\n") {
			name, sha, ok := strings.Cut(strings.TrimSpace(line), " ")
			if ok && name != "" {
				out[name] = sha
			}
		}
	}
	return out
}
