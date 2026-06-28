// apply.go - Proposal-apply engine (Phase 2 of the acceptance model). Turns a
// cross-repo proposal (an edit whose canonical this workspace owns) into the
// owner's authoritative same-repo edit by re-issuing the proposal's field deltas
// through the extension's existing Update*/Edit* path. The mirror is same-repo,
// so it wins resolution under the gating; the owner's concurrent edits survive
// because only the proposal's deltas are applied (S3).
//
// social and memo cannot have cross-repo proposals (their edit APIs reject
// cross-repo writes), so accept covers pm, release, and review content edits.
// PR lifecycle (close/merge/draft) is handled by the base-owner re-home model,
// not accept.
package proposals

import (
	"fmt"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/core/text"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
)

// Result aliases the shared Result type for user-facing codes.
type Result[T any] = result.Result[T]

// Outcome describes the mirror edit produced by accepting a proposal.
type Outcome struct {
	Ext           string // canonical extension (pm, release, review)
	CanonicalRef  string // the owner's canonical the mirror edits
	MirrorRepoURL string // the same-repo mirror edit's coordinates
	MirrorHash    string
	MirrorBranch  string
}

// applyProposal authors a same-repo mirror edit for a cross-repo proposal whose
// canonical this workspace owns. proposalRef must be a fully-qualified commit ref
// to the proposing edit.
func applyProposal(workdir, proposalRef string) Result[Outcome] {
	parsed := protocol.ParseRef(proposalRef)
	if parsed.Value == "" || parsed.Branch == "" {
		return result.Err[Outcome]("INVALID_REF", "proposal ref must be a fully-qualified commit ref")
	}
	editRepo := protocol.NormalizeURL(parsed.Repository)
	editHash, editBranch := parsed.Value, parsed.Branch

	// 1. Resolve canonical + authorize: the proposal must be a cross-repo edit
	// whose canonical this workspace owns.
	ver, err := cache.GetCanonical(editRepo, editHash, editBranch)
	if err != nil {
		return result.Err[Outcome]("LOOKUP_FAILED", err.Error())
	}
	if ver == nil {
		return result.Err[Outcome]("NOT_A_PROPOSAL", "ref is not an edit of any canonical")
	}
	if ver.EditRepoURL == ver.CanonicalRepoURL {
		return result.Err[Outcome]("NOT_A_PROPOSAL", "edit is already same-repo (nothing to accept)")
	}
	workspaceURL := protocol.NormalizeURL(gitmsg.ResolveRepoURL(workdir))
	if ver.CanonicalRepoURL != workspaceURL {
		return result.Err[Outcome]("NOT_OWNER", "you do not own this item's canonical")
	}

	// 2. Refuse if the canonical has been retracted (S5).
	if lv, lerr := cache.GetLatestVersion(ver.CanonicalRepoURL, ver.CanonicalHash, ver.CanonicalBranch); lerr == nil && lv.IsRetracted {
		return result.Err[Outcome]("CANONICAL_RETRACTED", "cannot accept: the item has been retracted")
	}

	// 3. Read the proposal and its parent (the version it edited) to derive deltas.
	proposalCommit, err := cache.GetCommit(editRepo, editHash, editBranch)
	if err != nil {
		return result.Err[Outcome]("LOOKUP_FAILED", "proposal: "+err.Error())
	}
	pMsg := protocol.ParseMessage(proposalCommit.Message)
	if pMsg == nil {
		return result.Err[Outcome]("PARSE_FAILED", "proposal message is not a GitMsg")
	}
	parMsg := parentMessage(pMsg, ver)
	canonicalRef := protocol.CreateRef(protocol.RefTypeCommit, ver.CanonicalHash, ver.CanonicalRepoURL, ver.CanonicalBranch)

	// Provenance snapshot: a GitMsg-Ref naming the proposal, preserving the
	// proposer's identity, fields, and content so the record survives fork deletion
	// (GITMSG.md §1.5). It keeps the proposal's real type; the "this is an
	// acceptance" relationship is carried by the mirror's accepts= header, set by
	// each extension's Update*/Edit* from this ref.
	snapshotFields := map[string]string{}
	for k, v := range pMsg.Header.Fields {
		if k == "edits" || k == "accepts" || v == "" {
			continue
		}
		snapshotFields[k] = v
	}
	attribution := &protocol.Ref{
		Ext:      pMsg.Header.Ext,
		Ref:      protocol.CreateRef(protocol.RefTypeCommit, editHash, editRepo, editBranch),
		V:        "0.1.0",
		Author:   proposalCommit.AuthorName,
		Email:    proposalCommit.AuthorEmail,
		Time:     proposalCommit.Timestamp.UTC().Format(time.RFC3339),
		Fields:   snapshotFields,
		Metadata: protocol.QuoteContent(pMsg.Content),
	}

	// 4. Dispatch on the canonical's extension. Field translation is per-extension;
	// the commit/version/cache mechanics are reused from each Update*/Edit*.
	var res Result[Outcome]
	switch pMsg.Header.Ext {
	case "pm":
		res = applyPM(workdir, canonicalRef, pMsg, parMsg, attribution)
	case "release":
		res = applyRelease(workdir, canonicalRef, pMsg, parMsg, attribution)
	case "review":
		res = applyReview(workdir, canonicalRef, pMsg, parMsg, attribution)
	default:
		return result.Err[Outcome]("UNSUPPORTED_EXT", fmt.Sprintf("accept not supported for %q", pMsg.Header.Ext))
	}

	// The mirror is now the canonical's latest same-repo edit; record its coords.
	if res.Success {
		if lv, lverr := cache.GetLatestVersion(ver.CanonicalRepoURL, ver.CanonicalHash, ver.CanonicalBranch); lverr == nil && lv.HasEdits {
			res.Data.MirrorRepoURL, res.Data.MirrorHash, res.Data.MirrorBranch = lv.RepoURL, lv.Hash, lv.Branch
		}
	}
	return res
}

// parentMessage returns the message of the version the proposal edited (its
// delta baseline), falling back to the canonical's current message.
func parentMessage(pMsg *protocol.Message, ver *cache.Version) *protocol.Message {
	if editsRef := pMsg.Header.Fields["edits"]; editsRef != "" {
		p := protocol.ParseRef(editsRef)
		if p.Value != "" {
			repo := protocol.NormalizeURL(p.Repository)
			if repo == "" {
				repo = ver.CanonicalRepoURL
			}
			br := p.Branch
			if br == "" {
				br = ver.CanonicalBranch
			}
			if c, err := cache.GetCommit(repo, p.Value, br); err == nil {
				if m := protocol.ParseMessage(c.Message); m != nil {
					return m
				}
			}
		}
	}
	if c, err := cache.GetCommit(ver.CanonicalRepoURL, ver.CanonicalHash, ver.CanonicalBranch); err == nil {
		if m := protocol.ParseMessage(c.Message); m != nil {
			return m
		}
	}
	return &protocol.Message{Header: protocol.Header{Fields: map[string]string{}}}
}

// applyPM applies a pm proposal's deltas via the existing pm Update* path.
func applyPM(workdir, canonicalRef string, pMsg, parMsg *protocol.Message, attribution *protocol.Ref) Result[Outcome] {
	pf, parf := pMsg.Header.Fields, parMsg.Header.Fields
	content, contentOK := contentDelta(pMsg, parMsg)
	switch pf["type"] {
	case "issue", "":
		opts := pm.UpdateIssueOptions{}
		if v, ok := changedField(pf, parf, "state"); ok {
			s := pm.State(v)
			opts.State = &s
		}
		if v, ok := changedField(pf, parf, "assignees"); ok {
			a := text.SplitCSV(v)
			opts.Assignees = &a
		}
		if v, ok := changedField(pf, parf, "due"); ok {
			if t, perr := time.Parse("2006-01-02", v); perr == nil {
				opts.Due = &t
			}
		}
		if v, ok := changedField(pf, parf, "labels"); ok {
			l := pm.ParseLabels(v)
			opts.Labels = &l
		}
		if contentOK {
			subj, body := protocol.SplitSubjectBody(content)
			opts.Subject, opts.Body = &subj, &body
		}
		if opts == (pm.UpdateIssueOptions{}) {
			return noDelta()
		}
		opts.Attribution = attribution
		return outcome(pm.UpdateIssue(workdir, canonicalRef, opts), "pm", canonicalRef)
	case "milestone":
		opts := pm.UpdateMilestoneOptions{}
		if v, ok := changedField(pf, parf, "state"); ok {
			s := pm.State(v)
			opts.State = &s
		}
		if v, ok := changedField(pf, parf, "due"); ok {
			if t, perr := time.Parse("2006-01-02", v); perr == nil {
				opts.Due = &t
			}
		}
		if v, ok := changedField(pf, parf, "labels"); ok {
			l := text.SplitCSV(v)
			opts.Labels = &l
		}
		if contentOK {
			subj, body := protocol.SplitSubjectBody(content)
			opts.Title, opts.Body = &subj, &body
		}
		if opts == (pm.UpdateMilestoneOptions{}) {
			return noDelta()
		}
		opts.Attribution = attribution
		return outcome(pm.UpdateMilestone(workdir, canonicalRef, opts), "pm", canonicalRef)
	case "sprint":
		opts := pm.UpdateSprintOptions{}
		if v, ok := changedField(pf, parf, "state"); ok {
			s := pm.SprintState(v)
			opts.State = &s
		}
		if v, ok := changedField(pf, parf, "start"); ok {
			if t, perr := time.Parse("2006-01-02", v); perr == nil {
				opts.Start = &t
			}
		}
		if v, ok := changedField(pf, parf, "end"); ok {
			if t, perr := time.Parse("2006-01-02", v); perr == nil {
				opts.End = &t
			}
		}
		if v, ok := changedField(pf, parf, "labels"); ok {
			l := text.SplitCSV(v)
			opts.Labels = &l
		}
		if contentOK {
			subj, body := protocol.SplitSubjectBody(content)
			opts.Title, opts.Body = &subj, &body
		}
		if opts == (pm.UpdateSprintOptions{}) {
			return noDelta()
		}
		opts.Attribution = attribution
		return outcome(pm.UpdateSprint(workdir, canonicalRef, opts), "pm", canonicalRef)
	default:
		return result.Err[Outcome]("UNSUPPORTED_TYPE", fmt.Sprintf("accept not supported for pm type %q", pf["type"]))
	}
}

// applyRelease applies a release proposal's deltas via release.EditRelease.
func applyRelease(workdir, canonicalRef string, pMsg, parMsg *protocol.Message, attribution *protocol.Ref) Result[Outcome] {
	pf, parf := pMsg.Header.Fields, parMsg.Header.Fields
	opts := release.EditReleaseOptions{}
	if content, ok := contentDelta(pMsg, parMsg); ok {
		subj, body := protocol.SplitSubjectBody(content)
		opts.Subject, opts.Body = &subj, &body
	}
	if v, ok := changedField(pf, parf, "tag"); ok {
		opts.Tag = &v
	}
	if v, ok := changedField(pf, parf, "version"); ok {
		opts.Version = &v
	}
	if v, ok := changedField(pf, parf, "prerelease"); ok {
		b := v == "true"
		opts.Prerelease = &b
	}
	if v, ok := changedField(pf, parf, "labels"); ok {
		l := text.SplitCSV(v)
		opts.Labels = &l
	}
	if opts == (release.EditReleaseOptions{}) {
		return noDelta()
	}
	opts.Attribution = attribution
	return outcome(release.EditRelease(workdir, canonicalRef, opts), "release", canonicalRef)
}

// applyReview applies a PR content/label proposal via review.UpdatePR. PR
// lifecycle (state/draft) is base-owner re-home / author-only, never accepted.
func applyReview(workdir, canonicalRef string, pMsg, parMsg *protocol.Message, attribution *protocol.Ref) Result[Outcome] {
	if pMsg.Header.Fields["type"] != "pull-request" {
		return result.Err[Outcome]("UNSUPPORTED_TYPE", "accept only supports pull-request content edits")
	}
	pf, parf := pMsg.Header.Fields, parMsg.Header.Fields
	opts := review.UpdatePROptions{}
	if content, ok := contentDelta(pMsg, parMsg); ok {
		subj, body := protocol.SplitSubjectBody(content)
		opts.Subject, opts.Body = &subj, &body
	}
	if v, ok := changedField(pf, parf, "labels"); ok {
		l := text.SplitCSV(v)
		opts.Labels = &l
	}
	if opts == (review.UpdatePROptions{}) {
		return result.Err[Outcome]("NO_DELTA", "no acceptable content/label change (PR lifecycle is not accepted)")
	}
	opts.Attribution = attribution
	return outcome(review.UpdatePR(workdir, canonicalRef, opts), "review", canonicalRef)
}

// outcome converts an extension's typed Result into an Outcome.
func outcome[T any](res result.Result[T], ext, canonicalRef string) Result[Outcome] {
	if !res.Success {
		return result.Err[Outcome](res.Error.Code, res.Error.Message)
	}
	return result.Ok(Outcome{Ext: ext, CanonicalRef: canonicalRef})
}

func noDelta() Result[Outcome] {
	return result.Err[Outcome]("NO_DELTA", "proposal makes no acceptable change")
}

// contentDelta returns the proposal's content when it differs from the parent's.
func contentDelta(p, par *protocol.Message) (string, bool) {
	if p.Content != par.Content {
		return p.Content, true
	}
	return "", false
}

// changedField returns the proposal's value for key when it differs from the
// parent's (i.e. the proposal changed it). Empty proposal values are treated as
// "not set", never as a deletion.
func changedField(proposal, parent map[string]string, key string) (string, bool) {
	v := proposal[key]
	if v == "" || v == parent[key] {
		return "", false
	}
	return v, true
}
