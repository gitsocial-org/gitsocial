// messages.go - Message builders for fast-import batch creation
package importpkg

import (
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var (
	issueFieldOrder     = []string{"state", "assignees", "due", "milestone", "sprint", "parent", "root", "blocks", "blocked-by", "related", "labels"}
	milestoneFieldOrder = []string{"state", "due"}
	releaseFieldOrder   = []string{"artifact-url", "artifacts", "checksums", "prerelease", "sbom", "signed-by", "tag", "version"}
	prFieldOrder        = []string{"state", "draft", "base", "base-tip", "head", "head-tip", "closes", "merge-base", "merge-head", "reviewers", "labels"}
	socialFieldOrder    = []string{"reply-to", "original"}
)

// buildMilestoneMessage constructs a milestone commit message matching pm.buildMilestoneContent.
func buildMilestoneMessage(title, body, state string, due *time.Time, editsRef string, origin *protocol.Origin) string {
	content := title
	if body != "" {
		content += "\n\n" + body
	}
	fields := map[string]string{
		"type":  "milestone",
		"state": state,
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if due != nil {
		fields["due"] = due.Format("2006-01-02")
	}
	protocol.ApplyOrigin(fields, origin)
	header := protocol.Header{Ext: "pm", V: "0.1.0", Fields: fields, FieldOrder: milestoneFieldOrder}
	return protocol.FormatMessage(content, header, nil)
}

// buildIssueMessage constructs an issue commit message matching pm.buildIssueContentWithEdits.
func buildIssueMessage(subject, body, state string, assignees []string, due *time.Time, milestone, labels string, editsRef string, origin *protocol.Origin) string {
	content := subject
	if body != "" {
		content += "\n\n" + body
	}
	if state == "" {
		state = "open"
	}
	fields := map[string]string{
		"type":  "issue",
		"state": state,
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if len(assignees) > 0 {
		fields["assignees"] = strings.Join(assignees, ",")
	}
	if due != nil {
		fields["due"] = due.Format("2006-01-02")
	}
	if milestone != "" {
		fields["milestone"] = milestone
	}
	if labels != "" {
		fields["labels"] = labels
	}
	protocol.ApplyOrigin(fields, origin)
	header := protocol.Header{Ext: "pm", V: "0.1.0", Fields: fields, FieldOrder: issueFieldOrder}
	return protocol.FormatMessage(content, header, nil)
}

// buildReleaseMessage constructs a release commit message matching release.buildReleaseContent.
func buildReleaseMessage(rel ImportRelease, editsRef string, origin *protocol.Origin) string {
	content := rel.Name
	if rel.Body != "" {
		content += "\n\n" + rel.Body
	}
	fields := map[string]string{
		"type": "release",
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if rel.ArtifactURL != "" {
		fields["artifact-url"] = rel.ArtifactURL
	}
	if len(rel.Artifacts) > 0 {
		fields["artifacts"] = strings.Join(rel.Artifacts, ",")
	}
	if rel.Checksums != "" {
		fields["checksums"] = rel.Checksums
	}
	if rel.Prerelease {
		fields["prerelease"] = "true"
	}
	if rel.SBOM != "" {
		fields["sbom"] = rel.SBOM
	}
	if rel.SignedBy != "" {
		fields["signed-by"] = rel.SignedBy
	}
	if rel.Tag != "" {
		fields["tag"] = rel.Tag
	}
	if rel.Version != "" {
		fields["version"] = rel.Version
	}
	protocol.ApplyOrigin(fields, origin)
	header := protocol.Header{Ext: "release", V: "0.1.0", Fields: fields, FieldOrder: releaseFieldOrder}
	return protocol.FormatMessage(content, header, nil)
}

// buildPRMessage constructs a pull request commit message matching review.buildPRContentWithState.
func buildPRMessage(subject, body, state string, draft bool, base, baseTip, head, headTip string, reviewers, labels []string, mergeBase, mergeHead, editsRef string, origin *protocol.Origin) string {
	content := subject
	if body != "" {
		content += "\n\n" + body
	}
	if state == "" {
		state = "open"
	}
	fields := map[string]string{
		"type":  "pull-request",
		"state": state,
	}
	if draft {
		fields["draft"] = "true"
	}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	if base != "" {
		fields["base"] = base
	}
	if head != "" {
		fields["head"] = head
	}
	if len(reviewers) > 0 {
		fields["reviewers"] = strings.Join(reviewers, ",")
	}
	if len(labels) > 0 {
		fields["labels"] = strings.Join(labels, ",")
	}
	if baseTip != "" {
		fields["base-tip"] = baseTip
	}
	if headTip != "" {
		fields["head-tip"] = headTip
	}
	if mergeBase != "" {
		fields["merge-base"] = mergeBase
	}
	if mergeHead != "" {
		fields["merge-head"] = mergeHead
	}
	protocol.ApplyOrigin(fields, origin)
	header := protocol.Header{Ext: "review", V: "0.1.0", Fields: fields, FieldOrder: prFieldOrder}
	return protocol.FormatMessage(content, header, nil)
}

// buildPostMessage constructs a social post commit message matching social.CreatePost.
func buildPostMessage(content, editsRef string, origin *protocol.Origin) string {
	fields := map[string]string{"type": "post"}
	if editsRef != "" {
		fields["edits"] = editsRef
	}
	protocol.ApplyOrigin(fields, origin)
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: fields, FieldOrder: socialFieldOrder}
	return protocol.FormatMessage(content, header, nil)
}

// buildCommentMessage constructs a social comment commit message matching social.createInteraction.
func buildCommentMessage(content, originalRef string, ref *protocol.Ref, origin *protocol.Origin) string {
	fields := map[string]string{
		"type":     "comment",
		"original": originalRef,
	}
	protocol.ApplyOrigin(fields, origin)
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: fields, FieldOrder: socialFieldOrder}
	var refs []protocol.Ref
	if ref != nil {
		refs = append(refs, *ref)
	}
	return protocol.FormatMessage(content, header, refs)
}
