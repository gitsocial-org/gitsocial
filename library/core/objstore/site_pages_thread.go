// site_pages_thread.go - thread assembly for the static HTML pages: corpus
// read-back, header parsing via core/protocol, root/reply classification,
// same-repo edit and retract resolution (GITMSG.md §1.5), and reply
// attachment in timestamp order.
//
// Top-level (root) detection per extension, derived from the specs:
//
//   social  (GITSOCIAL §1.1/§1.3): `post` (including implicit posts carrying
//           no GitMsg header), `repost`, and `quote` are roots. `comment` is
//           always a reply; its `original` field names the thread root
//           directly (never an intermediate comment), `reply-to` only nests
//           it. A repost/quote's `original` is a share relation, not a reply
//           relation, so both stay top-level.
//   pm      (GITPM): `issue`, `milestone`, and `sprint` are all roots. The
//           milestone/sprint/parent/root/blocks fields are hierarchy links
//           BETWEEN roots, not thread membership; sub-issues get their own
//           pages. Discussion on PM items arrives as social comments
//           (ext="social") on the social data branch, resolved by hash.
//   review  (GITREVIEW): `pull-request` is a root; `feedback` is always a
//           reply to the PR named by its `pull-request` field (original as a
//           fallback).
//   release (GITRELEASE): `release` is a root; discussion is social comments.
//   memo    (no spec; extension code): `memo` is a root; memos have no reply
//           type.
//
// Anything carrying `edits` is a version of its canonical, never a root or a
// reply itself. A cross-repo edit is a proposal (GITMSG §1.5) and MUST NOT
// change resolved state, so it is dropped here; the owner's accepting mirror
// edit is a same-repo edit and resolves normally.

package objstore

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// sitePageMsg is one corpus entry with its GitMsg header parsed: the metadata
// index supplies identity/author/time/subject, the bodies corpus the full raw
// message (may be empty when the bodies corpus lags the index).
type sitePageMsg struct {
	Ext     string // items index (data branch) the entry came from
	SHA     string // full 40-hex sha
	Short   string // 12-hex identity refs and page keys use
	Idx     int    // position in the extension's ingestion-ordered corpus (branch order)
	Author  string
	Email   string
	TS      int64
	Subject string
	Message string           // full raw commit message from the bodies corpus
	Header  *protocol.Header // nil for plain commits (implicit posts)
}

// sitePageItem is one resolved item (a root or a reply): the canonical message
// plus the latest same-repo version's content and state.
type sitePageItem struct {
	Msg       *sitePageMsg // canonical (identity, author, creation time)
	Resolved  *sitePageMsg // latest version (== Msg when never edited)
	Edited    bool
	Retracted bool
	InReplyTo string          // author of the parent comment a nested reply answers ("" = replies to the root)
	Replies   []*sitePageItem // for roots: the thread, ascending by effective time
}

// sitePageDefaultTypes mirrors the shell's EXT_DEFAULT_TYPE: the item type
// assumed when a header carries none (or the commit has no header at all).
var sitePageDefaultTypes = map[string]string{"social": "post", "pm": "issue", "review": "pull-request", "release": "release", "memo": "memo"}

// pageHeaderField returns a header field, "" when the header is absent.
func pageHeaderField(m *sitePageMsg, key string) string {
	if m == nil || m.Header == nil {
		return ""
	}
	return m.Header.Fields[key]
}

// pageMsgType returns a message's item type: the header type, else the source
// extension's default.
func pageMsgType(m *sitePageMsg) string {
	if t := pageHeaderField(m, "type"); t != "" {
		return t
	}
	return sitePageDefaultTypes[m.Ext]
}

// pageReplyRootRef returns the reference naming the thread root a message
// replies to, or "" for a top-level item (see the file-top mapping).
func pageReplyRootRef(m *sitePageMsg) string {
	if m.Header == nil {
		return ""
	}
	switch {
	case m.Header.Ext == "social" && m.Header.Fields["type"] == "comment":
		if ref := m.Header.Fields["original"]; ref != "" {
			return ref
		}
		return m.Header.Fields["reply-to"]
	case m.Header.Ext == "review" && m.Header.Fields["type"] == "feedback":
		if ref := m.Header.Fields["pull-request"]; ref != "" {
			return ref
		}
		return m.Header.Fields["original"]
	}
	return ""
}

// pageRefHashRe extracts the hex hash from a relation ref of ANY type
// ("#<type>:<hash>[@branch]"), mirroring the shell's anyRefHash: relation
// fields can carry a non-commit ref type (e.g. `pull-request="#unknown:<hash>"`),
// so a commit-only parse would miss real thread members.
var pageRefHashRe = regexp.MustCompile(`[#:]([0-9a-f]{7,40})(?:@|$)`)

// pageLocalCommitHash resolves a reference to a local (same-repo) 12-hex commit
// hash, or "" when the ref is absent, malformed, or names another repository
// (local refs start with "#"; remote refs carry a URL before it).
func pageLocalCommitHash(ref string) string {
	if !strings.HasPrefix(ref, "#") {
		return ""
	}
	m := pageRefHashRe.FindStringSubmatch(ref)
	if m == nil {
		return ""
	}
	hash := m[1]
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return hash
}

// pageEffectiveTime returns a message's display/sort time: origin-time (set on
// imported content) over the git author time, mirroring the shell.
func pageEffectiveTime(m *sitePageMsg) int64 {
	if t := pageHeaderField(m, "origin-time"); t != "" {
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts.Unix()
		}
	}
	return m.TS
}

// pageDisplayAuthor returns a message's display author name and email,
// preferring the origin-* provenance of imported content.
func pageDisplayAuthor(m *sitePageMsg) (name, email string) {
	name, email = m.Author, m.Email
	if o := protocol.ExtractOrigin(m.Header); o != nil {
		if n := protocol.OriginDisplayAuthor(o); n != "" {
			name = n
		}
		if o.AuthorEmail != "" {
			email = o.AuthorEmail
		}
	}
	return name, email
}

// pageItemType returns an item's type from the canonical header (edits — and
// especially retract edits — may omit it), falling back to the latest version,
// then the extension default.
func pageItemType(it *sitePageItem) string {
	if t := pageHeaderField(it.Msg, "type"); t != "" {
		return t
	}
	return pageMsgType(it.Resolved)
}

// pageItemField returns an item's resolved header field: the latest version's
// value wins (state transitions are edits), falling back to the canonical.
func pageItemField(it *sitePageItem, key string) string {
	if v := pageHeaderField(it.Resolved, key); v != "" {
		return v
	}
	return pageHeaderField(it.Msg, key)
}

// pageItemBody returns an item's resolved clean content (protocol metadata
// stripped), falling back to the indexed subject when the bodies corpus has no
// entry for the resolved version.
func pageItemBody(it *sitePageItem) string {
	if it.Resolved.Message != "" {
		return protocol.ExtractCleanContent(it.Resolved.Message)
	}
	return it.Resolved.Subject
}

// buildSitePageThreads resolves versions and assembles reply threads across
// every extension's messages, returning each extension's top-level items
// newest-first (effective time, sha-descending tiebreak). Replies whose root
// lives in another repository (or was never fetched) have no local page and
// are dropped.
func buildSitePageThreads(msgs map[string][]sitePageMsg) map[string][]*sitePageItem {
	versions := map[string][]*sitePageMsg{}
	var items []*sitePageItem
	for ext := range msgs {
		for i := range msgs[ext] {
			m := &msgs[ext][i]
			if pageHeaderField(m, "edits") != "" {
				if canonical := pageLocalCommitHash(pageHeaderField(m, "edits")); canonical != "" {
					versions[canonical] = append(versions[canonical], m)
				}
				continue
			}
			items = append(items, &sitePageItem{Msg: m, Resolved: m})
		}
	}
	byShort := make(map[string]*sitePageItem, len(items))
	for _, it := range items {
		byShort[it.Msg.Short] = it
		applyPageVersions(it, versions[it.Msg.Short])
	}
	roots := map[string][]*sitePageItem{}
	for _, it := range items {
		ref := pageReplyRootRef(it.Msg)
		if ref == "" {
			roots[it.Msg.Ext] = append(roots[it.Msg.Ext], it)
			continue
		}
		if root := pageResolveRoot(byShort, ref); root != nil && root != it {
			if parentHash := pageLocalCommitHash(pageHeaderField(it.Msg, "reply-to")); parentHash != "" {
				if parent := byShort[parentHash]; parent != nil && parent != root {
					it.InReplyTo, _ = pageDisplayAuthor(parent.Msg)
				}
			}
			root.Replies = append(root.Replies, it)
		}
	}
	for ext := range roots {
		for _, r := range roots[ext] {
			sort.Slice(r.Replies, func(i, j int) bool {
				ti, tj := pageEffectiveTime(r.Replies[i].Msg), pageEffectiveTime(r.Replies[j].Msg)
				if ti != tj {
					return ti < tj
				}
				return r.Replies[i].Msg.SHA < r.Replies[j].Msg.SHA
			})
		}
		rs := roots[ext]
		sort.Slice(rs, func(i, j int) bool {
			ti, tj := pageEffectiveTime(rs[i].Msg), pageEffectiveTime(rs[j].Msg)
			if ti != tj {
				return ti > tj
			}
			return rs[i].Msg.SHA > rs[j].Msg.SHA
		})
	}
	return roots
}

// pageResolveRoot follows a reply reference to its top-level item: `original`
// names the root directly per GITSOCIAL §1.3, but a comment carrying only
// `reply-to` lands on its parent, so the chain is walked (bounded) to the root.
func pageResolveRoot(byShort map[string]*sitePageItem, ref string) *sitePageItem {
	target := byShort[pageLocalCommitHash(ref)]
	for depth := 0; target != nil && depth < 16; depth++ {
		up := pageReplyRootRef(target.Msg)
		if up == "" {
			return target
		}
		next := byShort[pageLocalCommitHash(up)]
		if next == nil || next == target {
			return target
		}
		target = next
	}
	return target
}

// applyPageVersions applies GITMSG §1.5 resolution: the latest same-repo edit
// supplies the resolved content and the retracted state; the canonical keeps
// the identity, author, and creation time. "Latest" is by timestamp, with
// same-second edits broken by branch (corpus ingestion) order — git timestamps
// have one-second resolution, so a retitle and a close landing in the same
// second MUST resolve to the later commit on the data branch, matching the
// shell's chain-order resolution (gs-core.js resolveItems). Hash descending is
// the final (cross-corpus) fallback.
func applyPageVersions(it *sitePageItem, vs []*sitePageMsg) {
	if len(vs) == 0 {
		return
	}
	later := func(v, cur *sitePageMsg) bool {
		if v.TS != cur.TS {
			return v.TS > cur.TS
		}
		if v.Ext == cur.Ext {
			return v.Idx > cur.Idx
		}
		return v.SHA > cur.SHA
	}
	latest := vs[0]
	for _, v := range vs[1:] {
		if later(v, latest) {
			latest = v
		}
	}
	it.Resolved = latest
	it.Edited = true
	it.Retracted = pageHeaderField(latest, "retracted") == "true"
}

// readSitePagesMeta reads back one extension's complete metadata index (the
// artifact the push just maintained) into parsed page messages, oldest-first as
// stored, with no bodies attached (Message stays "").
func readSitePagesMeta(client *Client, prefix, ext string, items *siteShardManifest) ([]sitePageMsg, error) {
	meta, err := readAllShardEntries(client, prefix, ext, itemsCorpus, items)
	if err != nil {
		return nil, err
	}
	msgs := make([]sitePageMsg, 0, len(meta))
	for _, e := range meta {
		if len(e.SHA) != 40 {
			continue
		}
		msgs = append(msgs, sitePageMsg{
			Ext: ext, SHA: e.SHA, Short: e.SHA[:12], Idx: len(msgs), Author: e.Author, Email: e.Email,
			TS: e.TS, Subject: e.Subject, Header: protocol.ParseHeader(e.Header),
		})
	}
	return msgs, nil
}

// readSitePagesCorpus reads back one extension's complete metadata index and
// bodies corpus and projects them into parsed page messages, oldest-first as
// stored (the full-regen path; incremental passes read bodies selectively via
// attachThreadBodies).
func readSitePagesCorpus(client *Client, prefix, ext string, items *siteShardManifest) ([]sitePageMsg, error) {
	msgs, err := readSitePagesMeta(client, prefix, ext, items)
	if err != nil {
		return nil, err
	}
	bodiesManifest, err := readBodiesManifest(client, prefix, ext)
	if err != nil {
		return nil, err
	}
	if bodiesManifest == nil {
		return msgs, nil
	}
	entries, err := readAllShardEntries(client, prefix, ext, bodiesCorpus, bodiesManifest)
	if err != nil {
		return nil, err
	}
	bodies := make(map[string]string, len(entries))
	for _, e := range entries {
		bodies[e.SHA] = e.Message
	}
	for i := range msgs {
		msgs[i].Message = bodies[msgs[i].SHA]
	}
	return msgs, nil
}

// attachThreadBodies reads back the message bodies the affected threads render
// (the resolved latest version of each root and reply).
func attachThreadBodies(client *Client, prefix string, affected []*sitePageItem) error {
	var msgs []*sitePageMsg
	for _, r := range affected {
		msgs = append(msgs, r.Resolved)
		for _, rep := range r.Replies {
			msgs = append(msgs, rep.Resolved)
		}
	}
	return attachMsgBodies(client, prefix, msgs)
}

// attachRootBodies reads back only the given roots' own resolved bodies
// (replies skipped) — the feed's incremental path renders just the entry
// items' content, never their threads.
func attachRootBodies(client *Client, prefix string, items []*sitePageItem) error {
	msgs := make([]*sitePageMsg, 0, len(items))
	for _, it := range items {
		msgs = append(msgs, it.Resolved)
	}
	return attachMsgBodies(client, prefix, msgs)
}

// attachMsgBodies fetches the still-missing bodies of the given messages,
// grouped per source extension so each bodies corpus is scanned once, newest
// shards first, only until every needed sha is found.
func attachMsgBodies(client *Client, prefix string, msgs []*sitePageMsg) error {
	need := map[string][]*sitePageMsg{}
	for _, m := range msgs {
		if m.Message == "" {
			need[m.Ext] = append(need[m.Ext], m)
		}
	}
	for ext, group := range need {
		shas := make(map[string]bool, len(group))
		for _, m := range group {
			shas[m.SHA] = true
		}
		bodies, err := readBodiesBySHAs(client, prefix, ext, shas)
		if err != nil {
			return err
		}
		for _, m := range group {
			m.Message = bodies[m.SHA]
		}
	}
	return nil
}

// readBodiesBySHAs fetches the bodies of the given shas from one extension's
// corpus: the head first (new content lives there), then sealed shards newest
// to oldest, stopping as soon as every sha is found. need is consumed. Missing
// shas simply stay absent from the result (the renderer falls back to the
// indexed subject).
func readBodiesBySHAs(client *Client, prefix, ext string, need map[string]bool) (map[string]string, error) {
	out := make(map[string]string, len(need))
	manifest, err := readBodiesManifest(client, prefix, ext)
	if err != nil || manifest == nil {
		return out, err
	}
	take := func(entries []siteBodyEntry) {
		for _, e := range entries {
			if need[e.SHA] {
				out[e.SHA] = e.Message
				delete(need, e.SHA)
			}
		}
	}
	head, err := readBodyDocItems(client, prefix+bodiesCorpus.headKey(ext))
	if err != nil {
		return nil, err
	}
	take(head)
	for i := len(manifest.Shards) - 1; i >= 0 && len(need) > 0; i-- {
		entries, err := readBodyDocItems(client, prefix+bodiesCorpus.dir(ext)+manifest.Shards[i].Key)
		if err != nil {
			return nil, err
		}
		take(entries)
	}
	return out, nil
}

// readAllShardEntries reads one corpus's full entry list: every sealed shard in
// manifest order (oldest first), then the head.
func readAllShardEntries[E shardEntry](client *Client, prefix, ext string, corpus shardCorpus[E], manifest *siteShardManifest) ([]E, error) {
	var out []E
	for _, s := range manifest.Shards {
		entries, err := readDocItems[E](client, prefix+corpus.dir(ext)+s.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	head, err := readDocItems[E](client, prefix+corpus.headKey(ext))
	if err != nil {
		return nil, err
	}
	return append(out, head...), nil
}
