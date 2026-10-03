// tags.go - Tag listing, the display order of tags and the commit count between two commits
package git

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Tag is one tag that points at a commit: its ref sha, the commit, and the date and author of the tag, else of the commit.
type Tag struct {
	Name   string
	SHA    string
	Commit string
	Author string
	Email  string
	Time   int64
}

// tagRefFields are the for-each-ref fields of a tag and of the commit under an annotated tag.
var tagRefFields = []string{"refname:strip=2", "objectname", "objecttype", "*objectname", "*objecttype",
	"taggername", "taggeremail", "taggerdate:unix", "authorname", "authoremail", "authordate:unix",
	"*authorname", "*authoremail", "*authordate:unix"}

// TagRefFormat is the for-each-ref format whose lines ParseTagRef reads.
var TagRefFormat = "%(" + strings.Join(tagRefFields, ")%00%(") + ")"

var (
	// tagHashSuffix matches a commit-hash component at the end of a tag name (v<version>.<hash>); it is not part of the version.
	tagHashSuffix = regexp.MustCompile(`[.+-][0-9a-f]{7,40}$`)
	// tagVersion matches a tag name's leading dotted version.
	tagVersion = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)`)
)

// ParseTagRef reads one line of TagRefFormat; false for a line that is not a tag on a commit.
func ParseTagRef(line string) (Tag, bool) {
	f := strings.Split(line, "\x00")
	if len(f) != len(tagRefFields) {
		return Tag{}, false
	}
	t := Tag{Name: f[0], SHA: f[1]}
	switch {
	case f[2] == "commit":
		t.Commit, t.Author, t.Email, t.Time = f[1], f[8], trimAngleEmail(f[9]), parseUnixTime(f[10])
	case f[2] == "tag" && f[4] == "commit" && f[7] != "":
		t.Commit, t.Author, t.Email, t.Time = f[3], f[5], trimAngleEmail(f[6]), parseUnixTime(f[7])
	case f[2] == "tag" && f[4] == "commit":
		t.Commit, t.Author, t.Email, t.Time = f[3], f[11], trimAngleEmail(f[12]), parseUnixTime(f[13])
	default:
		return Tag{}, false
	}
	return t, true
}

// ListTags returns the tags of a repository that point at commits, in display order.
func ListTags(workdir string) ([]Tag, error) {
	out, err := execGitSimple(workdir, []string{"for-each-ref", "--format=" + TagRefFormat, "refs/tags"})
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	var tags []Tag
	for _, line := range strings.Split(out, "\n") {
		if t, ok := ParseTagRef(line); ok {
			tags = append(tags, t)
		}
	}
	names, times := make([]string, len(tags)), make([]int64, len(tags))
	for i, t := range tags {
		names[i], times[i] = t.Name, t.Time
	}
	ordered := make([]Tag, len(tags))
	for i, j := range TagOrder(names, times) {
		ordered[i] = tags[j]
	}
	return ordered, nil
}

// TagOrder returns the indexes of the tags in display order: highest version first by CompareTagsDesc, and the tags that differ only by a hash suffix by time, newest first.
func TagOrder(names []string, times []int64) []int {
	order := make([]int, len(names))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return CompareTagsDesc(names[order[a]], names[order[b]]) < 0 })
	groups := map[string][]int{}
	keys := []string{}
	for slot, i := range order {
		key := tagHashSuffix.ReplaceAllString(names[i], "")
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], slot)
	}
	out := append([]int{}, order...)
	for _, key := range keys {
		slots := groups[key]
		if len(slots) < 2 {
			continue
		}
		members := make([]int, len(slots))
		for j, slot := range slots {
			members[j] = order[slot]
		}
		sort.SliceStable(members, func(a, b int) bool { return times[members[a]] > times[members[b]] })
		for j, slot := range slots {
			out[slot] = members[j]
		}
	}
	return out
}

// CompareTagsDesc orders tag names highest version first, a plain version before its pre-release, and non-version names after them by name descending.
func CompareTagsDesc(a, b string) int {
	va, vb := tagVersionKey(a), tagVersionKey(b)
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
		sa, sb := tagVersionSuffix(a), tagVersionSuffix(b)
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

// versionPart returns one version component, 0 past the end.
func versionPart(v []float64, i int) float64 {
	if i < len(v) {
		return v[i]
	}
	return 0
}

// tagVersionKey extracts a tag name's dotted version as numbers; nil when the name carries none.
func tagVersionKey(name string) []float64 {
	m := tagVersion.FindStringSubmatch(tagHashSuffix.ReplaceAllString(name, ""))
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

// tagVersionSuffix is what follows the version of a tag name, the hash suffix removed.
func tagVersionSuffix(name string) string {
	return tagVersion.ReplaceAllString(tagHashSuffix.ReplaceAllString(name, ""), "")
}

// CountCommits counts the commits reachable from commit and not from base; an empty base counts the history of commit.
func CountCommits(workdir, commit, base string) (int, error) {
	args := []string{"rev-list", "--count", commit}
	if base != "" {
		args = append(args, "^"+base)
	}
	out, err := execGitSimple(workdir, args)
	if err != nil {
		return 0, fmt.Errorf("count commits: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("count commits: %w", err)
	}
	return n, nil
}

// trimAngleEmail strips the angle brackets for-each-ref puts around an email.
func trimAngleEmail(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "<"), ">")
}

// parseUnixTime parses a unix time field, 0 when empty or malformed.
func parseUnixTime(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64) // a malformed date orders as the oldest
	return n
}
