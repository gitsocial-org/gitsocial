// site_tags.go - the tags artifact: each tag's date, author and commits since the previous tag, and a range document for each tag

package site

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// siteTagsKey is the tags artifact the Tags page reads in one request.
const siteTagsKey = ".gitsocial/site/tags.json"

// siteTagsVersion is the artifact schema version; another version reads as absent.
const siteTagsVersion = 1

// siteRangeVersion is the range document schema version, part of its key and the value of an entry's range flag.
const siteRangeVersion = 1

// siteRangesPrefix holds one immutable range document per pair of adjacent tag commits.
const siteRangesPrefix = ".gitsocial/site/ranges/v1/"

const (
	// siteRangeCommitCap is the app's DETAIL_WALK_CAP; a longer range leaves its commits out.
	siteRangeCommitCap = 2000
	// siteRangeFileCap bounds the files of a range document, which then carries truncated.
	siteRangeFileCap = 1000
)

// siteTags is the artifact: the tags in display order, newest first.
type siteTags struct {
	Version int            `json:"version"`
	Shallow bool           `json:"shallow,omitempty"`
	Tags    []siteTagEntry `json:"tags"`
}

// siteTagEntry is one tag: its ref sha, peeled commit, date, author, and the commit count since the next entry.
type siteTagEntry struct {
	Name       string `json:"name"`
	SHA        string `json:"sha"`
	Commit     string `json:"commit"`
	Time       int64  `json:"time"`
	Author     string `json:"author"`
	Email      string `json:"email"`
	Prev       string `json:"prev,omitempty"`
	PrevCommit string `json:"prevCommit,omitempty"`
	Count      *int   `json:"count,omitempty"`
	Range      int    `json:"range,omitempty"`
}

// siteRange is the range document of one tag: its merge base with the previous tag, the commits since it, and the files changed.
type siteRange struct {
	MergeBase string          `json:"mergeBase"`
	Commits   [][]any         `json:"commits,omitempty"`
	Files     []siteRangeFile `json:"files"`
	Truncated bool            `json:"truncated,omitempty"`
}

// siteRangeFile is one changed file, in the shape of the app's diffTrees records; an absent side has empty fields.
type siteRangeFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	ShaA   string `json:"shaA,omitempty"`
	ShaB   string `json:"shaB,omitempty"`
	ModeA  string `json:"modeA,omitempty"`
	ModeB  string `json:"modeB,omitempty"`
}

// writeSiteTags publishes the tags artifact from the local odb; a bucket with no tags deletes it, and no local source keeps the old one.
func writeSiteTags(client *objstore.Client, prefix string, refs map[string]string, src *objstore.LocalCommitSource) error {
	bucketTags := map[string]string{}
	for ref, sha := range refs {
		if name, ok := strings.CutPrefix(ref, "refs/tags/"); ok && len(sha) == 40 {
			bucketTags[name] = sha
		}
	}
	if len(bucketTags) == 0 {
		return client.Delete(prefix + siteTagsKey)
	}
	if src == nil {
		return nil
	}
	var prior siteTags
	found, err := objstore.ReadCompressedJSON(client, prefix+siteTagsKey, &prior)
	if err != nil {
		return err
	}
	if !found || prior.Version != siteTagsVersion {
		prior = siteTags{}
	}
	local, err := readLocalTags(src, bucketTags)
	if err != nil {
		return nil // a failed read keeps the old artifact, whose stale entries the app ignores
	}
	shallow := siteRepoShallow(src)
	entries := orderSiteTags(carrySiteTags(local, prior.Tags, bucketTags))
	fillSiteTagCounts(src, entries, prior.Tags, shallow)
	// Range documents go up before tags.json, so no reader sees a flag without its document.
	writeSiteTagRanges(client, prefix, src, entries, prior.Tags, shallow)
	data, err := objstore.CompressJSON(siteTags{Version: siteTagsVersion, Shallow: shallow, Tags: entries}, objstore.BrotliQualityFull)
	if err != nil {
		return err
	}
	return objstore.PutCompressed(client, prefix+siteTagsKey, data, "")
}

// readLocalTags reads each bucket tag the local odb holds at the same sha; a tag it lacks or holds at another sha is left out.
func readLocalTags(src *objstore.LocalCommitSource, bucketTags map[string]string) ([]siteTagEntry, error) {
	out, err := siteGitCommand(src, "for-each-ref", "--format="+git.TagRefFormat, "refs/tags").Output()
	if err != nil {
		return nil, fmt.Errorf("list local tags: %w", err)
	}
	entries := []siteTagEntry{}
	for _, line := range strings.Split(string(out), "\n") {
		t, ok := git.ParseTagRef(line)
		if !ok || bucketTags[t.Name] != t.SHA {
			continue
		}
		entries = append(entries, siteTagEntry{Name: t.Name, SHA: t.SHA, Commit: t.Commit, Time: t.Time, Author: t.Author, Email: t.Email})
	}
	return entries, nil
}

// carrySiteTags adds the prior entry of each bucket tag the local odb lacks while its sha still matches, so a clone without that tag does not drop it.
func carrySiteTags(local, prior []siteTagEntry, bucketTags map[string]string) []siteTagEntry {
	have := map[string]bool{}
	for _, e := range local {
		have[e.Name] = true
	}
	out := append([]siteTagEntry{}, local...)
	for _, e := range prior {
		if !have[e.Name] && bucketTags[e.Name] == e.SHA {
			out = append(out, e)
		}
	}
	return out
}

// parseUnix parses a unix time field, 0 when empty or malformed.
func parseUnix(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64) // a malformed date orders as the oldest
	return n
}

// siteRepoShallow reports whether the local repository is shallow, where a commit count would be short.
func siteRepoShallow(src *objstore.LocalCommitSource) bool {
	out, err := siteGitCommand(src, "rev-parse", "--is-shallow-repository").Output()
	return err != nil || strings.TrimSpace(string(out)) != "false"
}

// fillSiteTagCounts links each entry to the next one and counts the commits between them, copying a count whose two commits did not change; a shallow clone only copies.
func fillSiteTagCounts(src *objstore.LocalCommitSource, entries, prior []siteTagEntry, shallow bool) {
	old := map[string]siteTagEntry{}
	for _, e := range prior {
		old[e.Name] = e
	}
	for i := range entries {
		e := &entries[i]
		// A carried entry arrives with its prior link and count, which hold only for its prior pair.
		e.Prev, e.PrevCommit, e.Count = "", "", nil
		if i+1 < len(entries) {
			e.Prev, e.PrevCommit = entries[i+1].Name, entries[i+1].Commit
		}
		if o, ok := old[e.Name]; ok && o.Count != nil && o.Commit == e.Commit && o.PrevCommit == e.PrevCommit {
			e.Count = o.Count
			continue
		}
		if shallow {
			continue
		}
		args := []string{"rev-list", "--count", e.Commit}
		if e.PrevCommit != "" {
			args = append(args, "^"+e.PrevCommit)
		}
		out, err := siteGitCommand(src, args...).Output()
		if err != nil {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
			e.Count = &n
		}
	}
}

// orderSiteTags sorts entries in the display order git.TagOrder defines, the one the app's compareTagsDesc and orderTagTies follow.
func orderSiteTags(entries []siteTagEntry) []siteTagEntry {
	names, times := make([]string, len(entries)), make([]int64, len(entries))
	for i, e := range entries {
		names[i], times[i] = e.Name, e.Time
	}
	out := make([]siteTagEntry, len(entries))
	for i, j := range git.TagOrder(names, times) {
		out[i] = entries[j]
	}
	return out
}

// siteRangeKey is the key of the range document for one pair of tag commits.
func siteRangeKey(prefix, prevCommit, commit string) string {
	return prefix + siteRangesPrefix + prevCommit + ".." + commit + ".json"
}

// writeSiteTagRanges writes the range document of each entry with a previous tag and flags the entry; an unchanged flagged pair keeps its flag, and any other entry is flagged only after its write.
func writeSiteTagRanges(client *objstore.Client, prefix string, src *objstore.LocalCommitSource, entries, prior []siteTagEntry, shallow bool) {
	old := map[string]siteTagEntry{}
	for _, e := range prior {
		old[e.Name] = e
	}
	todo := []int{}
	for i, e := range entries {
		// A carried entry arrives with its prior flag, which holds only for its prior pair.
		entries[i].Range = 0
		if e.PrevCommit == "" {
			continue
		}
		if o, ok := old[e.Name]; ok && o.Range == siteRangeVersion && o.Commit == e.Commit && o.PrevCommit == e.PrevCommit {
			entries[i].Range = siteRangeVersion
		} else if !shallow {
			todo = append(todo, i)
		}
	}
	// Each worker writes only its own entry, so the flags need no lock.
	objstore.RunParallel(len(todo), func(k int) error {
		e := &entries[todo[k]]
		doc, err := readSiteRange(src, e.PrevCommit, e.Commit)
		if err != nil {
			return nil // an unreadable range leaves the entry to the app's walk
		}
		data, err := objstore.CompressJSON(doc, objstore.BrotliQualityFull)
		if err != nil {
			return nil // as above
		}
		if err := objstore.PutCompressed(client, siteRangeKey(prefix, e.PrevCommit, e.Commit), data, objstore.CacheControlImmutable); err != nil {
			fmt.Fprintf(os.Stderr, "gitsocial s3: site tag range %s: %v\n", e.Name, err)
			return nil
		}
		e.Range = siteRangeVersion
		return nil
	})
}

// readSiteRange reads one range from the local odb: the merge base, the commits of commit ^prevCommit, and the files changed since the merge base.
func readSiteRange(src *objstore.LocalCommitSource, prevCommit, commit string) (siteRange, error) {
	doc := siteRange{Files: []siteRangeFile{}}
	out, err := siteGitCommand(src, "merge-base", commit, prevCommit).Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		doc.MergeBase = strings.TrimSpace(string(out))
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		// Exit 1 with no output is git's "no common ancestor".
	default:
		return doc, fmt.Errorf("merge-base: %w", err)
	}
	out, err = siteGitCommand(src, "log", "--no-show-signature", "-z", "--format=%H %at%n%B", "-n", strconv.Itoa(siteRangeCommitCap+1), commit, "^"+prevCommit).Output()
	if err != nil {
		return doc, fmt.Errorf("log: %w", err)
	}
	commits := [][]any{}
	for _, rec := range strings.Split(string(out), "\x00") {
		head, message, _ := strings.Cut(rec, "\n")
		sha, at, ok := strings.Cut(head, " ")
		if !ok || len(sha) != 40 {
			continue
		}
		commits = append(commits, []any{sha, parseUnix(at), contentFirstLine(message)})
	}
	if len(commits) <= siteRangeCommitCap {
		sort.SliceStable(commits, func(i, j int) bool { return commits[i][1].(int64) > commits[j][1].(int64) })
		doc.Commits = commits
	}
	base := doc.MergeBase
	if base == "" {
		base = prevCommit
	}
	out, err = siteGitCommand(src, "diff-tree", "-r", "--no-renames", "-z", base, commit).Output()
	if err != nil {
		return doc, fmt.Errorf("diff-tree: %w", err)
	}
	doc.Files, doc.Truncated = parseSiteRangeFiles(string(out))
	return doc, nil
}

// parseSiteRangeFiles parses diff-tree -z raw output into file records, at most siteRangeFileCap, and reports whether it cut any.
func parseSiteRangeFiles(out string) ([]siteRangeFile, bool) {
	files := []siteRangeFile{}
	fields := strings.Split(out, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(meta) != 5 {
			continue
		}
		if len(files) == siteRangeFileCap {
			return files, true
		}
		f := siteRangeFile{Path: fields[i+1], ModeA: meta[0], ModeB: meta[1], ShaA: meta[2], ShaB: meta[3], Status: "modified"}
		switch meta[4] {
		case "A":
			f.Status, f.ModeA, f.ShaA = "added", "", ""
		case "D":
			f.Status, f.ModeB, f.ShaB = "deleted", "", ""
		}
		files = append(files, f)
	}
	return files, false
}
