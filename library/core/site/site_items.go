// site_items.go - the per-extension metadata index and search-body corpora

package site

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// siteItemsExts lists the extension data branches the item artifacts cover.
var siteItemsExts = []string{"social", "pm", "review", "release", "memo"}

const (
	// siteItemsKeyPrefix is the bucket namespace of the per-extension metadata indexes.
	siteItemsKeyPrefix = ".gitsocial/site/items/"
	// siteBodiesKeyPrefix is the bucket namespace of the per-extension search corpora.
	siteBodiesKeyPrefix = ".gitsocial/site/bodies/"
	// siteItemsVersion is the artifact JSON schema version shared by every gitmsg corpus doc.
	siteItemsVersion = 4
	// siteCodeItemsVersion is the code corpus's schema version; its entries carry parent shas.
	siteCodeItemsVersion = 5
)

// siteItemsWalkBudget bounds one push's artifact walk; a larger branch bootstraps over several pushes.
var siteItemsWalkBudget = siteItemsWalkBudgetFromEnv()

// siteItemsWalkBudgetFromEnv returns the per-push walk budget, honoring GITSOCIAL_SITE_WALK_BUDGET.
func siteItemsWalkBudgetFromEnv() int {
	if v := os.Getenv("GITSOCIAL_SITE_WALK_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 50000
}

// siteItemsDoc is one metadata document: a sealed shard, or the head, of an extension's items index.
type siteItemsDoc struct {
	Version int             `json:"version"`
	Tip     string          `json:"tip"`
	Items   []siteMetaEntry `json:"items"`
}

// siteMetaEntry is one indexed commit's metadata: sha, author identity and time, the raw GitMsg header line, and the subject.
type siteMetaEntry struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	TS      int64  `json:"ts"`
	Header  string `json:"header"`
	Subject string `json:"subject"`
	// Branch is a code entry's attributed branch; omitempty keeps gitmsg entries byte-identical.
	Branch string `json:"branch,omitempty"`
	// Parents is a code entry's parent shas, which the repository graph needs.
	Parents []string `json:"parents,omitempty"`
}

// entrySHA implements shardEntry for the metadata index corpus.
func (e siteMetaEntry) entrySHA() string { return e.SHA }

// siteBodyIndex is one bodies document: a sealed shard, or the head, of an extension's search corpus.
type siteBodyIndex struct {
	Version int             `json:"version"`
	Tip     string          `json:"tip"`
	Items   []siteBodyEntry `json:"items"`
}

// siteBodyEntry is one commit's search record: sha, author, time and the full raw message.
type siteBodyEntry struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	TS      int64  `json:"ts"`
	Message string `json:"message"`
}

// walkedItem is one commit read back from the bucket, carrying every field the two corpora project.
type walkedItem struct {
	SHA     string
	Author  string
	Email   string
	TS      int64
	Header  string
	Message string
	// Branch is set only by the code corpus walk; the gitmsg walks leave it empty.
	Branch string
	// Parents is set only by the code corpus walk; the gitmsg walks leave it nil.
	Parents []string
}

// metaOf projects a walked commit into a metadata-index entry.
func metaOf(w walkedItem) siteMetaEntry {
	return siteMetaEntry{SHA: w.SHA, Author: w.Author, Email: w.Email, TS: w.TS, Header: w.Header, Subject: subjectOf(w.Message)}
}

// siteLinkRefDefRE matches a CommonMark link reference definition line, which renders as nothing.
var siteLinkRefDefRE = regexp.MustCompile(`^ {0,3}\[[^\]]{1,128}\]:\s*\S+(\s+("[^"]*"|'[^']*'|\([^)]*\)))?\s*$`)

// siteHTMLCommentRE matches an HTML comment, the other thing that renders as nothing.
var siteHTMLCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

// siteSubjectMarkdown unwraps "[text](href)" and "![alt](src)" to their words.
var siteSubjectMarkdown = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// siteSubjectInlineHTML matches the inline HTML a comment body may carry.
var siteSubjectInlineHTML = regexp.MustCompile(`(?i)</?(br|hr|p|div|span|b|i|em|strong|code|kbd|sup|sub|img|a|details|summary)(\s[^<>]*)?/?>`)

// siteSubjectUnwrap is the rule set gs-core.js SUBJECT_UNWRAP mirrors, written without backreferences so one set serves both engines.
var siteSubjectUnwrap = []struct {
	re *regexp.Regexp
	to string
}{
	{regexp.MustCompile(`^\s{0,3}(#{1,6}\s+|>\s?)`), ""},
	{regexp.MustCompile(`\*\*([^*]+)\*\*`), "$1"},
	{regexp.MustCompile(`\*([^\s*][^*]*)\*`), "$1"},
	// Underscore emphasis is word-bounded, and RE2 has no lookaround, so the boundary is captured and re-emitted.
	{regexp.MustCompile(`(^|[\s(])__([^\s_][^_]*)__($|[\s).,;:!?])`), "${1}${2}${3}"},
	{regexp.MustCompile(`(^|[\s(])_([^\s_][^_]*)_($|[\s).,;:!?])`), "${1}${2}${3}"},
	{regexp.MustCompile("`([^`]+)`"), "$1"},
	{siteSubjectInlineHTML, " "},
}

var siteSubjectSpace = regexp.MustCompile(`\s+`)

// siteSubjectText projects a raw first line to the text a title shows; mirrors gs-core.js subjectText.
func siteSubjectText(line string) string {
	out := line
	for pass := 0; pass < 3 && siteSubjectMarkdown.MatchString(out); pass++ {
		out = siteSubjectMarkdown.ReplaceAllString(out, "$1")
	}
	// Twice: one match consumes the boundary the next one needs, so a single pass can leave the second wrapped.
	for pass := 0; pass < 2; pass++ {
		for _, rule := range siteSubjectUnwrap {
			out = rule.re.ReplaceAllString(out, rule.to)
		}
	}
	return strings.TrimSpace(siteSubjectSpace.ReplaceAllString(out, " "))
}

// siteStripLinkRefDefs removes HTML comments and the link reference definitions at a block start outside fenced code, as gs-core.js collectLinkRefDefs does.
func siteStripLinkRefDefs(content string) string {
	if strings.Contains(content, "<!--") {
		content = siteHTMLCommentRE.ReplaceAllString(content, "")
	}
	if !strings.Contains(content, "]:") {
		return content
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r", ""), "\n")
	kept := make([]string, 0, len(lines))
	fenced, atBlockStart := false, true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			fenced, atBlockStart = !fenced, false
		case fenced:
		case trimmed == "":
			atBlockStart = true
		case atBlockStart && siteLinkRefDefRE.MatchString(line):
			continue
		default:
			atBlockStart = false
		}
		kept = append(kept, line)
	}
	return strings.TrimLeft(strings.Join(kept, "\n"), "\n")
}

// subjectOf returns a message's subject line: the content with the GitMsg trailer block stripped, then its first line.
func subjectOf(message string) string {
	content := message
	if strings.HasPrefix(message, "GitMsg: ") {
		content = ""
	} else if i := strings.Index(message, "\nGitMsg: "); i != -1 {
		content = message[:i]
	}
	content = strings.TrimSpace(siteStripLinkRefDefs(strings.ReplaceAll(content, "\r", "")))
	subject, _, _ := strings.Cut(content, "\n")
	return siteSubjectText(subject)
}

// bodyOf projects a walked commit into a bodies-corpus entry.
func bodyOf(w walkedItem) siteBodyEntry {
	return siteBodyEntry{SHA: w.SHA, Author: w.Author, TS: w.TS, Message: w.Message}
}

// siteItemsDir is the per-extension metadata-index namespace.
func siteItemsDir(ext string) string {
	return siteItemsKeyPrefix + ext + "/"
}

// siteItemsManifestKey returns one extension's metadata-index manifest key.
func siteItemsManifestKey(ext string) string {
	return siteItemsDir(ext) + "manifest.json"
}

// siteItemsHeadKey returns one extension's metadata-index head key.
func siteItemsHeadKey(ext string) string {
	return siteItemsDir(ext) + "head.json"
}

// siteItemsCursorKey returns one extension's bootstrap-cursor key; a separate key so backfill and append writes do not contend.
func siteItemsCursorKey(ext string) string {
	return siteItemsDir(ext) + "cursor.json"
}

// siteItemsCursor records an in-progress bootstrap on the bucket, so it resumes across pushes and machines.
type siteItemsCursor struct {
	Version       int    `json:"version"`
	Tip           string `json:"tip"`
	OldestIndexed string `json:"oldestIndexed"`
	Complete      bool   `json:"complete"`
}

// readItemsCursor fetches one extension's bootstrap cursor; nil (no error) when
// absent, an older version, or unparseable.
func readItemsCursor(client *objstore.Client, prefix, ext string) (*siteItemsCursor, error) {
	var c siteItemsCursor
	found, err := objstore.ReadCompressedJSON(client, prefix+siteItemsCursorKey(ext), &c)
	if err != nil {
		return nil, err
	}
	if !found || c.Version != itemsDocVersion(ext) || len(c.OldestIndexed) != 40 {
		return nil, nil
	}
	return &c, nil
}

// putItemsCursor writes one extension's bootstrap cursor (no-cache, brotli q9).
func putItemsCursor(client *objstore.Client, prefix, ext, tip, oldestIndexed string) error {
	comp, err := objstore.CompressJSON(&siteItemsCursor{Version: itemsDocVersion(ext), Tip: tip, OldestIndexed: oldestIndexed}, objstore.BrotliQualityFull)
	if err != nil {
		return err
	}
	return objstore.PutCompressed(client, prefix+siteItemsCursorKey(ext), comp, "")
}

// deleteItemsCursor removes one extension's bootstrap cursor (the walk reached
// the branch root, so nothing is left to backfill).
func deleteItemsCursor(client *objstore.Client, prefix, ext string) error {
	return client.Delete(prefix + siteItemsCursorKey(ext))
}

// finalizeCursor writes or clears the bootstrap cursor; a nil pending both clears it and marks the manifests complete, so the two cannot disagree.
func finalizeCursor(client *objstore.Client, prefix, ext string, pending *siteItemsCursor) error {
	if pending == nil {
		return deleteItemsCursor(client, prefix, ext)
	}
	return putItemsCursor(client, prefix, ext, pending.Tip, pending.OldestIndexed)
}

// manifestOldestSha returns the oldest sha an items index covers; this is the backfill frontier, not the cursor's copy of it.
func manifestOldestSha(client *objstore.Client, prefix, ext string, manifest *siteShardManifest, itemsHead []siteMetaEntry) (string, error) {
	if manifest != nil && len(manifest.Shards) > 0 {
		entries, err := readItemsHeadEntries(client, prefix+siteItemsDir(ext)+manifest.Shards[0].Key)
		if err != nil {
			return "", err
		}
		if len(entries) > 0 {
			return entries[0].SHA, nil
		}
	}
	if len(itemsHead) > 0 {
		return itemsHead[0].SHA, nil
	}
	head, err := readItemsHeadEntries(client, prefix+siteItemsHeadKey(ext))
	if err != nil {
		return "", err
	}
	if len(head) > 0 {
		return head[0].SHA, nil
	}
	return "", nil
}

// reconstructCursor rebuilds a lost bootstrap cursor from an incomplete items manifest; nil when the manifest covers nothing.
func reconstructCursor(client *objstore.Client, prefix, ext string, manifest *siteShardManifest, newTip string) (*siteItemsCursor, error) {
	oldest, err := manifestOldestSha(client, prefix, ext, manifest, nil)
	if err != nil || len(oldest) != 40 {
		return nil, err
	}
	tip := manifest.Tip
	if tip == "" {
		tip = newTip
	}
	return &siteItemsCursor{Version: itemsDocVersion(ext), Tip: tip, OldestIndexed: oldest}, nil
}

// siteItemsShardKey returns a sealed metadata shard's full key under one
// extension's items dir.
func siteItemsShardKey(ext, hash string) string {
	return siteItemsDir(ext) + shardObjectName(hash)
}

// itemsDocVersion returns one corpus's metadata-index schema version.
func itemsDocVersion(ext string) int {
	if ext == siteCodeExt {
		return siteCodeItemsVersion
	}
	return siteItemsVersion
}

// itemsCorpus wires the metadata-index key names and doc marshaling into the
// generic shard layer.
var itemsCorpus = shardCorpus[siteMetaEntry]{
	label:       "items",
	manifestKey: siteItemsManifestKey,
	headKey:     siteItemsHeadKey,
	shardName:   shardObjectName,
	shardKey:    siteItemsShardKey,
	dir:         siteItemsDir,
	version:     itemsDocVersion,
	marshalDoc: func(ext, tip string, entries []siteMetaEntry) any {
		return &siteItemsDoc{Version: itemsDocVersion(ext), Tip: tip, Items: entries}
	},
}

// siteItemsExt maps a pushed ref name to the extension it indexes.
func siteItemsExt(refName string) string {
	ext, ok := strings.CutPrefix(refName, "refs/heads/gitmsg/")
	if !ok {
		return ""
	}
	for _, known := range siteItemsExts {
		if ext == known {
			return ext
		}
	}
	return ""
}

// bucketCommit is one commit read back from the bucket's loose objects.
type bucketCommit struct {
	item    walkedItem
	parents []string
}

// getBucketCommit fetches and parses one commit from the bucket: the loose key first, then the pack map on a miss.
func getBucketCommit(client *objstore.Client, prefix, sha string) (bucketCommit, error) {
	compressed, err := client.GetRetry(prefix + "objects/" + sha[:2] + "/" + sha[2:])
	if errors.Is(err, objstore.ErrNotFound) {
		c, ok, packErr := getPackedBucketCommit(client, prefix, sha)
		if packErr != nil {
			return bucketCommit{}, packErr
		}
		if ok {
			return c, nil
		}
	}
	if err != nil {
		return bucketCommit{}, fmt.Errorf("get object %s: %w", sha, err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return bucketCommit{}, fmt.Errorf("inflate object %s: %w", sha, err)
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return bucketCommit{}, fmt.Errorf("inflate object %s: %w", sha, err)
	}
	nul := bytes.IndexByte(raw, 0)
	if nul < 0 || !bytes.HasPrefix(raw, []byte("commit ")) {
		return bucketCommit{}, fmt.Errorf("object %s: not a commit", sha)
	}
	return parseBucketCommit(sha, raw[nul+1:])
}

// getPackedBucketCommit resolves one commit out of the bucket's packfiles; ok is false when the pack map has no usable entry.
func getPackedBucketCommit(client *objstore.Client, prefix, sha string) (bucketCommit, bool, error) {
	objType, body, ok, err := objstore.ReadPackedObject(client, prefix, sha)
	if err != nil || !ok {
		return bucketCommit{}, false, err
	}
	if objType != "commit" {
		return bucketCommit{}, false, fmt.Errorf("object %s: not a commit", sha)
	}
	c, err := parseBucketCommit(sha, body)
	if err != nil {
		return bucketCommit{}, false, err
	}
	return c, true, nil
}

// parseBucketCommit extracts parents, author identity and time, the message and the GitMsg header line from a raw commit body.
func parseBucketCommit(sha string, body []byte) (bucketCommit, error) {
	text := string(body)
	header, message, found := strings.Cut(text, "\n\n")
	if !found {
		header, message = text, ""
	}
	c := bucketCommit{item: walkedItem{SHA: sha, Message: message, Header: extractHeaderLine(message)}}
	for _, line := range strings.Split(header, "\n") {
		if parent, ok := strings.CutPrefix(line, "parent "); ok {
			c.parents = append(c.parents, strings.TrimSpace(parent))
		} else if author, ok := strings.CutPrefix(line, "author "); ok {
			c.item.Author, c.item.Email, c.item.TS = parseAuthorIdent(author)
		}
	}
	return c, nil
}

// extractHeaderLine returns the message's `GitMsg: ...` trailer line verbatim,
// or "" when the commit carries none.
func extractHeaderLine(message string) string {
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, "GitMsg: ") {
			return line
		}
	}
	return ""
}

// parseAuthorIdent splits a "Name <email> <unix-ts> <tz>" ident line.
func parseAuthorIdent(ident string) (name, email string, ts int64) {
	open := strings.LastIndex(ident, "<")
	end := strings.LastIndex(ident, ">")
	if open < 0 || end < open {
		return strings.TrimSpace(ident), "", 0
	}
	name = strings.TrimSpace(ident[:open])
	email = ident[open+1 : end]
	fields := strings.Fields(ident[end+1:])
	if len(fields) > 0 {
		ts, _ = strconv.ParseInt(fields[0], 10, 64)
	}
	return name, email, ts
}

// walkBucketItems walks parents from tip over the bucket's objects, stopping at stopAt and collecting at most budget commits newest-first.
func walkBucketItems(client *objstore.Client, prefix, tip string, stopAt map[string]bool, budget int, sp *siteProgress) ([]walkedItem, map[string]bool, bool, error) {
	return walkBucketItemsProgress(client, prefix, tip, stopAt, budget, 0, sp)
}

// walkBucketItemsProgress is walkBucketItems with an explicit progress ceiling; walkTotal 0 reports a plain count and changes no walk.
func walkBucketItemsProgress(client *objstore.Client, prefix, tip string, stopAt map[string]bool, budget, walkTotal int, sp *siteProgress) ([]walkedItem, map[string]bool, bool, error) {
	visited := map[string]bool{}
	met := map[string]bool{}
	frontier := []string{tip}
	items := []walkedItem{}
	for len(frontier) > 0 {
		sha := frontier[0]
		frontier = frontier[1:]
		if visited[sha] {
			continue
		}
		visited[sha] = true
		if stopAt[sha] {
			met[sha] = true
			continue
		}
		if len(items) >= budget {
			return items, met, true, nil
		}
		c, err := getCommit(sp.commitSource(), client, prefix, sha)
		if err != nil {
			return nil, nil, false, err
		}
		items = append(items, c.item)
		sp.walk(len(items), walkTotal)
		frontier = append(append([]string{}, c.parents...), frontier...)
	}
	return items, met, false, nil
}

// planItems seals a full items rebuild's shards and returns the plan.
func planItems(client *objstore.Client, prefix, ext string, meta []siteMetaEntry, sp *siteProgress) (shardPlan[siteMetaEntry], error) {
	return planSharded(client, itemsCorpus, prefix, ext, meta, nil, sp)
}

// planItemsAppend seals any shards an items gap fills, returning the plan.
func planItemsAppend(client *objstore.Client, prefix, ext string, gap, headItems []siteMetaEntry, manifest *siteShardManifest, sp *siteProgress) (shardPlan[siteMetaEntry], error) {
	return planAppend(client, itemsCorpus, prefix, ext, gap, headItems, manifest, sp)
}

// planItemsTail rebuilds an items corpus from its kept sealed shards plus a
// freshly-walked tail (REPAIR).
func planItemsTail(client *objstore.Client, prefix, ext string, keptShards []siteShardEntry, tail []siteMetaEntry, sp *siteProgress) (shardPlan[siteMetaEntry], error) {
	return planTail(client, itemsCorpus, prefix, ext, keptShards, tail, sp)
}

// putItemsHead writes an items plan's head document.
func putItemsHead(client *objstore.Client, prefix, ext, tip string, plan *shardPlan[siteMetaEntry]) error {
	return putHead(client, itemsCorpus, prefix, ext, tip, plan)
}

// putItemsManifest writes an items plan's manifest, recording the bodies corpus's compressed size.
func putItemsManifest(client *objstore.Client, prefix, ext, tip string, plan shardPlan[siteMetaEntry], bodiesBytes int, complete bool) error {
	_, err := putManifest(client, itemsCorpus, prefix, ext, tip, plan, bodiesBytes, complete)
	return err
}

// readItemsManifest fetches one extension's metadata-index manifest; nil when absent or unreadable.
func readItemsManifest(client *objstore.Client, prefix, ext string) (*siteShardManifest, error) {
	return readShardManifest(client, itemsCorpus, prefix, ext)
}

// readItemsHeadEntries fetches one metadata-index head document's entries.
func readItemsHeadEntries(client *objstore.Client, key string) ([]siteMetaEntry, error) {
	return readDocItems[siteMetaEntry](client, key)
}

// putSiteArtifacts rebuilds both corpora for one extension in the pinned write order; the manifests land last.
func putSiteArtifacts(client *objstore.Client, prefix, ext, tip string, items []walkedItem, complete bool, sp *siteProgress) error {
	meta := make([]siteMetaEntry, len(items))
	bodies := make([]siteBodyEntry, len(items))
	for i, w := range items {
		meta[i] = metaOf(w)
		bodies[i] = bodyOf(w)
	}
	bodiesPlan, err := planBodies(client, prefix, ext, bodies, sp)
	if err != nil {
		return err
	}
	itemsPlan, err := planItems(client, prefix, ext, meta, sp)
	if err != nil {
		return err
	}
	if err := putBodiesHead(client, prefix, ext, tip, &bodiesPlan); err != nil {
		return err
	}
	if err := putItemsHead(client, prefix, ext, tip, &itemsPlan); err != nil {
		return err
	}
	total, err := putBodiesManifest(client, prefix, ext, tip, bodiesPlan, complete)
	if err != nil {
		return err
	}
	return putItemsManifest(client, prefix, ext, tip, itemsPlan, total, complete)
}

// deleteSiteArtifacts removes every artifact for one extension whose branch is gone.
func deleteSiteArtifacts(client *objstore.Client, prefix, ext string) error {
	manifest, err := readItemsManifest(client, prefix, ext)
	if err != nil {
		return err
	}
	if manifest != nil {
		for _, s := range manifest.Shards {
			if err := client.Delete(prefix + siteItemsDir(ext) + s.Key); err != nil {
				return err
			}
		}
	}
	if err := client.Delete(prefix + siteItemsHeadKey(ext)); err != nil {
		return err
	}
	if err := client.Delete(prefix + siteItemsManifestKey(ext)); err != nil {
		return err
	}
	return deleteBodiesSharded(client, prefix, ext)
}

// updateSiteItemsIndex brings one extension's artifacts to newTip: it reads both manifests and head counts, classifies the state, and dispatches.
func updateSiteItemsIndex(client *objstore.Client, prefix, ext, newTip string, sp *siteProgress) error {
	items, err := readItemsManifest(client, prefix, ext)
	if err != nil {
		return err
	}
	bodies, err := readBodiesManifest(client, prefix, ext)
	if err != nil {
		return err
	}
	cursor, err := readItemsCursor(client, prefix, ext)
	if err != nil {
		return err
	}
	// A torn bootstrap has no cursor on the bucket, so rebuild it from the manifest rather than read the index as finished.
	if cursor == nil && items != nil && !items.Complete {
		if cursor, err = reconstructCursor(client, prefix, ext, items, newTip); err != nil {
			return err
		}
	}
	itemsHead, err := readItemsHeadEntries(client, prefix+siteItemsHeadKey(ext))
	if err != nil {
		return err
	}
	bodiesHead, err := readBodyDocItems(client, prefix+bodiesHeadKey(ext))
	if err != nil {
		return err
	}
	switch classifyItemsState(items, bodies, cursor, len(itemsHead), len(bodiesHead), newTip) {
	case actionNoOp:
		return nil
	case actionAppend:
		return appendItemsGap(client, prefix, ext, newTip, items, bodies, cursor, itemsHead, bodiesHead, sp)
	case actionRepair:
		return repairItemsState(client, prefix, ext, newTip, items, bodies, cursor, sp)
	case actionBackfill:
		return backfillItems(client, prefix, ext, cursor, items, itemsHead, sp)
	default: // actionBootstrap
		return bootstrapItems(client, prefix, ext, newTip, sp)
	}
}

// appendItemsGap walks the bounded gap from newTip to the corpora's common tip and extends both heads; a walk that misses that tip falls through to repair.
func appendItemsGap(client *objstore.Client, prefix, ext, newTip string, items, bodies *siteShardManifest, cursor *siteItemsCursor, itemsHead []siteMetaEntry, bodiesHead []siteBodyEntry, sp *siteProgress) error {
	known := map[string]bool{items.Tip: true}
	for _, e := range itemsHead {
		known[e.SHA] = true
	}
	gap, met, _, err := walkBucketItems(client, prefix, newTip, known, siteItemsWalkBudget, sp)
	if err != nil || !met[items.Tip] {
		return repairItemsState(client, prefix, ext, newTip, items, bodies, cursor, sp)
	}
	// Append owns the newest end alone: a pending bootstrap stays pending, with oldestIndexed untouched.
	if cursor == nil {
		return putGapArtifacts(client, prefix, ext, newTip, gap, items, bodies, itemsHead, bodiesHead, true, sp)
	}
	pending := &siteItemsCursor{Tip: newTip, OldestIndexed: cursor.OldestIndexed}
	if err := putGapArtifacts(client, prefix, ext, newTip, gap, items, bodies, itemsHead, bodiesHead, pending == nil, sp); err != nil {
		return err
	}
	return finalizeCursor(client, prefix, ext, pending)
}

// bootstrapItems seals the first budget segment of a fresh index, leaving a cursor when the budget is hit before the root.
func bootstrapItems(client *objstore.Client, prefix, ext, newTip string, sp *siteProgress) error {
	walked, _, budgetHit, err := walkBucketItemsProgress(client, prefix, newTip, nil, siteItemsWalkBudget, 0, sp)
	if err != nil {
		return err
	}
	var pending *siteItemsCursor
	if budgetHit {
		pending = &siteItemsCursor{Tip: newTip, OldestIndexed: walked[len(walked)-1].SHA}
	}
	if err := putSiteArtifacts(client, prefix, ext, newTip, walked, pending == nil, sp); err != nil {
		return err
	}
	return finalizeCursor(client, prefix, ext, pending)
}

// backfillItems seals the next older budget segment of an in-progress bootstrap and prepends it to both manifests, leaving the head alone.
func backfillItems(client *objstore.Client, prefix, ext string, cursor *siteItemsCursor, items *siteShardManifest, itemsHead []siteMetaEntry, sp *siteProgress) error {
	frontier, err := manifestOldestSha(client, prefix, ext, items, itemsHead)
	if err != nil {
		return err
	}
	if frontier == "" {
		return completeBackfill(client, prefix, ext)
	}
	oldest, err := getCommit(sp.commitSource(), client, prefix, frontier)
	if err != nil {
		return err
	}
	if len(oldest.parents) == 0 {
		// No older history: the frontier is the branch root; just complete it.
		return completeBackfill(client, prefix, ext)
	}
	// Stop at every indexed boundary, not the frontier alone, so a merge parent reachable from two sides lands in one shard.
	stop := map[string]bool{frontier: true}
	if items != nil {
		stop[items.Tip] = true
		for _, s := range items.Shards {
			stop[s.EndTip] = true
		}
	}
	for _, e := range itemsHead {
		stop[e.SHA] = true
	}
	segment, budgetHit := []walkedItem{}, false
	for _, p := range oldest.parents {
		seg, _, hit, err := walkBucketItemsProgress(client, prefix, p, stop, siteItemsWalkBudget-len(segment), 0, sp)
		if err != nil {
			return err
		}
		for _, w := range seg {
			stop[w.SHA] = true
		}
		segment = append(segment, seg...)
		if hit {
			budgetHit = true
			break
		}
	}
	if len(segment) == 0 {
		return completeBackfill(client, prefix, ext)
	}
	var pending *siteItemsCursor
	if budgetHit {
		pending = &siteItemsCursor{Tip: cursor.Tip, OldestIndexed: segment[len(segment)-1].SHA}
	}
	if err := prependSegment(client, prefix, ext, segment, pending == nil, sp); err != nil {
		return err
	}
	return finalizeCursor(client, prefix, ext, pending)
}

// completeBackfill marks both manifests complete and clears the cursor when no older history remains.
func completeBackfill(client *objstore.Client, prefix, ext string) error {
	if err := markManifestComplete(client, bodiesCorpus, prefix, ext); err != nil {
		return err
	}
	if err := markManifestComplete(client, itemsCorpus, prefix, ext); err != nil {
		return err
	}
	return deleteItemsCursor(client, prefix, ext)
}

// markManifestComplete re-reads one corpus's manifest and re-writes it complete; a no-op when it is gone.
func markManifestComplete[E shardEntry](client *objstore.Client, corpus shardCorpus[E], prefix, ext string) error {
	m, err := readShardManifest(client, corpus, prefix, ext)
	if err != nil || m == nil {
		return err
	}
	m.Complete = true
	return putShardManifest(client, corpus, prefix, ext, m)
}

// prependSegment seals one backfilled older segment for both corpora and prepends it to their manifests.
func prependSegment(client *objstore.Client, prefix, ext string, segment []walkedItem, complete bool, sp *siteProgress) error {
	segMeta := make([]siteMetaEntry, 0, len(segment))
	segBodies := make([]siteBodyEntry, 0, len(segment))
	for _, w := range segment {
		segMeta = append(segMeta, metaOf(w))
		segBodies = append(segBodies, bodyOf(w))
	}
	bodiesHead, err := readBodyDocItems(client, prefix+bodiesHeadKey(ext))
	if err != nil {
		return err
	}
	bodiesPlan, bodiesTip, err := prependSegmentPlan(client, bodiesCorpus, prefix, ext, segBodies, bodiesHead, sp)
	if err != nil {
		return err
	}
	bodiesBytes, err := putManifest(client, bodiesCorpus, prefix, ext, bodiesTip, bodiesPlan, 0, complete)
	if err != nil {
		return err
	}
	itemsHead, err := readItemsHeadEntries(client, prefix+siteItemsHeadKey(ext))
	if err != nil {
		return err
	}
	itemsPlan, itemsTip, err := prependSegmentPlan(client, itemsCorpus, prefix, ext, segMeta, itemsHead, sp)
	if err != nil {
		return err
	}
	_, err = putManifest(client, itemsCorpus, prefix, ext, itemsTip, itemsPlan, bodiesBytes, complete)
	return err
}

// putGapArtifacts appends a freshly-walked gap to both corpora's heads in the pinned write order.
func putGapArtifacts(client *objstore.Client, prefix, ext, newTip string, gap []walkedItem, itemsManifest, bodiesManifest *siteShardManifest, itemsHead []siteMetaEntry, bodiesHead []siteBodyEntry, complete bool, sp *siteProgress) error {
	gapMeta := make([]siteMetaEntry, 0, len(gap))
	gapBodies := make([]siteBodyEntry, 0, len(gap))
	for _, w := range gap {
		gapMeta = append(gapMeta, metaOf(w))
		gapBodies = append(gapBodies, bodyOf(w))
	}
	bodiesPlan, err := planBodiesAppend(client, prefix, ext, gapBodies, bodiesHead, bodiesManifest, sp)
	if err != nil {
		return err
	}
	itemsPlan, err := planItemsAppend(client, prefix, ext, gapMeta, itemsHead, itemsManifest, sp)
	if err != nil {
		return err
	}
	if err := putBodiesHead(client, prefix, ext, newTip, &bodiesPlan); err != nil {
		return err
	}
	if err := putItemsHead(client, prefix, ext, newTip, &itemsPlan); err != nil {
		return err
	}
	total, err := putBodiesManifest(client, prefix, ext, newTip, bodiesPlan, complete)
	if err != nil {
		return err
	}
	return putItemsManifest(client, prefix, ext, newTip, itemsPlan, total, complete)
}

// rebuildSiteItems drives every data branch in refs, plus the code index, through the same state machine as a helper push.
func rebuildSiteItems(client *objstore.Client, prefix string, refs map[string]string, defaultBranch string, src *objstore.LocalCommitSource, progress objstore.Progress) error {
	for _, ext := range siteItemsExts {
		tip, ok := refs["refs/heads/gitmsg/"+ext]
		if !ok {
			continue
		}
		sp := &siteProgress{progress: progress, ext: ext, src: src}
		if err := updateSiteItemsIndex(client, prefix, ext, tip, sp); err != nil {
			return fmt.Errorf("build items index %s: %w", ext, err)
		}
	}
	tips := codeBranchTips(refs, defaultBranch)
	sp := &siteProgress{progress: progress, ext: siteCodeExt, src: src}
	if err := updateSiteCodeIndex(client, prefix, tips, defaultBranch, sp); err != nil {
		return fmt.Errorf("build code index: %w", err)
	}
	return nil
}

// siteItemsBootstrapPending reports whether any items index still needs work a full site pass runs; a read error counts as pending.
func siteItemsBootstrapPending(client *objstore.Client, prefix string, refs map[string]string) bool {
	for _, ext := range siteItemsExts {
		if _, ok := refs["refs/heads/gitmsg/"+ext]; !ok {
			continue
		}
		items, err := readItemsManifest(client, prefix, ext)
		if err != nil || items == nil || !items.Complete {
			return true
		}
	}
	return codeIndexBootstrapPending(client, prefix)
}
