// site_tags.go - the tags artifact: each tag's date, author and commits since the previous tag, and a range document for each tag

package site

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

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

var (
	// siteTagHashSuffix matches a commit-hash component at the end of a tag name (v<version>.<hash>); it is not part of the version.
	siteTagHashSuffix = regexp.MustCompile(`[.+-][0-9a-f]{7,40}$`)
	// siteTagVersion matches a tag name's leading dotted version.
	siteTagVersion = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)`)
)

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
	fields := []string{"refname:strip=2", "objectname", "objecttype", "*objectname", "*objecttype",
		"taggername", "taggeremail", "taggerdate:unix", "authorname", "authoremail", "authordate:unix",
		"*authorname", "*authoremail", "*authordate:unix"}
	format := "%(" + strings.Join(fields, ")%00%(") + ")"
	out, err := siteGitCommand(src, "for-each-ref", "--format="+format, "refs/tags").Output()
	if err != nil {
		return nil, fmt.Errorf("list local tags: %w", err)
	}
	entries := []siteTagEntry{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != len(fields) || bucketTags[f[0]] != f[1] {
			continue
		}
		e := siteTagEntry{Name: f[0], SHA: f[1]}
		switch {
		case f[2] == "commit":
			e.Commit, e.Author, e.Email, e.Time = f[1], f[8], trimEmail(f[9]), parseUnix(f[10])
		case f[2] == "tag" && f[4] == "commit" && f[7] != "":
			e.Commit, e.Author, e.Email, e.Time = f[3], f[5], trimEmail(f[6]), parseUnix(f[7])
		case f[2] == "tag" && f[4] == "commit":
			e.Commit, e.Author, e.Email, e.Time = f[3], f[11], trimEmail(f[12]), parseUnix(f[13])
		default:
			continue
		}
		entries = append(entries, e)
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

// trimEmail strips the angle brackets for-each-ref puts around an email.
func trimEmail(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "<"), ">")
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

// fillSiteTagCounts links each entry to the next one and counts the commits between them, copying a count whose two commits did not change.
func fillSiteTagCounts(src *objstore.LocalCommitSource, entries, prior []siteTagEntry, shallow bool) {
	old := map[string]siteTagEntry{}
	for _, e := range prior {
		old[e.Name] = e
	}
	for i := range entries {
		e := &entries[i]
		if i+1 < len(entries) {
			e.Prev, e.PrevCommit = entries[i+1].Name, entries[i+1].Commit
		}
		if shallow {
			continue
		}
		if o, ok := old[e.Name]; ok && o.Count != nil && o.Commit == e.Commit && o.PrevCommit == e.PrevCommit {
			e.Count = o.Count
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

// orderSiteTags sorts entries as the app's compareTagsDesc and orderTagTies do: highest version first, then each group that differs only by a hash suffix by date.
func orderSiteTags(entries []siteTagEntry) []siteTagEntry {
	sorted := append([]siteTagEntry{}, entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return compareSiteTagsDesc(sorted[i].Name, sorted[j].Name) < 0 })
	groups := map[string][]int{}
	keys := []string{}
	for i, e := range sorted {
		key := siteTagHashSuffix.ReplaceAllString(e.Name, "")
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], i)
	}
	out := append([]siteTagEntry{}, sorted...)
	for _, key := range keys {
		slots := groups[key]
		if len(slots) < 2 {
			continue
		}
		group := make([]siteTagEntry, len(slots))
		for j, slot := range slots {
			group[j] = sorted[slot]
		}
		sort.SliceStable(group, func(a, b int) bool { return group[a].Time > group[b].Time })
		for j, slot := range slots {
			out[slot] = group[j]
		}
	}
	return out
}

// compareSiteTagsDesc orders tag names highest version first, non-version names after them by name descending; it mirrors the app's compareTagsDesc.
func compareSiteTagsDesc(a, b string) int {
	va, vb := siteTagVersionKey(a), siteTagVersionKey(b)
	switch {
	case va != nil && vb != nil:
		for i := 0; i < max(len(va), len(vb)); i++ {
			if d := versionPart(vb, i) - versionPart(va, i); d != 0 {
				if d < 0 {
					return -1
				}
				return 1
			}
		}
		sa, sb := siteTagVersionSuffix(a), siteTagVersionSuffix(b)
		if sa == "" && sb != "" {
			return -1
		}
		if sa != "" && sb == "" {
			return 1
		}
		return strings.Compare(a, b)
	case va != nil:
		return -1
	case vb != nil:
		return 1
	}
	return strings.Compare(b, a)
}

// versionPart returns one version component, 0 past the end, as the app's `|| 0`.
func versionPart(v []float64, i int) float64 {
	if i < len(v) {
		return v[i]
	}
	return 0
}

// siteTagVersionKey extracts a tag name's dotted version as numbers, parsed as the app's Number does; nil when the name carries none.
func siteTagVersionKey(name string) []float64 {
	m := siteTagVersion.FindStringSubmatch(siteTagHashSuffix.ReplaceAllString(name, ""))
	if m == nil {
		return nil
	}
	parts := strings.Split(m[1], ".")
	key := make([]float64, len(parts))
	for i, p := range parts {
		key[i], _ = strconv.ParseFloat(p, 64) // the regexp admits only digits
	}
	return key
}

// siteTagVersionSuffix returns the text after a tag name's leading version, with any hash suffix removed.
func siteTagVersionSuffix(name string) string {
	return siteTagVersion.ReplaceAllString(siteTagHashSuffix.ReplaceAllString(name, ""), "")
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
