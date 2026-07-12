// site_items.go - push-maintained per-extension static-site item artifacts.
//
// Two artifacts are written per gitmsg extension data branch, both append-only,
// ingestion-order sharded (see site_shards.go) under a per-extension directory,
// both brotli-compressed and uploaded with `Content-Encoding: br` so browsers
// (and Node's fetch) decode them transparently over HTTPS/localhost:
//
//   - .gitsocial/site/items/<ext>/  metadata index: one entry per commit
//     carrying only what the reader needs before a body is fetched (sha, author
//     identity, author time, the raw `GitMsg:` header line, and the subject
//     line). The header line keeps every relation (edits/original/reply-to/type/
//     state/origin-*) parseable by the browser's existing header parser, so list
//     ordering, edit/retraction resolution, and thread/relationship walks all
//     work from the index alone. The user-visible message BODY (beyond the
//     subject) is deliberately absent — the reader lazily fetches the loose
//     object for the handful of items actually on screen. Sharded so the reader
//     loads only the newest shard + head every page, not the whole lifetime.
//   - .gitsocial/site/bodies/<ext>/  search corpus: one entry per commit with the
//     full raw message (plus sha/author/ts search needs to rank hits). Loaded
//     only by the search route, on the user's explicit full-text request — light
//     search runs over the metadata index alone.
//
// Both are built from the bucket's own loose objects (uploaded before any ref
// moves), so the artifacts always match what a reader can resolve — and objstore
// stays free of a core/git dependency. Both corpora advance together in one
// push, in a pinned write order (seal bodies shards, seal items shards, bodies
// head, items head, bodies manifest, items manifest): manifests land last, the
// bodies manifest's TotalBytes threads into the items manifest as bodiesBytes,
// and any interruption leaves a state the repair machine (site_repair.go)
// rebuilds on the next push.

package objstore

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
)

// siteItemsExts lists the extension data branches the item artifacts cover.
var siteItemsExts = []string{"social", "pm", "review", "release", "memo"}

const (
	// siteItemsKeyPrefix is the bucket namespace of the per-extension metadata indexes.
	siteItemsKeyPrefix = ".gitsocial/site/items/"
	// siteBodiesKeyPrefix is the bucket namespace of the per-extension search corpora.
	siteBodiesKeyPrefix = ".gitsocial/site/bodies/"
	// siteItemsVersion is the artifact JSON schema version, shared by every
	// artifact doc (items shards, bodies shards, both manifests, the cursor).
	// A reader treats anything not at the current version as absent and falls
	// back to the bounded loose-object walk until a push rewrites the artifacts.
	siteItemsVersion = 4
	// siteCodeItemsVersion is the CODE corpus's schema version: v5 entries carry
	// the commit's parent shas so the repository graph renders from the index
	// instead of a per-commit loose-object walk. The version salts the code
	// corpus's shard content hashes (shardContentHash), so a push onto a v4
	// bucket sees no valid manifest, re-bootstraps, and seals fresh v5 shards
	// under new keys. The gitmsg corpora stay at siteItemsVersion, byte-identical.
	siteCodeItemsVersion = 5
	// brotliQualityFull is used for every no-cache doc (both corpora's heads and
	// manifests, the cursor). Quality 9 (not the max 11) because max quality on a
	// ~50MB corpus takes ~60s in the pure-Go encoder for only ~10% smaller output,
	// not worth the wall time; sealed shards use max quality once
	// (brotliQualityShard). Both decode transparently via Content-Encoding.
	brotliQualityFull = 9
)

// siteItemsWalkBudget bounds ONE push's artifact walk. It is no longer a fatal
// cap: a branch larger than the budget bootstraps over many pushes (each seals
// up to a budget's worth of older commits and leaves a cursor for the next push
// to resume from), so the walk returns "budget hit vs root reached" rather than
// erroring. A var (not a const) so tests can lower it, and
// GITSOCIAL_SITE_WALK_BUDGET overrides it for the site-test fixture (unset in
// production, so the value stays 50000).
var siteItemsWalkBudget = siteItemsWalkBudgetFromEnv()

// siteItemsWalkBudgetFromEnv returns the per-push walk budget, honoring a
// positive GITSOCIAL_SITE_WALK_BUDGET override, else the 50000 default.
func siteItemsWalkBudgetFromEnv() int {
	if v := os.Getenv("GITSOCIAL_SITE_WALK_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 50000
}

// siteItemsDoc is one metadata document (a sealed shard or the head under
// .gitsocial/site/items/<ext>/) — the metadata-index counterpart of
// siteBodyIndex, carrying one siteMetaEntry per commit.
type siteItemsDoc struct {
	Version int             `json:"version"`
	Tip     string          `json:"tip"`
	Items   []siteMetaEntry `json:"items"`
}

// siteMetaEntry is one indexed commit's metadata: sha, the author identity and
// time (native items have no origin-* in the header, so display and sort need
// these before any body fetch), the raw `GitMsg:` header line (relations,
// type/state, origin-* — parsed by the reader's existing header parser), and
// the subject line (first line of the trailer-stripped content). The subject
// rides in the always-loaded index so the site's light search matches items by
// title for free, without downloading the bodies corpus.
type siteMetaEntry struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	TS      int64  `json:"ts"`
	Header  string `json:"header"`
	Subject string `json:"subject"`
	// Branch is the attributed branch for a CODE-corpus entry (the default branch
	// when the commit is reachable from it, else the first code branch that reached
	// it), so the reader links a code commit's card under a real branch route
	// without a loose-object walk. `omitempty` keeps every gitmsg-extension entry
	// byte-identical to before (they never set it), so their sealed shards' content
	// hashes and keys are unchanged.
	Branch string `json:"branch,omitempty"`
	// Parents is the commit's parent shas, set only for CODE-corpus entries
	// (v5+): the repository graph needs the parent DAG, which nothing else in the
	// index carries. `omitempty` keeps gitmsg-extension entries byte-identical.
	Parents []string `json:"parents,omitempty"`
}

// entrySHA implements shardEntry for the metadata index corpus.
func (e siteMetaEntry) entrySHA() string { return e.SHA }

// siteBodyIndex is one bodies document (a sealed shard or the head under
// .gitsocial/site/bodies/<ext>/) — the shared search-corpus doc shape.
type siteBodyIndex struct {
	Version int             `json:"version"`
	Tip     string          `json:"tip"`
	Items   []siteBodyEntry `json:"items"`
}

// siteBodyEntry is one commit's search record: sha, author and time (to rank
// hits) and the full raw message (the searchable content plus header fields).
type siteBodyEntry struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	TS      int64  `json:"ts"`
	Message string `json:"message"`
}

// walkedItem is one commit read back from the bucket, carrying every field the
// two artifacts project (metadata index and bodies corpus).
type walkedItem struct {
	SHA     string
	Author  string
	Email   string
	TS      int64
	Header  string
	Message string
	// Branch is set only by the code corpus walk (walkCodeItems): the branch a
	// code commit is attributed to. Empty for the gitmsg-extension walks.
	Branch string
	// Parents is set only by the code corpus walk: the commit's parent shas,
	// projected into the v5 code metadata entries for the graph. Nil for the
	// gitmsg-extension walks.
	Parents []string
}

// metaOf projects a walked commit into a metadata-index entry.
func metaOf(w walkedItem) siteMetaEntry {
	return siteMetaEntry{SHA: w.SHA, Author: w.Author, Email: w.Email, TS: w.TS, Header: w.Header, Subject: subjectOf(w.Message)}
}

// subjectOf returns a message's subject line: the content with the GitMsg
// trailer block stripped (mirroring gs-core.js cleanContent — the trailer
// starts at the first line beginning `GitMsg: `), CRs removed, then the first
// line, trimmed.
func subjectOf(message string) string {
	content := message
	if strings.HasPrefix(message, "GitMsg: ") {
		content = ""
	} else if i := strings.Index(message, "\nGitMsg: "); i != -1 {
		content = message[:i]
	}
	content = strings.TrimSpace(strings.ReplaceAll(content, "\r", ""))
	subject, _, _ := strings.Cut(content, "\n")
	return strings.TrimSpace(subject)
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

// siteItemsCursorKey returns one extension's bootstrap-cursor key. A tiny
// separate key (not a manifest field) so the "backfill one older segment" write
// and the "append new tip commits" write never contend on the same object.
func siteItemsCursorKey(ext string) string {
	return siteItemsDir(ext) + "cursor.json"
}

// siteItemsCursor records an in-progress bootstrap so it resumes across pushes
// (and machines: it lives on the bucket). tip is the branch tip the partial
// index was grown toward (guards against a concurrent tip advance mid-bootstrap);
// oldestIndexed is the oldest sha sealed so far (BACKFILL's frontier for the next
// budget segment); complete flips true once the walk reaches the branch root, at
// which point the cursor key is deleted. Like every head/manifest it is
// brotli-compressed and served no-cache.
type siteItemsCursor struct {
	Version       int    `json:"version"`
	Tip           string `json:"tip"`
	OldestIndexed string `json:"oldestIndexed"`
	Complete      bool   `json:"complete"`
}

// readItemsCursor fetches one extension's bootstrap cursor; nil (no error) when
// absent, an older version, or unparseable.
func readItemsCursor(client *Client, prefix, ext string) (*siteItemsCursor, error) {
	var c siteItemsCursor
	found, err := readCompressedJSON(client, prefix+siteItemsCursorKey(ext), &c)
	if err != nil {
		return nil, err
	}
	if !found || c.Version != itemsDocVersion(ext) || len(c.OldestIndexed) != 40 {
		return nil, nil
	}
	return &c, nil
}

// putItemsCursor writes one extension's bootstrap cursor (no-cache, brotli q9).
func putItemsCursor(client *Client, prefix, ext, tip, oldestIndexed string) error {
	comp, err := compressJSON(&siteItemsCursor{Version: itemsDocVersion(ext), Tip: tip, OldestIndexed: oldestIndexed}, brotliQualityFull)
	if err != nil {
		return err
	}
	return putCompressed(client, prefix+siteItemsCursorKey(ext), comp)
}

// deleteItemsCursor removes one extension's bootstrap cursor (the walk reached
// the branch root, so nothing is left to backfill).
func deleteItemsCursor(client *Client, prefix, ext string) error {
	return client.Delete(prefix + siteItemsCursorKey(ext))
}

// finalizeCursor writes or clears the bootstrap cursor to match the walk's
// post-write state: a nil pending clears it (the walk reached the root — index
// complete), else it records tip/oldestIndexed for the next backfill. It is the
// single translation of "is the index complete?" into the cursor object, and the
// same nil-ness feeds the manifests' complete flag (pending == nil), so no writer
// path can mark a manifest complete while leaving a cursor pending. Called after
// the manifests (the pinned cursor-last order), so an
// interrupted cursor write leaves only complete=false manifests to reconstruct.
func finalizeCursor(client *Client, prefix, ext string, pending *siteItemsCursor) error {
	if pending == nil {
		return deleteItemsCursor(client, prefix, ext)
	}
	return putItemsCursor(client, prefix, ext, pending.Tip, pending.OldestIndexed)
}

// manifestOldestSha returns the oldest sha an items index already covers: the
// first (oldest) member of the oldest sealed shard, or the oldest head entry
// when nothing is sealed yet, or "" when it covers nothing. This is the
// authoritative backfill frontier (the manifest, not the cursor's stale copy).
func manifestOldestSha(client *Client, prefix, ext string, manifest *siteShardManifest, itemsHead []siteMetaEntry) (string, error) {
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

// reconstructCursor rebuilds an in-progress bootstrap cursor from an incomplete
// items manifest whose real cursor was lost (a BOOTSTRAP interrupted between its
// manifest writes and its cursor write). tip carries the manifest's recorded
// branch tip (the index was grown toward it) so BACKFILL and APPEND see the same
// tip a live cursor would. Returns nil (no error) only when the manifest covers
// nothing indexable, in which case the caller keeps a nil cursor.
func reconstructCursor(client *Client, prefix, ext string, manifest *siteShardManifest, newTip string) (*siteItemsCursor, error) {
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

// itemsDocVersion returns the metadata-index schema version for one corpus:
// siteCodeItemsVersion for the code corpus (entries carry parents), else the
// shared siteItemsVersion.
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

// siteItemsExt maps a pushed ref name to the extension it indexes ("" for
// refs outside the well-known gitmsg data branches).
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

// getBucketCommit fetches and parses one commit object from the bucket. The GET
// retries transient faults (a 503 mid-walk, a dropped connection): a long
// bootstrap walk over thousands of commits must survive one provider hiccup
// rather than lose the whole pass.
func getBucketCommit(client *Client, prefix, sha string) (bucketCommit, error) {
	compressed, err := client.GetRetry(prefix + "objects/" + sha[:2] + "/" + sha[2:])
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

// parseBucketCommit extracts parents, author identity/time, the verbatim
// message (for the bodies corpus) and the `GitMsg:` header line (for the
// metadata index) from a raw commit object body.
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

// walkBucketItems walks parent pointers from tip over the bucket's objects
// (mirroring the site's BFS: parents ahead of the remaining frontier), skipping
// descent into shas listed in stopAt, collecting AT MOST budget commits. It
// returns the collected commits newest-first, the set of stopAt shas
// encountered, and budgetHit — true when the walk stopped because it reached the
// budget with commits still unvisited (the branch is larger than one push can
// index, so BOOTSTRAP/BACKFILL resume from the collected segment's oldest sha),
// false when the frontier emptied first (every reachable commit down to root or
// stopAt is collected). A bounded gap walk (APPEND / REPAIR tail) passes the
// same budget; its stopAt frontier terminates it well below the budget, so
// budgetHit stays false there.
func walkBucketItems(client *Client, prefix, tip string, stopAt map[string]bool, budget int, sp *siteProgress) ([]walkedItem, map[string]bool, bool, error) {
	return walkBucketItemsProgress(client, prefix, tip, stopAt, budget, 0, sp)
}

// walkBucketItemsProgress is walkBucketItems with an explicit progress total:
// walkTotal is the ceiling reported to the Progress hook. Every current caller
// passes 0 (a plain count): the walk budget is a per-push CAP the walk usually
// won't reach, not the branch size, so reporting done/budget would show a
// misleading percentage; and neither the manifest nor the cursor tracks the true
// remaining commit count. walkTotal never affects the walk itself, only the
// progress label — a caller that DID know the real remaining size could pass it
// for an honest percentage.
func walkBucketItemsProgress(client *Client, prefix, tip string, stopAt map[string]bool, budget, walkTotal int, sp *siteProgress) ([]walkedItem, map[string]bool, bool, error) {
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

// brotliCompress encodes bytes at the given quality.
func brotliCompress(data []byte, quality int) ([]byte, error) {
	var buf bytes.Buffer
	w := brotli.NewWriterOptions(&buf, brotli.WriterOptions{Quality: quality})
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("brotli write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("brotli close: %w", err)
	}
	return buf.Bytes(), nil
}

// brotliDecompress decodes brotli bytes.
func brotliDecompress(data []byte) ([]byte, error) {
	return io.ReadAll(brotli.NewReader(bytes.NewReader(data)))
}

// compressJSON marshals a document and brotli-compresses it at the given quality.
func compressJSON(v any, quality int) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	compressed, err := brotliCompress(data, quality)
	if err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	return compressed, nil
}

// putCompressed uploads pre-compressed bytes with `Content-Encoding: br` object
// metadata so readers decode transparently.
func putCompressed(client *Client, key string, compressed []byte) error {
	resp, err := client.do(http.MethodPut, key, nil, compressed, map[string]string{
		"Content-Type":     "application/json",
		"Content-Encoding": "br",
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	resp.Body.Close()
	return nil
}

// planItems seals a full items (re)build's shards, returning the plan (staged so
// the two corpora can interleave shard/head/manifest writes).
func planItems(client *Client, prefix, ext string, meta []siteMetaEntry, sp *siteProgress) (shardPlan[siteMetaEntry], error) {
	return planSharded(client, itemsCorpus, prefix, ext, meta, nil, sp)
}

// planItemsAppend seals any shards an items gap fills, returning the plan.
func planItemsAppend(client *Client, prefix, ext string, gap, headItems []siteMetaEntry, manifest *siteShardManifest, sp *siteProgress) (shardPlan[siteMetaEntry], error) {
	return planAppend(client, itemsCorpus, prefix, ext, gap, headItems, manifest, sp)
}

// planItemsTail rebuilds an items corpus from its kept sealed shards plus a
// freshly-walked tail (REPAIR).
func planItemsTail(client *Client, prefix, ext string, keptShards []siteShardEntry, tail []siteMetaEntry, sp *siteProgress) (shardPlan[siteMetaEntry], error) {
	return planTail(client, itemsCorpus, prefix, ext, keptShards, tail, sp)
}

// putItemsHead writes an items plan's head document.
func putItemsHead(client *Client, prefix, ext, tip string, plan *shardPlan[siteMetaEntry]) error {
	return putHead(client, itemsCorpus, prefix, ext, tip, plan)
}

// putItemsManifest assembles and writes an items plan's manifest, recording the
// bodies corpus's total compressed size (bodiesBytes). complete is false while a
// bootstrap is still backfilling older history.
func putItemsManifest(client *Client, prefix, ext, tip string, plan shardPlan[siteMetaEntry], bodiesBytes int, complete bool) error {
	_, err := putManifest(client, itemsCorpus, prefix, ext, tip, plan, bodiesBytes, complete)
	return err
}

// readItemsManifest fetches one extension's metadata-index manifest; nil (no
// error) when absent, an older version, or unparseable.
func readItemsManifest(client *Client, prefix, ext string) (*siteShardManifest, error) {
	return readShardManifest(client, itemsCorpus, prefix, ext)
}

// readItemsHeadEntries fetches one metadata-index head document's entries (nil
// when absent or unparseable).
func readItemsHeadEntries(client *Client, key string) ([]siteMetaEntry, error) {
	return readDocItems[siteMetaEntry](client, key)
}

// readCompressedJSON fetches and brotli-decodes a document into v. found is
// false (no error) when the key is absent, or when the stored bytes are not
// valid brotli/JSON — so callers fall back to a fresh walk.
func readCompressedJSON(client *Client, key string, v any) (found bool, err error) {
	data, err := client.GetRetry(key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", key, err)
	}
	raw, err := brotliDecompress(data)
	if err != nil {
		return false, nil
	}
	if json.Unmarshal(raw, v) != nil {
		return false, nil
	}
	return true, nil
}

// putSiteArtifacts (re)builds both sharded corpora for one extension from a
// walked commit list, in the pinned write order (seal bodies shards, seal items
// shards, bodies head, items head, bodies manifest, items manifest). Manifests
// are the only commit points and land last, so any interruption leaves at worst
// "bodies ahead of items", which REPAIR handles; sealed shards are content-hash
// keyed (skip-existing) and heads are re-writable, so every earlier write is
// idempotent on retry. bodiesBytes is threaded from the bodies manifest onto the
// items manifest for the reader's full-search download size. complete is false
// when the walk stopped at the budget with older history still to backfill.
func putSiteArtifacts(client *Client, prefix, ext, tip string, items []walkedItem, complete bool, sp *siteProgress) error {
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

// deleteSiteArtifacts removes every artifact for one extension (branch deleted):
// the whole sharded items set (each shard enumerated from its manifest, then
// head + manifest) plus the whole sharded bodies set.
func deleteSiteArtifacts(client *Client, prefix, ext string) error {
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

// updateSiteItemsIndex brings one extension's artifacts to newTip. It reads the
// four bucket-derived inputs (both manifests + both live head counts),
// classifies the state (see classifyItemsState / site_repair.go), and dispatches:
//
//   - NO-OP: both corpora already at newTip with matching head counts.
//   - APPEND: both corpora lockstepped at a common tip; walk only the bounded gap
//     newTip → that tip and extend both heads (sealing shards as they fill).
//   - REPAIR: any observable mismatch WITH an items manifest present, never a
//     from-scratch capped walk. Each corpus is rebuilt from its own reachable
//     sealed shards plus a bounded tail re-walk (see repairItemsState); only a
//     genuinely unreachable manifest tip (history rewrite) resets to bootstrap.
//   - BOOTSTRAP: no items manifest at all (fresh / wiped). Walk up to one push's
//     budget from newTip; if the branch fits, both corpora complete in one push
//     exactly as before. If the budget is hit, seal the newest budget prefix
//     (still a valid servable newest-first index) and leave a cursor so the next
//     push backfills the next older segment. A branch past the budget can no
//     longer error, so it never permanently wedges the index.
//   - BACKFILL: a cursor is pending and the newest end is already at newTip; seal
//     the next older budget segment (from oldestIndexed toward the root) and
//     prepend it to both manifests, advancing (or, on reaching the root, clearing)
//     the cursor. A newTip that advanced mid-bootstrap classifies as APPEND
//     instead, which owns the newest end and bumps cursor.tip.
func updateSiteItemsIndex(client *Client, prefix, ext, newTip string, sp *siteProgress) error {
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
	// A torn bootstrap (incomplete manifest whose cursor PUT was lost) has no
	// cursor on the bucket; reconstruct it from the manifest so the bootstrap
	// resumes instead of freezing as a completed small branch. This is what makes
	// manifest.Complete the authoritative "is a bootstrap pending" signal.
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

// appendItemsGap walks the bounded gap newTip → the corpora's common tip and
// extends both heads (the steady-state APPEND path, and, mid-bootstrap, the
// newest-end owner: it keeps the manifest incomplete and bumps cursor.tip so
// BACKFILL later resumes from the unchanged oldestIndexed). The classifier
// guarantees both manifests are present, lockstepped (items.Tip == bodies.Tip)
// and their head counts match; if the bounded walk cannot reach that tip (an
// unexpected concurrent rewrite between the read and the walk) it falls through
// to REPAIR, which never does a from-scratch capped walk.
func appendItemsGap(client *Client, prefix, ext, newTip string, items, bodies *siteShardManifest, cursor *siteItemsCursor, itemsHead []siteMetaEntry, bodiesHead []siteBodyEntry, sp *siteProgress) error {
	known := map[string]bool{items.Tip: true}
	for _, e := range itemsHead {
		known[e.SHA] = true
	}
	gap, met, _, err := walkBucketItems(client, prefix, newTip, known, siteItemsWalkBudget, sp)
	if err != nil || !met[items.Tip] {
		return repairItemsState(client, prefix, ext, newTip, items, bodies, cursor, sp)
	}
	// APPEND owns only the newest end. With no cursor the index is already
	// complete and the cursor stays absent (no finalize needed). With a bootstrap
	// in flight it stays in flight: the head advances, oldestIndexed is untouched,
	// and cursor.tip bumps to newTip.
	if cursor == nil {
		return putGapArtifacts(client, prefix, ext, newTip, gap, items, bodies, itemsHead, bodiesHead, true, sp)
	}
	pending := &siteItemsCursor{Tip: newTip, OldestIndexed: cursor.OldestIndexed}
	if err := putGapArtifacts(client, prefix, ext, newTip, gap, items, bodies, itemsHead, bodiesHead, pending == nil, sp); err != nil {
		return err
	}
	return finalizeCursor(client, prefix, ext, pending)
}

// bootstrapItems seals the first budget segment of a fresh/wiped index. It walks
// up to one push's budget from newTip; if the whole branch fits (root reached),
// both corpora complete in one push and no cursor appears (small branches keep
// the prior behavior). If the budget is hit, it seals the newest budget prefix
// (a valid servable newest-first index) with complete=false and writes a cursor
// whose oldestIndexed is the oldest sealed sha, so the next push backfills older.
func bootstrapItems(client *Client, prefix, ext, newTip string, sp *siteProgress) error {
	// The walk budget is a per-push CAP, not the branch size: a branch of any
	// size (from a handful of commits to millions) walks against the same 50k
	// ceiling, so reporting done/budget would show a misleading "12%" on a branch
	// that is actually nearly done. The true remaining size is unknown until the
	// frontier empties, so report a plain count (total=0).
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

// backfillItems seals the next older budget segment of an in-progress bootstrap.
// It walks up to budget from the oldest-indexed sha's parents toward the root
// (stopAt = the already-indexed set, so it never re-walks sealed history),
// prepends the sealed segment to both manifests (older than everything there;
// the head is untouched), and advances the cursor — deleting it and marking the
// manifests complete once the root is reached. Write order within the segment:
// seal bodies shard(s), seal items shard(s), bodies manifest, items manifest,
// cursor last; the head is never rewritten (APPEND owns it). Cursor last means an
// interruption after the manifests re-walks the same segment (skip-existing) and
// re-writes the same manifests on the next push, fully idempotent. The classifier
// only routes here when the newest end is already at newTip, so the corpora tips
// (read from the current manifests) are authoritative and no newTip is needed.
//
// The backfill frontier is derived from the MANIFEST (its oldest sealed sha),
// not the cursor's oldestIndexed field: an interruption between the manifest
// writes and the cursor write leaves the cursor's copy stale (lagging the
// manifest), so trusting it would either re-walk covered history or stop short.
// The manifest is authoritative for what is sealed; the cursor only carries the
// bootstrap tip and the complete flag.
//
// stopAt is seeded with every cheaply-available already-indexed boundary — the
// oldest sealed sha, every sealed shard's endTip, the head shas, and the
// manifest tip — not just the frontier. On a strictly-linear chain the frontier
// alone suffices, but a gitmsg data branch can carry a rare multi-machine
// auto-merge; without the extra stop points a merge parent reachable both from
// the frontier and from an already-indexed newer commit could be re-walked into
// a backfill shard, duplicating that membership across two shards. The extra
// boundaries make the walk halt at any indexed frontier.
func backfillItems(client *Client, prefix, ext string, cursor *siteItemsCursor, items *siteShardManifest, itemsHead []siteMetaEntry, sp *siteProgress) error {
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
		// Plain count (total=0): the budget is a per-push cap and the manifest/
		// cursor track only the oldest-indexed frontier, not how many older commits
		// remain, so no honest percentage is knowable here (see bootstrapItems).
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

// completeBackfill marks both manifests complete and clears the cursor when a
// backfill discovers no older history remains (the cursor lagged the root by one
// push). It re-reads and re-writes each manifest with complete=true, shards and
// head untouched.
func completeBackfill(client *Client, prefix, ext string) error {
	if err := markManifestComplete(client, bodiesCorpus, prefix, ext); err != nil {
		return err
	}
	if err := markManifestComplete(client, itemsCorpus, prefix, ext); err != nil {
		return err
	}
	return deleteItemsCursor(client, prefix, ext)
}

// markManifestComplete re-reads one corpus's manifest and re-writes it with
// complete=true (shards/head/tip/bytes unchanged); a no-op if it is already gone.
func markManifestComplete[E shardEntry](client *Client, corpus shardCorpus[E], prefix, ext string) error {
	m, err := readShardManifest(client, corpus, prefix, ext)
	if err != nil || m == nil {
		return err
	}
	m.Complete = true
	return putShardManifest(client, corpus, prefix, ext, m)
}

// prependSegment seals one backfilled older segment for both corpora and prepends
// it to their manifests (re-reading each manifest immediately before its write so
// a concurrent APPEND's newer head/tip survives; a clobber self-heals via REPAIR
// on the next push). Write order: bodies shard(s) + manifest, then items shard(s)
// + manifest; the head is untouched. complete is true only when this segment
// reached the branch root.
func prependSegment(client *Client, prefix, ext string, segment []walkedItem, complete bool, sp *siteProgress) error {
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

// putGapArtifacts appends the freshly-walked gap to both sharded corpora's heads
// (sealing any newly-full shards; prior sealed shards are untouched), in the
// pinned write order (seal bodies shards, seal items shards, bodies head, items
// head, bodies manifest, items manifest). Manifests land last so an interruption
// leaves at worst "bodies ahead of items", which REPAIR handles. complete is
// false while a bootstrap is still backfilling older history.
func putGapArtifacts(client *Client, prefix, ext, newTip string, gap []walkedItem, itemsManifest, bodiesManifest *siteShardManifest, itemsHead []siteMetaEntry, bodiesHead []siteBodyEntry, complete bool, sp *siteProgress) error {
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

// rebuildSiteItems drives every extension data branch present in refs, plus the
// single code items index across every code branch, through the same state
// machine as a helper push (used by `gitsocial site push`). It is idempotent and
// budget-aware: a small branch (re)builds in one call exactly as before; a branch
// past the budget starts (or advances) the resumable bootstrap instead of
// erroring, reusing every already-sealed shard via skip-existing. defaultBranch is
// the repo's default (from the bucket HEAD) so the code index attributes commits
// to it correctly.
func rebuildSiteItems(client *Client, prefix string, refs map[string]string, defaultBranch string, src *localCommitSource, progress Progress) error {
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

// siteItemsBootstrapPending reports whether any extension's items index is still
// an incomplete bootstrap after a pass (its manifest.Complete is false), so the
// caller can decline to stamp the push-state marker: a follow-up push must not be
// skipped while a bootstrap has more older segments to backfill (which no ref
// move signals). Best-effort — a read error is reported as pending, so at worst
// the marker is left unstamped and the next push does a (harmless) full pass.
func siteItemsBootstrapPending(client *Client, prefix string, refs map[string]string) bool {
	for _, ext := range siteItemsExts {
		if _, ok := refs["refs/heads/gitmsg/"+ext]; !ok {
			continue
		}
		items, err := readItemsManifest(client, prefix, ext)
		if err != nil {
			return true
		}
		if items != nil && !items.Complete {
			return true
		}
	}
	return codeIndexBootstrapPending(client, prefix)
}
