// site_pages_thread.go - thread assembly for the static pages: root and reply classification, version resolution, and reply ordering
//
// Which types are roots follows the specs: specs/GITSOCIAL.md §1.1 and §1.3,
// specs/GITPM.md, specs/GITREVIEW.md and specs/GITRELEASE.md. Anything carrying
// `edits` is a version of its canonical, and a cross-repo edit is a proposal
// (specs/GITMSG.md §1.5), so it is dropped here rather than resolved.

package objstore

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// sitePageMsg is one corpus entry with its GitMsg header parsed; Message is empty when the bodies corpus lags the index.
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

// sitePageItem is one resolved item: the canonical message plus the latest same-repo version's content and state.
type sitePageItem struct {
	Msg       *sitePageMsg // canonical (identity, author, creation time)
	Resolved  *sitePageMsg // latest version (== Msg when never edited)
	Edited    bool
	Retracted bool
	InReplyTo string          // author of the parent comment a nested reply answers ("" = replies to the root)
	Depth     int             // visual indent of a nested reply, capped at sitePageThreadMaxDepth
	parent    *sitePageItem   // the reply this one answers, nil when it answers the root
	Replies   []*sitePageItem // for roots: the thread, parent before children, siblings by time
}

// sitePageDefaultTypes mirrors the shell's EXT_DEFAULT_TYPE: the type assumed when a header names none.
var sitePageDefaultTypes = map[string]string{"social": "post", "pm": "issue", "review": "pull-request", "release": "release", "memo": "memo"}

// pageHeaderField returns a header field, "" when the header is absent.
func pageHeaderField(m *sitePageMsg, key string) string {
	if m == nil || m.Header == nil {
		return ""
	}
	return m.Header.Fields[key]
}

// pageMsgType returns a message's item type: the header type, else the extension's default.
func pageMsgType(m *sitePageMsg) string {
	if t := pageHeaderField(m, "type"); t != "" {
		return t
	}
	return sitePageDefaultTypes[m.Ext]
}

// pageReplyRootRef returns the reference naming the thread root a message replies to, or "" for a top-level item.
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

// pageRefHashRe extracts the hex hash from a relation ref of any type, since a relation field can carry a non-commit ref type.
var pageRefHashRe = regexp.MustCompile(`[#:]([0-9a-f]{7,40})(?:@|$)`)

// pageLocalCommitHash resolves a reference to a same-repo 12-hex commit hash, or "" when it names another repository.
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

// pageEffectiveTime returns a message's display time: origin-time over the git author time, mirroring the shell.
func pageEffectiveTime(m *sitePageMsg) int64 {
	if t := pageHeaderField(m, "origin-time"); t != "" {
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts.Unix()
		}
	}
	return m.TS
}

// pageDisplayAuthor returns a message's display author, preferring the origin provenance of imported content.
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

// pageItemType returns an item's type from the canonical header, which an edit may omit, then the latest version, then the extension default.
func pageItemType(it *sitePageItem) string {
	if t := pageHeaderField(it.Msg, "type"); t != "" {
		return t
	}
	return pageMsgType(it.Resolved)
}

// pageItemField returns an item's resolved header field: the latest version wins, since a state transition is an edit.
func pageItemField(it *sitePageItem, key string) string {
	if v := pageHeaderField(it.Resolved, key); v != "" {
		return v
	}
	return pageHeaderField(it.Msg, key)
}

// pageItemBody returns an item's resolved clean content, falling back to the indexed subject when no body was fetched.
func pageItemBody(it *sitePageItem) string {
	if it.Resolved.Message != "" {
		// Drop link reference definitions here, the one accessor every page builder reads an item through.
		return siteStripLinkRefDefs(protocol.ExtractCleanContent(it.Resolved.Message))
	}
	return it.Resolved.Subject
}

// buildSitePageThreads resolves versions and assembles reply threads, returning each extension's top-level items newest-first; a reply with no local root is dropped.
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
					it.parent = parent
				}
			}
			root.Replies = append(root.Replies, it)
		}
	}
	for ext := range roots {
		for _, r := range roots[ext] {
			r.Replies = pageOrderThread(r.Replies)
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

// sitePageThreadMaxDepth caps a nested reply's visual indent, mirroring gs-core.js THREAD_MAX_DEPTH.
const sitePageThreadMaxDepth = 4

// pageOrderThread flattens a root's replies as gs-core.js does: a reply follows the one it answers, siblings run oldest first, depth is capped.
func pageOrderThread(replies []*sitePageItem) []*sitePageItem {
	children := map[*sitePageItem][]*sitePageItem{}
	var roots []*sitePageItem
	present := make(map[*sitePageItem]bool, len(replies))
	for _, r := range replies {
		present[r] = true
	}
	for _, r := range replies {
		if r.parent != nil && present[r.parent] {
			children[r.parent] = append(children[r.parent], r)
			continue
		}
		roots = append(roots, r)
	}
	byTime := func(list []*sitePageItem) {
		sort.Slice(list, func(i, j int) bool {
			ti, tj := pageEffectiveTime(list[i].Msg), pageEffectiveTime(list[j].Msg)
			if ti != tj {
				return ti < tj
			}
			return list[i].Msg.SHA < list[j].Msg.SHA
		})
	}
	out := make([]*sitePageItem, 0, len(replies))
	seen := map[*sitePageItem]bool{}
	var walk func(list []*sitePageItem, depth int)
	walk = func(list []*sitePageItem, depth int) {
		byTime(list)
		for _, r := range list {
			if seen[r] {
				continue
			}
			seen[r] = true
			r.Depth = depth
			if r.Depth > sitePageThreadMaxDepth {
				r.Depth = sitePageThreadMaxDepth
			}
			out = append(out, r)
			walk(children[r], depth+1)
		}
	}
	walk(roots, 0)
	return out
}

// pageResolveRoot follows a reply reference to its top-level item, walking a bounded chain when only `reply-to` is set.
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

// applyPageVersions applies GITMSG §1.5 resolution: the latest same-repo edit supplies the content and state. Git timestamps have one-second resolution, so a same-second tie breaks on branch order, then hash.
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

// readSitePagesMeta reads back one extension's metadata index into parsed page messages, oldest-first, with no bodies attached.
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

// readSitePagesCorpus reads back one extension's metadata index and bodies corpus together, the full-regen path.
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

// attachThreadBodies reads back the bodies the affected threads render.
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

// attachRootBodies reads back the given roots' own bodies alone, which is all a feed entry renders.
func attachRootBodies(client *Client, prefix string, items []*sitePageItem) error {
	msgs := make([]*sitePageMsg, 0, len(items))
	for _, it := range items {
		msgs = append(msgs, it.Resolved)
	}
	return attachMsgBodies(client, prefix, msgs)
}

// attachMsgBodies fetches the still-missing bodies, grouped per extension so each corpus is scanned once.
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

// readBodiesBySHAs fetches the given shas' bodies from one corpus, head first then sealed shards newest to oldest, stopping once all are found. need is consumed.
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

// readAllShardEntries reads one corpus's full entry list: every sealed shard in manifest order, then the head.
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
